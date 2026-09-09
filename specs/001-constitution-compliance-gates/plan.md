# 実装計画: 憲章準拠のツール整備

**ブランチ**: `001-constitution-compliance-gates` | **日付**: 2026-09-09 | **仕様**: [spec.md](./spec.md)

**入力**: 次の機能仕様 `/specs/001-constitution-compliance-gates/spec.md`

**注記**: `Branch` は `.specify/feature.json` が示す機能ディレクトリ名である。実際の
git ブランチは `specify`（`before_specify` フックが無いためブランチは作成していない）。

## 概要

憲章が定めた 4 つの MUST（ファイル冒頭の規約、依存ライセンスの許容リスト、
静的解析と整形、到達可能な既知脆弱性）を、人手のレビューに頼らず機械的に強制する
ゲートとして実装する。

技術的な方針は 4 つある。

1. **判定ロジックは標準ライブラリのみの Go プログラムとして書く。** `tools/checkheaders`
   と `tools/checklicenses` を追加する。既存の `tools/genexecuteall` と同じ構成
   （`doc.go` + `main.go`、標準ライブラリのみ、SPDX ヘッダ付き）に揃える。ルート
   モジュールに属するため `make build-all` / `make test` の既存の反復に自動的に乗り、
   憲章 II の単体テスト要件を満たせる。
2. **外部ツールは版を Makefile に固定し、CI は版を持たない。** CI は `make` ターゲットを
   呼ぶ。これにより手元と CI が同一の版・同一の設定で動く（FR-015）。
3. **依存ライセンスの判定には `go-licenses` を使う。** `syft` は判別不能が 15 件出て
   ゲートとして使えないことを実測で確認した（[research.md](./research.md) D1）。
   `syft` は SBOM 生成用途として `sbom.yml` で継続利用する。
4. **検査対象モジュールは `go.mod` の位置から導出する。** 現在の Makefile は
   `MODULES` をハードコードしており、FR-027 と SC-008 を満たせない。

脆弱性検査の位置づけは憲章 v2.0.0 で変わった。当初はリリース専用のゲートとして設計して
いたが、`main` への統合を止めるゲートになった（[research.md](./research.md) D7）。
`main` を常にリリース可能な状態に保つためであり、新しい脆弱性の公表で脆弱性を含まない
変更が止まる事象は承知のうえで受け入れる。迂回の経路は実装しない。

計画の作成中に、`misc/*` の 4 モジュールに `LICENSE` が無く、独立に取得した利用者へ
ライセンスが届かない不備を発見したため、ルートの `LICENSE` を複製して是正した
（[research.md](./research.md) D3）。これにより `go-licenses` の判別不能が 0 件になり、
検査側に自前モジュールの除外ロジックが不要になった。

## 技術的な前提

**言語 / バージョン**: Go 1.27（`go.mod` は `go 1.27.0`、確認環境は go1.27.1 linux/amd64）

**主要な依存**: 新規の Go コードは**標準ライブラリのみ**。いずれのモジュールの
`go.mod` も変更しない。外部実行バイナリとして `golangci-lint` 2.13.2、`govulncheck` v1.7.0、
`go-licenses` v2.0.1 を版固定で用いる。版の取得手段はツールごとに異なる（research.md D9）

**保存先**: 該当なし（永続データを持たない。入力はリポジトリ内のファイルと依存グラフ）

**テスト**: `go test ./... -race -count=1`（既存の `make test`）。新規の 2 ツールは
テーブル駆動の単体テストを付ける

**対象環境**: 開発者環境（Linux / macOS）と CI（`ubuntu-latest`）

**プロジェクト種別**: Go ライブラリ（複数モジュール）＋ リポジトリ内ツール

**性能目標**: `make check-headers` は数百ファイルを 1 秒程度で走査する。
`make check-licenses` は `go-licenses` がパッケージをビルドするため、5 モジュール合計で
数分を要する（実測: `misc/gcp` が最も重い）。`make check-lint` は既存の `make lint` と同等

