// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"

	dpf "github.com/iij/dpf-go"
	"github.com/miekg/dns"
)

// ErrZoneNotFound はドメイン名・ゾーン名・サービスコードに対応するゾーンが
// 見つからなかった場合に返される。
var ErrZoneNotFound = errors.New("dpf: zone not found")

// GetZoneFromName はドメイン名から対応するゾーンを取得する。
// ドメイン名に longest match で対応するゾーンを返す。存在しない場合は ErrZoneNotFound を返す。
//
// ドメイン名の比較は miekg/dns の関数で行う。dns.CanonicalName で小文字化・FQDN 化
// したうえで、dns.IsSubDomain で包含関係を、dns.CountLabel でラベル数（マッチの長さ）を判定する。
//
// parent が false の場合は name と完全一致するゾーンも対象とする。
// parent が true の場合は name の真の上位（祖先）ゾーンのみを対象とし、
// name と完全一致するゾーン自身は除外する。
func GetZoneFromName(ctx context.Context, c dpf.ZonesApi, name string, parent bool) (*dpf.Zone, error) {
	zones, _, err := c.GetZoneList(ctx).ExecuteAll()
	if err != nil {
		return nil, err
	}
	if zones == nil {
		return nil, ErrZoneNotFound
	}

	n := dns.CanonicalName(name)
	var best *dpf.Zone
	bestLabels := -1
	for i := range zones.Results {
		z := &zones.Results[i]
		zn := dns.CanonicalName(z.Name)

		// zn が n を包含する（zn == n もしくは zn が n の上位ゾーン）か。
		if !dns.IsSubDomain(zn, n) {
			continue
		}
		// 完全一致は parent 検索では除外する。
		if parent && dns.IsSubDomain(n, zn) {
			continue
		}

		if labels := dns.CountLabel(zn); labels > bestLabels {
			best = z
			bestLabels = labels
		}
	}

	if best == nil {
		return nil, ErrZoneNotFound
	}
	return best, nil
}

// GetZoneIDFromZonename はドメイン名から対応するゾーンIDを取得する。
// ドメイン名に longest match で対応するゾーンIDを返す。存在しない場合は ErrZoneNotFound を返す。
func GetZoneIDFromZonename(ctx context.Context, c dpf.ZonesApi, zonename string) (string, error) {
	z, err := GetZoneFromName(ctx, c, zonename, false)
	if err != nil {
		return "", err
	}
	return z.Id, nil
}

// GetZoneFromZonename はドメイン名から対応するゾーンを取得する。
// ドメイン名に longest match で対応するゾーンを返す。存在しない場合は ErrZoneNotFound を返す。
// 返却される Zone の Name は FQDN 形式（末尾ドット付き）。
func GetZoneFromZonename(ctx context.Context, c dpf.ZonesApi, zonename string) (*dpf.Zone, error) {
	return GetZoneFromName(ctx, c, zonename, false)
}

// GetZoneFromServiceCode はサービスコードに対応するゾーンを取得する。
// 存在しない場合は ErrZoneNotFound を返す。
func GetZoneFromServiceCode(ctx context.Context, c dpf.ZonesApi, serviceCode string) (*dpf.Zone, error) {
	zones, _, err := c.GetZoneList(ctx).KeywordsServiceCode([]string{serviceCode}).ExecuteAll()
	if err != nil {
		return nil, err
	}
	if zones != nil {
		for i := range zones.Results {
			if zones.Results[i].ServiceCode == serviceCode {
				return &zones.Results[i], nil
			}
		}
	}
	return nil, ErrZoneNotFound
}

// GetZoneIdFromServiceCode はサービスコードに対応するゾーンIDを取得する。
// 存在しない場合は ErrZoneNotFound を返す。
func GetZoneIdFromServiceCode(ctx context.Context, c dpf.ZonesApi, serviceCode string) (string, error) {
	z, err := GetZoneFromServiceCode(ctx, c, serviceCode)
	if err != nil {
		return "", err
	}
	return z.Id, nil
}
