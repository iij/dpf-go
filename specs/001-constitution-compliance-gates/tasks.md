---

description: "憲章準拠のツール整備のタスク一覧"
---

# タスク: 憲章準拠のツール整備

**入力**: 次の設計文書 `/specs/001-constitution-compliance-gates/`

**前提**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**憲章**: v2.0.0（`.specify/memory/constitution.md`）

**テスト**: テストタスクを含む。仕様がテストを明示的に要求しているわけではないが、
憲章 原則 II（テストを伴う実装、NON-NEGOTIABLE）が「手書きで追加・変更した公開挙動には
単体テストを付与しなければならない（MUST）」と定めているため、新規の Go コードに対する
単体テストは任意ではなく必須である。

**構成**: タスクは user story ごとに分かれており、各 story を独立に実装・検証できる。

## 書式: `[ID] [P?] [シナリオ] 説明`

- **[P]**: 並列実行可能（別ファイル、未完了タスクへの依存なし）
- **[Story]**: 対応する user story（US1〜US4）
- 説明には正確なファイルパスを含む

## パスの規約

本リポジトリは複数モジュール構成の Go ライブラリである。パスはすべてリポジトリルートからの相対。

- 判定ロジック: `tools/checkheaders/`, `tools/checklicenses/`（ルートモジュール、標準ライブラリのみ）
- 設定: `.golangci.yml`, `licenses-allowlist.txt`（リポジトリルート）
- 実行の入口: `Makefile`
- CI: `.github/workflows/`

## 憲章による制約（全タスク共通）

- 新規の Go ファイルは先頭行に `// SPDX-License-Identifier: Apache-2.0` を置く（原則 III）
- 新規パッケージは `doc.go` を持ち、godoc は日本語で書く（原則 III）
- どのモジュールの `go.mod` にも依存を追加しない。新規コードは標準ライブラリのみ（原則 V）
- 新規の手書きファイルは `.openapi-generator-ignore` に登録する（原則 I）
- `unsafe` を使用しない（原則 V）
- 検査は作業ツリーを書き換えない（FR-028、SC-011。書き換えは既存の `make fmt` のみ）
- ツール未導入・版不一致・版の抽出失敗はいずれも終了コード 2 で中断する（FR-025）
- **4 種の検査はすべて統合（`main` へのマージ）を止めるゲートである。** 報告のみの経路、
  迂回、一時的な無効化を実装してはならない（FR-021、FR-022）

---

## フェーズ 1: 準備 (共通の基盤)

**目的**: 版の固定と保護設定。以降のすべての検査がこの土台を使う。

- [X] T001 `Makefile` に検査ツールの固定版を定義する。`GOLANGCI_LINT_VERSION := 2.13.2`、`GOVULNCHECK_VERSION := v1.7.0`、`GO_LICENSES_VERSION := v2.0.1`。CI に版を持たせないための唯一の出典とする（FR-029, research.md D6・D9）
- [X] T002 `Makefile` に版検証ヘルパーを追加する。**版の取得手段はツールごとに異なる**（`golangci-lint --version` / `govulncheck --version` の `Scanner: govulncheck@<版>` 行 / `go-licenses` は自身の版を報告できないため `go version -m $(command -v go-licenses)` の `mod` 行）。未導入・版不一致・**抽出失敗**のいずれでも導入方法を示して終了コード 2 で中断し、成功扱いにしない（FR-025, FR-029, research.md D9, data-model.md の PinnedTool）
- [X] T003 [P] `.openapi-generator-ignore` に `.golangci.yml` と `licenses-allowlist.txt` を登録する（原則 I。`tools/**`・`.github/**`・`misc/**` は既に登録済み）

---

## フェーズ 2: 土台 (先行必須)

**目的**: 4 種すべての検査が依存する共通の土台

**⚠️ CRITICAL**: このフェーズが完了するまで、どの user story も着手できない