**制約**:
- どのモジュールの `go.mod` にも依存を追加しない（憲章 原則 V）
- 生成物を直接編集しない（憲章 原則 I）。生成物のヘッダはテンプレート由来を維持する
- 検査は作業ツリーを書き換えない（書き換えるのは既存の `make fmt` のみ。FR-028、SC-011）
- ツール未導入・版不一致で成功扱いにしない（FR-025）。版の抽出に失敗した場合も中断する
- 検査ツールの版は Makefile を唯一の出典とし、CI は独自に版を持たない（FR-029）

**規模 / 範囲**: 5 モジュール、Go ファイル約 300（うち生成物が大半）、検査 4 種、
一括実行が呼ぶゲート 7 種、依存パッケージ 65 件（重複除去後、import されるもの）、
要件 30 件（FR）／成功基準 11 件（SC）

**要明確化**: なし。仕様に未解決のマーカーは無い。計画に委ねられていた
「依存ライセンス検査の手段」は Phase 0 の D1 で、ツールの版の固定値と取得手段は D9 で決定した。

## 憲章への適合確認

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

憲章 **v2.0.0** に対する評価。Phase 1 設計後の再評価を含む。v2.0.0 は脆弱性検査を
マージ前ゲートへ反転させ、`make check` の対象をマージ前の全ゲートと明確化した。

| 原則・節 | 要求 | 本計画での扱い | 判定 |
|---|---|---|---|
| I. 生成コードと手書きコードの分離 | 生成物を直接編集しない。手書きファイルは `.openapi-generator-ignore` に登録する | 生成物を編集しない。新規の手書きファイル（`.golangci.yml`、`licenses-allowlist.txt`）を同ファイルへ登録する。`tools/**` と `.github/**` は既に登録済み | PASS |
| I. 生成物のヘッダ | 生成物にも SPDX ヘッダを要求する | 検査は生成物を除外しない。ヘッダはテンプレート（`partial_header.mustache`）由来を維持する | PASS |
| II. テストを伴う実装 | 手書きコードに単体テストを付ける。`make test` はネットワーク不要 | 新規 2 ツールにテーブル駆動の単体テストを付ける。テストは固定の入力に対する判定のみでネットワークを使わない | PASS |
| II. 統合テストの隔離 | 実 API に依存するテストは `integration` タグで隔離 | 新規テストは実 API に触れない。タグ不要 | PASS |
| III. godoc とドキュメント | 公開要素に godoc、各パッケージに `doc.go`、全 Go ファイルに SPDX、記述は日本語 | 新規ツールは `doc.go` + SPDX ヘッダを持つ（`tools/genexecuteall` と同形）。利用者に見える挙動の変更を伴うため `CHANGELOG.md` を更新する | PASS |
| IV. ドメイン名は miekg/dns | ドメイン名操作に `strings` を使わない | 本機能はドメイン名を扱わない | N/A |
| V. ライブラリとしての規律 | `unsafe` 不使用、ライブラリ内でログ出力しない、本体の依存を増やさない | `unsafe` 不使用。新規コードはライブラリ本体ではなくツールであり、ログ出力の禁止は適用されない。標準ライブラリのみで `go.mod` を変更しない | PASS |
| V. optional な機能の隔離 | 他パッケージに依存する機能は `misc/` の独立モジュールへ | 外部ツールは実行バイナリとして呼ぶだけで、Go の依存として取り込まない。新しいモジュールを作らない | PASS |
| セキュリティとリリース | CI ログに検出箇所を出さない | ライセンスと脆弱性はシークレットではなく、詳細を出すことが目的に適う。シークレット検査の方針とは対象が異なる | PASS |
| セキュリティとリリース | 検査を省略して通過させない | ツール未導入・版不一致は終了コード 2 で中断する。CI が省略した場合は理由をログとサマリに残す | PASS |
| ライセンス（新設） | 全 Go ファイルに SPDX、依存は許容リストに限る、機械的に検査する | 本機能そのものがこの要求の実装である | PASS |
| Go コード品質（新設） | `gofmt` / `golangci-lint` / `govulncheck` を基準とし、版と設定を固定する | D5・D6 で設計。`golangci-lint config verify` で設定案の妥当性を確認済み | PASS |
| 開発ワークフローと品質ゲート | マージ前 7 ゲート（ビルド / 単体テスト / 整形・静的解析 / ファイル冒頭の規約 / 依存ライセンス / **到達可能な既知脆弱性** / シークレット検査）。リリース専用のゲートは無い | 本機能が 4 ゲートを実装する。`make lint` は `make check-lint` に置き換わり、整形は同ターゲットが担う。ビルドとシークレット検査は変更しない | PASS |
| 品質ゲート: `make check` | **マージ前の 7 ゲートすべて**をまとめて実行し、1 つ失敗しても継続して最後に報告する（MUST） | Polish フェーズで実装する。本機能の 4 種だけでなく既存のビルド・単体テスト・シークレット検査も入口から呼ぶ（FR-030）。検査自体は変更しない。段階的充足は spec の Assumptions に明記済み | PASS |
| 品質ゲート: 版の単一出典 | 版を単一の出典で固定し、CI が独自に版を持たない（MUST / MUST NOT） | Makefile を唯一の出典とし、CI は `make` を呼ぶ（research.md D6）。固定値と取得手段は D9 で確定（FR-029） | PASS |
| 品質ゲート: 作業ツリー不変 | ゲートは作業ツリーを書き換えてはならない（MUST NOT） | 4 種すべて検査のみ。書き換えは既存の `make fmt` に限る（FR-028、SC-011） | PASS |
| 品質ゲート: 省略の禁止 | ツール未導入・版不一致で成功扱いにしない（MUST NOT） | 終了コード 2 で中断する。版の抽出に失敗した場合も中断する（D9、FR-025） | PASS |
| 品質ゲート: 脆弱性のマージ阻止 | 到達可能な既知脆弱性が報告される状態でマージしてはならない（MUST NOT） | `check-vuln` を統合前の必須ゲートとして CI に置く（research.md D7、FR-021） | PASS |
| 品質ゲート: 迂回の禁止 | ゲートの迂回・一時的な無効化・報告のみへの格下げで通してはならない（MUST NOT）。対処は依存の更新または到達しない形への修正（MUST） | 実装側に迂回の経路を用意しない。`continue-on-error` を使わず、失敗した検査の job も失敗させる（FR-022） | PASS |
| 原則 II: モジュールの導出 | ビルド・テスト・各検査の対象モジュールを `go.mod` の位置から導出し、固定の列挙を持たない（MUST / MUST NOT） | Makefile の `MODULES` を導出化する（research.md D8、FR-027）。現在はハードコードされており、本機能で是正する | PASS |
| ライセンス: 母集団 | 判定は「実際に import されるパッケージ」を母集団とし、モジュールグラフ全体を用いない（MUST / MUST NOT） | `go-licenses` の既定の挙動が母集団と一致する（research.md D1・D2）。spec の FR-007 にも規範として明記済み | PASS |

