// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/internal/dnsprobe"
	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// 本ファイルは、ゾーン単位の排他を保持したままゾーンを反映する 2 つの流れを、
// 実 API で通しで確認する。specs/006-zone-lock-redesign の前提
// 「取得の後にレコードを編集し、取得した SOA レコードをそのままゾーンへ反映する」が
// 実際に成立するかを見る。
//
// 最大の関心は、**反映の後に排他が保持されたままかどうか**である。
//
// 009-zone-label-lock で排他の状態がゾーンのラベルへ移り、**どちらの反映方法でも排他は
// 影響を受けなくなった。** ゾーンのラベルはレコードとは別のリソースであり、未反映の編集の
// 概念も持たないためである。本ファイルはその性質を固定する。
//
// v0.5.0 までは排他の状態が SOA の未反映の編集としてのみ存在したため、一括置き換え
// （PatchZoneAtomicChanges）で排他が黙って失われていた（2026-09-18 の実測、
// specs/007-zone-atomic-apply/research.md D1）。本ファイルの期待値はその反転である。
//
// あわせて、排他がゾーンに未反映の編集を作らないこと、レコードのラベルを触らないことも
// 見る（009 FR-001〜FR-003）。

// logLockState はゾーンのラベルの排他と、SOA レコードの state を出力する。
// 戻り値は、**有効な排他を自分が保持しているか**である。
//
// owner の一致だけでは足りない。過去の実行が残したラベル（奪ってよい時刻が過去）が
// 残っているため、同じ owner を使っていると期限切れの残骸を「保持している」と
// 誤判定してしまう。
//
// SOA の state も出すのは、**排他がレコードを触っていないこと**を目で確かめられるように
// するためである（009 以降、排他は SOA を編集予定にしない）。
func logLockState(t *testing.T, ctx context.Context, c *utils.Client, zoneID, phase, owner string) bool {
	t.Helper()

	v := zoneLockLabel(t, ctx, c, zoneID)
	gotOwner, deadline, ok := parseZoneLock(v)
	held := ok && gotOwner == owner && time.Now().Unix() < deadline
	t.Logf("[%s] ゾーンのラベル %s=%q (owner=%q deadline=%d 解釈可=%t)",
		phase, utils.ZoneLockLabelKey, v, gotOwner, deadline, ok)

	for _, r := range soaRecords(t, ctx, c, zoneID) {
		state := map[dpf.RecordsState]string{
			dpf.RECORDSSTATE__0: "反映済み",
			dpf.RECORDSSTATE__1: "追加予定",
			dpf.RECORDSSTATE__2: "削除予定",
			dpf.RECORDSSTATE__3: "更新予定",
			dpf.RECORDSSTATE__5: "更新前の状態",
		}[r.State]
		t.Logf("[%s] SOA: id=%s state=%d(%s) labels=%v", phase, r.Id, r.State, state, r.Labels)
	}
	t.Logf("[%s] 未反映件数=%d 排他の保持(owner=%s)=%t",
		phase, pendingCount(t, ctx, c, zoneID), owner, held)
	return held
}

// zoneLockLabel はゾーンのラベルから排他の値を読む。無い場合は空文字。
func zoneLockLabel(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) string {
	t.Helper()
	return zoneLabelsOfZone(t, ctx, c, zoneID)[utils.ZoneLockLabelKey]
}

// zoneLabelsOfZone はゾーンのラベルを取得する。
func zoneLabelsOfZone(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) map[string]string {
	t.Helper()
	res, _, err := c.GetAPIClient().ZonesAPI.GetZoneLabels(ctx, zoneID).Execute()
	if err != nil {
		t.Fatalf("ゾーンのラベルを取得できない: %v", err)
	}
	if res == nil {
		t.Fatal("ゾーンのラベルの応答が空である")
	}
	return res.Result.Labels
}

