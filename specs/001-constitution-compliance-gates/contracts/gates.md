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
| 成功 | 違反なし。終了コード 0 |
| 失敗 | 違反あり、または検査を実行できなかった。終了コードは非 0 |
| 出力 | 違反 1 件につき 1 行。機械的に読める形（`パス または 依存名`、`区分`、`詳細`） |
| 冪等性 | 作業ツリーを書き換えない。検査のみを行う（FR-028）。検査の前後で `git status` の差分が変わらない（SC-011） |
| 実行場所 | リポジトリルート。手元と CI で同じ結果になる（FR-015、FR-024） |

### 「違反あり」と「検査できなかった」の区別

「違反があった」と「検査できなかった」は対応が異なる。前者はコードや依存を直す。
後者は環境を直す。同じものとして扱うと、環境の不備を違反として扱ってしまい、
原因の切り分けが遅れる。憲章も「検査できなかったことを違反なしとして扱わない」を
MUST NOT としている。

**ただし、この区別を `make` の終了コードで表すことはできない。** GNU make は
レシピが失敗すると必ず 2 で終了するため、`make check-headers` が違反 1 件で
失敗しても make 自身の終了コードは 2 になる（[research.md](../research.md) D10）。
区別は次の 3 か所で表現する。

| 表現の場所 | 区別の仕方 |
|---|---|
| **ツール単体の終了コード** | `go run ./tools/checkheaders` / `go run ./tools/checklicenses` は 0（違反なし）/ 1（違反あり）/ 2（検査不能）を返す。区別を要するスクリプトはこちらを呼ぶ |
| **`make check` の実行順序** | 4 つのツールの導入と版をゲート実行前にまとめて検証する。ここで止まれば「検査できなかった」、通った後の失敗は「違反」と決まる |
| **`make check` の `RESULT` 行** | `RESULT: ok` または `RESULT: violated` と、失敗したゲート名を出す |

各ゲートは、検査できなかった場合に必ず失敗しなければならない（MUST）。
ツール未導入、版の固定値との不一致、**版の取得に失敗した場合**のいずれも
成功扱いにしてはならない（MUST NOT）。「版が取れなかった」を「一致した」として
扱わない。

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

- 外部ツールに依存しないため、検査不能になるのは走査や読み込みの失敗時のみ（ツール単体の終了コード 2）

## `make check-licenses`

依存ライセンスを許容リストと照合する（FR-007〜FR-013）。

- 実装: モジュールごとに `go-licenses` を実行し、その出力を
  `go run ./tools/checklicenses` が許容リストと突き合わせる
- 対象: 各モジュールが実際に import しているパッケージ（D2）
- 違反の種別: `undetermined` / `disallowed` / `root-reciprocal`
  （[data-model.md](../data-model.md) の Verdict に対応）
- ライセンスは単一の識別子だけでなく SPDX の複合式を取りうる。`OR` は収まる
  選択肢が一つでもあれば許容し、**選んだ側を報告に残す**（入力の式ではない）。
  `AND` はすべてが収まることを求め、義務は最も重いものを採る。解釈しない形
  （括弧、`AND` と `OR` の混在、`WITH`、`LicenseRef-`、`+`、末尾に演算子が
  残る不完全な式）は `undetermined` として失敗する
- 報告: 違反が無い場合も、`reciprocal` の依存の一覧を出す（FR-011）。
  この一覧は終了コードに影響しない
- 出力例:

  ```text
  misc/vault	github.com/hashicorp/vault/api	reciprocal	MPL-2.0	(義務あり。終了コードに影響しない)
  .	example.com/foo	disallowed	GPL-3.0	許容リストに無い
  ```

- `go-licenses` が未導入、版が固定値と一致しない、版を取得できない場合はいずれも失敗する

### `checklicenses` の入力契約

`tools/checklicenses` が読む入力の形式を固定する。上流の出力形式が変わったときに
静かに誤判定しないよう、契約として明示する。

| 項目 | 契約 |
|---|---|
| 形式 | `go-licenses csv` の出力。1 行 1 パッケージ、カンマ区切りの 3 列 |
| 列 | `パッケージパス`, `ライセンス本文の URL`, `SPDX 識別子` |
| 判別不能 | 3 列目が `Unknown`。空文字ではない |
| モジュール帰属 | 入力自体には含まれない。**呼び出し側がモジュールごとに実行し、どのモジュールの出力かを引数で与える**（`data-model.md` の Dependency.OwnerModule） |
| 列数の異なる行 | 検査不能として中断する（ツール単体の終了コード 2）。無視してはならない。上流の形式変更を「違反なし」に見せないため |

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
- `golangci-lint` が未導入、版が固定値と一致しない、版を取得できない場合はいずれも失敗する

