// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/internal/dnsprobe"
	"github.com/iij/dpf-go/utils"
)

// 本ファイルは、ゾーン単位の排他を保持したままゾーンを反映する 2 つの流れを、
// 実 API で通しで確認する。specs/006-zone-lock-redesign の前提
// 「取得の後にレコードを編集し、取得した SOA レコードをそのままゾーンへ反映する」が
// 実際に成立するかを見る。
//
// 最大の関心は、反映の後に SOA レコードがどの状態になるかである。
//
//   - 編集予定 (state=3) のまま残る → 排他は保持されたまま。Unlock で解放できる
//   - 反映済み (state=0) でロックのラベルを保つ → 排他は保持されたまま。
//     Unlock は新たな編集予定を作って解放する
//   - 反映済み (state=0) でロックのラベルが消える → **排他が黙って失われる**。
//     編集の途中で他者がロックを取得できてしまう
//
// 2026-09-18 の実測では、一括置き換え（PatchZoneAtomicChanges）が 3 つ目、
// 編集中レコードのゾーン反映（PatchZoneChanges）が 2 つ目であった。前者はロックと
// 併用できない。ロックのラベルは SOA の「未反映の編集」としてのみ存在し、一括置き換えは
// 未反映の編集を引き継がないためである。overwrite_soa の値は関係しない。
// 本ファイルはその違いを固定する。一括置き換えを安全に行う手段は utils.ZoneApplier で
// あり、その確認も本ファイルで行う。

// logSOAState は SOA レコードの state とロックのラベルを出力する。
// 戻り値は、**有効な排他を自分が保持しているか**である。
//
// owner の一致だけでは足りない。ゾーン反映を伴う流れでは、過去の実行が残した
// ロックのラベルが反映済みとして残ることがある。同じ owner を使っていると、
// 期限切れの残骸を「保持している」と誤判定してしまう。
func logSOAState(t *testing.T, ctx context.Context, c *utils.Client, zoneID, phase, owner string) bool {
	t.Helper()

	held := false
	recs := soaRecords(t, ctx, c, zoneID)
	for _, r := range recs {
		state := map[dpf.RecordsState]string{
			dpf.RECORDSSTATE__0: "反映済み",
			dpf.RECORDSSTATE__1: "追加予定",
			dpf.RECORDSSTATE__2: "削除予定",
			dpf.RECORDSSTATE__3: "更新予定",
			dpf.RECORDSSTATE__5: "更新前の状態",
		}[r.State]
		t.Logf("[%s] SOA: id=%s state=%d(%s) owner=%q deadline=%q",
			phase, r.Id, r.State, state,
			r.Labels[utils.LockOwnerLabelKey], r.Labels[utils.LockDeadlineLabelKey])
		if r.Labels[utils.LockOwnerLabelKey] == owner && !lockExpired(r.Labels) {
			held = true
		}
	}
	if len(recs) == 0 {
		t.Errorf("[%s] SOA レコードが 1 件も見つからない", phase)
	}
	t.Logf("[%s] 未反映件数=%d ロックのラベル(owner=%s)=%t",
		phase, pendingCount(t, ctx, c, zoneID), owner, held)
	return held
}

// assertStillLocked は、ロックが自分に保持されたままであることを確認する。
// 別の owner で取得を試み、拒否されることを見る。取得できてしまった場合は
// 排他が失われているため、テストを失敗させたうえで解放する。
func assertStillLocked(t *testing.T, ctx context.Context, c *utils.Client, zoneID, phase string) {
	t.Helper()

	other := utils.NewMutex(c.GetAPIClient().RecordsAPI, zoneID,
		utils.WithOwner("someone-else"),
		utils.WithTTL(2*time.Minute))
	err := other.Lock(ctx)
	if errors.Is(err, utils.ErrStillLock) {
		t.Logf("[%s] 別 owner の取得は拒否された。排他は保持されている", phase)
		return
	}
	if err != nil {
		t.Errorf("[%s] 別 owner の取得が想定外のエラーになった: %v", phase, err)
		return
	}

	t.Errorf("[%s] 別 owner が排他を取得できてしまった。"+
		"この反映方法はロックを保持したまま使えない", phase)
	if err := other.Unlock(ctx); err != nil {
		t.Errorf("[%s] 奪った排他を解放できない: %v", phase, err)
	}
}

