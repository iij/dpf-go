// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"errors"
	"strconv"
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
// discardSOAChanges、ロックの TTL の経過、一時レコード追加ロックの失効が担う。
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

// waitSOALabel は SOA のラベルが条件を満たすまで待つ。
//
// utils.Mutex は内部の PatchRecord の AsyncResponse を破棄しており SyncWait できない。
// Lock と Renew は書き込んだ内容が読み出せることを自分で確認してから復帰するため
// 待機は不要だが、Unlock は確認しない。「非同期 JOB を重ねない」制約を守るため、
// ラベルが実際に反映されるまでここで待つ。
func waitSOALabel(t *testing.T, ctx context.Context, c *utils.Client, zoneID string, cond func(map[string]string) bool, what string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	for {
		for _, r := range soaRecords(t, ctx, c, zoneID) {
			if cond(r.Labels) {
				return
			}
		}
		if time.Now().After(deadline) {
			for _, r := range soaRecords(t, ctx, c, zoneID) {
				t.Logf("SOA: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
			}
			t.Fatalf("%s が反映されなかった", what)
		}
		time.Sleep(3 * time.Second)
	}
}

// soaLockDeadline は SOA レコードのロックの奪ってよい時刻を返す。
func soaLockDeadline(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) int64 {
	t.Helper()

	for _, r := range soaRecords(t, ctx, c, zoneID) {
		v, ok := r.Labels[utils.LockDeadlineLabelKey]
		if !ok {
			continue
		}
		d, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("奪ってよい時刻 %q を解釈できない: %v", v, err)
		}
		return d
	}
	t.Fatal("SOA レコードにロックのラベルが無い")
	return 0
}

// TestZoneMutex はゾーン単位ロックの取得・競合・延長・解放を検証する。
//
// ロックは SOA レコードの「編集予定」状態を利用しているため、反映して
// しまうとロックとして機能しなくなる。このテストはゾーン反映を行わず、
// 最後に SOA の未反映編集ごと破棄して元の状態に戻す。
func TestZoneMutex(t *testing.T) {
	c := writeClient(t)
	ctx := testContext(t)
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)

	// GetRecordList が返す SOA の行を記録に残す。2026-09-18 の実測では、ロック前は
	// state=0 の 1 行、ロック後は同じ ID の state=3 の 1 行のみで、state=5（更新前の
	// 状態）の行は現れなかった。utils/lock.go の getSOA は state=3 を優先し、一意に
	// 決まらない場合はエラーとするため、応答が変わっても古い行を掴むことはない。
	// 応答が変わった場合はこのログで気づける。
	for _, r := range soaRecords(t, ctx, c, zone.Id) {
		t.Logf("ロック前の SOA: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
	}

	recordsAPI := c.GetAPIClient().RecordsAPI
	mu := utils.NewMutex(recordsAPI, zone.Id,
		utils.WithOwner(ciLockOwner),
		utils.WithTTL(2*time.Minute))

	// 後始末: SOA の未反映編集を必ず破棄する。
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		discardSOAChanges(t, cctx, c, zone.Id)
	})

	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("ロックを取得できない: %v", err)
	}
	waitSOALabel(t, ctx, c, zone.Id, func(l map[string]string) bool {
		return l[utils.LockOwnerLabelKey] == ciLockOwner && l[utils.LockDeadlineLabelKey] != ""
	}, "ロックのラベル")

	for _, r := range soaRecords(t, ctx, c, zone.Id) {
		t.Logf("ロック後の SOA: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
	}

	// 一時レコード追加ロックの専用レコードは、排他が取得できた時点で取り消される。
	// 権威サーバへ公開されず、未反映としても残らない（SC-005、FR-009）。
	if recs := lockRecords(t, ctx, c, zone.Id); len(recs) != 0 {
		for _, r := range recs {
			t.Logf("専用レコード: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
		}
		t.Errorf("取得後に専用レコード %s が %d 件残っている", lockRecordName(), len(recs))
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 1 {
		t.Errorf("取得後の未反映件数が %d 件。SOA の 1 件だけであること", n)
	}

	// 別の owner はロックを奪えない。
	other := utils.NewMutex(recordsAPI, zone.Id,
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
	before := soaLockDeadline(t, ctx, c, zone.Id)
	if err := mu.Renew(ctx); err != nil {
		t.Fatalf("ロックを延長できない: %v", err)
	}
	waitSOALabel(t, ctx, c, zone.Id, func(l map[string]string) bool {
		d, err := strconv.ParseInt(l[utils.LockDeadlineLabelKey], 10, 64)
		return err == nil && d > before
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
	waitSOALabel(t, ctx, c, zone.Id, func(l map[string]string) bool {
		v, ok := l[utils.LockDeadlineLabelKey]
		if !ok {
			return false
		}
		deadline, err := strconv.ParseInt(v, 10, 64)
		return err == nil && deadline <= time.Now().Unix()
	}, "ロック解放のラベル")

	// 解放後は別 owner でも取得できる。
	if err := other.Lock(ctx); err != nil {
		t.Fatalf("解放後に別 owner がロックを取得できない: %v", err)
	}
	if err := other.Unlock(ctx); err != nil {
		t.Fatalf("別 owner のロックを解放できない: %v", err)
	}
}

// discardSOAChanges は SOA レコードの未反映編集を破棄する。
// ゾーン全体の DeleteZoneChanges ではなく、対象を絞って取り消す。
func discardSOAChanges(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) {
	t.Helper()

	for _, r := range soaRecords(t, ctx, c, zoneID) {
		if r.State == dpf.RECORDSSTATE__0 {
			continue // 反映済みの行には未反映の編集が無い。
		}
		t.Logf("SOA (id=%s state=%d) の未反映編集を破棄する", r.Id, r.State)
		syncWaiter(t, c, "SOA 編集の破棄")(
			c.GetAPIClient().RecordsAPI.DeleteRecordChanges(ctx, zoneID, r.Id).Execute())
		break
	}

	if n := pendingCount(t, ctx, c, zoneID); n != 0 {
		t.Errorf("後始末後も未反映の編集が %d 件残っている", n)
	}
}