- [X] T004 `Makefile` の `MODULES` を、リポジトリ内の `go.mod` の位置から導出する形に変更する。`vendor/` と `testdata/` は除外する。固定の列挙（`. misc/vault misc/aws misc/azure misc/gcp`）を削除する（FR-027, SC-008, research.md D8, 憲章 原則 II の MUST）
- [X] T005 `Makefile` の既存ターゲット（`build-all`, `test`, `lint`, `tidy-all`）が T004 の導出した `MODULES` で従来どおり動作することを確認する。`make build-all` と `make test` が 5 モジュールを対象にすることを確認する
- [X] T006 [P] `Makefile` に検査ゲートの共通規約をコメントで明記する。終了コード（0 = 違反なし、1 = 違反あり、2 = 検査不能）、ゲートが作業ツリーを書き換えないこと、迂回・無効化の経路を設けないこと（FR-022, FR-028, contracts/gates.md）

**関門**: モジュール導出と版固定が整い、各 user story の検査を追加できる

---

## フェーズ 3: 利用シナリオ 1 - ファイル冒頭の規約違反が機械的に止まる (優先度: P1) 🎯 MVP

**目標**: ライセンスヘッダの欠落と、ファイル全体に効く位置に置かれた抑制指示が、レビューを待たずに機械的に止まる。4 種の検査が共有する性質（未導入時の中断、省略理由の記録、対象モジュールの導出、手元での再現）もここで確立する。

**独立した検証**: `make check-headers` を実行し `internal/integration/` の 8 ファイルが列挙されることを確認する。付与後に 0 件になることを確認する。`package` 宣言より前に抑制指示を置いたファイルを作り、理由と対象を伴っていても検出されることを確認する。

### 利用シナリオ 1 のテスト

> **NOTE: 実装より先に書き、失敗することを確認する**

- [X] T007 [P] [US1] `tools/checkheaders/check_test.go` にテーブル駆動テストを作成する。ケース: 先頭行に SPDX がある / ない / 2 行目以降にある（不足とみなす） / `package` 宣言前に `nolint` がある（理由と対象 linter を伴う場合も検出する） / 対象行に限定された `nolint`（検出しない。FR-017 の領域） / `vendor/` 配下 / `testdata/` 配下（いずれも除外）。ネットワークを使わない（原則 II）

### 利用シナリオ 1 の実装

- [X] T008 [P] [US1] `tools/checkheaders/doc.go` を作成する。検査する 2 つの規約（先頭行の SPDX、`package` 宣言前の抑制指示の禁止）と使い方を日本語の godoc で記述し、先頭行に SPDX ヘッダを置く（原則 III。`tools/genexecuteall/doc.go` と同形）
- [X] T009 [US1] `tools/checkheaders/check.go` に判定ロジックを実装する。先頭行の SPDX 判定、`package` 宣言前の抑制指示の検出、`vendor/` と `testdata/` の除外。標準ライブラリのみを使う（FR-001, FR-002, FR-018, data-model.md の FileHeader）
- [X] T010 [US1] `tools/checkheaders/main.go` に走査と出力を実装する。対象はリポジトリ内のすべての `.go` ファイル（テスト・ビルドタグ付き・生成物を含む）。違反は全件を `パス<TAB>種別<TAB>詳細`（種別は `missing-spdx` / `preamble-pragma`）で列挙し、最初の 1 件で打ち切らない。終了コードは 0 / 1、内部エラーのみ 2（FR-003, FR-004, contracts/gates.md）
- [X] T011 [US1] `Makefile` に `check-headers` ターゲットを追加する（`go run ./tools/checkheaders`）。作業ツリーを書き換えない（FR-028, contracts/gates.md）
- [X] T012 [P] [US1] `internal/integration/connectivity_test.go`, `delegations_test.go`, `helpers_test.go`, `lifecycle_test.go`, `main_test.go`, `mutex_test.go`, `readonly_test.go`, `zonelookup_test.go` の 8 ファイルの先頭行に `// SPDX-License-Identifier: Apache-2.0` を付与する（FR-005, SC-001）
- [X] T013 [US1] `.github/workflows/checks.yml` を新規作成する。`pull_request` と `push` で `make check-headers` を呼び、失敗時は統合を止める。CI は版を自前で持たない（FR-029）。検査を省略した場合は理由をログと `GITHUB_STEP_SUMMARY` に残す。`continue-on-error` を使わない（FR-015, FR-026, SC-002, US1 シナリオ 8）
- [X] T014 [US1] `tools/checkheaders/check.go` の広域抑制検出が実ファイルで効くことを確認する。`package` 宣言前に理由と対象 linter を伴う `nolint` を置いた一時ファイルで `make check-headers` が `preamble-pragma` を報告することを確認し、確認後に削除する（FR-018, US1 シナリオ 6, quickstart.md 手順 2）
- [X] T015 [US1] `make check-headers` が生成物を除外しないことを確認する。**再生成は行わず機構の検証に留めた**: `check-headers` は生成物を含めて 0 件で通り、`templates/partial_header.mustache` の先頭行が SPDX 識別子であることを確認した。`make generate` は生成物 約 300 ファイルを削除して `:latest` イメージで再生成するため、本機能と無関係な大規模差分を生む恐れがある（FR-006, US1 シナリオ 5, 原則 I）

