# Contract: 検査ゲートのインターフェース

**Date**: 2026-09-09
**Spec**: [../spec.md](../spec.md) / **Data model**: [../data-model.md](../data-model.md)

本機能が外部（開発者・CI）へ公開するインターフェースは、`make` ターゲットと
2 つのコマンドである。ここではその契約を定める。ライブラリの公開 API は変更しない。

## 共通の契約

すべての検査ターゲットに共通して次を満たす。

| 項目 | 契約 |
|---|---|
| 引数 | 取らない。対象はリポジトリ全体（すべてのモジュール）に固定される |
| 終了コード 0 | 違反なし |
| 終了コード 1 | 違反あり。違反の一覧を標準出力へ出す |
| 終了コード 2 | 検査を実行できなかった（ツール未導入、版の不一致、内部エラー）。**違反なしとして扱ってはならない** |
| 出力 | 違反 1 件につき 1 行。機械的に読める形（`パス または 依存名`、`区分`、`詳細`） |
| 冪等性 | 作業ツリーを書き換えない。検査のみを行う（FR-028）。検査の前後で `git status` の差分が変わらない（SC-011） |
| 実行場所 | リポジトリルート。手元と CI で同じ結果になる（FR-015、FR-024） |

**終了コード 2 を 1 と分ける理由**: 「違反があった」と「検査できなかった」は
対応が異なる。前者はコードを直す。後者は環境を直す。同じコードにすると、
環境の不備を違反として扱ってしまい、原因の切り分けが遅れる。

## `make check-headers`

ファイル冒頭の規約を検査する（FR-001〜FR-006、FR-018）。

- 実装: `go run ./tools/checkheaders`（標準ライブラリのみ。外部ツール不要）
- 対象: リポジトリ内のすべての `.go` ファイル。`vendor/` と `testdata/` を除く
- 違反の種別:
  - `missing-spdx` — 先頭行が `// SPDX-License-Identifier: Apache-2.0` でない
  - `preamble-pragma` — `package` 宣言より前に `nolint` 指示がある
- 出力例:

  ```text
  internal/integration/main_test.go	missing-spdx	先頭行に SPDX 識別子がない
  ```

- 外部ツールに依存しないため、終了コード 2 は内部エラー時のみ

## `make check-licenses`

依存ライセンスを許容リストと照合する（FR-007〜FR-013）。

- 実装: モジュールごとに `go-licenses` を実行し、その出力を
  `go run ./tools/checklicenses` が許容リストと突き合わせる
- 対象: 各モジュールが実際に import しているパッケージ（D2）
- 違反の種別: `undetermined` / `disallowed` / `root-reciprocal`
  （[data-model.md](../data-model.md) の Verdict に対応）
- 報告: 違反が無い場合も、`reciprocal` の依存の一覧を出す（FR-011）。
  この一覧は終了コードに影響しない
- 出力例:

  ```text
  misc/vault	github.com/hashicorp/vault/api	reciprocal	MPL-2.0	(義務あり。終了コードに影響しない)
  .	example.com/foo	disallowed	GPL-3.0	許容リストに無い
  ```

- `go-licenses` が未導入、または版が固定値と一致しない場合は終了コード 2

### `checklicenses` の入力契約

`tools/checklicenses` が読む入力の形式を固定する。上流の出力形式が変わったときに
静かに誤判定しないよう、契約として明示する。

| 項目 | 契約 |
|---|---|
| 形式 | `go-licenses csv` の出力。1 行 1 パッケージ、カンマ区切りの 3 列 |
| 列 | `パッケージパス`, `ライセンス本文の URL`, `SPDX 識別子` |
| 判別不能 | 3 列目が `Unknown`。空文字ではない |
| モジュール帰属 | 入力自体には含まれない。**呼び出し側がモジュールごとに実行し、どのモジュールの出力かを引数で与える**（`data-model.md` の Dependency.OwnerModule） |
| 列数の異なる行 | 終了コード 2 で中断する。無視してはならない。上流の形式変更を「違反なし」に見せないため |

例:

```text
github.com/hashicorp/vault/api,https://github.com/hashicorp/vault/blob/api/v1.23.0/api/LICENSE,MPL-2.0
github.com/cespare/xxhash/v2,https://github.com/cespare/xxhash/blob/v2.3.0/LICENSE.txt,MIT
```

