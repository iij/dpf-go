// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"

	dpf "github.com/iij/dpf-go"
	"github.com/miekg/dns"
)

// ErrRecordNotFound は指定された名前・RRTYPE に対応するレコードが
// 見つからなかった場合に返される。
var ErrRecordNotFound = errors.New("dpf: record not found")

// GetRecordFromZoneID はゾーンIDの中から、名前と RRTYPE が一致するレコードを取得する。
// 存在しない場合は ErrRecordNotFound を返す。
func GetRecordFromZoneID(ctx context.Context, cr dpf.RecordsApi, zoneID string, recordName string, rrtype dpf.RecordsRrtype) (*dpf.Record, error) {
	records, _, err := cr.GetRecordList(ctx, zoneID).
		KeywordsName([]string{recordName}).
		KeywordsRrtype(rrtype).
		ExecuteAll()
	if err != nil {
		return nil, err
	}

	// KeywordsName は部分一致のため、クライアント側で名前・RRTYPE の完全一致を確認する。
	// ドメイン名は miekg/dns の CanonicalName で正規化してから比較する。
	n := dns.CanonicalName(recordName)
	if records != nil {
		for i := range records.Results {
			r := &records.Results[i]
			if r.Rrtype == rrtype && dns.CanonicalName(r.Name) == n {
				return r, nil
			}
		}
	}
	return nil, ErrRecordNotFound
}

// GetRecordFromZonename はゾーン名とレコード名から、対応するゾーンとレコードを取得する。
// ゾーンは zonename に longest match で対応するものを使用する。
// ゾーンが存在しない場合は ErrZoneNotFound、レコードが存在しない場合は ErrRecordNotFound を返す。
func GetRecordFromZonename(ctx context.Context, cz dpf.ZonesApi, cr dpf.RecordsApi, zonename string, recordName string, rrtype dpf.RecordsRrtype) (*dpf.Zone, *dpf.Record, error) {
	zone, err := GetZoneFromZonename(ctx, cz, zonename)
	if err != nil {
		return nil, nil, err
	}
	record, err := GetRecordFromZoneID(ctx, cr, zone.Id, recordName, rrtype)
	if err != nil {
		return zone, nil, err
	}
	return zone, record, nil
}

// GetRecordFromRecordName はレコード名（FQDN）から、それを含むゾーンとレコードを取得する。
// レコード名に longest match で対応するゾーンを特定し、その中からレコードを取得する。
// ゾーンが存在しない場合は ErrZoneNotFound、レコードが存在しない場合は ErrRecordNotFound を返す。
func GetRecordFromRecordName(ctx context.Context, cz dpf.ZonesApi, cr dpf.RecordsApi, recordName string, rrtype dpf.RecordsRrtype) (*dpf.Zone, *dpf.Record, error) {
	zone, err := GetZoneFromName(ctx, cz, recordName, false)
	if err != nil {
		return nil, nil, err
	}
	record, err := GetRecordFromZoneID(ctx, cr, zone.Id, recordName, rrtype)
	if err != nil {
		return zone, nil, err
	}
	return zone, record, nil
}