**関門**: US1 が独立して動作する。ファイル冒頭の規約違反が統合前に止まり、共通の土台が確立している

---

## フェーズ 4: 利用シナリオ 2 - 許容外ライセンスの依存が持ち込めない (優先度: P2)

**目標**: 許容リスト外・判別不能のライセンスを持つ依存が、間接依存として入った場合も含めて機械的に止まる。ソース提供義務を伴う依存が本体モジュールへ入ることも止まる。

**独立した検証**: `make check-licenses` を実行し、違反 0 件で通ること、`reciprocal` の一覧に HashiCorp 系 10 件（すべて `misc/vault`）が出ることを確認する。`licenses-allowlist.txt` から `MPL-2.0` を一時的に外すと 10 件が `disallowed` として失敗することを確認する。

### 利用シナリオ 2 のテスト

- [X] T016 [P] [US2] `tools/checklicenses/allowlist_test.go` にテーブル駆動テストを作成する。ケース: 正常なリスト / コメントと空行 / 同じ識別子の重複（エラー） / 未知の区分（エラー） / 大文字小文字の違い（別物として扱う）（contracts/allowlist.md）
- [X] T017 [P] [US2] `tools/checklicenses/input_test.go` にテーブル駆動テストを作成する。`go-licenses csv` の 3 列を正しく読むこと、`Unknown` を判別不能として扱うこと、**列数の異なる行でエラーにすること**（無視してはならない）（contracts/gates.md の入力契約）
- [X] T018 [P] [US2] `tools/checklicenses/verdict_test.go` にテーブル駆動テストを作成する。data-model.md の Verdict の 5 値それぞれと判定順序（`undetermined` を最優先で見ること）を網羅する。とくに `reciprocal` が本体モジュールなら違反、それ以外なら許容となる分岐（FR-012）

### 利用シナリオ 2 の実装

- [X] T019 [P] [US2] `licenses-allowlist.txt` をリポジトリルートに作成する。`Apache-2.0` / `MIT` / `BSD-2-Clause` / `BSD-3-Clause` / `ISC` を `notice`、`MPL-2.0` を `reciprocal` として TSV で記述し、憲章の許容リストと一対一で対応することと変更手続きをコメントに書く（FR-008, FR-013, contracts/allowlist.md）
- [X] T020 [P] [US2] `tools/checklicenses/doc.go` を作成する。母集団が「実際に import されるパッケージ」であり、モジュールグラフ全体を用いないこと（憲章の規範）と使い方を日本語の godoc で記述する（原則 III, FR-007, research.md D2）
- [X] T021 [US2] `tools/checklicenses/allowlist.go` に許容リストの読み込みを実装する。重複と未知の区分をエラーにする（FR-008, FR-013, contracts/allowlist.md）
- [X] T022 [US2] `tools/checklicenses/input.go` に `go-licenses csv` の出力の解析を実装する。3 列（パッケージパス / ライセンス URL / SPDX 識別子）を読み、`Unknown` は判別不能として扱う。列数の異なる行はエラーとして返し、呼び出し側が終了コード 2 で中断できるようにする。標準入出力に触れず、テストから直接呼べる形にする（contracts/gates.md の入力契約, plan.md の Structure Decision）
- [X] T023 [US2] `tools/checklicenses/verdict.go` に判定ロジックを実装する。data-model.md の Verdict と判定順序に従う（FR-009, FR-011, FR-012）
- [X] T024 [US2] `tools/checklicenses/main.go` に入出力の接続と報告を実装する。判定と解析は `allowlist.go` / `input.go` / `verdict.go` に委ね、`main.go` には接続だけを残す。違反は `モジュール<TAB>パッケージ<TAB>区分<TAB>ライセンス<TAB>詳細` で全件列挙し、間接依存はどの直接依存を経由したかを示す。`reciprocal` の一覧は違反がなくても出し、終了コードに影響させない（FR-007, FR-010, FR-011, SC-005）
- [X] T025 [US2] `Makefile` に `check-licenses` ターゲットを追加する。T002 の版検証（`go version -m` 経由）を通したうえで、`MODULES` の各モジュールで `go-licenses` を実行し、その出力とモジュール名を `go run ./tools/checklicenses` に渡す。作業ツリーを書き換えない（FR-024, FR-025, FR-028, SC-005）
- [X] T026 [US2] `.github/workflows/checks.yml` に `make check-licenses` のステップを追加する（T013 が作成したファイルへの追記）。失敗時は統合を止める。ライセンスはシークレットではないため検出内容を出力してよい（憲章のシークレット検査の方針とは対象が異なる。SC-002）
- [X] T027 [US2] 本体モジュール（`.`）の依存に `reciprocal` が現れないことを確認する。`licenses-allowlist.txt` で `Apache-2.0` を一時的に `reciprocal` に変えると `root-reciprocal` で失敗することを確認し、元に戻す（FR-012, SC-006, US2 シナリオ 6, quickstart.md 手順 3）