## `make check-vuln`

到達可能な既知脆弱性の検査（FR-020、FR-023）。

- 実装: `govulncheck ./...` をモジュールごとに実行する
- 失敗は「到達可能な報告あり」を意味する。到達しない脆弱性では失敗しない
- **統合前の必須ゲートである。** 到達可能な報告がある状態でマージできない（FR-021）。
  リリースは統合済みの状態から行うため、リリース専用のゲートは設けない
- 迂回・一時的な無効化・報告のみへの格下げの経路を設けてはならない（FR-022）。
  止まった場合の対処は依存の更新、または脆弱な経路へ到達しない形への修正である
- `govulncheck` が未導入、版が固定値と一致しない、版を取得できない場合はいずれも失敗する

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
| `$(SECRET_SCAN_TARGET)`（現在は `make betterleaks`） | 既存 |

シークレット検査は変数経由で呼ぶ。憲章が定めるツール名の変更に、一括実行側を
1 行で追随させるためである（gitleaks から betterleaks への移行で実際に機能した）。

- 既存の 3 つの検査自体は変更しない。入口から呼ぶだけである
- **先に 4 つのツールの導入と版をまとめて検証する。** ここで止まれば「検査できなかった」
  であり、通った後の失敗は「違反」と決まる（上記の区別を順序で担保する）
- 1 つが失敗しても残りを実行し、最後にまとめて報告する。最初の失敗で打ち切らない
  （1 つ直すたびに全部を実行し直す手戻りを避けるため）
- 最後に `RESULT: ok` または `RESULT: violated` と、失敗したゲート名を出す
- これがコントリビューターが提出前に実行する単一の入口である

## CI の契約

憲章のマージ前 7 ゲートは、すべて CI で強制されなければならない。憲章は
「手元と CI で同じ結果が再現されなければならない（MUST）」と定めており、CI で
実行されないゲートはこの条件を満たせない。

| ワークフロー | トリガ | 呼ぶもの | 失敗時 |
|---|---|---|---|
| チェック | `pull_request`, `push` | `make check-headers`, `make build-all`, `make test`, `make check-lint`, `make check-licenses` | 統合を止める |
| 脆弱性 | `pull_request`, `push` | `make check-vuln` | **統合を止める**（FR-021） |
| シークレット | `pull_request`, `push` | `make betterleaks` | 統合を止める |

ビルドと単体テストを外部ツールの導入より前に置く。壊れたビルドでツールの
ダウンロードに時間を使わないため。

脆弱性とシークレットを別のワークフローに分けるのは、前者が外部の脆弱性
データベースの状態に依存し、後者が履歴全体の取得（`fetch-depth: 0`）を要する
ためである。それぞれを独立に再実行できる。

リリース専用のゲートは設けない。`main` がマージ前ゲートをすべて満たしているため、
リリース時点で改めて満たすべき品質ゲートは無い（憲章「リリースは `main` から行う」）。

**CI は版を自前で持たない。** `make` ターゲットを呼ぶことで、版の定義を
Makefile の 1 か所に保つ（D6、FR-015、FR-029）。

**版の検証はツールごとに手段が異なる。** 単一のヘルパーで 3 ツールを扱えない
（research.md D9）。

| ツール | 固定値の変数 | 版の取得 |
|---|---|---|
| `golangci-lint` | `GOLANGCI_LINT_VERSION` | `golangci-lint --version`（`has version <版>` を含む行） |
| `govulncheck` | `GOVULNCHECK_VERSION` | `govulncheck --version`（`Scanner: govulncheck@<版>` の行） |
| `go-licenses` | `GO_LICENSES_VERSION` | `go version -m $(command -v go-licenses)` の `mod` 行。自身の版を報告する手段を持たない |
| `betterleaks` | `BETTERLEAKS_VERSION` | `go version -m $(command -v betterleaks)` の `mod` 行。`--version` は `dev` を返す |

版を取得するコマンドは `make` のレシピでコマンド位置へ直接展開する。シェル変数へ
代入して `eval` してはならない（[research.md](../research.md) D11）。代入時の二重展開で
`awk` のフィールド参照とコマンド置換が失われ、版が常に空になる。

版の抽出に失敗した場合も失敗として扱う。「版が取れなかった」を「一致した」として
扱ってはならない。

**`continue-on-error` を使わない。** 失敗を成功に見せてしまうため（`sbom.yml` と同じ方針）。
検査が失敗したなら、その job も失敗させる。4 種すべてが統合を止めるゲートになったため、
「報告のみ」の経路は存在しない。

**検査を省略した場合は理由を残す。** ログと実行サマリに省略の理由を書く（FR-026）。
省略を「違反なし」に見せてはならない。
