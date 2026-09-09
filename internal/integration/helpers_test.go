// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/internal/dnsprobe"
	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// descriptionMaxLen は DPF-API の Description の最大長。
// 超えるとパラメータエラーになるため切り詰める。
const descriptionMaxLen = 80

// truncateDescription はコメントを API の上限に収める。
func truncateDescription(s string) string {
	if len(s) <= descriptionMaxLen {
		return s
	}
	return s[:descriptionMaxLen]
}

// syncWaiter は非同期 API の戻り値をそのまま渡して JOB の完了を待つ関数を返す。
// 失敗した場合はテストを終了させる。
//
//	syncWaiter(t, c, "ゾーン反映")(api.ZonesAPI.PatchZoneChanges(ctx, id).PatchZoneCommit(b).Execute())
//
// クロージャを返すのは、SyncWait と同じく Execute() の 3 つの戻り値を
// そのまま受け渡せるようにするため。
func syncWaiter(t *testing.T, c *utils.Client, what string) func(*dpf.AsyncResponse, *http.Response, error) *dpf.GetJobs {
	t.Helper()
	return func(async *dpf.AsyncResponse, resp *http.Response, err error) *dpf.GetJobs {
		t.Helper()
		job, _, waitErr := c.GetAPIClient().JobsAPI.SyncWait(async, resp, err)
		if waitErr != nil {
			t.Fatalf("%s に失敗した: %v", what, waitErr)
		}
		return job
	}
}

// findZoneByServiceCode はサービスコードからゾーンを取得する。
func findZoneByServiceCode(t *testing.T, ctx context.Context, c *utils.Client, serviceCode string) *dpf.Zone {
	t.Helper()
	z, err := utils.GetZoneFromServiceCode(ctx, c.GetAPIClient().ZonesAPI, serviceCode)
	if err != nil {
		t.Fatalf("サービスコード %s のゾーンを取得できない: %v%s", serviceCode, err, visibleZonesHint(ctx, c))
	}
	return z
}

// visibleZonesHint は失敗時の診断用に、トークンから見えるゾーンを列挙した
// 文字列を返す。ゾーンが見つからない原因が「権限で見えていない」のか
// 「名前が違う」のかを、その場で切り分けられるようにする。
func visibleZonesHint(ctx context.Context, c *utils.Client) string {
	names, err := visibleZoneNames(ctx, c)
	if err != nil {
		return fmt.Sprintf("\n（見えるゾーンの列挙にも失敗した: %v）", err)
	}
	if len(names) == 0 {
		return "\n（このトークンからはゾーンが 1 件も見えない。" +
			"管理対象の権限設定を確認すること。TestConnectivity_VisibleZones も参照）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n見えるゾーン(%d 件):", len(names))
	for _, n := range names {
		b.WriteString("\n  " + n)
	}
	return b.String()
}

// writeZone は書き込み対象ゾーンを、二重確認を経て返す。
//
// utils.GetZoneFromZonename は longest match であり完全一致ではない。ゾーン名
// だけで解決すると、対象ゾーンが契約から消えていた場合に黙って親ゾーン
// （sub.dns-tool-test.jp. や dns-tool-test.jp.）にフォールバックしてしまう。
// 本テストはゾーン反映や未反映編集の破棄を行うため、それは意図しないゾーンを
// 壊すことになる。
//
// そこでサービスコード（完全一致で解決）とゾーン名（コード上の定数）という
// 独立した 2 つの識別子の一致を要求する。片方でも食い違えば即座に失敗する。
func writeZone(t *testing.T, ctx context.Context, c *utils.Client) *dpf.Zone {
	t.Helper()
	serviceCode := requireEnv(t, envServiceCode, "書き込み対象ゾーンの特定に必要")

	z := findZoneByServiceCode(t, ctx, c, serviceCode)

	if dns.CanonicalName(z.Name) != dns.CanonicalName(zoneSubSub) {
		t.Fatalf("安全確認に失敗した。"+
			"サービスコード %q のゾーンは %q だが、書き込み対象は %q でなければならない。"+
			"%s の設定を確認すること。",
			serviceCode, z.Name, zoneSubSub, envServiceCode)
	}
	t.Logf("書き込み対象ゾーン: %s (service_code=%s, id=%s)", z.Name, z.ServiceCode, z.Id)
	return z
}

