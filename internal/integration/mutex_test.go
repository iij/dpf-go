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
// 既定の owner はホスト名だが、CI ランナーのホスト名は実行ごとに変わるため、
// 途中で異常終了するとロックが TTL の間どの実行からも取得できなくなる。
// 固定の owner にしておくと utils.Mutex の「owner が自分自身ならロック可能」
// という判定で短絡し、次の実行が自己回復できる。
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
// utils.Mutex の Lock / Unlock は内部の PatchRecord の AsyncResponse を
// 破棄しており SyncWait できない。「非同期 JOB を重ねない」制約を守るため、
// ラベルが実際に反映されるまでここで待つ必要がある。
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

// TestZoneMutex はゾーン単位ロックの取得・競合・解放を検証する。
//
// ロックは SOA レコードの「編集予定」状態を利用しているため、反映して
// しまうとロックとして機能しなくなる。このテストはゾーン反映を行わず、
// 最後に SOA の未反映編集ごと破棄して元の状態に戻す。
func TestZoneMutex(t *testing.T) {
	c := writeClient(t)
	ctx := testContext(t)
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)

	// 実測しないと分からない挙動なので記録に残す。GetRecordList が
	// state 5（更新前の状態）の行も返すかどうかで、utils/lock.go の
	// getSOA がスナップショット行を掴む可能性が変わる。
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

	// 別の owner はロックを奪えない。
	other := utils.NewMutex(recordsAPI, zone.Id,
		utils.WithOwner("someone-else"),
		utils.WithTTL(2*time.Minute))
	if err := other.Lock(ctx); !errors.Is(err, utils.ErrStillLock) {
		t.Fatalf("別 owner のロックが %v、期待は utils.ErrStillLock", err)
	}

	// 同じ owner なら再取得できる（異常終了からの自己回復に必要）。
	if err := mu.Lock(ctx); err != nil {
		t.Fatalf("同一 owner でロックを再取得できない: %v", err)
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
