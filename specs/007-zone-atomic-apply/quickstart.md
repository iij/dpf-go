# 検証手順: 排他を保持したままの実行と、ゾーン全体の一括置き換え

**機能**: [spec.md](./spec.md) | **計画**: [plan.md](./plan.md) | **日付**: 2026-09-18

実装が仕様を満たしていることを確認する手順。個々の作業の分解は `/speckit-tasks` の出力
（tasks.md）で行う。

## 前提

```bash
cd $(git rev-parse --show-toplevel)
```

- Go 1.27 以上。`make check` の各ツールが導入済みであること（未導入なら中断する。
  憲章「開発ワークフローと品質ゲート」）。
- 第 4 節（実 API）には `DPF_TOKEN_RO` / `DPF_TOKEN_RW` / `DPF_TEST_SERVICE_CODE` が必要。
  詳細は [internal/integration/README.md](../../internal/integration/README.md)。

## 1. 品質ゲート

```bash
make check
```

マージ前の 7 ゲートをまとめて実行する。本機能は goroutine を使うため、`make test` が
`-race` で走ることが特に重要である。

**期待**: すべて成功。`go.mod` に差分が無いこと（依存を追加しない）。

## 2. 契約の検証 (単体テスト)

```bash
go test ./utils/ -race -run 'TestMutexDo|TestZoneApplier' -v
```

[contracts/api.md](./contracts/api.md) の各約束に対応するテストが存在すること。

### 排他を保持したままの実行

| 確認すること | 対応 |
|---|---|
| 処理が排他の下で実行され、終了後に解放される | FR-001・FR-007 |
| 処理へ渡される context が、呼び出し側の context から派生している | FR-006 |
| 保持期間より長い処理でも、延長によって保持が続く | FR-002・SC-001 |
| 延長の間隔の既定が保持期間の 1/3 である | FR-003 |
| **排他を奪われたとき、処理の context が打ち切られる** | FR-004・SC-002 |
| 排他を奪われたとき、`ErrNotLockHolder` が返る。処理のエラーも併せて判別できる | FR-004 |
| 延長が一時的に失敗しても処理は継続し、次の周期で再試行される | FR-005 |
| 処理が返したエラーが、種類を判別できる形のまま返る | FR-008 |
| 排他を取得できない場合、処理が実行されない | FR-010・SC-008 |
| 待機を指定しない場合はただちに `ErrStillLock` が返る | FR-011 |
| 待機を指定した場合は取得できるまで繰り返す | FR-011 |
| 待機が呼び出し側の打ち切りに従う | FR-012 |
| **`Do` の復帰後に goroutine が残らない** | [plan.md](./plan.md) 方針 5 |

最後の項目は `-race` と、テスト終了時の goroutine 数の比較で確認する。

### ゾーン全体の一括置き換え

| 確認すること | 対応 |
|---|---|
| 公開されているレコードが、リクエストの型へ変換されて編集関数へ渡される | FR-014 |
| **受け取った一覧をそのまま返した場合、ラベル・TTL・コメントが変わらない** | FR-014b・SC-009 |
| 編集の結果で一括置き換えが行われ、反映の完了を待って復帰する | FR-013・FR-019 |
| **取り込みの可否を決める 2 つのフラグが、常に「取り込まない」で送られる** | FR-015・FR-016・SC-003 |
| 編集の結果に SOA / Zone Apex NS が無い場合に補われる | [research.md](./research.md) D9 |
| 編集の結果が空の場合、API を呼ばずに `ErrNoRecords` が返る | FR-018 |
| **成功した場合、無条件の解放が行われない**（`ErrNotLockHolder` が返らない） | FR-017・SC-004 |
| 反映の後に排他が残っていた場合に限り解放される | FR-021 |
| 反映より前の失敗で排他が解放される | SC-005 |
| 反映のコメントが Option で指定できる | FR-020 |
| 排他が公開 API に現れない（`ZoneApplier` から取り出せない） | FR-017 |

**継ぎ目**: 既存の `utils/lock_test.go` の `lockServer`（`httptest`）を、`GetRecordCurrents`、
`PatchZoneAtomicChanges`、JOB の状態取得を扱えるように拡張する。**ネットワークは使わない。**

