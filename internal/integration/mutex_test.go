// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
)

// ciLockOwner はロックの owner。
//
// 既定の owner はインスタンスごとに一意な値であり、ラベルを見ただけでは CI の実行と
// 判別できない。固定の owner にしておくと、残っているラベルが CI 由来かどうかが分かる。
//
// 以前は「owner が自分自身ならロック可能」という判定で中断からの自己回復を得ていたが、
// specs/006-zone-lock-redesign によりその判定は無くなった（同一 owner を持つ
// プログラム同士が互いを排他できなくなるため）。中断からの回復は t.Cleanup の
// clearZoneLock、ロックの TTL の経過、一時レコード追加ロックの失効が担う。
const ciLockOwner = "dpf-go-ci"

// soaRecord はゾーンの SOA レコードをすべて返す（state 込み）。
func soaRecords(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) []dpf.Record {
	t.Helper()

	list, _, err := c.GetAPIClient().RecordsAPI.GetRecordList(ctx, zoneID).
		KeywordsRrtype(dpf.RECORDSRRTYPE_SOA).
		ExecuteAll()
	if err != nil {
		t.Fatalf("SOA レコードを取得できない: %v", err)
	}
	var out []dpf.Record
	if list != nil {
		for _, r := range list.Results {
			if r.Rrtype == dpf.RECORDSRRTYPE_SOA {
				out = append(out, r)
			}
		}
	}
	return out
}

// waitZoneLockLabel はゾーンのラベルの排他が条件を満たすまで待つ。
//
// utils.Mutex は内部の PatchRecord の AsyncResponse を破棄しており SyncWait できない。
// Lock と Renew は書き込んだ内容が読み出せることを自分で確認してから復帰するため
// 待機は不要だが、Unlock は確認しない。「非同期 JOB を重ねない」制約を守るため、
// ラベルが実際に反映されるまでここで待つ。
// waitZoneLockLabel は、ゾーンのラベルの排他が条件を満たすまで待つ。
// ゾーンのラベルの更新は非同期であるため、書き込みの直後は読めないことがある。
func waitZoneLockLabel(t *testing.T, ctx context.Context, c *utils.Client, zoneID string, cond func(string) bool, what string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for {
		v := zoneLockLabel(t, ctx, c, zoneID)
		if cond(v) {
			return
		}
		if time.Now().After(deadline) {
			t.Logf("ゾーンのラベル: %v", zoneLabelsOfZone(t, ctx, c, zoneID))
			t.Fatalf("%s が反映されなかった", what)
		}
		time.Sleep(3 * time.Second)
	}
}

// zoneLockDeadline はゾーンのラベルの排他の奪ってよい時刻を返す。
func zoneLockDeadline(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) int64 {
	t.Helper()

	v := zoneLockLabel(t, ctx, c, zoneID)
	_, d, ok := parseZoneLock(v)
	if !ok {
		t.Fatalf("ゾーンのラベルに排他が無い、または形式を満たさない: %q", v)
	}
	return d
}