// lockExpired は、奪ってよい時刻を過ぎている（または判定できない）かを返す。
func lockExpired(labels map[string]string) bool {
	v, ok := labels[utils.LockDeadlineLabelKey]
	if !ok {
		return true
	}
	deadline, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return true
	}
	return time.Now().Unix() >= deadline
}

// clearSOALock は SOA からロックのラベルを取り除いて反映する。
//
// ゾーン反映を伴う流れでは、ロックのラベルが反映済みになる。その後の Unlock は
// 未反映の編集を作るため、これを破棄すると「奪ってよい時刻が未来の排他」が反映済みと
// して残り、ゾーンが保持期間のあいだロックされたままになる。実際にこの後始末の誤りで、
// 後続のテストが 5 分間ずっと ErrStillLock で落ちた。ゾーン反映を行うテストでは、
// discardSOAChanges ではなくこちらを使う。
func clearSOALock(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) {
	t.Helper()

	recs := soaRecords(t, ctx, c, zoneID)
	if len(recs) == 0 {
		return
	}
	// 編集予定があればそれを、無ければ反映済みを対象にする。
	target := recs[0]
	for _, r := range recs {
		if r.State == dpf.RECORDSSTATE__3 {
			target = r
			break
		}
	}

	_, hasOwner := target.Labels[utils.LockOwnerLabelKey]
	_, hasDeadline := target.Labels[utils.LockDeadlineLabelKey]
	pending := pendingCount(t, ctx, c, zoneID)
	if !hasOwner && !hasDeadline && pending == 0 {
		return
	}

	labels := map[string]string{}
	for k, v := range target.Labels {
		if k != utils.LockOwnerLabelKey && k != utils.LockDeadlineLabelKey {
			labels[k] = v
		}
	}
	t.Logf("後始末: SOA (id=%s) からロックのラベルを取り除いて反映する", target.Id)
	syncWaiter(t, c, "ロックのラベルの除去")(
		c.GetAPIClient().RecordsAPI.PatchRecord(ctx, zoneID, target.Id).
			PatchRecord(dpf.PatchRecord{Labels: &labels}).Execute())
	applyZone(t, ctx, c, zoneID, pendingCount(t, ctx, c, zoneID), "dpf-go ci: clear soa lock")

	if n := pendingCount(t, ctx, c, zoneID); n != 0 {
		t.Errorf("後始末後も未反映の編集が %d 件残っている", n)
	}
}

// assertLockLost は、排他が失われていることを確認する。
// 別の owner が取得できることを見る。取得できた場合は解放しておく。
func assertLockLost(t *testing.T, ctx context.Context, c *utils.Client, zoneID, phase string) {
	t.Helper()

	other := utils.NewMutex(c.GetAPIClient().RecordsAPI, zoneID,
		utils.WithOwner("someone-else"),
		utils.WithTTL(2*time.Minute))
	if err := other.Lock(ctx); err != nil {
		t.Errorf("[%s] 排他が失われているはずだが、別 owner が取得できない: %v", phase, err)
		return
	}
	t.Logf("[%s] 別 owner が取得できた。排他は失われている", phase)
	if err := other.Unlock(ctx); err != nil {
		t.Errorf("[%s] 取得した排他を解放できない: %v", phase, err)
	}
}

// overwriteRecordsFromCurrents は反映済みレコードを PatchZoneAtomicChanges の
// records へ渡せる形に変換する。
func overwriteRecordsFromCurrents(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) []dpf.OverwriteRecordsInner {
	t.Helper()

	list, _, err := c.GetAPIClient().RecordsAPI.GetRecordCurrents(ctx, zoneID).ExecuteAll()
	if err != nil {
		t.Fatalf("反映済みレコードを取得できない: %v", err)
	}
	if list == nil {
		t.Fatal("反映済みレコードの応答が空である")
	}

	out := make([]dpf.OverwriteRecordsInner, 0, len(list.Results))
	for i := range list.Results {
		r := &list.Results[i]
		t.Logf("反映済み: %s %s state=%d", r.Name, r.Rrtype, r.State)
		out = append(out, dpf.OverwriteRecordsInner{
			Name:        r.Name,
			Ttl:         r.Ttl,
			Rrtype:      r.Rrtype,
			Rdata:       r.Rdata,
			Description: r.Description,
			Labels:      r.Labels,
		})
	}
	return out
}

