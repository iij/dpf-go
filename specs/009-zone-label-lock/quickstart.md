# 検証手順: ゾーン単位の排他のラベルをゾーンへ移す

**機能**: [spec.md](./spec.md) | **計画**: [plan.md](./plan.md) | **日付**: 2026-09-29

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

生成物に手を入れていないことも確認する。`GetZoneLabels` / `PutZoneLabels` は既に生成済みで
あり、`openapi.json` の変更は不要である。

```bash
git diff --name-only | grep -E '^(api_|model_|client|configuration|response|utils|executeall_gen)\.go$' \
  && echo "NG: 生成物を編集している" || echo "OK"
```

## 2. 単体テスト

```bash
go test ./utils/... -race -count=1 -v
```

### 排他の状態の置き場

| 確認すること | 対応 |
|---|---|
| 排他の状態がゾーンのラベルへ書かれる。**レコードのラベルは触られない** | FR-001・SC-005 |
| 消費するゾーンのラベルが **1 個**である | FR-005b・SC-009 |
| 値の形式が `<保持者>-<奪ってよい時刻 10 桁>` である | [data-model.md](./data-model.md) 第 1 節 |
| **保持者に区切り文字（`-`）が含まれていても正しく読める**（右端から固定幅で読む） | [research.md](./research.md) D2 |
| 書き込みが**他のラベルを含めて置き換える**（利用者のラベルが消えない） | FR-006・[contracts/zone-label.md](./contracts/zone-label.md) 不変条件 2 |
| 取得・延長・解放のいずれもゾーン反映を行わない | FR-002・FR-003 |
| 解放でラベルが削除されず、奪ってよい時刻が現在時刻になる | FR-002b |
| 形式を満たさない値は「排他が成立していない」として取得できる | [contracts/zone-label.md](./contracts/zone-label.md) 第 5 節 |

### 保持者

| 確認すること | 対応 |
|---|---|
| 32 文字を超える保持者で `ErrOwnerTooLong` が返り、**書き込みが行われない** | FR-005d・SC-010 |
| 33 文字以上でも**切り詰められない** | FR-005d |
| 空文字を指定した場合は既定の保持者が使われ、エラーにならない | FR-005e |
| 既定の保持者のホスト名の枠が 15 文字に固定され、**先頭を残して右側が落ちる** | FR-005d・[research.md](./research.md) D3 |
| 既定の保持者で PID がゼロ詰めされない | [research.md](./research.md) D3 |
| 取得を待つ指定があっても、`ErrOwnerTooLong` と `ErrLabelLimit` では待たない | [contracts/api.md](./contracts/api.md) 第 3 節 |

### ラベルの枠

| 確認すること | 対応 |
|---|---|
| ゾーンのラベルが 9 個ある状態で取得できる（排他を足して 10 個） | SC-009 |
| ゾーンのラベルが 10 個埋まっている状態で `ErrLabelLimit` が返り、**書き込みが行われない** | FR-006 |
| **SOA レコードのラベルが 10 個埋まっていても取得できる** | SC-005 |

### 既存の約束の維持

| 確認すること | 対応 |
|---|---|
| `utils/lockertest` の契約テストが通る（10 項目） | FR-010 |
| 再入できない・取得は待たない・延長が喪失を報告する・他者の排他を解放しない | FR-011 |
| **レコードの一括更新とゾーン反映の後も排他が保持され、解放が成功する** | FR-008・SC-004 |
| ゾーン名の取得が 2 回目以降は行われない（呼び出し回数） | [research.md](./research.md) D5 |
| `consumesLockOnZoneApply` が既定の排他に対して **false** を返す | [research.md](./research.md) D11 |

**継ぎ目**: 既存の `utils/lock_test.go` の `lockServer`（`httptest`）を、ゾーンのラベルの
`GET` と `PUT` を扱えるように拡張する。**ネットワークは使わない。** `PUT` はマップ全体の
置き換えとして振る舞わせ、利用者のラベルが消える誤りをテストが検出できるようにする。

## 3. 文書の確認

| ファイル | 確認すること |
|---|---|
| `utils/lock.go` | `Mutex` の godoc が [contracts/api.md](./contracts/api.md) 第 6 節の 6 点を含む |
| `utils/lock.go` | `ErrLabelLimit` の godoc が「ゾーンのラベル」を指している（SOA のままになっていない） |
| `utils/doc.go` | 2 つの方式の比較表が実態に合っている。**「別ユーザ・管理画面からの編集を止められる」が既定の排他の欄から外れている** |
| `utils/doc.go` | 2 者の同時取得が一時レコード追加ロックで機械的に防がれることが書かれている |
| `utils/example_test.go` | `NewMutex` の例が `client.ZonesAPI` を渡す形になっている |
| `CHANGELOG.md` | 破壊的変更 2 件（`NewMutex` の引数、`ZonesApi` の拡張）と挙動の変更が記載されている |
| `README.md` | `dpf/utils` の説明が実態に合っている |
| `specs/007-zone-atomic-apply` | 前提「一括置き換えは排他を解く」が既定の排他に当てはまらなくなったことが追記されている |

## 4. 実 API での確認 (必須)

憲章「開発ワークフローと品質ゲート」により、ゾーン反映・ロック・非同期ジョブの待ち合わせに
触れる変更は `make test-integration` で確認する。本機能は排他そのものを作り替えるため必須で
ある。**トークン未設定でスキップされた結果を確認結果として報告してはならない。**

```bash
make test-integration
```

個別に実行する場合:

```bash
go test ./internal/integration/ -tags=integration -count=1 -parallel 1 -timeout 45m \
  -run 'TestLockFlow|TestZoneMutex|TestZoneApplier|TestLockRecord|TestZoneLabel' -v
```

**期待**:

| 確認すること | 対応 |
|---|---|
| 取得・延長・解放を通して、**ゾーンの未反映の編集が 0 件のままである** | SC-001 |
| 排他の操作の前後で、権威サーバに公開されているレコードが変わらない | SC-002 |
| 保持中に SOA レコードが編集予定にならない（ラベルも付かない） | FR-001 |
| **レコードの一括更新とゾーン反映の後も排他が保持され、解放が成功する** | FR-008・SC-004 |
| 同じ操作の後もゾーンのラベルが失われていない | [research.md](./research.md) D12 の (1) |
| 解放の後もゾーンのラベルが 1 つ残る（奪ってよい時刻が過去） | FR-002b |
| 保持期間より長い処理で保持が続く | FR-017 |
| 一時レコード追加ロックの専用レコードが権威サーバへ公開されない | SC-007 |
| **ゾーンが公開前の状態でもラベルを読み書きできる** | [research.md](./research.md) D12 の (2) |
| 非同期の更新の確定が読み戻しで観測でき、上限 10 秒で足りる | [research.md](./research.md) D4 |

### 公開前のゾーンについて

**公開状態は排他に影響しない。** 公開状態が決めるのは DNS への反映の有無であり、ゾーンの
ラベルは管理属性であって権威サーバのデータではない（利用者の判断、2026-09-29。
[research.md](./research.md) D12）。統合テストには公開前のゾーンがあれば確かめる形で
残してあるが、**無ければスキップしてよい。**

### 後始末

旧版が SOA レコードへ残したラベルは本機能では掃除しない（仕様の範囲の境界）。統合テストが
作った SOA のラベルは、既存の後始末の手順（ロックのラベルを取り除いて反映する）をそのまま
使う。本機能で新しく残るのは**ゾーンのラベル 1 つ**であり、これは設計どおり残るものなので
後始末の対象にしない。
