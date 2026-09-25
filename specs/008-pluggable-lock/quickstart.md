# 検証手順: 排他の仕組みを差し替えられるようにする

**機能**: [spec.md](./spec.md) | **計画**: [plan.md](./plan.md) | **日付**: 2026-09-24

実装が仕様を満たしていることを確認する手順。個々の作業の分解は `/speckit-tasks` の出力
（tasks.md）で行う。

## 前提

```bash
cd $(git rev-parse --show-toplevel)
```

- Go 1.27 以上。`make check` の各ツールが導入済みであること。
- 第 4 節（実 API）には `DPF_TOKEN_RO` / `DPF_TOKEN_RW` / `DPF_TEST_SERVICE_CODE` が必要。

## 1. 品質ゲート

```bash
make check
```

**期待**: すべて成功。`go.mod` に差分が無いこと（依存を追加しない）。

新設する `utils/lockertest` が `testing` を import するため、**`utils` 本体が `testing` を
import していないこと**を確認する。

```bash
go list -deps ./utils | grep -x testing && echo "NG: utils が testing に依存している" || echo "OK"
```

## 2. 契約の確認手段そのものの検証

```bash
go test ./utils/lockertest/ -race -count=1 -v
```

確認の手段が正しく働くことを、**契約を満たす実装と、わざと満たさない実装の両方**で
確かめる（FR-011〜FR-013、SC-003）。

| 確認すること | 対応 |
|---|---|
| 参照実装（契約を満たす）が通る | FR-011 |
| 再入を許す実装が落ちる。どの約束に反しているかが示される | FR-012、契約 第 2 節 |
| 取得できるまで待つ実装が落ちる | 契約 第 2 節 |
| `Renew` が保持の喪失を報告しない実装が落ちる | FR-010、契約 第 3 節 |
| `Unlock` が他者の排他を解放する実装が落ちる | 契約 第 4 節 |
| 番兵エラーに `errors.Is` で一致しない実装が落ちる | FR-003、契約 第 6 節 |
| ネットワークを必要としない | FR-013 |

## 3. 抽象と既定の実装

```bash
go test ./utils/ -race -count=1 -run 'TestRunLocked|TestLocker|TestZoneApplier|TestMutex' -v
```

| 確認すること | 対応 |
|---|---|
| `utils.Mutex` が `Locker` を満たす（コンパイル時の確認） | FR-005 |
| `Mutex` が延長の間隔として保持期間の 1/3 を申告する | [research.md](./research.md) D5 |
| `RunLocked` が任意の実装で動く（参照実装で確認） | FR-001 |
| 実装が延長の間隔を申告すれば、それが使われる | FR-009 相当 |
| 利用者の指定が実装の申告より優先される | [data-model.md](./data-model.md) 第 4 節 |
| 申告も指定も無い場合は既定値が使われる | 同上 |
| **`Mutex.Do` が `RunLocked` と同じ結果になる**（v0.4.0 の書き方がそのまま動く） | FR-007・SC-002 |
| `ZoneApplier` が既定でレコードを使う排他を使う | FR-006 |
| `WithLocker` で差し替えた排他が使われる | FR-001 |
| **差し替えた排他でも `Apply` の結果が変わらない**（反映が排他を解かない実装で、解放が正しく行われる） | FR-008・SC-005 |
| 差し替えた場合、`WithLockOptions` が無視される | [contracts/api.md](./contracts/api.md) 第 2 節 |
| 排他が `ErrStillLock` を返せば取得できなかった扱いになる | FR-003 |
| 排他が `ErrNotLockHolder` を返せば処理の context が打ち切られる | FR-003 |

**継ぎ目**: 差し替えの確認には `lockertest` の参照実装を使う。DPF-API の模擬サーバは
既定の実装の確認にのみ使う。

## 4. 実 API での確認 (必須)

本機能は既定の実装の挙動を変えない。**既存の統合テストがそのまま回帰の確認になる**
（FR-020）。

```bash
make test-integration
```

**期待**: 全件通過。とくに次が 008 の前と同じ結果であること。

| 確認すること | 対応 |
|---|---|
| `TestZoneMutex`（取得・競合・延長・解放） | FR-007 |
| `TestZoneApplierApply`（一括置き換え） | FR-007・FR-020 |
| `TestZoneMutexDoRenews`（保持期間より長い処理） | FR-007 |
| `TestLockFlow_AtomicChangesLosesLock` / `TestLockFlow_ZoneChanges` | FR-020 |

**トークン未設定でスキップされた結果を確認結果として報告してはならない。**

統合テストのコードは変更しない。変更が必要になった場合、それは既定の実装の挙動が変わった
ということであり、FR-007 に反する。

## 5. 文書の確認

| ファイル | 確認すること |
|---|---|
| `go doc ./utils Locker` | 契約の要点（再入の禁止、2 種類のエラー、延長の意味）が書かれている |
| `go doc ./utils RunLocked` | [contracts/api.md](./contracts/api.md) 第 1 節の約束 |
| `go doc ./utils NewZoneApplier` | `WithLocker` / `WithLockOptions` の使い分け |
| `go doc ./utils/lockertest` | 自作の実装を確かめる手順 |
| `utils/doc.go` | 2 つの方式の比較表があり、**外部の排他では別ユーザ・管理画面からの編集を止められない**ことが書かれている（FR-014・FR-016・SC-006） |
| `utils/example_test.go` | 差し替えの例がある |
| `README.md` | `dpf/utils` の説明に排他の差し替えが入っている |
| `CHANGELOG.md` | `[Unreleased]` に機能追加と破壊的変更（`NewZoneApplier` の可変長引数）がある |

## 6. 移行の確認

v0.4.0 の書き方が、どこまでそのまま動くかを確かめる。

```bash
go doc ./utils Mutex.Do      # 残っていること
```

| 書き方 | 本機能の後 |
|---|---|
| `mu := utils.NewMutex(...)` → `mu.Lock/Renew/Unlock` | 変わらない |
| `mu.Do(ctx, fn)` | 変わらない |
| `utils.NewZoneApplier(a, b, c, id)` | 変わらない |
| `utils.NewZoneApplier(a, b, c, id, utils.WithTTL(x))` | **`utils.WithLockOptions(utils.WithTTL(x))` へ機械的に置き換える** |
