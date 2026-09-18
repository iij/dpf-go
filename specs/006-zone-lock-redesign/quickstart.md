# 検証手順: ゾーン単位の排他の見直し

**機能**: [spec.md](./spec.md) | **計画**: [plan.md](./plan.md) | **日付**: 2026-09-18

実装が仕様を満たしていることを確認する手順。実装の詳細ではなく、**何を実行して何を確認
するか**を定める。個々の作業の分解は `/speckit-tasks` の出力（tasks.md）で行う。

## 前提

```bash
cd $(git rev-parse --show-toplevel)
```

- Go 1.27 以上。`make check` の各ツールが導入済みであること（版は単一の出典で固定されて
  いる。未導入なら中断する。憲章「開発ワークフローと品質ゲート」）。
- 第 4 節（実 API）には `DPF_TOKEN_RO` / `DPF_TOKEN_RW` / `DPF_TEST_SERVICE_CODE` が必要。
  詳細は [internal/integration/README.md](../../internal/integration/README.md)。

## 1. 品質ゲート

```bash
make check
```

マージ前の 7 ゲート（ビルド / 単体テスト / 整形・静的解析 / ファイル冒頭の規約 /
依存ライセンス / 脆弱性 / シークレット検査）をまとめて実行する。個別に確認する場合:

```bash
make build-all
make test              # -race -count=1、ネットワーク不要
make check-lint        # --build-tags=integration を含む
make check-headers
make check-licenses    # 依存は増えないため母集団は変わらない
make check-vuln
make betterleaks
```

**期待**: すべて成功。`go.mod` に差分が無いこと（依存を追加しない）。

## 2. 契約の検証 (単体テスト)

`go test ./utils/ -run TestMutex -v` で確認する。[contracts/mutex.md](./contracts/mutex.md)
の各約束に対応するテストが存在すること。

| 確認すること | 対応 |
|---|---|
| 排他が無い状態で取得できる | FR-001 |
| **他者が保持中のとき、専用レコードを作らずに `ErrStillLock` を返す**（追加の API を呼ばない） | FR-002・SC-008 |
| 事前判定では取得できたが、一時ロック取得後の判定では取得できない場合、書き込まずに解放して `ErrStillLock` | FR-003 |
| ラベルが上限を超える場合、専用レコードを作らずに `ErrLabelLimit` | FR-002・FR-014 |
| 他者が保持中（奪ってよい時刻を過ぎていない）は `ErrStillLock` | FR-018 |
| 奪ってよい時刻を過ぎていれば取得できる | FR-018 |
| **自分自身が保持中でも `ErrStillLock`（再入禁止）** | FR-017 |
| 奪ってよい時刻が数値でない場合は取得できる | FR-019 |
| SOA のラベルが上限に達している場合は `ErrLabelLimit`。**書き込みを試みない** | FR-014 |
| SOA の既存ラベルが保持される | FR-013 |
| 取得の前後で専用レコードが残らない | FR-009 |
| 専用レコードが追加予定で失効前なら `ErrStillLock` | FR-007 |
| 専用レコードが追加予定で失効後なら、取り消して取得できる | FR-006 |
| 専用レコードが反映済みなら、削除予定にして取得できる。**削除予定は残る** | FR-008 |
| 専用レコードの追加予定が 2 件のとき、ID 昇順で勝者が決まる | [research.md](./research.md) D7 |
| 一時ロックの解放に失敗した場合は取得を失敗として返す | FR-009 |
| 書き込んだ値が読み出せない場合は `ErrStillLock` | FR-020 |
| 延長で奪ってよい時刻が進む | FR-022 |
| 保持者でない状態の延長・解放は `ErrNotLockHolder` | FR-023・FR-024 |
| 排他が無い状態の解放は何もせず成功 | FR-025 |
| 他者が保持する排他を解放しようとしても変更されない | FR-024 |
| 既定 owner がインスタンスごとに異なり、63 文字以内で、ホスト名と PID を含む | FR-015・FR-016 |
| 取得・延長・解放でゾーン反映の API を呼ばない | FR-024 |
| SOA の行が state=3 と state=0 の両方あるとき state=3 を使う | [research.md](./research.md) D10 |

