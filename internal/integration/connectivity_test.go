// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"os"
	"sort"
	"testing"

	"github.com/iij/dpf-go/utils"
)

// ファイル名は意図的に先頭（アルファベット順）にしている。
// 疎通と権限の確認を最初に走らせ、以降の失敗の切り分けを容易にするため。

// endpointForLog は実際に使われるエンドポイントを返す（診断用）。
func endpointForLog() string {
	if ep := os.Getenv(envEndpoint); ep != "" {
		return ep + " (" + envEndpoint + " による指定)"
	}
	return "https://api.dns-platform.jp/dpf/v1 (既定の本番エンドポイント)"
}

// TestConnectivity_Ping は API への疎通とアクセストークンの有効性を確認する。
//
// /ping はスコープ(dpf_read など)さえ有効なら成功する。逆に言えば、ping が
// 通るのにゾーンが 1 件も見えない場合、原因はトークンではなく
// 「管理対象の権限設定」にある。
func TestConnectivity_Ping(t *testing.T) {
	t.Logf("エンドポイント: %s", endpointForLog())

	c := readOnlyClient(t)
	ctx := testContext(t)

	ping, _, err := c.GetAPIClient().PingAPI.GetPing(ctx).Execute()
	if err != nil {
		t.Fatalf("API に疎通できない、またはアクセストークンが無効である: %s\n"+
			"確認事項:\n"+
			"  - %s のトークンが有効で、dpf_read スコープを持つこと\n"+
			"  - %s（未設定なら本番）が正しいこと",
			describeAPIError(err), envTokenRO, envEndpoint)
	}
	t.Logf("ping 成功 (request_id=%s)", ping.GetRequestId())
}

// TestConnectivity_PingWithWriteToken は更新系トークンの有効性を確認する。
func TestConnectivity_PingWithWriteToken(t *testing.T) {
	c := writeClient(t)
	ctx := testContext(t)

	ping, _, err := c.GetAPIClient().PingAPI.GetPing(ctx).Execute()
	if err != nil {
		t.Fatalf("更新系トークンで API に疎通できない: %s", describeAPIError(err))
	}
	t.Logf("ping 成功 (request_id=%s)", ping.GetRequestId())
}

// visibleZoneNames はトークンから見えるゾーン名を返す（診断用）。
func visibleZoneNames(ctx context.Context, c *utils.Client) ([]string, error) {
	zones, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).ExecuteAll()
	if err != nil {
		return nil, err
	}
	if zones == nil {
		return nil, nil
	}
	names := make([]string, 0, len(zones.Results))
	for _, z := range zones.Results {
		names = append(names, z.Name+" ("+z.ServiceCode+")")
	}
	sort.Strings(names)
	return names, nil
}

// TestConnectivity_VisibleZones は、トークンから実際に見えるゾーンを列挙する。
//
// ping が通るのにゾーンが 0 件の場合、DPF-API のリファレンスにあるとおり
// 「アクセストークンの許可するスコープが適切であっても、管理対象の権限が
// 付与されていない場合はAPIを実行できません」という状態にあたる。
// この確認を独立させることで、以降のテストの失敗が権限の問題なのか
// ロジックの問題なのかを切り分けられる。
func TestConnectivity_VisibleZones(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)

	names, err := visibleZoneNames(ctx, c)
	if err != nil {
		t.Fatalf("ゾーン一覧を取得できない: %s", describeAPIError(err))
	}

	if len(names) == 0 {
		t.Fatalf("API 疎通は成功しているが、ゾーンが 1 件も見えない。\n"+
			"エンドポイント: %s\n"+
			"アクセストークン自体は有効なので、次のいずれかが原因である:\n"+
			"  1. IIJ ID アカウントに対象契約の「管理対象の権限」が付与されていない\n"+
			"     （スコープが正しくても、契約ごとの参照権限が別途必要）\n"+
			"  2. トークンが、テスト契約とは別の IIJ ID アカウントで発行されている\n"+
			"  3. エンドポイントが誤っている（検証環境のトークンで本番を叩いている等）",
			endpointForLog())
	}

	t.Logf("見えるゾーン: %d 件", len(names))
	for _, n := range names {
		t.Logf("  %s", n)
	}
}