模擬サーバは `PatchZoneAtomicChanges` のリクエストボディを記録し、2 つのフラグが
`false` で送られていることをテストが確認できるようにする。また、一括置き換えを受けたら
**SOA を作り直す**（ID を変え、ラベルを落とす）ことで実 API の挙動を再現する
（[research.md](./research.md) D1）。

## 3. 公開 API の確認

```bash
go doc ./utils Mutex.Do
go doc ./utils ZoneApplier
go doc ./utils ZoneApplier.Apply
go doc . ZonesApi
go doc . JobsApi
```

**期待**:

- `Mutex.Do` のコールバックの引数が context 1 つだけであること（FR-017）。
- `Mutex.Do` の godoc に [contracts/api.md](./contracts/api.md) 第 5 節の 6 点が
  書かれていること。とくに**延長が失敗し続ける間は保護が切れた状態で処理が走りうること**。
- `ZoneApplier` から内部の排他を取り出す手段が公開されていないこと。
- `dpf.ZonesApi` に `PatchZoneAtomicChanges` があること。`dpf.JobsApi` があること。
- `CHANGELOG.md` の `[Unreleased]` に機能追加と破壊的変更が記載されていること。

## 4. 実 API での確認 (必須)

憲章「開発ワークフローと品質ゲート」により、ゾーン反映・ロック・非同期ジョブの待ち合わせに
触れる変更は `make test-integration` で確認する。本機能は 3 つすべてに触れる。
**トークン未設定でスキップされた結果を確認結果として報告してはならない。**

```bash
make test-integration
```

個別に実行する場合:

```bash
go test ./internal/integration/ -tags=integration -count=1 -parallel 1 -timeout 45m \
  -run 'TestLockFlow|TestZoneMutex' -v
```

**期待**:

| 確認すること | 対応 |
|---|---|
| `TestLockFlow_AtomicChanges` が通る（期待値を「排他が失われる」へ反転済み） | [research.md](./research.md) D1 の固定 |
| `TestLockFlow_ZoneChanges` が通る（1 レコードずつの流れでは排他が保たれる） | 同 |
| `ZoneApplier` でゾーンを置き換えられる | FR-013 |
| 置き換えの後、SOA と Zone Apex の NS が変わっていない | SC-003 |
| **SOA に利用者のラベルを付けた状態で実行し、そのラベルが残っている** | SC-009・[research.md](./research.md) D1b |
| SOA 以外のレコードのラベルも残っている | SC-009 |
| 置き換えの後、排他も未反映の編集も残らない | SC-005・SC-007 |
| `Apply` が成功した場合、`ErrNotLockHolder` が返らない | SC-004 |
| `Mutex.Do` の中で 1 レコードずつ変更して反映し、解放まで通る | 契約 1 の約束 5 |

### 保持期間より長い処理の確認 (SC-001)

実 API で延長が効いていることを確認するには、保持期間より長い処理を走らせる必要がある。
保持期間の下限は DPF-API の制約ではなく本ライブラリの設定であるため、統合テストでは
**保持期間を短く設定する**（たとえば 1 分、延長の間隔は 20 秒）ことで、待ち時間を抑えて
確認する。

**判断**: 既定の 15 分で確認すると 1 回のテストに 15 分以上かかり、`make test-integration`
全体の上限（`Makefile` の `-timeout 45m`）を圧迫する。保持期間を短くしても、確認したい
「延長によって保持が続く」という性質は変わらない。

## 5. 文書の確認

| ファイル | 確認すること |
|---|---|
| `utils/lock.go` | `Mutex` の godoc が、一括置き換えについて `ZoneApplier` を案内する形になっている（「併用できない」で終わっていない） |
| `utils/apply.go` | `ZoneApplier` の godoc が契約 第 5 節の 5 点を含む |
| `utils/doc.go` | 2 つの操作の説明がある |
| `utils/example_test.go` | `overwrite_soa=false` なら安全という前提で書いた例が残っていない。`ZoneApplier` と `Mutex.Do` の例がある |
| `README.md` | 88 行の `dpf/utils` の説明に 2 つの操作が入っている |
| `CHANGELOG.md` | `[Unreleased]` に機能追加と破壊的変更がある |