**関門**: US1 と US2 が独立して動作する

---

## フェーズ 5: 利用シナリオ 3 - 静的解析の結果が手元と統合前で一致する (優先度: P3)

**目標**: 有効な検査項目と版が固定され、手元で通ったものが統合前で落ちない。理由のない抑制と、対象 linter を明示しない抑制が機械的に止まる。

**独立した検証**: 同一コミットに対する `make check-lint` の出力と CI の出力を比べ、指摘の集合が一致することを確認する。理由のない `//nolint` と、linter 名のない `//nolint` をそれぞれ入れて失敗することを確認する。ファイル全体に効く抑制の検出は US1 が担うため、本 story の検証には含めない。

### 利用シナリオ 3 の実装

- [X] T028 [US3] `.golangci.yml` をリポジトリルートに作成する。`version: "2"`、`linters.enable` に `nolintlint`、`linters.settings.nolintlint` に `require-explanation: true`（FR-017、US3 シナリオ 2）/ `require-specific: true`（US3 シナリオ 3）/ `allow-unused: false`、`formatters.enable` に `gofmt`（2.13.2 では `gofmt` は linter ではなく formatter。FR-014, FR-019, SC-004, research.md D5）
- [X] T029 [US3] `golangci-lint config verify --config .golangci.yml` が通ることを確認する（JSON スキーマ検証。research.md D5 で確認済みの構成）
- [X] T030 [US3] `Makefile` の `lint` ターゲットを `check-lint` に置き換える。T002 の版検証を通したうえで、`MODULES` の各モジュールに `golangci-lint run --build-tags=integration ./...` を実行する。`integration` タグ配下も検査対象に含める。作業ツリーを書き換えない（FR-016, FR-028, US3 シナリオ 4）
- [X] T031 [US3] `.golangci.yml` の設定で手書きコード（`utils/`, `misc/*/`, `tools/`, `internal/`, `jobs_syncwait.go`, `interfaces.go`, `doc.go`）に出る指摘を解消する。対象ファイルは `make check-lint` の出力で確定する。生成物（`api_*.go` / `model_*.go` / `client.go` / `configuration.go` / `response.go` / `utils.go` / `executeall_gen.go`）は直接編集せず、`templates/` の修正か `.golangci.yml` の除外で対応する（原則 I）。抑制は対象行に限定し `nolint` に理由と対象 linter を併記する（FR-017, SC-004, FR-018 の切り分け）
- [X] T032 [US3] `.github/workflows/checks.yml` に `make check-lint` のステップを追加する（T013 が作成したファイルへの追記）。CI は版を自前で持たず T001 の固定値を使う（FR-015, FR-029, SC-002, SC-003）

**関門**: US1〜US3 が独立して動作する

---

