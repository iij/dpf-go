//go:build integration

// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/internal/dnsprobe"
	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// apexZone は最上位のテストゾーンを、名前の完全一致を確認したうえで返す。
//
// utils.GetZoneFromZonename は longest match なので、返ってきたゾーンが
// 本当に目的のゾーンかを必ず確認する。
func apexZone(t *testing.T, ctx context.Context, c *utils.Client) *dpf.Zone {
	t.Helper()

	z, err := utils.GetZoneFromZonename(ctx, c.GetAPIClient().ZonesAPI, zoneApex)
	if err != nil {
		t.Fatalf("ゾーン %q を取得できない: %v", zoneApex, err)
	}
	if dns.CanonicalName(z.Name) != dns.CanonicalName(zoneApex) {
		t.Fatalf("解決されたゾーンが %q、期待は %q", z.Name, zoneApex)
	}
	return z
}

// findDelegation は候補一覧から指定ゾーンの申請情報を探す。
func findDelegation(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) (dpf.Delegation, bool) {
	t.Helper()

	list, _, err := c.GetAPIClient().DelegationsAPI.GetDelegationList(ctx).ExecuteAll()
	if err != nil {
		t.Fatalf("ネームサーバ申請候補の一覧を取得できない: %v", err)
	}
	if list != nil {
		for _, d := range list.Results {
			if d.Id == zoneID {
				return d, true
			}
		}
	}
	return dpf.Delegation{}, false
}

// TestDelegations_List は申請候補の一覧が取得でき、件数取得 API と一致し、
// 最上位のテストゾーンが候補に含まれることを確認する。
func TestDelegations_List(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)
	api := c.GetAPIClient()

	list, _, err := api.DelegationsAPI.GetDelegationList(ctx).ExecuteAll()
	if err != nil {
		t.Fatalf("申請候補の一覧を取得できない: %v", err)
	}
	cnt, _, err := api.DelegationsAPI.GetDelegationListCount(ctx).Execute()
	if err != nil {
		t.Fatalf("申請候補の件数を取得できない: %v", err)
	}

	got := int32(len(list.Results))
	if want := cnt.GetResult().Count; got != want {
		t.Fatalf("ExecuteAll の件数が %d、GetDelegationListCount は %d", got, want)
	}
	t.Logf("申請候補: %d 件", got)

	zone := apexZone(t, ctx, c)
	if _, ok := findDelegation(t, ctx, c, zone.Id); !ok {
		t.Fatalf("ゾーン %q (id=%s) が申請候補に含まれていない", zone.Name, zone.Id)
	}
}

// requireAllServersLive はゾーンの権威サーバ全台が権威応答を返すことを要求する。
//
// ネームサーバ申請は「動いていないネームサーバへ委任してはいけない」という
// 制約があるため、1 台でも確認できなければ申請せずに失敗させる。
// 実行環境から到達できないだけの場合も失敗するが、確認できないまま委任するより
// 安全側に倒す。
func requireAllServersLive(t *testing.T, ctx context.Context, c *utils.Client, zone *dpf.Zone) {
	t.Helper()

	res, _, err := c.GetAPIClient().ZonesAPI.GetZoneManagedDnsServers(ctx, zone.Id).Execute()
	if err != nil {
		t.Fatalf("権威サーバ一覧を取得できない: %v", err)
	}
	if res == nil || len(res.Results) == 0 {
		t.Fatalf("ゾーン %q の権威サーバが 0 件である。委任先を確認できないため申請しない", zone.Name)
	}

	addrs, err := dnsprobe.ResolveServers(ctx, res.Results)
	if err != nil {
		t.Fatalf("権威サーバの名前を解決できない。委任先を確認できないため申請しない: %v", err)
	}

	alive, dropped := dnsprobe.Preflight(ctx, dnsprobe.Exchange, addrs, zone.Name)
	if len(dropped) > 0 {
		for _, d := range dropped {
			t.Errorf("権威サーバ %s がゾーン %q の権威応答を返さない: %s", d.Server, zone.Name, d.Reason)
		}
		t.Fatalf("動作を確認できない権威サーバがあるため、ネームサーバ申請を行わない（%d/%d 台が応答）",
			len(alive), len(addrs))
	}
	t.Logf("権威サーバ %d 台すべてがゾーン %q の権威応答を返した: %v", len(alive), zone.Name, alive)
}

// TestDelegations_Request はネームサーバ申請を実行し、申請日時が
// 更新されることを確認する。
//
// 申請は何度でも実行できるため CI で繰り返してよい。ただし申請の前に
// 委任先の権威サーバが実際に動作していることを必ず確認する。
func TestDelegations_Request(t *testing.T) {
	c := writeClient(t)
	ctx := testContext(t)
	zone := apexZone(t, ctx, c)

	before, ok := findDelegation(t, ctx, c, zone.Id)
	if !ok {
		t.Fatalf("ゾーン %q が申請候補に含まれていないため申請できない", zone.Name)
	}
	t.Logf("申請前の delegation_requested_at: %s", before.DelegationRequestedAt)

	// 申請前ゲート。ここを通らない限り POST は行わない。
	requireAllServersLive(t, ctx, c, zone)

	start := time.Now().UTC()
	body := dpf.PostDelegations{ZoneIds: []string{zone.Id}}
	syncWaiter(t, c, "ネームサーバ申請")(
		c.GetAPIClient().DelegationsAPI.PostDelegation(ctx).PostDelegations(body).Execute())

	after, ok := findDelegation(t, ctx, c, zone.Id)
	if !ok {
		t.Fatalf("申請後にゾーン %q が申請候補の一覧から消えた。"+
			"再申請可能という前提と異なるため、テストの想定を見直すこと。", zone.Name)
	}
	t.Logf("申請後の delegation_requested_at: %s", after.DelegationRequestedAt)

	switch {
	case after.DelegationRequestedAt.After(before.DelegationRequestedAt):
		// 期待どおり更新された。
	case before.DelegationRequestedAt.IsZero() && !after.DelegationRequestedAt.IsZero():
		// 初回申請。
	default:
		t.Errorf("申請日時が更新されていない（申請前 %s / 申請後 %s / 申請実行 %s）",
			before.DelegationRequestedAt, after.DelegationRequestedAt, start)
	}
}