**違反なし。** Phase 1 設計後に再評価し、判定は変わっていない。

**憲章側の追随は完了している。** 本計画の作成時点で挙げていた 3 点（品質ゲート表への追加、
「全 5 モジュール」記述の見直し、依存ライセンスの母集団の明示）は憲章 v1.2.0 で取り込まれ、
続く v2.0.0 で脆弱性検査がマージ前ゲートへ移り `make check` の対象が明確化された。
したがって本計画は憲章の改訂を待つ必要がなく、**上表の 20 行はいずれも v2.0.0 の
現行条文に対する評価である**。逆向きの追随（本計画が憲章に合わせる側）は
[research.md](./research.md) D7 に記録した。

## プロジェクト構成

### 文書 (本機能)

```text
specs/001-constitution-compliance-gates/
├── plan.md              # This file (/speckit-plan command output)
├── research.md           # Phase 0 output
├── data-model.md         # Phase 1 output
├── quickstart.md         # Phase 1 output
├── contracts/            # Phase 1 output
│   ├── gates.md          # make ターゲットとコマンドの契約
│   └── allowlist.md      # 許容リストのファイル形式
├── checklists/
│   └── requirements.md   # /speckit-specify が作成した品質チェックリスト
└── tasks.md              # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### ソースコード (リポジトリルート)

```text
.golangci.yml                     # 新規: 静的解析と整形の設定 (version: "2")
licenses-allowlist.txt            # 新規: 許容ライセンスリスト（憲章の表と一対一）
Makefile                          # 変更: MODULES の導出化、check-* ターゲットの追加
.openapi-generator-ignore         # 変更: 新規の手書きファイルを登録
CHANGELOG.md                      # 変更: 利用者に見える変更の記録
README.md                         # 変更: 4 種の検査の実行方法を 1 か所に示す (SC-010)