## フェーズ 6: 利用シナリオ 4 - 到達可能な既知脆弱性を抱えたまま統合できない (優先度: P4)

**目標**: 到達可能な既知脆弱性を抱えた変更が統合できない。到達しない脆弱性では止まらない。リリースは統合済みの状態から行うため、リリース専用のゲートは設けない。

**独立した検証**: `make check-vuln` を実行し、到達可能な報告が 0 件であることを確認する。統合前の検査で失敗した場合に変更が統合できないことを確認する。`Makefile` と CI を読み、迂回の経路が存在しないことを確認する。

### 利用シナリオ 4 の実装

- [X] T033 [US4] `Makefile` に `check-vuln` ターゲットを追加する。T002 の版検証を通したうえで、`MODULES` の各モジュールに `govulncheck ./...` を実行する。終了コード 1 は「到達可能な報告あり」を意味し、到達しない脆弱性では 1 にしない。作業ツリーを書き換えない（FR-020, FR-023, FR-028, US4 シナリオ 1・3）
- [X] T034 [US4] `.github/workflows/govulncheck.yml` を新規作成する。`pull_request` と `push` で `make check-vuln` を実行し、**失敗時は統合を止める**。`continue-on-error` を使わない。報告のみのジョブ、`release` 専用のジョブ、検査を飛ばす入力は作らない（FR-021, SC-002, SC-007, US4 シナリオ 2, research.md D7, 憲章 v2.0.0）
- [X] T035 [US4] `Makefile` と `.github/workflows/govulncheck.yml` を読み、脆弱性検査を飛ばす・無効化する・報告のみへ格下げするオプションや環境変数が存在しないことを確認する。止まった場合に通す手段が「依存の更新」または「脆弱な経路へ到達しない形への修正」だけであることを確認する（FR-022, US4 シナリオ 4, quickstart.md 手順 5）

**関門**: 4 つの user story すべてが独立して動作する

---

## フェーズ 7: 仕上げと横断的な事項

**目的**: 統合前のすべてのゲートを 1 つの入口にまとめ、文書と憲章の整合を取る

- [X] T036 `Makefile` に `check` ターゲットを追加し、**統合前に満たすべき 7 ゲートすべて**をまとめて実行する。`make build-all` / `make test` / `make check-lint` / `make check-headers` / `make check-licenses` / `make check-vuln` / `make betterleaks`。既存の 3 つの検査自体は変更せず、入口から呼ぶだけとする。1 つが失敗しても残りを実行し、最後にまとめて報告する。終了コードは、いずれかが 2 なら 2、そうでなくいずれかが 1 なら 1、すべて 0 なら 0（FR-024, FR-030, SC-010, contracts/gates.md）
- [X] T037 `make -n check` の出力に 7 つのゲートすべてが現れることを確認する。本機能の 4 種だけでは足りない。1 つが失敗しても残りが実行され最後にまとめて報告されることも確認する（FR-030, quickstart.md 手順 7）
- [X] T038 [P] `Makefile` の `.PHONY` に新しいターゲット（`check`, `check-headers`, `check-licenses`, `check-lint`, `check-vuln`）を追加し、削除した `lint` を外す
- [X] T039 [P] `README.md` に統合前の検査の実行方法を 1 か所で示す。clone 直後の開発者が追加の説明を読まずに `make check` を実行できる状態にする（SC-010）
- [X] T040 [P] `CHANGELOG.md`（Keep a Changelog 形式）に、`misc/*` への `LICENSE` 追加と検査ゲートの追加を記録する。`misc/*` の `LICENSE` は利用者に見える変更である（原則 III）
- [X] T041 `make test` を実行し、`tools/checkheaders/check_test.go` と `tools/checklicenses/{allowlist,input,verdict}_test.go` を含めて全モジュールが `-race -count=1` で通ることを確認する（原則 II）
- [X] T042 `Makefile` の `fmt` ターゲットを実行し、`.golangci.yml` の `formatters` による整形検査が 0 件で通ることを確認する（FR-019）
- [X] T043 `make check` の実行前後で `git status --porcelain` の出力が一致することを確認する。7 ゲートが作業ツリーを書き換えないこと（FR-028, SC-011, quickstart.md 手順 8）
- [X] T044 `PATH` からツールを外した状態、および版を固定値と違えた状態で各 `check-*` ターゲットが終了コード 2 になることを確認する。版の抽出に失敗する場合も 2 であり、0 にならないこと（FR-025, SC-009, research.md D9）
- [X] T045 [quickstart.md](./quickstart.md) の手順 1〜8 をすべて実行し、仕様の受け入れシナリオ 25 件に対応する確認が通ることを検証する
- [X] T046 `misc/vault/LICENSE`, `misc/aws/LICENSE`, `misc/azure/LICENSE`, `misc/gcp/LICENSE`（本計画の作成時に追加済み）が `make check-licenses` の判別不能を 0 件にしていることを確認する（research.md D3）
- [X] T048 `.github/workflows/checks.yml` に `make build-all` と `make test` のステップを追加する。憲章のマージ前 7 ゲートはすべて CI で強制されなければならない（憲章「手元と CI で同じ結果が再現されなければならない（MUST）」）。外部ツールの導入より前に置き、壊れたビルドでダウンロードに時間を使わないようにする
- [X] T047 `.specify/memory/constitution.md`（v2.0.0）の品質ゲート表と `Makefile` が一致することを確認する。マージ前 7 行に対応する `make` ターゲットがすべて存在し、削除した `lint` が表に残っていないこと。リリース専用のゲートが実装されていないことも確認する

