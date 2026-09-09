// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// TestZoneLookup_LongestMatch は utils.GetZoneFromName の longest match と
// parent 引数の挙動を、実 API が返すゾーンに対して検証する。
//
// テスト契約には dns-tool-test.jp. / sub.dns-tool-test.jp. /
// sub.sub.dns-tool-test.jp. という 3 階層の入れ子構造があり、これが
// 「最も深いゾーンを選ぶ」「parent=true では自身を除外する」という
// 仕様を実データで確認できる唯一の場所になる。
func TestZoneLookup_LongestMatch(t *testing.T) {
	logSkipHint(t)
	c := readOnlyClient(t)
	ctx := testContext(t)
	zonesAPI := c.GetAPIClient().ZonesAPI

	tests := []struct {
		name    string
		input   string
		parent  bool
		want    string
		wantErr error
	}{
		{
			name:  "サブドメインは最も深いゾーンに一致する",
			input: "www." + zoneSubSub,
			want:  zoneSubSub,
		},
		{
			name:  "ゾーン名そのものは完全一致する",
			input: zoneSubSub,
			want:  zoneSubSub,
		},
		{
			name:   "parent=true では自身を除外して親を返す",
			input:  zoneSubSub,
			parent: true,
			want:   zoneSub,
		},
		{
			name:  "中間ゾーンのサブドメイン",
			input: "www." + zoneSub,
			want:  zoneSub,
		},
		{
			name:   "中間ゾーンの parent は最上位ゾーン",
			input:  zoneSub,
			parent: true,
			want:   zoneApex,
		},
		{
			name:    "最上位ゾーンに親は無い",
			input:   zoneApex,
			parent:  true,
			wantErr: utils.ErrZoneNotFound,
		},
		{
			// ドメイン名の比較は miekg/dns で正規化してから行うという
			// 要件 (憲章 原則 IV / specs/003-utils-highlevel-api/spec.md FR-030) を、
			// 実 API が返す Zone.Name の表記に対して裏付ける。
			name:  "大文字小文字を区別しない",
			input: "WWW.SUB.SUB.DNS-TOOL-TEST.JP.",
			want:  zoneSubSub,
		},
		{
			name:  "末尾ドットが無くても FQDN として扱う",
			input: "www.sub.sub.dns-tool-test.jp",
			want:  zoneSubSub,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.GetZoneFromName(ctx, zonesAPI, tt.input, tt.parent)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetZoneFromName(%q, parent=%t) のエラーが %v、期待は %v（zone=%+v）",
						tt.input, tt.parent, err, tt.wantErr, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("GetZoneFromName(%q, parent=%t) が失敗した: %v%s",
					tt.input, tt.parent, err, visibleZonesHint(ctx, c))
			}
			if dns.CanonicalName(got.Name) != dns.CanonicalName(tt.want) {
				t.Fatalf("GetZoneFromName(%q, parent=%t) = %q、期待は %q",
					tt.input, tt.parent, got.Name, tt.want)
			}
		})
	}
}

// TestZoneLookup_Consistency は、ゾーン名からの解決とサービスコードからの
// 解決が同じゾーンに収束することを確認する。
func TestZoneLookup_Consistency(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)
	zonesAPI := c.GetAPIClient().ZonesAPI

	for _, zoneName := range []string{zoneApex, zoneSub, zoneSubSub} {
		t.Run(zoneName, func(t *testing.T) {
			zone, err := utils.GetZoneFromZonename(ctx, zonesAPI, zoneName)
			if err != nil {
				t.Fatalf("GetZoneFromZonename(%q) が失敗した: %v%s",
					zoneName, err, visibleZonesHint(ctx, c))
			}
			if dns.CanonicalName(zone.Name) != dns.CanonicalName(zoneName) {
				t.Fatalf("解決されたゾーンが %q、期待は %q", zone.Name, zoneName)
			}

			id, err := utils.GetZoneIDFromZonename(ctx, zonesAPI, zoneName)
			if err != nil {
				t.Fatalf("GetZoneIDFromZonename(%q) が失敗した: %v", zoneName, err)
			}
			if id != zone.Id {
				t.Errorf("GetZoneIDFromZonename = %q、GetZoneFromZonename の ID は %q", id, zone.Id)
			}

			// サービスコード経由でも同じゾーンに到達すること。
			byCode, err := utils.GetZoneFromServiceCode(ctx, zonesAPI, zone.ServiceCode)
			if err != nil {
				t.Fatalf("GetZoneFromServiceCode(%q) が失敗した: %v", zone.ServiceCode, err)
			}
			if byCode.Id != zone.Id {
				t.Errorf("サービスコード経由の ID が %q、ゾーン名経由は %q", byCode.Id, zone.Id)
			}

			codeID, err := utils.GetZoneIdFromServiceCode(ctx, zonesAPI, zone.ServiceCode)
			if err != nil {
				t.Fatalf("GetZoneIdFromServiceCode(%q) が失敗した: %v", zone.ServiceCode, err)
			}
			if codeID != zone.Id {
				t.Errorf("GetZoneIdFromServiceCode = %q、期待は %q", codeID, zone.Id)
			}

			// Zone.Name は FQDN 形式（末尾ドット付き）で返るはず。
			if !dns.IsFqdn(zone.Name) {
				t.Errorf("Zone.Name が FQDN 形式でない: %q", zone.Name)
			}
		})
	}
}

// TestZoneLookup_NotFound は、契約内に存在しないドメイン名に対して
// ErrZoneNotFound が返ることを確認する。
func TestZoneLookup_NotFound(t *testing.T) {
	c := readOnlyClient(t)
	ctx := testContext(t)

	_, err := utils.GetZoneFromZonename(ctx, c.GetAPIClient().ZonesAPI,
		"nonexistent.example.invalid.")
	if !errors.Is(err, utils.ErrZoneNotFound) {
		t.Fatalf("エラーが %v、期待は utils.ErrZoneNotFound", err)
	}
}