// TestLockFlow_AtomicChangesLosesLock は、一括置き換え（PatchZoneAtomicChanges）が
// 排他を解くことを固定する。overwrite_soa は false で実行する。
//
// 排他のラベルは SOA の「未反映の編集」としてのみ存在し、一括置き換えは未反映の編集を
// 引き継がない。したがって overwrite_soa の値によらず排他は失われる。この性質は
// utils.ZoneApplier の設計の根拠であり（specs/007-zone-atomic-apply/research.md D1）、
// 変わった場合は気づけるようにしておく。
func TestLockFlow_AtomicChangesLosesLock(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()
	zone := writeZone(t, ctx, c)

	if zone.State != dpf.ZONESSTATE__2 {
		t.Fatalf("ゾーン %q が公開状態でない（state=%d）", zone.Name, zone.State)
	}
	resetPendingChanges(t, ctx, c, zone.Id)

	suffix := uniqueSuffix(t)
	recordName := fmt.Sprintf("_dpf-go-ci-atomic-%s.%s", suffix, zoneSubSub)
	value := "dpf-go-ci-atomic-" + suffix
	t.Logf("テスト用レコード: %s TXT %q", recordName, value)

	mu := utils.NewMutex(api.RecordsAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(5*time.Minute))

	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearSOALock(t, cctx, c, zone.Id)
	})

	logSOAState(t, ctx, c, zone.Id, "ロック前", ciLockOwner)

	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	if !logSOAState(t, ctx, c, zone.Id, "ロック後", ciLockOwner) {
		t.Fatal("ロック後に SOA へロックのラベルが無い")
	}

	// 反映済みレコードを全件取り、テスト用レコードを足して一括置き換えする。
	// SOA と Zone Apex NS は取り込まない（overwrite_soa / overwrite_zone_apex_ns=false）。
	records := overwriteRecordsFromCurrents(t, ctx, c, zone.Id)
	records = append(records, dpf.OverwriteRecordsInner{
		Name:        recordName,
		Ttl:         *dpf.NewNullableInt32(dpf.PtrInt32(60)),
		Rrtype:      dpf.RECORDSRRTYPE_TXT,
		Rdata:       []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata(value))}},
		Description: "dpf-go integration test",
		Labels:      map[string]string{},
	})
	body := dpf.PatchZoneAtomicChanges{
		Records:             records,
		Description:         dpf.PtrString(truncateDescription("dpf-go ci: atomic " + suffix)),
		OverwriteSoa:        dpf.PtrBool(false),
		OverwriteZoneApexNs: dpf.PtrBool(false),
	}
	t.Logf("atomic_changes を実行する: records=%d overwrite_soa=false overwrite_zone_apex_ns=false",
		len(records))
	syncWaiter(t, c, "atomic_changes")(
		api.ZonesAPI.PatchZoneAtomicChanges(ctx, zone.Id).PatchZoneAtomicChanges(body).Execute())

	// ここが本題。ロックのラベルは失われていなければならない。
	if held := logSOAState(t, ctx, c, zone.Id, "atomic_changes 後", ciLockOwner); held {
		t.Error("一括置き換えの後も SOA にロックのラベルが残っている。" +
			"specs/007-zone-atomic-apply/research.md D1 の前提（一括置き換えは排他を解く）が" +
			"変わった可能性がある。ZoneApplier の設計を見直すこと")
	}

	// レコードの置き換えそのものは成功していること。
	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)

	// 排他が実際に失われていることを、別 owner の取得で確かめる。
	assertLockLost(t, ctx, c, zone.Id, "atomic_changes 後")

	// 解放は「保持者でない」になる。これが ZoneApplier が無条件の解放を行わない理由である。
	err := mu.Unlock(ctx)
	t.Logf("Unlock: err=%v", err)
	if !errors.Is(err, utils.ErrNotLockHolder) {
		t.Errorf("一括置き換えの後の解放が %v、期待は utils.ErrNotLockHolder", err)
	}
	logSOAState(t, ctx, c, zone.Id, "Unlock 後", ciLockOwner)
}