// pendingCount は未反映（編集中）のレコード件数を返す。
//
// GetRecordDiffs ではなく GetRecordDiffsCount を使う。GetRecordsDiffs の
// results[] はスキーマ上 new と old の両方が必須だが、追加予定のレコードには
// old が存在しないため、デコードが丸ごと失敗しうる。GetCount はフラットな
// 構造なので影響を受けない。
func pendingCount(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) int32 {
	t.Helper()
	cnt, _, err := c.GetAPIClient().RecordsAPI.GetRecordDiffsCount(ctx, zoneID).Execute()
	if err != nil {
		t.Fatalf("未反映件数を取得できない: %v", err)
	}
	if cnt == nil {
		t.Fatal("未反映件数の応答が空である")
	}
	return cnt.GetResult().Count
}

// resetPendingChanges はゾーンに未反映の編集が残っていれば破棄する。
//
// 前回の実行が中断された場合に自己回復させるための処理。専用のテストゾーンで
// あることを writeZone の二重確認で保証したうえでのみ実行する。
func resetPendingChanges(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) {
	t.Helper()
	n := pendingCount(t, ctx, c, zoneID)
	if n == 0 {
		return
	}

	t.Logf("未反映の編集が %d 件残っているため破棄する（前回の実行が中断された可能性がある）", n)
	api := c.GetAPIClient()
	syncWaiter(t, c, "未反映編集の破棄")(
		api.ZonesAPI.DeleteZoneChanges(ctx, zoneID).Execute())

	if after := pendingCount(t, ctx, c, zoneID); after != 0 {
		t.Fatalf("未反映編集を破棄したあとも %d 件残っている", after)
	}
}

// applyZone はゾーンを反映（公開）する。
//
// 反映前に未反映件数が想定どおりであることを確認する。他者が作業中の編集を
// 巻き込んで公開してしまわないための歯止め。
func applyZone(t *testing.T, ctx context.Context, c *utils.Client, zoneID string, wantPending int32, description string) {
	t.Helper()

	if got := pendingCount(t, ctx, c, zoneID); got != wantPending {
		t.Fatalf("反映前の未反映件数が想定と異なる: %d 件（想定 %d 件）。"+
			"他者の編集を巻き込む恐れがあるため中止する。", got, wantPending)
	}

	api := c.GetAPIClient()
	body := dpf.PatchZoneCommit{Description: dpf.PtrString(truncateDescription(description))}
	syncWaiter(t, c, "ゾーン反映")(
		api.ZonesAPI.PatchZoneChanges(ctx, zoneID).PatchZoneCommit(body).Execute())
}

// findRecord はゾーン内から名前と RRTYPE が一致するレコードをすべて返す。
// state の確認に使うため、utils.GetRecordFromZoneID と違い複数件を返す。
func findRecords(t *testing.T, ctx context.Context, c *utils.Client, zoneID, name string, rrtype dpf.RecordsRrtype) []dpf.Record {
	t.Helper()
	list, _, err := c.GetAPIClient().RecordsAPI.GetRecordList(ctx, zoneID).
		KeywordsRrtype(rrtype).
		ExecuteAll()
	if err != nil {
		t.Fatalf("レコード一覧を取得できない: %v", err)
	}

	want := dns.CanonicalName(name)
	var out []dpf.Record
	if list != nil {
		for _, r := range list.Results {
			if r.Rrtype == rrtype && dns.CanonicalName(r.Name) == want {
				out = append(out, r)
			}
		}
	}
	return out
}

// currentRecordValues は反映済み（DNS に出ている）レコードの rdata を返す。
func currentRecordValues(t *testing.T, ctx context.Context, c *utils.Client, zoneID, name string, rrtype dpf.RecordsRrtype) []string {
	t.Helper()
	list, _, err := c.GetAPIClient().RecordsAPI.GetRecordCurrents(ctx, zoneID).ExecuteAll()
	if err != nil {
		t.Fatalf("反映済みレコードを取得できない: %v", err)
	}

	want := dns.CanonicalName(name)
	var out []string
	if list != nil {
		for _, r := range list.Results {
			if r.Rrtype != rrtype || dns.CanonicalName(r.Name) != want {
				continue
			}
			for _, rd := range r.Rdata {
				out = append(out, dnsprobe.NormalizeRdata(rd.GetValue()))
			}
		}
	}
	return out
}