**継ぎ目**: 既存の `utils/lock_test.go` の `lockServer`（`httptest`）を、`POST /records`、
`DELETE /records/{id}`、`DELETE /records/{id}/changes` を扱えるように拡張する。重複拒否は
`duplicated` / `record` を含む 400 を返して模擬する。**ネットワークは使わない。**

## 3. 破壊的変更の確認

```bash
go doc ./utils Mutex
go doc ./utils Mutex.Renew
go doc . RecordsApi
```

**期待**:

- `Mutex` の godoc に [contracts/mutex.md](./contracts/mutex.md) 第 5 節の 6 点が書かれて
  いる（排他の強さの違い、再入禁止と `Renew`、`WithOwner` の一意性、ゾーン反映なし、
  専用レコードの公開とその回復、ラベル 2 つの消費）。
- `dpf.RecordsApi` に `PostRecord` / `DeleteRecord` / `DeleteRecordChanges` がある。
- `CHANGELOG.md` の `[Unreleased]` に破壊的変更 5 件が記載されている
  （[plan.md](./plan.md)「破壊的変更の一覧」）。

## 4. 実 API での確認 (必須)

憲章「開発ワークフローと品質ゲート」により、ロックの変更は `make test-integration` で
確認する。**トークン未設定でスキップされた結果を確認結果として報告してはならない。**

```bash
make test-integration
```

個別に実行する場合:

```bash
go test ./internal/integration/ -tags=integration -count=1 -parallel 1 -timeout 45m \
  -run 'TestZoneMutex|TestLockRecordPremises' -v
```

**期待**:

| 確認すること | 対応 |
|---|---|
| `TestLockRecordPremises` が通る（前提が変わっていない） | FR-028 の (1)(2) |
| `TestZoneMutex` が通る。**同一 owner での再取得が `ErrStillLock` になる**（期待値の反転） | FR-017 |
| `TestZoneMutex` で `Renew` が成功し、奪ってよい時刻が進む | FR-022 |
| 取得後の SOA が編集予定になり、ロックのラベルが乗っている | FR-012 |
| 取得の前後で専用レコードが 1 件も残らない | SC-005 |
| 取得・延長・解放の後も未反映件数が SOA の 1 件だけである（専用レコードの分が残らない） | FR-009・FR-024 |
| ログに出る SOA の行が 1 件であること（state=5 の行が返らないこと） | [research.md](./research.md) D10 |

最後の項目は既存の `mutex_test.go` が出力する「ロック前の SOA」「ロック後の SOA」のログで
確認する。**2026-09-18 の実測では 1 行のみであった**（ロック前 state=0、ロック後 state=3、
いずれも同じ ID）。2 件以上返るようになった場合は、D10 の判定（state=3 を優先）が実際に
効いていることを確認する。

### 同一ユーザでの同時取得の確認 (SC-001)

同一のトークンで 2 つのプログラムから同時に取得を試み、成立するのが 1 つだけであることを
確認する。統合テストの中で 2 つの `Mutex` を作って**逐次**取得すれば FR-017 の確認になるが、
SC-001 が問うのは同時性である。`internal/integration` は API 呼び出しを直列化する制約
（`-parallel 1`、同時実行数 1）を持つため、同時取得の確認はこの制約と両立しない。

**判断**: 同時性の確認は `tasks.md` で手動手順として扱う。2 つのプロセスを同じトークンで
同時に起動し、一方だけが取得することを確認する。自動化する場合は直列化の制約を回す必要が
あるため、統合テスト本体には含めない。

## 5. 文書の確認 (FR-029〜FR-031)

| ファイル | 確認すること |
|---|---|
| `utils/doc.go` | 16 行・84-87 行の排他の説明が、競合相手ごとの強さの違いを含む形に書き換わっている |
| `internal/integration/README.md` | 中断時の自己回復の説明が、owner 固定による短絡ではなく TTL と一時ロックの失効に基づく形になっている |
| `CHANGELOG.md` | `[Unreleased]` に破壊的変更と修正が記載されている |
| `specs/003-utils-highlevel-api/spec.md` | 本仕様が置き換えた旨が追記され、双方から辿れる |
| `README.md` | 88 行の記述は変更されていない（本機能で変わらない） |
