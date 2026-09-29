# 契約: 公開 API

**機能**: [spec.md](../spec.md) | **計画**: [plan.md](../plan.md) | **日付**: 2026-09-29

本機能が変える公開 API と、その約束を定める。値の形式は
[data-model.md](../data-model.md)、読み書きの手順は [zone-label.md](./zone-label.md) を参照。

## 1. 排他の生成

```go
func NewMutex(cr dpf.RecordsApi, cz dpf.ZonesApi, zoneID string, opts ...Option) *Mutex
```

**破壊的変更**: 第 2 引数に `dpf.ZonesApi` が入る。既存の呼び出しは引数を 1 つ足す機械的な
置き換えで対応できる。

```go
// v0.5.0
mu := utils.NewMutex(client.RecordsAPI, zoneID)
// 以降
mu := utils.NewMutex(client.RecordsAPI, client.ZonesAPI, zoneID)
```

`NewZoneApplier` の呼び出し方は変わらない（既に `cz` を受け取っており、内部で渡す）。

### 約束

1. **`Lock` / `Renew` / `Unlock` の呼び出し方と戻り値の意味は変わらない。** 008 で定めた
   排他の契約をそのまま満たす。`utils/lockertest` による確認に適合する
2. **排他の取得・延長・解放は、ゾーンに未反映の編集を作らない。** ゾーン反映も行わない
3. **排他の操作は、権威サーバへ公開されるデータに影響しない**
4. **レコードのラベルを消費しない。** 利用者は SOA レコードのラベルを 10 個すべて使える
5. **ゾーンのラベルを 1 つ消費する。** 一度でも取得したゾーンは、解放の後もそのラベルを
   保持し続ける。利用者がそのゾーンへ付けられるラベルは以降 9 個までである
6. **レコードの一括更新とゾーン反映は排他に影響しない。** 反映の後も排他は保持され、通常
   どおり解放できる

## 2. Option

| Option | 対象 | 既定値 | 0 値・空値の扱い | 本機能での変更 |
|---|---|---|---|---|
| `WithOwner(s)` | 保持者 | ホスト名・PID・乱数から生成 | 空文字は無視（既定のまま） | **32 文字を超える値は `Lock` で `ErrOwnerTooLong`** |
| `WithTTL(d)` | 保持期間 | 15 分 | 0 以下は無視 | 変更なし |
| `WithRenewInterval(d)` | 延長の間隔 | 保持期間の 1/3 | 0 以下は無視 | 変更なし |
| `WithVerifyTimeout(d)` | 書き込みの確認の上限 | 10 秒 | 0 以下は無視 | **確認の対象がゾーンのラベルになる** |

`Option` はエラーを返さない作法であるため、**保持者の長さの検証は `Lock` の入口で行う**
（[research.md](../research.md) D10）。

## 3. エラー

| 番兵 | 変更 | `errors.Is` の互換 |
|---|---|---|
| `ErrStillLock` | 変更なし | 保たれる |
| `ErrNotLockHolder` | 変更なし | 保たれる |
| `ErrLabelLimit` | **意味が「ゾーンのラベルの枠が足りない」へ変わる。** 値は同じ | **保たれる** |
| `ErrOwnerTooLong` | **新規** | — |
| `ErrRecordNotFound` | 変更なし（ゾーン名の取得で返りうる） | 保たれる |

### 約束

1. `ErrLabelLimit` と `ErrOwnerTooLong` は、**いずれも書き込みを試みる前に返る。** 待っても
   解消しないため、取得を待つ指定があっても待たない
2. 2 つを分けるのは対処が異なるためである（前者はラベルを削る、後者は保持者を短くする）

## 4. ラベルのキー

| 定数 | 値 | 付く先 | 本機能での変更 |
|---|---|---|---|
| `ZoneLockLabelKey` | `lock.dpf-go` | **ゾーン** | **新規** |
| `LockOwnerLabelKey` | `owner.lock.dpf-go` | 一時レコード追加ロックの専用レコード | 用途が専用レコードだけになる |
| `LockDeadlineLabelKey` | `deadline.lock.dpf-go` | 同上 | 同上 |

既存の 2 つの定数は**削除しない**。専用レコードで引き続き使う。

## 5. インターフェースの変更

```go
type ZonesApi interface {
	GetZoneList(ctx context.Context) ApiGetZoneListRequest
	PatchZoneAtomicChanges(ctx context.Context, zoneId string) ApiPatchZoneAtomicChangesRequest
	GetZoneLabels(ctx context.Context, zoneId string) ApiGetZoneLabelsRequest   // 追加
	PutZoneLabels(ctx context.Context, zoneId string) ApiPutZoneLabelsRequest   // 追加
}
```

**破壊的変更**: 自分で `ZonesApi` を実装している利用者は 2 メソッドの追加が必要である。
`*dpf.ZonesAPIService` は追加後も満たすため、`client.ZonesAPI` を渡している利用者への影響は
無い。007 が `PatchZoneAtomicChanges` を追加したのと同じ種類の変更である。

## 6. godoc に明記すること

1. **排他の効く相手が変わること。** 保持中に他ユーザや管理画面からの**手作業の編集を止める
   効果は失われる**。一方、**2 者が同時に取得しないことは一時レコード追加ロックによる重複の
   拒否によって機械的に保証され、ユーザをまたいで効く**（仕様 FR-012）
2. **ゾーンのラベルを 1 つ、恒久的に消費すること。** 解放してもラベルは残る（仕様 FR-006）
3. **保持者は 1〜32 文字であること。** 超える値は書き込みを試みずにエラーになり、黙って
   切り詰められることはない（仕様 FR-006）
4. **旧版（レコードのラベルを用いる方式）と混在させられないこと。** 互いの排他を認識でき
   ない。ライブラリは検出できないため、同じゾーンを触るプログラムをまとめて更新する必要が
   ある（仕様 FR-013）
5. **旧版が SOA レコードに残したラベルと未反映の編集は掃除されないこと**（仕様 FR-014）
6. 取得・延長・解放がゾーン反映を伴わないこと（006 から継承）