// TestZoneMutex はゾーン単位ロックの取得・競合・延長・解放を検証する。
//
// 009 以降、排他の状態はゾーンのラベルにあり、**ゾーンに未反映の編集を作らない。**
// 最後にゾーンのラベルから排他を取り除いて元の状態に戻す（ゾーン反映は不要である）。
func TestZoneMutex(t *testing.T) {
	c := writeClient(t)
	ctx := testContext(t)
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)

	// SOA の行を記録に残す。009 以降、排他は SOA を触らないため、ロックの前後で
	// state もラベルも変わらないはずである。変わった場合はこのログで気づける。
	for _, r := range soaRecords(t, ctx, c, zone.Id) {
		t.Logf("ロック前の SOA: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
	}

	recordsAPI := c.GetAPIClient().RecordsAPI
	zonesAPI := c.GetAPIClient().ZonesAPI
	mu := utils.NewMutex(recordsAPI, zonesAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(2*time.Minute))

	// 後始末: ゾーンのラベルから排他を必ず取り除く。
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		clearZoneLock(t, cctx, c, zone.Id)
	})

	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	waitZoneLockLabel(t, ctx, c, zone.Id, func(v string) bool {
		owner, _, ok := parseZoneLock(v)
		return ok && owner == ciLockOwner
	}, "排他のラベル")
	t.Logf("ロック後のゾーンのラベル: %v", zoneLabelsOfZone(t, ctx, c, zone.Id))

	// **排他は SOA を触らない。** ロックの前後で state もラベルも変わらない。
	for _, r := range soaRecords(t, ctx, c, zone.Id) {
		t.Logf("ロック後の SOA: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
		if _, ok := r.Labels[utils.LockOwnerLabelKey]; ok {
			t.Errorf("SOA に排他のラベルが付いている: %v", r.Labels)
		}
	}

	// 一時レコード追加ロックの専用レコードは、排他が取得できた時点で取り消される。
	// 権威サーバへ公開されず、未反映としても残らない（SC-005、FR-009）。
	if recs := lockRecords(t, ctx, c, zone.Id); len(recs) != 0 {
		for _, r := range recs {
			t.Logf("専用レコード: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
		}
		t.Errorf("取得後に専用レコード %s が %d 件残っている", lockRecordName(), len(recs))
	}
	// 009: 排他はゾーンに未反映の編集を作らない。
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Errorf("取得後の未反映件数が %d 件。排他はゾーン反映を伴わないこと", n)
	}

	// 別の owner はロックを奪えない。
	other := utils.NewMutex(recordsAPI, zonesAPI, zone.Id,
		utils.WithOwner("someone-else"),
		utils.WithTTL(2*time.Minute))
	if err := other.Lock(ctx); !errors.Is(err, utils.ErrStillLock) {
		t.Fatalf("別 owner のロックが %v、期待は utils.ErrStillLock", err)
	}

	// 同じ owner でも再入はできない（specs/006-zone-lock-redesign FR-017）。
	if err := mu.Lock(ctx); !errors.Is(err, utils.ErrStillLock) {
		t.Fatalf("同一 owner での再取得が %v、期待は utils.ErrStillLock", err)
	}

	// 保持期間の延長は Renew で行う（FR-022）。
	before := zoneLockDeadline(t, ctx, c, zone.Id)
	if err := mu.Renew(ctx); err != nil {
		t.Fatalf("ロックを延長できない: %v", err)
	}
	waitZoneLockLabel(t, ctx, c, zone.Id, func(v string) bool {
		_, d, ok := parseZoneLock(v)
		return ok && d > before
	}, "ロック延長のラベル")
	t.Logf("ロックを延長した: 奪ってよい時刻が %d より後になった", before)

	// 保持者でない Mutex は延長・解放できない（FR-023・FR-024）。
	if err := other.Renew(ctx); !errors.Is(err, utils.ErrNotLockHolder) {
		t.Fatalf("保持者でない延長が %v、期待は utils.ErrNotLockHolder", err)
	}
	if err := other.Unlock(ctx); !errors.Is(err, utils.ErrNotLockHolder) {
		t.Fatalf("保持者でない解放が %v、期待は utils.ErrNotLockHolder", err)
	}

	if err := mu.Unlock(ctx); err != nil {
		t.Fatalf("ロックを解放できない: %v", err)
	}
	waitZoneLockLabel(t, ctx, c, zone.Id, func(v string) bool {
		_, d, ok := parseZoneLock(v)
		return ok && d <= time.Now().Unix()
	}, "ロック解放のラベル")
	// 解放してもラベルは残る（009 FR-002b）。
	if v := zoneLockLabel(t, ctx, c, zone.Id); v == "" {
		t.Error("解放でラベルが削除されている。削除してはならない")
	}

	// 解放後は別 owner でも取得できる。
	if err := other.Lock(ctx); err != nil {
		t.Fatalf("解放後に別 owner がロックを取得できない: %v", err)
	}
	if err := other.Unlock(ctx); err != nil {
		t.Fatalf("別 owner のロックを解放できない: %v", err)
	}
}
