// SPDX-License-Identifier: Apache-2.0

package dpf

import "context"

// ZonesApi はゾーン取得処理（utils パッケージ）が必要とする ZonesAPIService の
// 部分インターフェース。テスト時のモック差し替えのために定義している。
// *ZonesAPIService がこれを満たす。
type ZonesApi interface {
	GetZoneList(ctx context.Context) ApiGetZoneListRequest
}

var _ ZonesApi = (*ZonesAPIService)(nil)

// RecordsApi はレコード取得・更新処理（utils パッケージ）が必要とする
// RecordsAPIService の部分インターフェース。テスト時のモック差し替えのために
// 定義している。*RecordsAPIService がこれを満たす。
type RecordsApi interface {
	GetRecordList(ctx context.Context, zoneId string) ApiGetRecordListRequest
	PatchRecord(ctx context.Context, zoneId string, recordId string) ApiPatchRecordRequest
	PostRecord(ctx context.Context, zoneId string) ApiPostRecordRequest
	DeleteRecord(ctx context.Context, zoneId string, recordId string) ApiDeleteRecordRequest
	DeleteRecordChanges(ctx context.Context, zoneId string, recordId string) ApiDeleteRecordChangesRequest
}

var _ RecordsApi = (*RecordsAPIService)(nil)