// TestLockFlow_ZoneChanges は「ロック取得 → レコード単位の変更 → PatchZoneChanges で
// 反映 → 解放」の流れを確認する。
//
// PatchZoneChanges は未反映の編集をすべて反映するため、SOA のロックの編集予定も
// 一緒に反映される。specs/006-zone-lock-redesign が前提とする使い方である。
func TestLockFlow_ZoneChanges(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()
	zone := writeZone(t, ctx, c)

	if zone.State != dpf.ZONESSTATE__2 {
		t.Fatalf("ゾーン %q が公開状態でない（state=%d）", zone.Name, zone.State)
	}
	resetPendingChanges(t, ctx, c, zone.Id)

	suffix := uniqueSuffix(t)
	recordName := fmt.Sprintf("_dpf-go-ci-changes-%s.%s", suffix, zoneSubSub)
	value := "dpf-go-ci-changes-" + suffix
	t.Logf("テスト用レコード: %s TXT %q", recordName, value)

	mu := utils.NewMutex(api.RecordsAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(5*time.Minute))

	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearSOALock(t, cctx, c, zone.Id)
	})

	logSOAState(t, ctx, c, zone.Id, "ロック前", ciLockOwner)

	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	if !logSOAState(t, ctx, c, zone.Id, "ロック後", ciLockOwner) {
		t.Fatal("ロック後に SOA へロックのラベルが無い")
	}

	// レコード単位の変更。
	post := dpf.PostRecord{
		Name:        recordName,
		Rrtype:      dpf.RECORDSRRTYPEWITHOUTSOA_TXT,
		Rdata:       []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata(value))}},
		Ttl:         *dpf.NewNullableInt32(dpf.PtrInt32(60)),
		Description: dpf.PtrString(truncateDescription("dpf-go integration test")),
	}
	syncWaiter(t, c, "レコード作成")(
		api.RecordsAPI.PostRecord(ctx, zone.Id).PostRecord(post).Execute())
	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__1)

	// 未反映は SOA のロックとテスト用レコードの 2 件である。
	if n := pendingCount(t, ctx, c, zone.Id); n != 2 {
		t.Fatalf("反映前の未反映件数が %d 件。SOA とレコードの 2 件であること", n)
	}

	// ゾーン反映。SOA のロックの編集予定も一緒に反映される。
	applyZone(t, ctx, c, zone.Id, 2, "dpf-go ci: changes "+suffix)

	held := logSOAState(t, ctx, c, zone.Id, "PatchZoneChanges 後", ciLockOwner)
	if !held {
		t.Error("PatchZoneChanges によって SOA のロックのラベルが失われた")
	}
	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)

	assertStillLocked(t, ctx, c, zone.Id, "PatchZoneChanges 後")

	err := mu.Unlock(ctx)
	t.Logf("Unlock: err=%v", err)
	if err != nil {
		t.Errorf("反映の後に解放できない: %v", err)
	}
	logSOAState(t, ctx, c, zone.Id, "Unlock 後", ciLockOwner)

	other := utils.NewMutex(api.RecordsAPI, zone.Id,
		utils.WithOwner("someone-else"), utils.WithTTL(2*time.Minute))
	if err := other.Lock(ctx); err != nil {
		t.Errorf("解放後に別 owner が取得できない: %v", err)
	} else if err := other.Unlock(ctx); err != nil {
		t.Errorf("別 owner の排他を解放できない: %v", err)
	}
}