## `make check-lint`

整形と静的解析（FR-014〜FR-019）。

- 実装: `golangci-lint run` をモジュールごとに実行する。設定は
  リポジトリルートの `.golangci.yml`（`version: "2"`）
- 既存の `make lint` との関係: `make lint` を置き換える。`--build-tags=integration`
  は維持する（FR-016）
- 整形の検査は `formatters` 節の `gofmt` が担う。作業ツリーを書き換えない
  （書き換えるのは既存の `make fmt` のみ）
- 抑制の検査は `nolintlint` が担う（`require-explanation` / `require-specific` /
  `allow-unused: false`）
- `golangci-lint` が未導入、または版が固定値と一致しない場合は終了コード 2

## `make check-vuln`

到達可能な既知脆弱性の検査（FR-020、FR-023）。

- 実装: `govulncheck ./...` をモジュールごとに実行する
- 終了コード 1 は「到達可能な報告あり」。到達しない脆弱性では 1 にならない
- **統合前の必須ゲートである。** 到達可能な報告がある状態でマージできない（FR-021）。
  リリースは統合済みの状態から行うため、リリース専用のゲートは設けない
- 迂回・一時的な無効化・報告のみへの格下げの経路を設けてはならない（FR-022）。
  止まった場合の対処は依存の更新、または脆弱な経路へ到達しない形への修正である
- `govulncheck` が未導入、または版が固定値と一致しない場合は終了コード 2

## `make check`

**変更を統合する前に満たすべきすべてのゲートをまとめて実行する**（FR-024、FR-030）。
上記 4 つだけではない。

| 呼ぶもの | 由来 |
|---|---|
| `make build-all` | 既存 |
| `make test` | 既存 |
| `make check-lint` | 本機能 |
| `make check-headers` | 本機能 |
| `make check-licenses` | 本機能 |
| `make check-vuln` | 本機能 |
| `make betterleaks` | 既存 |

- 既存の 3 つの検査自体は変更しない。入口から呼ぶだけである
- 1 つが失敗しても残りを実行し、最後にまとめて報告する。最初の失敗で打ち切らない
  （1 つ直すたびに全部を実行し直す手戻りを避けるため）
- 終了コードは、いずれかが 2 なら 2、そうでなくいずれかが 1 なら 1、すべて 0 なら 0
- これがコントリビューターが提出前に実行する単一の入口である

## CI の契約

| ワークフロー | トリガ | 呼ぶもの | 失敗時 |
|---|---|---|---|
| 新規チェック | `pull_request`, `push` | `make check-headers`, `make check-licenses`, `make check-lint` | 統合を止める |
| 脆弱性 | `pull_request`, `push` | `make check-vuln` | **統合を止める**（FR-021） |

リリース専用のゲートは設けない。`main` がマージ前ゲートをすべて満たしているため、
リリース時点で改めて満たすべき品質ゲートは無い（憲章「リリースは `main` から行う」）。

**CI は版を自前で持たない。** `make` ターゲットを呼ぶことで、版の定義を
Makefile の 1 か所に保つ（D6、FR-015、FR-029）。

**版の検証はツールごとに手段が異なる。** 単一のヘルパーで 3 ツールを扱えない
（research.md D9）。

| ツール | 版の取得 |
|---|---|
| `golangci-lint` | `golangci-lint --version` |
| `govulncheck` | `govulncheck --version`（`Scanner: govulncheck@<版>` の行） |
| `go-licenses` | `go version -m $(command -v go-licenses)` の `mod` 行。自身の版を報告する手段を持たない |

版の抽出に失敗した場合も終了コード 2 で中断する。「版が取れなかった」を
「一致した」として扱ってはならない。

**`continue-on-error` を使わない。** 失敗を成功に見せてしまうため（`sbom.yml` と同じ方針）。
検査が失敗したなら、その job も失敗させる。4 種すべてが統合を止めるゲートになったため、
「報告のみ」の経路は存在しない。

**検査を省略した場合は理由を残す。** ログと実行サマリに省略の理由を書く（FR-026）。
省略を「違反なし」に見せてはならない。