---

## 依存関係と実行順序

### フェーズ間の依存

- **Setup (フェーズ 1)**: 依存なし。即着手できる
- **Foundational (フェーズ 2)**: Setup の完了に依存し、**すべての user story をブロックする**
- **User Stories (フェーズ 3〜6)**: すべて Foundational の完了に依存する
  - 以降は並列に進められる（人員があれば）。または優先度順に P1 → P2 → P3 → P4
- **Polish (フェーズ 7)**: T036・T037・T043・T044・T045・T047 は 4 つの story すべての完了に依存する。それ以外は部分的に先行できる

### 利用シナリオ間の依存

- **US1 (P1)**: Foundational の後に着手できる。他の story に依存しない
- **US2 (P2)**: Foundational の後に着手できる。US1 に依存しない（別のツール・別のターゲット）
- **US3 (P3)**: Foundational の後に着手できる。**US1 に依存しない。** FR-018 が spec の「ファイル冒頭の規約」グループへ移り US1 の受け入れシナリオになったため、US3 の検証範囲から外れた
- **US4 (P4)**: Foundational の後に着手できる。他の story に依存しない。CI は別ファイル（`govulncheck.yml`）を使うため `checks.yml` と競合しない

**4 つの story はいずれも互いに独立している。** ただし US1・US2・US3 は
`.github/workflows/checks.yml` という同じファイルを触る（T013・T026・T032）。
**このファイルは並列にしてはならない。** T013 が新規作成するため、T026 と T032 は
T013 の後に順に行う。

### 各利用シナリオの内部

- テストを先に書き、実装前に失敗することを確認する（原則 II）
- 判定と解析（`check.go` / `allowlist.go` / `input.go` / `verdict.go`）を `main.go` より先に実装する
- `Makefile` のターゲットは、それが呼ぶツールの実装より後に追加する
- CI のステップは、ローカルで `make` ターゲットが通った後に追加する
- 動作確認タスク（T014・T015・T027・T035）は、当該 story の `make` ターゲット完成後に行う

### 並行できる箇所

- T003 は T001 / T002 と並列に実行できる（別ファイル）
- T006 は T004 / T005 と並列に実行できる
- US1 の T007（テスト）と T008（doc.go）は並列に実行できる
- T012（8 ファイルへの SPDX 付与）は US1 の他のタスクと並列に実行できる（`tools/` を触らないため）
- US2 の T016 / T017 / T018 / T019 / T020 は並列に実行できる（すべて別ファイル）
- Foundational の完了後、US1 / US2 / US3 / US4 は並列に進められる（`checks.yml` の競合のみ注意）
- Polish の T038 / T039 / T040 は並列に実行できる

---

## 並行実行の例: 利用シナリオ 2