// TestZoneApplierApply は utils.ZoneApplier でゾーン全体を安全に置き換えられることを
// 確認する（specs/007-zone-atomic-apply の SC-003・SC-004・SC-007・SC-009）。
//
// 一括置き換えは排他を解くため、生の API を使う流れ
// （TestLockFlow_AtomicChangesLosesLock）では反映の後の解放が失敗する。ZoneApplier は
// そこまでを引き受けるため、成功した呼び出しが解放に起因して失敗しない。
//
// SOA に利用者のラベルを付けた状態を作って実行し、そのラベルが残ることも確認する。
func TestZoneApplierApply(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()
	zone := writeZone(t, ctx, c)

	if zone.State != dpf.ZONESSTATE__2 {
		t.Fatalf("ゾーン %q が公開状態でない（state=%d）", zone.Name, zone.State)
	}
	resetPendingChanges(t, ctx, c, zone.Id)

	suffix := uniqueSuffix(t)
	recordName := fmt.Sprintf("_dpf-go-ci-applier-%s.%s", suffix, zoneSubSub)
	value := "dpf-go-ci-applier-" + suffix
	const userLabelKey = "applier.test.dpf-go"
	userLabelValue := "keep-" + suffix
	t.Logf("テスト用レコード: %s TXT %q / SOA のラベル %s=%s",
		recordName, value, userLabelKey, userLabelValue)

	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		removeSOALabel(t, cctx, c, zone.Id, userLabelKey)
	})

	// SOA へ利用者のラベルを付けて反映する。これが一括置き換えの後も残ることを見る。
	soa := requireSOA(t, ctx, c, zone.Id)
	labels := map[string]string{userLabelKey: userLabelValue}
	for k, v := range soa.Labels {
		labels[k] = v
	}
	syncWaiter(t, c, "SOA へラベルを付ける")(
		api.RecordsAPI.PatchRecord(ctx, zone.Id, soa.Id).
			PatchRecord(dpf.PatchRecord{Labels: &labels}).Execute())
	applyZone(t, ctx, c, zone.Id, 1, "dpf-go ci: soa label "+suffix)
	logSOAState(t, ctx, c, zone.Id, "ラベル反映後", ciLockOwner)

	// ZoneApplier でレコードを 1 件足す。
	ap := utils.NewZoneApplier(api.RecordsAPI, api.ZonesAPI, api.JobsAPI, zone.Id,
		utils.WithOwner(ciLockOwner), utils.WithTTL(5*time.Minute))

	var sawSOA bool
	err := ap.Apply(ctx, func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		for _, r := range records {
			t.Logf("編集関数へ渡った: %s %s labels=%v", r.Name, r.Rrtype, r.Labels)
			if r.Rrtype == dpf.RECORDSRRTYPE_SOA {
				sawSOA = true
			}
		}
		return append(records, dpf.OverwriteRecordsInner{
			Name:        recordName,
			Ttl:         *dpf.NewNullableInt32(dpf.PtrInt32(60)),
			Rrtype:      dpf.RECORDSRRTYPE_TXT,
			Rdata:       []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata(value))}},
			Description: "dpf-go integration test",
			Labels:      map[string]string{},
		}), nil
	}, utils.WithApplyDescription("dpf-go ci: applier "+suffix))
	if err != nil {
		t.Fatalf("ZoneApplier.Apply が失敗した: %v", err)
	}
	if !sawSOA {
		t.Error("編集関数へ SOA が渡っていない")
	}

	// 置き換えが反映されている。
	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)

	// 排他も未反映の編集も残らない（SC-005・SC-007）。
	logSOAState(t, ctx, c, zone.Id, "Apply 後", ciLockOwner)
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("Apply の後に未反映の編集が %d 件残っている", n)
	}
	for _, r := range soaRecords(t, ctx, c, zone.Id) {
		// 排他は保持されていない。過去の実行が残したラベルが反映済みとして
		// 残っていることがあるため、キーの有無ではなく有効期限で判定する。
		if r.Labels[utils.LockOwnerLabelKey] == ciLockOwner && !lockExpired(r.Labels) {
			t.Errorf("Apply の後も排他が保持されている: %v", r.Labels)
		}
		// 利用者のラベルは残る（SC-009）。
		if got := r.Labels[userLabelKey]; got != userLabelValue {
			t.Errorf("利用者が SOA に付けたラベルが失われた: %s=%q（想定 %q）labels=%v",
				userLabelKey, got, userLabelValue, r.Labels)
		}
	}

	// Zone Apex の NS が置き換わっていない（SC-003）。
	if ns := findRecords(t, ctx, c, zone.Id, zoneSubSub, dpf.RECORDSRRTYPE_NS); len(ns) == 0 {
		t.Error("Zone Apex の NS レコードが失われた")
	}
}

// TestZoneMutexDoRenews は、保持期間より長い処理でも保持が続くことを実 API で確認する
// （SC-001）。保持期間を短く設定して待ち時間を抑える
// （specs/007-zone-atomic-apply/quickstart.md 第 4 節の判断）。
func TestZoneMutexDoRenews(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearSOALock(t, cctx, c, zone.Id)
	})

	const ttl = time.Minute
	mu := utils.NewMutex(c.GetAPIClient().RecordsAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(ttl),
		utils.WithRenewInterval(20*time.Second))

	var before, after int64
	err := mu.Do(ctx, func(ctx context.Context) error {
		before = soaLockDeadline(t, ctx, c, zone.Id)
		t.Logf("処理の開始時の奪ってよい時刻: %d（保持期間 %s）", before, ttl)

		// 保持期間より長く待つ。延長が無ければこの時点で他者に奪える状態になる。
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ttl + 10*time.Second):
		}

		after = soaLockDeadline(t, ctx, c, zone.Id)
		t.Logf("処理の終了時の奪ってよい時刻: %d", after)

		// まだ自分が保持している。
		other := utils.NewMutex(c.GetAPIClient().RecordsAPI, zone.Id,
			utils.WithOwner("someone-else"), utils.WithTTL(time.Minute))
		if err := other.Lock(ctx); !errors.Is(err, utils.ErrStillLock) {
			t.Errorf("保持期間より長い処理の途中で排他が失われた: 別 owner の取得が %v", err)
			if err == nil {
				_ = other.Unlock(ctx)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do が失敗した: %v", err)
	}
	if after <= before {
		t.Errorf("奪ってよい時刻が進んでいない: %d → %d。延長が走っていない", before, after)
	}
}

