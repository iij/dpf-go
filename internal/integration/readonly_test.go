//go:build integration

// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"errors"
	"testing"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// TestReadOnly_ZoneList はゾーン一覧が取得でき、テスト用の 3 ゾーンが
// すべて含まれることを確認する。
func TestReadOnly_ZoneList(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)

	zones, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).ExecuteAll()
	if err != nil {
		t.Fatalf("ゾーン一覧を取得できない: %v", err)
	}
	if zones == nil || len(zones.Results) == 0 {
		t.Fatal("ゾーンが 1 件も返らなかった。" +
			"原因の切り分けは TestConnectivity_VisibleZones の出力を参照すること。")
	}

	found := make(map[string]bool)
	for _, z := range zones.Results {
		// 生成コードのデコードが正しく行われているかの確認も兼ねる。
		if z.Id == "" || z.Name == "" || z.ServiceCode == "" {
			t.Errorf("必須フィールドが空のゾーンがある: %+v", z)
		}
		found[dns.CanonicalName(z.Name)] = true
	}

	for _, want := range []string{zoneApex, zoneSub, zoneSubSub} {
		if !found[dns.CanonicalName(want)] {
			t.Errorf("テスト用ゾーン %q が一覧に含まれていない", want)
		}
	}
	t.Logf("ゾーン数: %d", len(zones.Results))
}

// TestReadOnly_ExecuteAllMatchesCount は ExecuteAll のページング結果が
// 件数取得 API と一致することを確認する。
func TestReadOnly_ExecuteAllMatchesCount(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()

	zone, err := utils.GetZoneFromZonename(ctx, api.ZonesAPI, zoneSubSub)
	if err != nil {
		t.Fatalf("ゾーンを取得できない: %v%s", err, visibleZonesHint(ctx, c))
	}

	list, _, err := api.RecordsAPI.GetRecordList(ctx, zone.Id).ExecuteAll()
	if err != nil {
		t.Fatalf("レコード一覧を取得できない: %v", err)
	}
	cnt, _, err := api.RecordsAPI.GetRecordListCount(ctx, zone.Id).Execute()
	if err != nil {
		t.Fatalf("レコード件数を取得できない: %v", err)
	}

	got := int32(len(list.Results))
	want := cnt.GetResult().Count
	if got != want {
		t.Fatalf("ExecuteAll の件数が %d、GetRecordListCount は %d", got, want)
	}
	t.Logf("レコード件数: %d", got)
}

// TestReadOnly_RecordLookup は SOA が取得でき、存在しない名前では
// ErrRecordNotFound が返ることを確認する。
func TestReadOnly_RecordLookup(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()

	zone, err := utils.GetZoneFromZonename(ctx, api.ZonesAPI, zoneSubSub)
	if err != nil {
		t.Fatalf("ゾーンを取得できない: %v%s", err, visibleZonesHint(ctx, c))
	}

	soa, err := utils.GetRecordFromZoneID(ctx, api.RecordsAPI, zone.Id, zone.Name,
		dpf.RECORDSRRTYPE_SOA)
	if err != nil {
		t.Fatalf("SOA を取得できない: %v", err)
	}
	if soa.Rrtype != dpf.RECORDSRRTYPE_SOA {
		t.Errorf("RRTYPE が %q、期待は SOA", soa.Rrtype)
	}
	t.Logf("SOA: id=%s state=%d labels=%v", soa.Id, soa.State, soa.Labels)

	_, err = utils.GetRecordFromZoneID(ctx, api.RecordsAPI, zone.Id,
		"definitely-not-present."+zoneSubSub, dpf.RECORDSRRTYPE_A)
	if !errors.Is(err, utils.ErrRecordNotFound) {
		t.Fatalf("エラーが %v、期待は utils.ErrRecordNotFound", err)
	}
}

// TestReadOnly_ManagedDnsServers は権威サーバ一覧が取得でき、
// 各サーバが実際に権威応答を返すことを確認する。
func TestReadOnly_ManagedDnsServers(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)

	zone, err := utils.GetZoneFromZonename(ctx, c.GetAPIClient().ZonesAPI, zoneSubSub)
	if err != nil {
		t.Fatalf("ゾーンを取得できない: %v%s", err, visibleZonesHint(ctx, c))
	}

	// managedServers 内で名前解決と権威応答(AA=1)の確認まで行う。
	servers := managedServers(t, ctx, c, zone)
	if len(servers) == 0 {
		t.Fatal("権威応答を返すサーバが 1 台も無い")
	}
}

// TestReadOnly_JobID は GetJobID がレスポンスから request_id を取り出せることを
// 確認する。ボディを読んだあとも呼び出し側が再度読めることも併せて確認する。
func TestReadOnly_JobID(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)

	zones, resp, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute()
	if err != nil {
		t.Fatalf("ゾーン一覧を取得できない: %v", err)
	}
	if id := utils.GetJobID(resp); id == "" {
		t.Error("GetJobID が空文字を返した")
	} else if id != zones.RequestId {
		t.Errorf("GetJobID = %q、レスポンスの request_id は %q", id, zones.RequestId)
	}
}