// parseZoneLock は排他の値を読み出す。形式は utils.ZoneLockLabelKey の godoc を参照。
// **右端から固定幅で読む**（末尾 10 桁が奪ってよい時刻、その手前 1 文字が区切り）。
func parseZoneLock(v string) (owner string, deadline int64, ok bool) {
	const digits = 10
	if len(v) < 1+1+digits {
		return "", 0, false
	}
	sepAt := len(v) - digits - 1
	if v[sepAt] != '-' {
		return "", 0, false
	}
	d, err := strconv.ParseInt(v[sepAt+1:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return v[:sepAt], d, true
}

// assertStillLocked は、ロックが自分に保持されたままであることを確認する。
// 別の owner で取得を試み、拒否されることを見る。取得できてしまった場合は
// 排他が失われているため、テストを失敗させたうえで解放する。
func assertStillLocked(t *testing.T, ctx context.Context, c *utils.Client, zoneID, phase string) {
	t.Helper()

	other := utils.NewMutex(c.GetAPIClient().RecordsAPI, c.GetAPIClient().ZonesAPI, zoneID,
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
		"排他はゾーンのラベルにあり、反映の影響を受けないこと", phase)
	if err := other.Unlock(ctx); err != nil {
		t.Errorf("[%s] 奪った排他を解放できない: %v", phase, err)
	}
}

// lockExpired は、排他の値が示す奪ってよい時刻を過ぎている（または判定できない）かを返す。
func lockExpired(v string) bool {
	_, deadline, ok := parseZoneLock(v)
	if !ok {
		return true
	}
	return time.Now().Unix() >= deadline
}

// clearZoneLock はゾーンのラベルから排他を取り除く。
//
// **ゾーン反映は不要である。** ゾーンのラベルは未反映の編集の概念を持たないため、
// 取り除いた時点で確定する（009）。排他を残したまま次のテストへ渡さないための後始末で
// あり、通常の解放（奪ってよい時刻を現在時刻にする）とは別物である。
func clearZoneLock(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) {
	t.Helper()

	labels := zoneLabelsOfZone(t, ctx, c, zoneID)
	if _, ok := labels[utils.ZoneLockLabelKey]; !ok {
		return
	}
	rest := map[string]string{}
	for k, v := range labels {
		if k != utils.ZoneLockLabelKey {
			rest[k] = v
		}
	}
	t.Logf("後始末: ゾーンのラベルから排他 (%s) を取り除く", utils.ZoneLockLabelKey)
	syncWaiter(t, c, "排他のラベルの除去")(
		c.GetAPIClient().ZonesAPI.PutZoneLabels(ctx, zoneID).
			ZoneLabels(dpf.ZoneLabels{Labels: rest}).Execute())
}

// assertLockLost は、排他が失われていることを確認する。
// 別の owner が取得できることを見る。取得できた場合は解放しておく。
func assertLockLost(t *testing.T, ctx context.Context, c *utils.Client, zoneID, phase string) {
	t.Helper()

	other := utils.NewMutex(c.GetAPIClient().RecordsAPI, c.GetAPIClient().ZonesAPI, zoneID,
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

// TestLockFlow_AtomicChangesKeepsLock は、レコードの一括更新とゾーン反映
// （PatchZoneAtomicChanges）が**排他に影響しない**ことを固定する。
//
// 置き換えの対象はレコードであり、排他の状態を持つゾーンのラベルは別のリソースである
// （specs/009-zone-label-lock の前提）。したがって反映の後も排他は保持され、解放は通常
// どおり成功する。**この前提が崩れると 009 の利用シナリオ 2 が成立しない**ため、崩れた
// 場合に気づけるようにしておく。
//
// v0.5.0 までは排他の状態が SOA の未反映の編集としてのみ存在したため、この操作で排他が
// 失われていた（specs/007-zone-atomic-apply/research.md D1）。本テストはその反転である。
func TestLockFlow_AtomicChangesKeepsLock(t *testing.T) {
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

	mu := utils.NewMutex(api.RecordsAPI, api.ZonesAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(5*time.Minute))

	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearZoneLock(t, cctx, c, zone.Id)
	})

	logLockState(t, ctx, c, zone.Id, "ロック前", ciLockOwner)

	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	if !logLockState(t, ctx, c, zone.Id, "ロック後", ciLockOwner) {
		t.Fatal("ロック後にゾーンへ排他のラベルが無い")
	}
	// 009: 排他はゾーンに未反映の編集を作らない。
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("ロック後に未反映の編集が %d 件ある。排他はゾーン反映を伴わないこと", n)
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

	// ここが本題。**排他は保持されたままでなければならない。**
	// 置き換えの対象はレコードであり、排他の状態を持つゾーンのラベルは別のリソースで
	// あるためである（009）。
	if held := logLockState(t, ctx, c, zone.Id, "atomic_changes 後", ciLockOwner); !held {
		t.Error("レコードの一括更新とゾーン反映の後に排他が失われている。" +
			"specs/009-zone-label-lock の前提（一括置き換えはゾーンのラベルに影響しない）が" +
			"崩れている。research.md D12 の判断が必要である")
	}

	// レコードの置き換えそのものは成功していること。
	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)

	// 排他が実際に保持されていることを、別 owner の取得が拒否されることで確かめる。
	assertStillLocked(t, ctx, c, zone.Id, "atomic_changes 後")

	// 解放は通常どおり成功する。007 の「反映の後の解放が保持者でないになる」制約は
	// 既定の排他には当てはまらなくなった。
	err := mu.Unlock(ctx)
	t.Logf("Unlock: err=%v", err)
	if err != nil {
		t.Errorf("一括置き換えの後の解放が失敗した: %v", err)
	}
	// **ゾーンのラベルの更新は非同期であり、Unlock は確定を確認しない**（009
	// contracts/zone-label.md 第 4 節）。復帰した直後は古い値が読めるため、確定を待つ。
	waitZoneLockLabel(t, ctx, c, zone.Id, lockExpired, "排他の解放")
	logLockState(t, ctx, c, zone.Id, "Unlock 後", ciLockOwner)
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

	mu := utils.NewMutex(api.RecordsAPI, api.ZonesAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(5*time.Minute))

	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearZoneLock(t, cctx, c, zone.Id)
	})

	logLockState(t, ctx, c, zone.Id, "ロック前", ciLockOwner)

	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	if !logLockState(t, ctx, c, zone.Id, "ロック後", ciLockOwner) {
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

	// **未反映はテスト用レコードの 1 件だけである。** 009 以降、排他はゾーンのラベルを
	// 使うため未反映の編集を作らない（v0.5.0 までは SOA の分と合わせて 2 件だった）。
	if n := pendingCount(t, ctx, c, zone.Id); n != 1 {
		t.Fatalf("反映前の未反映件数が %d 件。テスト用レコードの 1 件だけであること", n)
	}

	// ゾーン反映。排他はゾーンのラベルにあるため、この反映の対象に含まれない。
	applyZone(t, ctx, c, zone.Id, 1, "dpf-go ci: changes "+suffix)

	held := logLockState(t, ctx, c, zone.Id, "PatchZoneChanges 後", ciLockOwner)
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
	// 解放の確定は非同期である（下の TestLockFlow_AtomicChangesKeepsLock と同じ）。
	waitZoneLockLabel(t, ctx, c, zone.Id, lockExpired, "排他の解放")
	logLockState(t, ctx, c, zone.Id, "Unlock 後", ciLockOwner)

	other := utils.NewMutex(api.RecordsAPI, api.ZonesAPI, zone.Id,
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
// 一括置き換えは排他に影響しない（TestLockFlow_AtomicChangesKeepsLock）。ZoneApplier は
// 取り込みの可否を決めるフラグの固定と反映の完了待ちを引き受け、終了時に通常どおり
// 解放する。
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
	logLockState(t, ctx, c, zone.Id, "ラベル反映後", ciLockOwner)

	// ZoneApplier でレコードを 1 件足す。
	ap := utils.NewZoneApplier(api.RecordsAPI, api.ZonesAPI, api.JobsAPI, zone.Id,
		utils.WithLockOptions(utils.WithOwner(ciLockOwner), utils.WithTTL(5*time.Minute)))

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
	logLockState(t, ctx, c, zone.Id, "Apply 後", ciLockOwner)
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("Apply の後に未反映の編集が %d 件残っている", n)
	}
	// 排他は解放されている。**解放の確定は非同期である**ため待つ（009
	// contracts/zone-label.md 第 4 節。Unlock は確定を確認しない）。
	waitZoneLockLabel(t, ctx, c, zone.Id, lockExpired, "Apply の後の排他の解放")
	for _, r := range soaRecords(t, ctx, c, zone.Id) {
		// 排他はレコードのラベルを使わない（009 FR-001）。
		if _, ok := r.Labels[utils.LockOwnerLabelKey]; ok {
			t.Errorf("SOA に排他のラベルが付いている: %v", r.Labels)
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

// TestZoneApplierSkipApply は、編集関数が utils.ErrSkipApply を返したときに反映が行われず、
// 排他が解放されることを実 API で確認する（specs/007-zone-atomic-apply の FR-018b）。
//
// 編集関数はレコードを 1 件足した一覧を番兵とともに返し、そのレコードが作られていないことで
// 「一覧が使われていない」ことを確かめる。
//
// 009 以降、省略しても反映しても**ゾーンに未反映の編集は残らない**（排他はゾーンのラベルを
// 使い、ゾーン反映を伴わない）。解放の後もラベルは残るが、奪ってよい時刻が過去になる。
func TestZoneApplierSkipApply(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()
	zone := writeZone(t, ctx, c)

	if zone.State != dpf.ZONESSTATE__2 {
		t.Fatalf("ゾーン %q が公開状態でない（state=%d）", zone.Name, zone.State)
	}
	resetPendingChanges(t, ctx, c, zone.Id)
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		resetPendingChanges(t, cctx, c, zone.Id)
	})

	suffix := uniqueSuffix(t)
	recordName := fmt.Sprintf("_dpf-go-ci-skip-%s.%s", suffix, zoneSubSub)
	t.Logf("作られてはならないレコード: %s TXT", recordName)
	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })

	ap := utils.NewZoneApplier(api.RecordsAPI, api.ZonesAPI, api.JobsAPI, zone.Id,
		utils.WithLockOptions(utils.WithOwner(ciLockOwner), utils.WithTTL(5*time.Minute)))

	var sawRecords int
	err := ap.Apply(ctx, func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		sawRecords = len(records)
		// 一覧を返しても、番兵があれば使われない。
		return append(records, dpf.OverwriteRecordsInner{
			Name:        recordName,
			Ttl:         *dpf.NewNullableInt32(dpf.PtrInt32(60)),
			Rrtype:      dpf.RECORDSRRTYPE_TXT,
			Rdata:       []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata("skip"))}},
			Description: "dpf-go integration test",
			Labels:      map[string]string{},
		}), utils.ErrSkipApply
	}, utils.WithApplyDescription("dpf-go ci: skip "+suffix))
	if err != nil {
		t.Fatalf("省略は成功として返ること: %v", err)
	}
	if sawRecords == 0 {
		t.Error("編集関数へレコードが渡っていない")
	}

	// 反映は行われていない。
	if got := findRecords(t, ctx, c, zone.Id, recordName, dpf.RECORDSRRTYPE_TXT); len(got) != 0 {
		t.Errorf("省略したのにレコードが作られている: %v", got)
	}

	// 排他は解放されている（ラベルは残るが奪ってよい時刻が過去である）。
	// **解放の確定は非同期である**ため待つ。
	waitZoneLockLabel(t, ctx, c, zone.Id, lockExpired, "省略の後の排他の解放")
	logLockState(t, ctx, c, zone.Id, "省略の後", ciLockOwner)
	// 009: 省略でも反映でも、ゾーンに未反映の編集は残らない。
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("省略の後に未反映の編集が %d 件残っている。排他はゾーン反映を伴わないこと", n)
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
		clearZoneLock(t, cctx, c, zone.Id)
	})

	const ttl = time.Minute
	mu := utils.NewMutex(c.GetAPIClient().RecordsAPI, c.GetAPIClient().ZonesAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(ttl),
		utils.WithRenewInterval(20*time.Second))

	var before, after int64
	err := mu.Do(ctx, func(ctx context.Context) error {
		before = zoneLockDeadline(t, ctx, c, zone.Id)
		t.Logf("処理の開始時の奪ってよい時刻: %d（保持期間 %s）", before, ttl)

		// 保持期間より長く待つ。延長が無ければこの時点で他者に奪える状態になる。
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(ttl + 10*time.Second):
		}

		after = zoneLockDeadline(t, ctx, c, zone.Id)
		t.Logf("処理の終了時の奪ってよい時刻: %d", after)

		// まだ自分が保持している。
		other := utils.NewMutex(c.GetAPIClient().RecordsAPI, c.GetAPIClient().ZonesAPI, zone.Id,
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
		clearZoneLock(t, cctx, c, zone.Id)
	})

	mu := utils.NewMutex(api.RecordsAPI, api.ZonesAPI, zone.Id,
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

		// 未反映はテスト用レコードの 1 件だけ（排他は未反映の編集を作らない）。
		applyZone(t, ctx, c, zone.Id, 1, "dpf-go ci: do "+suffix)
		return nil
	})
	if err != nil {
		t.Fatalf("Do が失敗した: %v", err)
	}

	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)
	logLockState(t, ctx, c, zone.Id, "Do 後", ciLockOwner)

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