LICENSE                           # 既存
misc/vault/LICENSE                # 新規（本計画の作成時に追加、D3）
misc/aws/LICENSE                  # 新規（同）
misc/azure/LICENSE                # 新規（同）
misc/gcp/LICENSE                  # 新規（同）

tools/
├── genexecuteall/                # 既存（構成の前例）
│   ├── doc.go
│   └── main.go
├── checkheaders/                 # 新規: ファイル冒頭の規約 (FR-001〜006, FR-018)
│   ├── doc.go
│   ├── main.go
│   ├── check.go                  # 判定ロジック（テスト対象）
│   └── check_test.go
└── checklicenses/                # 新規: 許容リストとの照合 (FR-007〜013)
    ├── doc.go
    ├── main.go                   # 入出力の接続のみ
    ├── allowlist.go              # リストの読み込み（テスト対象）
    ├── input.go                  # go-licenses の出力の解析（テスト対象）
    ├── verdict.go                # 判定ロジック（テスト対象）
    ├── allowlist_test.go
    ├── input_test.go
    └── verdict_test.go

internal/integration/            # 変更: 8 ファイルへ SPDX ヘッダを付与 (FR-005)
├── connectivity_test.go
├── delegations_test.go
├── helpers_test.go
├── lifecycle_test.go
├── main_test.go
├── mutex_test.go
├── readonly_test.go
└── zonelookup_test.go

.github/workflows/
├── checks.yml                    # 新規: headers / licenses / lint を統合前に走らせる
└── govulncheck.yml               # 新規: vuln を統合前に走らせる。統合を止める (D7)
```

**構成の決定**: 判定ロジックを `tools/` 配下の 2 つの `main` パッケージに置く。
既存の `tools/genexecuteall` が同じ位置・同じ構成の前例であり、ルートモジュールに
属するため `make build-all` / `make test` の既存の反復にそのまま乗る。判定と解析を
`check.go` / `allowlist.go` / `input.go` / `verdict.go` へ分けて `main.go` から
切り離すのは、単体テストを標準入出力を介さずに書けるようにするためである（憲章 II）。
`main.go` に残すのは入出力の接続だけとする。

とくに `go-licenses` の出力の解析を `input.go` として独立させるのは、上流の出力形式が
変わったときに壊れる箇所であり、列数の異常を終了コード 2 で中断する挙動
（[contracts/gates.md](./contracts/gates.md) の入力契約）をテストで固定する必要が
あるためである。これを `main.go` に置くと、標準入力を用意しないとテストできない。

CI のワークフローを `checks.yml` と `govulncheck.yml` の 2 つに分けるのは、脆弱性検査だけが
外部の脆弱性データベースの状態に依存し、コード変更なしに結果が変わるためである。
どちらも `pull_request` / `push` で統合を止めるゲートだが、別ファイルにしておくと
「コードは変わっていないのに落ちた」場合に、その検査だけを再実行して切り分けられる。

シェルスクリプトを置くディレクトリは作らない。判定をシェルで書くと憲章 II の
単体テスト要件を満たしにくく、除外規則が増えたときに壊れやすい
（[research.md](./research.md) D4）。

## 複雑さの記録

> **Fill ONLY if Constitution Check has violations that must be justified**

憲章違反なし。記載事項なし。

判断として却下した複雑化を 2 点記録する（いずれも [research.md](./research.md) に詳細）。

| 却下した選択 | 却下の理由 |
|---|---|
| ツール専用の独立モジュール（`tools/go.mod`） | 版固定のためだけにモジュールが 6 つになる。憲章の「全 5 モジュール」記述の改訂を伴い、対価が見合わない（D6） |
| `syft` + 許容リスト照合 | SBOM 生成と共用できる利点はあるが、判別不能が 15 件出てゲートにならず、モジュール帰属も取れないため FR-012 を実装できない（D1） |
