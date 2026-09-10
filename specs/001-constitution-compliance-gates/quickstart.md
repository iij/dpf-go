# Quickstart: 検査ゲートの動作確認

**Date**: 2026-09-09
**Spec**: [spec.md](./spec.md) / **Contracts**: [contracts/gates.md](./contracts/gates.md)

実装後、仕様の受け入れシナリオが満たされていることを手元で確認する手順。
すべてリポジトリルートで実行する。

## 前提

```bash
go version                                        # go1.27 以上
golangci-lint --version                           # 2.13.2 と一致すること
govulncheck --version                             # Scanner: govulncheck@v1.7.0
go version -m "$(command -v go-licenses)" | grep go-licenses   # v2.0.1
go version -m "$(command -v betterleaks)" | grep betterleaks   # v1.8.1
```

`go-licenses` と `betterleaks` が取得方法が違うのは、自身の版を報告する手段が
無い（前者）か `dev` を返す（後者）ためである（[research.md](./research.md) D9）。

版が固定値と違う場合、検査は省略されず中断する。これは想定どおりの挙動である
（FR-025）。

導入方法は失敗時のメッセージが示す。

**終了コードについて**: `make` はレシピが失敗すると必ず 2 で終了するため、
`make` の終了コードで「違反あり」と「検査できなかった」を区別できない
（[research.md](./research.md) D10）。区別は `make check` の `RESULT` 行、
または `go run ./tools/checkheaders` のようにツールを直接呼んで確認する。

## 1. 一括実行

```bash
make check
```

期待: 終了コード 0 と `RESULT: ok`。統合前に満たすべき 7 ゲートすべてが通る
（FR-030。本機能の 4 種と、既存のビルド・単体テスト・シークレット検査）。
`reciprocal` の依存（HashiCorp 系 10 件）は報告として出るが、失敗にはならない。
実測の所要時間は約 25 秒（Go のビルドキャッシュが温まっている状態）。

内訳を個別に確かめる場合は手順 2〜5 を、一括実行が本当に 7 ゲートを呼んでいるかは
手順 7 を参照する。

## 2. ファイル冒頭の規約 (US1)

```bash
make check-headers
```

期待: 終了コード 0、違反 0 件。

**違反を作って確認する**（US1 シナリオ 1、4）:

```bash
printf 'package foo\n' > /tmp/nospdx.go && cp /tmp/nospdx.go ./nospdx_test.go
make check-headers; echo "exit=$?"     # exit=1、nospdx_test.go が missing-spdx で列挙される
rm ./nospdx_test.go
```

**広域抑制を作って確認する**（US1 シナリオ 6 / FR-018）:

`package` 宣言より前に `//nolint:errcheck // 理由` を置いたファイルを一時的に作り、
`preamble-pragma` として検出されることを確認する。理由と linter 名を伴っていても
検出される点が `nolintlint` との違いである。この検査は US1 が担う（FR-018 は
「ファイル冒頭の規約」グループに属する）。

**再生成後も維持されること**（US1 シナリオ 5）:

```bash
make generate && make check-headers   # Docker が必要。期待: 終了コード 0
```

## 3. 依存ライセンス (US2)

```bash
make check-licenses
```

期待: 終了コード 0。`reciprocal` の一覧に HashiCorp 系 10 件（すべて `misc/vault`）が出る。
本体モジュール（`.`）の依存に `reciprocal` は現れない。

**判定の内訳**（実装前の実測値。[research.md](./research.md) D1）:

| モジュール | パッケージ数 | 内訳 |
|---|---|---|
| `.` | 17 | BSD-3-Clause 8 / Apache-2.0 8 / MIT 1 |
| `misc/vault` | 19 | MPL-2.0 10 / BSD-3-Clause 4 / MIT 3 / Apache-2.0 2 |
| `misc/aws` | 8 | Apache-2.0 5 / BSD-3-Clause 2 / Apache-2.0 1 |
| `misc/azure` | 7 | MIT 4 / BSD-3-Clause 2 / Apache-2.0 1 |
| `misc/gcp` | 24 | Apache-2.0 12 / BSD-3-Clause 10 / MIT 1 / Apache-2.0 1 |

**許容外を作って確認する**（US2 シナリオ 2、4）:

`licenses-allowlist.txt` から `MPL-2.0` の行を一時的に外して `make check-licenses` を
実行すると、`misc/vault` の 10 件が `disallowed` として列挙され終了コード 1 になる。
実依存を汚さずに、許容外の検出とモジュール帰属の両方を確認できる。

**本体への reciprocal 混入を確認する**（US2 シナリオ 6 / FR-012）:

`licenses-allowlist.txt` で `Apache-2.0` を一時的に `reciprocal` に変えて実行すると、
本体モジュールの Apache-2.0 依存が `root-reciprocal` として失敗する。FR-012 の
分岐が実際に効いていることを確認できる。

## 4. 整形と静的解析 (US3)

```bash
make check-lint
```

期待: 終了コード 0。`--build-tags=integration` により `internal/integration/` も
検査対象に入る（FR-016）。

**手元と CI の一致を確認する**（US3 シナリオ 1 / FR-015）:

同一コミットに対する CI の出力と手元の出力を比べ、指摘の集合が一致することを見る。
CI は版を自前で持たず `make` を呼ぶため、乖離が起きるのは版の固定値を変えたときだけである。

**対象 linter を明示しない抑制を確認する**（US3 シナリオ 3 / FR-017）:

`//nolint`（linter 名なし）を付けて実行すると、`nolintlint` の `require-specific` が
報告して終了コード 1 になる。

**理由のない抑制を確認する**（US3 シナリオ 2 / FR-017）:

任意の指摘に `//nolint:errcheck`（理由なし）を付けて実行すると、`nolintlint` が
不足を報告して終了コード 1 になる。

**整形崩れを確認する**（US3 シナリオ 5 / FR-019, FR-028）:

任意の `.go` ファイルのインデントを崩して実行すると、`gofmt`（formatter）が
報告して終了コード 1 になる。`make check-lint` はファイルを書き換えない。

## 5. 脆弱性 (US4)

```bash
make check-vuln
```

期待: 終了コード 0（到達可能な報告なし。US4 シナリオ 1）。到達しない既知脆弱性では
1 にならない（US4 シナリオ 3）。

**統合が止まることを確認する**（US4 シナリオ 2 / FR-021）:

到達可能な既知脆弱性がある状態では、統合前の CI で失敗し、変更は統合できない。
脆弱性の識別子と到達経路が示される。`continue-on-error` は使っていないため、
「失敗したのに成功に見える」状態にはならない。

**迂回の経路が無いことを確認する**（US4 シナリオ 4 / FR-022）:

`Makefile` と CI ワークフローを読み、脆弱性の検査を飛ばす・無効化する・報告のみへ
格下げするオプションや環境変数が**存在しないこと**を確認する。新しい脆弱性の公表で
脆弱性を含まない変更が止まった場合、通す手段は依存の更新、または脆弱な経路へ
到達しない形への修正だけである。

リリース専用のゲートは無い。`main` がマージ前ゲートをすべて満たしているため、
リリース時点で改めて確認することは無い。

## 6. 共通の性質 (US1 シナリオ 7〜10)

**ツール未導入時に中断すること**（US1 シナリオ 7 / FR-025）:

```bash
PATH=/usr/bin:/bin make check-licenses; echo "exit=$?"   # exit=2。0 にならないこと
```

版が固定値と違う場合も同じく `exit=2` になること。版の抽出に失敗した場合も
`exit=2` であり、「一致した」として通してはならない（research.md D9）。

**モジュール一覧が導出されること**（US1 シナリオ 9 / FR-027）:

```bash
mkdir -p /tmp/m && cd /tmp/m   # 一時モジュールをリポジトリ内に作って確認する場合は
                               # 作業ツリーを汚さないよう後始末すること
```

リポジトリ内に `go.mod` を持つディレクトリを 1 つ増やし、Makefile を書き換えずに
`make check` の対象に入ることを確認する。確認後は削除する。

**手元で CI と同じ結果が再現されること**（US1 シナリオ 10 / FR-024）:

`make check` の 1 コマンドで、統合前に満たすべき**すべてのゲート**が実行される
（FR-030。本機能の 4 種に加え、既存のビルド・単体テスト・シークレット検査）。
CI も同じターゲットを呼ぶ。
段階的に実装している間は、当該 story の検査コマンドが単一の入口となる（spec の Assumptions）。

**CI が省略した理由が残ること**（US1 シナリオ 8 / FR-026）:

手元では確認できない。統合前の検査が何らかの事情で検査を省略した実行を開き、
ログと実行サマリに省略の理由が残っていることを確認する。`continue-on-error` を
使っていないため、「失敗したのに成功に見える」状態にはならない。

## 7. 一括実行がすべてのゲートを呼ぶこと (FR-030)

```bash
make -n check
```

期待: 出力に 7 つのゲート（`build-all`, `test`, `check-lint`, `check-headers`,
`check-licenses`, `check-vuln`, `betterleaks`）がすべて現れる。本機能の 4 種だけでは
足りない。1 つが失敗しても残りが実行され、最後に `RESULT` 行でまとめて報告される
ことも確認する。

シークレット検査は `SECRET_SCAN_TARGET` 変数から呼ばれるため、憲章が定める
ツール名が変わってもこの一覧は追随する。

## 8. 作業ツリーが変わらないこと (FR-028 / SC-011)

```bash
git status --porcelain > /tmp/before.txt
make check
git status --porcelain > /tmp/after.txt
diff /tmp/before.txt /tmp/after.txt && echo "作業ツリーに変化なし"
```

期待: 差分 0 件。検査は報告のみを行い、書き換えは `make fmt` に限られる。

## 対応表

| 確認手順 | 仕様の受け入れシナリオ |
|---|---|
| 2 | US1 シナリオ 1〜6 |
| 3 | US2 シナリオ 1〜6 |
| 4 | US3 シナリオ 1〜5 |
| 5 | US4 シナリオ 1〜4 |
| 6 | US1 シナリオ 7〜10 |
| 7 | FR-030（受け入れシナリオではなく要件の確認） |
| 8 | SC-011（受け入れシナリオではなく成功基準の確認） |