// TestReadOnly_ErrorModel は、エラー応答からエラーコードと対象属性を
// 取り出せることを確認する（specs/002-openapi-generation-pipeline/spec.md FR-019）。
//
// 実測した DPF-API の挙動:
//   - 存在しないゾーンでも 404 ではなく 400 ParameterError が返り、
//     error_details に code=not_found / attribute=zone が入る
//   - 書式から外れた ID は code=invalid / attribute=schema になる
//
// つまりゾーン取得において NotFoundError モデルは使われない。
func TestReadOnly_ErrorModel(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()

	tests := []struct {
		name          string
		zoneID        string
		wantCode      string
		wantAttribute string
	}{
		{
			name:          "書式は正しいが存在しないゾーンID",
			zoneID:        "mzzzzzzzzzzzzz",
			wantCode:      "not_found",
			wantAttribute: "zone",
		},
		{
			name:          "書式から外れたゾーンID",
			zoneID:        "zzzzzzzzzzzzzz",
			wantCode:      "invalid",
			wantAttribute: "schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := api.ZonesAPI.GetZone(ctx, tt.zoneID).Execute()
			if err == nil {
				t.Fatalf("ゾーン ID %q でエラーにならなかった", tt.zoneID)
			}

			apiErr, ok := apiErrorOf(err)
			if !ok {
				t.Fatalf("*dpf.GenericOpenAPIError ではない: %v", err)
			}
			if apiErr.Model() == nil {
				t.Fatal("Model() が nil である。エラーモデルが対応付けられていない")
			}

			if !hasErrorDetail(err, tt.wantCode, tt.wantAttribute) {
				t.Errorf("error_details に code=%q attribute=%q が無い: %s",
					tt.wantCode, tt.wantAttribute, describeAPIError(err))
			}
		})
	}
}

// TestReadOnly_TokenCannotWrite は参照系トークンで更新系 API を実行できない
// ことを確認する。
//
// ステータスコードは決め打ちしない。openapi.json は records 系に 401/403 を
// 定義しておらず、マニュアル上はトークン不正が 400 ParameterError になる。
// ここでは「失敗すること」だけを表明し、実際の内容はログに残す。
func TestReadOnly_TokenCannotWrite(t *testing.T) {
	ro := readOnlyClient(t)
	ctx := testContext(t)
	api := ro.GetAPIClient()

	zone, err := utils.GetZoneFromZonename(ctx, api.ZonesAPI, zoneSubSub)
	if err != nil {
		t.Fatalf("ゾーンを取得できない: %v%s", err, visibleZonesHint(ctx, ro))
	}

	body := dpf.PostRecord{
		Name:   "_ro-should-fail." + zoneSubSub,
		Rrtype: dpf.RECORDSRRTYPEWITHOUTSOA_TXT,
		Rdata:  []dpf.RecordsRdataInner{{Value: dpf.PtrString(`"should not be created"`)}},
	}
	async, _, err := api.RecordsAPI.PostRecord(ctx, zone.Id).PostRecord(body).Execute()

	if err == nil {
		// 想定外に成功した場合、未反映の編集が残ってしまうので必ず片付ける。
		t.Errorf("参照系トークンでレコードを作成できてしまった（request_id=%s）", async.GetRequestId())
		cleanupUnexpectedRecord(t, zone.Id, body.Name)
		return
	}

	if _, ok := apiErrorOf(err); !ok {
		t.Fatalf("*dpf.GenericOpenAPIError ではない: %v", err)
	}
	// 実測では 403 ではなく 400 ParameterError で、
	// error_details に code=invalid / attribute=access_token が入る。
	if !hasErrorDetail(err, "invalid", "access_token") {
		t.Errorf("スコープ不足のエラーに code=invalid attribute=access_token が無い: %s",
			describeAPIError(err))
	}
	t.Logf("参照系トークンでの作成は想定どおり失敗した: %s", describeAPIError(err))
}

// cleanupUnexpectedRecord は想定外に作成されたレコードの未反映編集を破棄する。
func cleanupUnexpectedRecord(t *testing.T, zoneID, name string) {
	t.Helper()

	token := requireEnv(t, envTokenRW, "想定外に作成されたレコードの後始末に必要")
	c, err := newClient(token, 0)
	if err != nil {
		t.Fatalf("後始末用クライアントを生成できない: %v", err)
	}

	ctx, cancel := cleanupContext()
	defer cancel()

	for _, r := range findRecords(t, ctx, c, zoneID, name, dpf.RECORDSRRTYPE_TXT) {
		t.Logf("想定外に作成されたレコード %s (id=%s) の編集を破棄する", r.Name, r.Id)
		syncWaiter(t, c, "レコード編集の破棄")(
			c.GetAPIClient().RecordsAPI.DeleteRecordChanges(ctx, zoneID, r.Id).Execute())
	}
}