// managedServers はゾーンの権威サーバを解決し、この実行環境から実際に
// 権威応答を返すものだけを返す。
//
// 到達できないサーバを除外しないと、「全サーバの一致」を要求する反映確認が
// DPF の挙動ではなく実行環境のネットワーク到達性を検証してしまう。
func managedServers(t *testing.T, ctx context.Context, c *utils.Client, zone *dpf.Zone) []string {
	t.Helper()

	res, _, err := c.GetAPIClient().ZonesAPI.GetZoneManagedDnsServers(ctx, zone.Id).Execute()
	if err != nil {
		t.Fatalf("権威サーバ一覧を取得できない: %v", err)
	}
	if res == nil || len(res.Results) == 0 {
		t.Skip("権威サーバが 0 件のため DNS の確認をスキップする（managed DNS 無効の可能性）")
	}
	t.Logf("権威サーバ（API 応答）: %v", res.Results)

	addrs, err := dnsprobe.ResolveServers(ctx, res.Results)
	if err != nil {
		t.Fatalf("権威サーバの名前を解決できない: %v", err)
	}

	alive, dropped := dnsprobe.Preflight(ctx, dnsprobe.Exchange, addrs, zone.Name)
	for _, d := range dropped {
		t.Logf("権威サーバ %s を除外する: %s", d.Server, d.Reason)
	}
	if len(alive) == 0 {
		t.Fatalf("この実行環境から権威応答を返すサーバが 1 台も無い（解決したアドレス: %v）", addrs)
	}
	t.Logf("反映確認に使う権威サーバ: %v", alive)
	return alive
}

// waitDNS は全権威サーバが want を満たすまで待つ。
func waitDNS(t *testing.T, ctx context.Context, servers []string, qname string, qtype uint16, want dnsprobe.Predicate, what string) {
	t.Helper()

	timeout := dnsTimeout(t)
	elapsed, err := dnsprobe.WaitAll(ctx, dnsprobe.Exchange, servers, qname, qtype, want,
		dnsprobe.WaitOptions{Timeout: timeout})
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	// 上限はあくまで異常検出の天井なので、実測値を残して実態を把握できるようにする。
	t.Logf("%s: %s で完了した（上限 %s）", what, elapsed.Round(time.Millisecond), timeout)
}

// assertNoDNSRecord は、いずれの権威サーバも指定の値を返さないことを確認する。
// 待機せず即座に確認するので、ゾーン反映前の状態確認に使う。
func assertNoDNSRecord(t *testing.T, ctx context.Context, servers []string, qname, value string) {
	t.Helper()
	notHas := dnsprobe.NotHasTXT(value)
	for _, s := range servers {
		a := dnsprobe.Exchange(ctx, s, qname, dns.TypeTXT)
		if !a.Usable() {
			t.Fatalf("権威サーバ %s から判定できる応答が得られない: %s", s, a)
		}
		if !notHas(a) {
			t.Fatalf("反映前にもかかわらず %s が値を返している: %s", s, a)
		}
	}
}

// apiErrorOf は error から *dpf.GenericOpenAPIError を取り出す。
func apiErrorOf(err error) (*dpf.GenericOpenAPIError, bool) {
	var apiErr *dpf.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// errorDetails は API エラーから error_details（エラーコードと対象属性）を取り出す。
//
// specs/002-openapi-generation-pipeline/spec.md の FR-019・FR-020「エラー応答を
// ステータスごとの型へ復号し、利用者が取り出せる形で保持する」が実際に機能して
// いるかを確認するために使う。
func errorDetails(err error) []dpf.ErrorDetail {
	apiErr, ok := apiErrorOf(err)
	if !ok {
		return nil
	}
	if m, ok := apiErr.Model().(dpf.ParameterErrorResponse); ok && m.ParameterError != nil {
		return m.ParameterError.ErrorDetails
	}
	return nil
}

// hasErrorDetail は指定した code / attribute の error_details があるかを返す。
func hasErrorDetail(err error, code, attribute string) bool {
	for _, d := range errorDetails(err) {
		if d.Code == code && d.Attribute == attribute {
			return true
		}
	}
	return false
}

// describeAPIError は診断用にエラーの内容を文字列にする。
func describeAPIError(err error) string {
	apiErr, ok := apiErrorOf(err)
	if !ok {
		return fmt.Sprintf("%v", err)
	}
	return fmt.Sprintf("%v / body=%s / model=%T",
		apiErr, strings.TrimSpace(string(apiErr.Body())), apiErr.Model())
}