```bash
# テスト 3 本と設定・文書を同時に着手できる（すべて別ファイル）:
Task: "tools/checklicenses/allowlist_test.go にテーブル駆動テストを作成"
Task: "tools/checklicenses/input_test.go に入力解析のテストを作成"
Task: "tools/checklicenses/verdict_test.go にテーブル駆動テストを作成"
Task: "licenses-allowlist.txt をリポジトリルートに作成"
Task: "tools/checklicenses/doc.go を作成"

# その後、実装は依存順に直列で進める:
# allowlist.go → input.go → verdict.go → main.go → Makefile → CI → 動作確認
```

---

## 実装の進め方

### まず MVP (利用シナリオ 1 のみ)

1. フェーズ 1: Setup を完了する（T001〜T003）
2. フェーズ 2: Foundational を完了する（T004〜T006。**すべての story をブロックする**）
3. フェーズ 3: 利用シナリオ 1 を完了する（T007〜T015）
4. **停止して検証**: `make check-headers` が 0 件で通り、ヘッダを欠いたファイルと
   `package` 宣言前の抑制のいずれでも失敗すること
5. この時点で、憲章が現に違反している唯一の項目（8 ファイルの SPDX 欠落）が解消し、
   再発が機械的に止まる

### 増分で届ける

1. Setup + Foundational → 土台ができる
2. US1 を追加 → 独立に検証 → **MVP**（現存する違反の解消）
3. US2 を追加 → 独立に検証（利用者の法務判断に関わる項目）
4. US3 を追加 → 独立に検証（手元と CI の一致）
5. US4 を追加 → 独立に検証（外部要因で止まりうるゲート）
6. Polish で 7 ゲートを `make check` に集約し、文書と憲章の整合を取る

各段階が単独で価値を持ち、前の段階を壊さない。FR-024 の「単一のコマンド」は
段階的に充足される（各段階では当該 story の検査コマンドが単一の入口。spec の Assumptions）。

### 複数人で並行する場合

複数人で進める場合:

1. Setup + Foundational を全員で完了させる
2. 完了後に分担する。4 つの story はいずれも独立している
   - 担当 A: US1（`tools/checkheaders` + 8 ファイル）
   - 担当 B: US2（`tools/checklicenses` + 許容リスト）
   - 担当 C: US3（`.golangci.yml` + 既存指摘の解消）
   - 担当 D: US4（`Makefile` + 脆弱性ワークフロー）
3. `.github/workflows/checks.yml` は US1・US2・US3 が触るため、T013 → T026 → T032 の順で直列化する

---

## 補足

- [P] は別ファイルで依存関係のないタスクを示す
- 実装前にテストが失敗することを確認する（原則 II）
- タスクごと、または論理的なまとまりごとにコミットする
- 各 Checkpoint で停止し、story を独立に検証できる
- 新規の Go ファイルには必ず SPDX ヘッダを置く。置き忘れは T010 完了後は `make check-headers` が検出する
- どのモジュールの `go.mod` も変更しない。変更が必要になった場合は原則 V に照らして設計を見直す
- 検査ターゲットは作業ツリーを書き換えない。書き換えるのは既存の `make fmt` のみ（FR-028）
- 「検査できなかった」を「違反なし」として扱ってはならない。終了コード 2 を 1 と混同しない
- 4 種すべてが統合を止めるゲートである。報告のみの経路や迂回を実装してはならない（FR-021, FR-022）

---

## フェーズ 8: 収束 (Convergence)

**目的**: 実装後にコードを spec・plan・tasks へ照らして評価し、残っていた差分を埋める。
`/speckit-converge` が検出した 5 件で、憲章違反は無い。