// TestZoneMutexDoPerRecord は、Mutex.Do の中でレコードを 1 つずつ変更して反映し、
// 解放まで通ることを確認する（contracts/api.md 第 1 節の約束 5）。
//
// この流れでは反映によって排他が解かれないため、Do の後始末で解放される。
func TestZoneMutexDoPerRecord(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)

	suffix := uniqueSuffix(t)
	recordName := fmt.Sprintf("_dpf-go-ci-do-%s.%s", suffix, zoneSubSub)
	value := "dpf-go-ci-do-" + suffix
	t.Logf("テスト用レコード: %s TXT %q", recordName, value)

	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearSOALock(t, cctx, c, zone.Id)
	})

	mu := utils.NewMutex(api.RecordsAPI, zone.Id,
		utils.WithOwner(ciLockOwner), utils.WithTTL(5*time.Minute))

	err := mu.Do(ctx, func(ctx context.Context) error {
		post := dpf.PostRecord{
			Name:        recordName,
			Rrtype:      dpf.RECORDSRRTYPEWITHOUTSOA_TXT,
			Rdata:       []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata(value))}},
			Ttl:         *dpf.NewNullableInt32(dpf.PtrInt32(60)),
			Description: dpf.PtrString(truncateDescription("dpf-go integration test")),
		}
		syncWaiter(t, c, "レコード作成")(
			api.RecordsAPI.PostRecord(ctx, zone.Id).PostRecord(post).Execute())

		// 未反映は SOA の排他とテスト用レコードの 2 件。
		applyZone(t, ctx, c, zone.Id, 2, "dpf-go ci: do "+suffix)
		return nil
	})
	if err != nil {
		t.Fatalf("Do が失敗した: %v", err)
	}

	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)
	logSOAState(t, ctx, c, zone.Id, "Do 後", ciLockOwner)

	// 解放されている（別 owner が取得できる）。
	assertLockLost(t, ctx, c, zone.Id, "Do 後")
}

// requireSOA は SOA レコードを 1 件返す。
func requireSOA(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) dpf.Record {
	t.Helper()

	recs := soaRecords(t, ctx, c, zoneID)
	if len(recs) != 1 {
		t.Fatalf("SOA レコードが %d 件ある（1 件であること）", len(recs))
	}
	return recs[0]
}

// removeSOALabel は SOA レコードから指定のラベルとロックのラベルを取り除いて反映する。
// 後始末用。反映を 1 回で済ませるため、ロックのラベルの除去も兼ねる。
func removeSOALabel(t *testing.T, ctx context.Context, c *utils.Client, zoneID, key string) {
	t.Helper()

	recs := soaRecords(t, ctx, c, zoneID)
	if len(recs) == 0 {
		return
	}
	soa := recs[0]
	labels := map[string]string{}
	for k, v := range soa.Labels {
		if k == key || k == utils.LockOwnerLabelKey || k == utils.LockDeadlineLabelKey {
			continue
		}
		labels[k] = v
	}
	if len(labels) == len(soa.Labels) && pendingCount(t, ctx, c, zoneID) == 0 {
		return
	}
	t.Logf("後始末: SOA のラベル %s とロックのラベルを取り除く", key)
	syncWaiter(t, c, "SOA のラベルの除去")(
		c.GetAPIClient().RecordsAPI.PatchRecord(ctx, zoneID, soa.Id).
			PatchRecord(dpf.PatchRecord{Labels: &labels}).Execute())
	applyZone(t, ctx, c, zoneID, pendingCount(t, ctx, c, zoneID), "dpf-go ci: cleanup soa label")
}
