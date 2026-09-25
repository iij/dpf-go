# 契約: 公開 API（利用者向け）

**機能**: [../spec.md](../spec.md) | **計画**: [../plan.md](../plan.md) | **日付**: 2026-09-24

署名は実装時に確定するが、**外から見える約束**は本ファイルを正とする。

## 1. 排他を保持したままの実行

```go
func RunLocked(ctx context.Context, l Locker, fn func(ctx context.Context) error,
	opts ...HoldOption) error
```

007 の `Mutex.Do` と同じ約束を、任意の排他に対して提供する。

- `fn` に渡される context は `ctx` から派生し、**排他を失った時点で打ち切られる**
- 実行中は自動で延長される。間隔は「実装の申告 → 利用者の指定（Option）→ 既定値」の順で決まる
- 排他を取得できない場合、`fn` は呼ばれず `ErrStillLock` を返す
- 終了時に解放する。`fn` のエラーは種類を判別できる形のまま返す
- 復帰した時点で、延長のための goroutine は終了している
- `fn` の異常終了（panic）は捕捉しない

`Mutex.Do` は `RunLocked` の薄い包みとして残る。**v0.4.0 の書き方はそのまま動く。**

### Option

| Option | 対象 | 既定値 |
|---|---|---|
| 延長の間隔 | 実装の申告を上書きする | 実装の申告 → 無ければ既定値 |
| 取得の待機 | 取得できるまで待つ間隔 | 待たない |

## 2. ゾーン全体の一括置き換え

```go
func NewZoneApplier(cr dpf.RecordsApi, cz dpf.ZonesApi, cj dpf.JobsApi, zoneID string,
	opts ...ApplierOption) *ZoneApplier

func WithLocker(l Locker) ApplierOption
func WithLockOptions(opts ...Option) ApplierOption
```

- `WithLocker` を指定しなければ、レコードを使う排他が既定で使われる（FR-006）
- `WithLockOptions` は既定の排他への設定である。`WithLocker` を指定した場合は意味を持たない
- **排他を差し替えても `Apply` の呼び出し方と結果は変わらない**（FR-008）。反映が排他を
  解く実装でも解かない実装でも、同じように動く

### 破壊的変更

`NewZoneApplier` の可変長引数の型が `Option` から `ApplierOption` へ変わる。

```go
// v0.4.0
utils.NewZoneApplier(cr, cz, cj, zoneID, utils.WithTTL(30*time.Minute))

// 本機能の後
utils.NewZoneApplier(cr, cz, cj, zoneID,
	utils.WithLockOptions(utils.WithTTL(30*time.Minute)))
```

機械的な置き換えで対応できる（FR-007）。

## 3. 既定の実装

`utils.Mutex` は `Locker` を満たす。`Lock` / `Renew` / `Unlock` の意味は 006・007 のまま
変わらない。加えて次を申告する。

- 延長の間隔として保持期間の 1/3 を返す（007 の既定と同じ）
- ゾーン反映が排他を解くことを申告する（非公開の形。利用者からは見えない）

## 4. 実装を書く人向け

契約は [locker.md](./locker.md) を正とする。確認の手段は `utils/lockertest` にある。

## 5. godoc に明記すること

1. **外部の仕組みを使う排他に替えると、別ユーザや管理画面からの編集を止める効果が失われる
   こと**（FR-014）。レコードを使う排他は、編集中のレコードへの他ユーザからの編集を
   DPF-API が拒否することによって、ライブラリを使っていない相手にも効く。
2. 2 つの方式の比較（FR-016）。

   | | レコードを使う排他（既定） | 外部の仕組みを使う排他 |
   |---|---|---|
   | 別ユーザ・管理画面からの編集を止める | **止まる**（DPF-API が拒否する） | 止まらない |
   | 追加の運用 | 不要 | 要る（ミドルウェアの運用） |
   | SOA のラベルの消費 | 2 つ（利用者が使えるのは 8 つ） | 無し |
   | 保持中のゾーンの状態 | 未反映の編集が残る（DNSSEC の有効化などが行えない） | 変化なし |
   | 取得 1 回あたりの API 呼び出し | 8 回 | 無し |
   | ゾーン全体の一括置き換え | 反映が排他を解く（ライブラリが吸収する） | 解かない |

3. 排他がどのゾーンに対応するかを決める責任が実装側にあること（FR-015）。
4. 両方を使いたい場合は、2 つを順に取得する実装を書けばよいこと。