// TestZoneLabelLock_NoAuthoritativeEffect は、排他の取得・延長・解放が権威サーバへ
// 公開されるデータに影響しないことを確認する（009 SC-002）。
//
// 公開されているレコードの一覧を排他の前後で比べる。ゾーンのラベルは管理属性であり、
// DNS の応答には現れない。
func TestZoneLabelLock_NoAuthoritativeEffect(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearZoneLock(t, cctx, c, zone.Id)
	})

	names := func(phase string) []string {
		recs := overwriteRecordsFromCurrents(t, ctx, c, zone.Id)
		out := make([]string, 0, len(recs))
		for _, r := range recs {
			out = append(out, fmt.Sprintf("%s/%s", dns.CanonicalName(r.Name), r.Rrtype))
		}
		sort.Strings(out)
		t.Logf("[%s] 公開されているレコード %d 件", phase, len(out))
		return out
	}

	before := names("排他の前")

	mu := utils.NewMutex(api.RecordsAPI, api.ZonesAPI, zone.Id,
		utils.WithOwner(ciLockOwner), utils.WithTTL(5*time.Minute))
	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("取得後に未反映の編集が %d 件ある。排他はゾーン反映を伴わないこと", n)
	}
	if err := mu.Renew(ctx); err != nil {
		t.Fatalf("ロックを延長できない: %v", err)
	}
	if err := mu.Unlock(ctx); err != nil {
		t.Fatalf("ロックを解放できない: %v", err)
	}

	after := names("排他の後")
	if !slices.Equal(before, after) {
		t.Errorf("公開されているレコードが変わっている\n前: %v\n後: %v", before, after)
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("解放後に未反映の編集が %d 件残っている", n)
	}
}