- [X] T049 `tools/checklicenses/verdict.go` の `verdictOf` が SPDX の複合式（`MIT OR Apache-2.0`、`Apache-2.0 AND BSD-3-Clause`）を解釈できるようにする。`OR` は許容リストに収まる選択肢が一つでもあれば許容とし、選択したライセンスを報告に残す。`AND` はすべてが許容リストに収まる場合のみ許容とする。現在は単純な map 参照のため複合式が `disallowed` になり、マージを止める偽陽性になる per spec: 境界的な状況（デュアルライセンス） (missing)
- [X] T050 `tools/checklicenses/why_test.go` に `why` の出力解析のテーブル駆動テストを作成する。`go mod why` の出力（コメント行、`(main module does not need ...)` の行、import の連鎖、空出力）を網羅し、経由元の抽出と特定できない場合の扱いを固定する。上流の出力形式に依存する箇所であり、現在のカバレッジは 0.0% per FR-010 / 憲章 II (partial)
- [X] T051 `tools/checkheaders/check_test.go` と `tools/checklicenses/main_test.go` に出力 1 行の形式のテストを追加する。`violation.String()` が `パス<TAB>種別<TAB>詳細`、`format()` が `モジュール<TAB>パッケージ<TAB>区分<TAB>ライセンス<TAB>詳細` を返すことを固定する。contracts/gates.md が機械的に読める形を契約として定めているのに、現在はいずれもカバレッジ 0.0% per contracts/gates.md: 出力の契約 / 憲章 II (partial)
- [X] T052 `tools/checkheaders/run` と `tools/checklicenses/run` にテストを追加する。前者は一時ディレクトリを作り、隠しディレクトリ・`vendor`・`testdata` が除外されること、生成物とテストファイルは除外されないこと、違反が全件パス順に並ぶことを確認する。後者は `reciprocal` が終了コードに影響せず一覧として出ること、違反があれば 1 を返すことを確認する。いずれも現在のカバレッジは 0.0% per FR-002, FR-003, FR-004, FR-009, FR-011, FR-012 / 憲章 II (partial)
- [X] T053 `make generate` の実行後に `make check-headers` が 0 件で通ることを確認した。**再生成による差分は 0 件**で、生成物 7 種（`api_*` / `model_*` / `client` / `configuration` / `response` / `utils` / `executeall_gen`）の先頭行はいずれも SPDX 識別子だった。T015 で機構の確認に留めていた部分を実測で埋めた per FR-006 (partial)

**Checkpoint**: 判定ロジックだけでなく入出力と走査の層も検証され、複合式の偽陽性が解消する

---

## フェーズ 9: 収束 (Convergence, 2 回目)

**目的**: フェーズ 8 の実装後に再評価した結果、複合式の解釈に緩い経路が残っていた。
`/speckit-converge` が検出した 4 件で、憲章違反は無い。

- [X] T054 `tools/checklicenses/verdict.go` の `evaluate` で、末尾に演算子だけが残る不完全な式（`MIT AND`、`MIT OR`）を判別不能として扱う。現在は被演算子が 1 つになると演算子を無視して単一ライセンスとして許容し、実測で `verdictOf("MIT AND")` が `allowed` / 報告 `MIT` を返す。`A AND B` が途中で切れた入力では B の義務が落ちるため判定が実際より緩くなる。解釈できない形は判別不能へ倒す方針に反する。`verdict_test.go` にケースを追加して固定する per spec: 境界的な状況（デュアルライセンス） (partial)
- [X] T055 `specs/001-constitution-compliance-gates/data-model.md` の Verdict 節と `contracts/gates.md` を、複合式の解釈と「選択したライセンスの報告」に合わせて更新する。現在の判定表は `LicenseID` が単一の識別子である前提で書かれており、`OR` で選択が生じた場合に報告へ残すライセンスが入力と異なることも、`AND` で義務が最も重いものになることも記述されていない per spec: 境界的な状況（デュアルライセンス） / contracts: Verdict (partial)
- [X] T056 `tools/checkheaders/check.go` の `skipPath` からバックスラッシュを区切りとする分岐を削除する。`main.go` が `filepath.ToSlash` で正規化してから呼ぶため到達不能であり、Windows でも ToSlash が変換する。加えて Linux では `\` が正当なファイル名文字であるため、`foo\vendor\bar.go` という 1 つのファイル名を誤って除外しうる。削除しない場合は到達する経路を示すコメントを残す per plan: checkheaders の走査 (unrequested)
- [X] T057 未テストの 2 分岐にテストを追加する。`tools/checkheaders/main.go` の `run()` の並び替えのタイブレーク（同一パスで `missing-spdx` と `preamble-pragma` の 2 件が出る場合の順序）と、`tools/checklicenses/verdict.go` の `evaluate` の空フィールド分岐（空白のみのライセンス識別子。実測では `undetermined` を返す） per FR-004 / 憲章 II (partial)

**Checkpoint**: 複合式の解釈に緩い経路が残らず、設計文書が実装と一致する
