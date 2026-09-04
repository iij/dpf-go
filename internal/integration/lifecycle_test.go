//go:build integration

// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/internal/dnsprobe"
	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// uniqueSuffix は実行ごとに一意な識別子を返す。
// CI では run id を含めて、どの実行が作ったレコードかを追えるようにする。
func uniqueSuffix(t *testing.T) string {
	t.Helper()

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("乱数を生成できない: %v", err)
	}
	suffix := hex.EncodeToString(b)

	if runID := os.Getenv("GITHUB_RUN_ID"); runID != "" {
		return runID + "-" + suffix
	}
	return suffix
}

// TestRecordLifecycle はレコードの作成・ゾーン反映・権威サーバでの確認・更新・
// 削除を通しで検証する。
//
// ゾーンロックは取らない。実行の分離は GitHub Actions の concurrency グループと、
// 実行ごとに一意なレコード名で担保する。ロックを取ると SOA の状態遷移が絡んで
// 不確定要素が増え、API 呼び出しも増えるため。
func TestRecordLifecycle(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()

	zone := writeZone(t, ctx, c)

	// 反映済みのゾーンでなければ DNS の確認に意味が無い。
	if zone.State != dpf.ZONESSTATE__2 {
		t.Fatalf("ゾーン %q が公開状態でない（state=%d）", zone.Name, zone.State)
	}

	resetPendingChanges(t, ctx, c, zone.Id)

	suffix := uniqueSuffix(t)
	recordName := fmt.Sprintf("_dpf-go-ci-%s.%s", suffix, zoneSubSub)
	initialValue := "dpf-go-ci-" + suffix
	updatedValue := "dpf-go-ci-updated-" + suffix

	t.Logf("テスト用レコード: %s TXT %q", recordName, initialValue)

	servers := managedServers(t, ctx, c, zone)

	// 後始末: レコードが残っていれば未反映編集を破棄する。
	// 反映済みまで進んでいた場合は削除して反映まで行う。
	t.Cleanup(func() { cleanupRecord(t, c, zone, recordName) })

	// --- 1. 作成 --------------------------------------------------------
	body := dpf.PostRecord{
		Name:        recordName,
		Rrtype:      dpf.RECORDSRRTYPEWITHOUTSOA_TXT,
		Rdata:       []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata(initialValue))}},
		Ttl:         *dpf.NewNullableInt32(dpf.PtrInt32(60)),
		Description: dpf.PtrString(truncateDescription("dpf-go integration test")),
	}
	syncWaiter(t, c, "レコード作成")(
		api.RecordsAPI.PostRecord(ctx, zone.Id).PostRecord(body).Execute())

	// 追加予定（state=1）として存在し、反映済み一覧には現れない。
	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__1)
	if vals := currentRecordValues(t, ctx, c, zone.Id, recordName, dpf.RECORDSRRTYPE_TXT); len(vals) != 0 {
		t.Fatalf("反映前にもかかわらず反映済み一覧にレコードがある: %v", vals)
	}

	// 反映前は権威 DNS も応答しない（未反映 ≠ 公開 の確認）。
	assertNoDNSRecord(t, ctx, servers, recordName, initialValue)

	// --- 2. 反映 --------------------------------------------------------
	applyZone(t, ctx, c, zone.Id, 1, "dpf-go ci: add "+suffix)

	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__0)
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Fatalf("反映後も未反映の編集が %d 件残っている", n)
	}

	waitDNS(t, ctx, servers, recordName, dns.TypeTXT,
		dnsprobe.HasTXT(initialValue), "レコード追加の権威サーバへの反映")

	// --- 3. 更新 --------------------------------------------------------
	// 権威サーバへの反映そのものは 2 と 5 で担保済みなので、ここでは API 側の
	// 反映のみ確認し、DNS の待ち時間を 1 回分節約する。
	rec := requireRecord(t, ctx, c, zone.Id, recordName)
	patch := dpf.PatchRecord{
		Rdata: []dpf.RecordsRdataInner{{Value: dpf.PtrString(dnsprobe.QuoteRdata(updatedValue))}},
	}
	syncWaiter(t, c, "レコード更新")(
		api.RecordsAPI.PatchRecord(ctx, zone.Id, rec.Id).PatchRecord(patch).Execute())

	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__3)
	applyZone(t, ctx, c, zone.Id, 1, "dpf-go ci: update "+suffix)

	vals := currentRecordValues(t, ctx, c, zone.Id, recordName, dpf.RECORDSRRTYPE_TXT)
	if len(vals) != 1 || vals[0] != updatedValue {
		t.Fatalf("反映済みの値が %v、期待は [%q]", vals, updatedValue)
	}

	// --- 4. 削除 --------------------------------------------------------
	rec = requireRecord(t, ctx, c, zone.Id, recordName)
	syncWaiter(t, c, "レコード削除")(
		api.RecordsAPI.DeleteRecord(ctx, zone.Id, rec.Id).Execute())

	assertRecordState(t, ctx, c, zone.Id, recordName, dpf.RECORDSSTATE__2)

	// --- 5. 削除の反映 --------------------------------------------------
	applyZone(t, ctx, c, zone.Id, 1, "dpf-go ci: delete "+suffix)

	waitDNS(t, ctx, servers, recordName, dns.TypeTXT,
		dnsprobe.NotHasTXT(updatedValue), "レコード削除の権威サーバへの反映")

	if _, err := utils.GetRecordFromZoneID(ctx, api.RecordsAPI, zone.Id, recordName,
		dpf.RECORDSRRTYPE_TXT); !errors.Is(err, utils.ErrRecordNotFound) {
		t.Fatalf("削除後のレコード取得が %v、期待は utils.ErrRecordNotFound", err)
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Fatalf("終了時に未反映の編集が %d 件残っている", n)
	}
}