// TestZoneLabelLock_UnpublishedZone は、公開前のゾーンでもゾーンのラベルを読み書きできる
// ことを確認する（009 research.md D12 の (2)）。
//
// `openapi.json` に記述が無いため実測で確かめる。**公開前のゾーンが見つからない場合は
// スキップする。** その場合この項目は未確認のままであり、確認結果として報告してはならない。
func TestZoneLabelLock_UnpublishedZone(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()

	list, _, err := api.ZonesAPI.GetZoneList(ctx).ExecuteAll()
	if err != nil {
		t.Fatalf("ゾーンの一覧を取得できない: %v", err)
	}
	if list == nil {
		t.Fatal("ゾーンの一覧の応答が空である")
	}

	var target *dpf.Zone
	for i := range list.Results {
		z := &list.Results[i]
		if z.State != dpf.ZONESSTATE__2 {
			target = z
			break
		}
	}
	if target == nil {
		t.Skip("公開前のゾーンが見つからない。この項目は未確認のままである（009 research.md D12 の (2)）")
	}
	t.Logf("公開前のゾーン: %s (id=%s state=%d)", target.Name, target.Id, target.State)

	// 読み取りができること。
	labels := zoneLabelsOfZone(t, ctx, c, target.Id)
	t.Logf("公開前のゾーンのラベル: %v", labels)

	// 排他の取得と解放ができること。
	mu := utils.NewMutex(api.RecordsAPI, api.ZonesAPI, target.Id,
		utils.WithOwner(ciLockOwner), utils.WithTTL(2*time.Minute))
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearZoneLock(t, cctx, c, target.Id)
	})
	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("公開前のゾーンで排他を取得できない: %v。"+
			"読み書きできない場合は、公開前のゾーンを対象外として文書に明記すること", err)
	}
	if err := mu.Unlock(ctx); err != nil {
		t.Errorf("公開前のゾーンで排他を解放できない: %v", err)
	}
	t.Log("公開前のゾーンでもラベルの読み書きができる")
}