// requireRecord は対象レコードを 1 件だけ取得する。
func requireRecord(t *testing.T, ctx context.Context, c *utils.Client, zoneID, name string) dpf.Record {
	t.Helper()

	recs := findRecords(t, ctx, c, zoneID, name, dpf.RECORDSRRTYPE_TXT)
	if len(recs) == 0 {
		t.Fatalf("レコード %q が見つからない", name)
	}
	// 更新中は「更新予定」と「更新前の状態」が並ぶことがあるため、
	// 反映済み以外を優先して選ぶ。
	for _, r := range recs {
		if r.State != dpf.RECORDSSTATE__5 {
			return r
		}
	}
	return recs[0]
}

// assertRecordState は対象レコードに期待した state の行が存在することを確認する。
func assertRecordState(t *testing.T, ctx context.Context, c *utils.Client, zoneID, name string, want dpf.RecordsState) {
	t.Helper()

	recs := findRecords(t, ctx, c, zoneID, name, dpf.RECORDSRRTYPE_TXT)
	if len(recs) == 0 {
		t.Fatalf("レコード %q が見つからない（期待した state=%d）", name, want)
	}
	for _, r := range recs {
		if r.State == want {
			return
		}
	}

	for _, r := range recs {
		t.Logf("レコード: id=%s state=%d rdata=%+v", r.Id, r.State, r.Rdata)
	}
	t.Fatalf("レコード %q に state=%d の行が無い", name, want)
}

// cleanupRecord はテスト用レコードを確実に取り除く。
//
// 未反映であれば編集を破棄し、反映済みであれば削除して反映する。
// ゾーン全体の DeleteZoneChanges は使わない（他者の編集を巻き込むため）。
func cleanupRecord(t *testing.T, c *utils.Client, zone *dpf.Zone, recordName string) {
	t.Helper()

	ctx, cancel := cleanupContext()
	defer cancel()

	recs := findRecords(t, ctx, c, zone.Id, recordName, dpf.RECORDSRRTYPE_TXT)
	if len(recs) == 0 {
		return
	}

	needApply := false
	for _, r := range recs {
		switch r.State {
		case dpf.RECORDSSTATE__0:
			// 反映済みで残っている場合は削除して反映まで行う。
			t.Logf("後始末: 反映済みのレコード %s (id=%s) を削除する", r.Name, r.Id)
			syncWaiter(t, c, "後始末のレコード削除")(
				c.GetAPIClient().RecordsAPI.DeleteRecord(ctx, zone.Id, r.Id).Execute())
			needApply = true
		case dpf.RECORDSSTATE__5:
			// 更新前のスナップショットは編集の破棄で一緒に消える。
		default:
			t.Logf("後始末: レコード %s (id=%s, state=%d) の未反映編集を破棄する", r.Name, r.Id, r.State)
			syncWaiter(t, c, "後始末のレコード編集破棄")(
				c.GetAPIClient().RecordsAPI.DeleteRecordChanges(ctx, zone.Id, r.Id).Execute())
		}
	}

	if needApply {
		body := dpf.PatchZoneCommit{
			Description: dpf.PtrString(truncateDescription("dpf-go ci: cleanup")),
		}
		syncWaiter(t, c, "後始末のゾーン反映")(
			c.GetAPIClient().ZonesAPI.PatchZoneChanges(ctx, zone.Id).PatchZoneCommit(body).Execute())
	}
}
