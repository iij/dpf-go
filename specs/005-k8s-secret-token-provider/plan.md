# 実装計画: Kubernetes Secret からのトークン取得

**ブランチ**: `005-k8s-secret-token-provider` | **日付**: 2026-09-18 | **仕様**: [spec.md](./spec.md)

**入力**: 次の機能仕様 `/specs/005-k8s-secret-token-provider/spec.md`

**注記**: `ブランチ` は `.specify/feature.json` が示す機能ディレクトリ名である。実際の
git ブランチは `main`（`before_specify` フックが無いためブランチは作成していない）。

## 概要

DPF-API のアクセストークンの取得元として、Kubernetes の Secret を 5 つ目の選択肢として
追加する。`misc/k8s` を独立したモジュールとして新設し、`misc/{vault,aws,azure,gcp}` と
同じ作法の `NewTokenProvider` を提供する。

技術的な方針は 4 つある。

1. **SDK は `k8s.io/client-go` v0.37.0 を用いる。** 通信・kubeconfig の解釈・
   クラスタ内資格情報の読み込み・エラーの分類という本機能が必要とする 4 つが 1 つの依存で
   揃う（[research.md](./research.md) D1）。**依存ライセンスは実測で許容リストに収まり、
   ソース提供義務を伴う依存は 1 件も入らない**（同 D2）。
2. **接続情報の選択は client-go に委ねる。** 候補の順序も接続設定の組み立ても
   `clientcmd.NewNonInteractiveDeferredLoadingClientConfig` に任せ、独自の順序を持たない。
   設定ファイル（`KUBECONFIG` が指すもの、無ければ `~/.kube/config`）を先に見て、
   そこから接続情報が得られない場合にクラスタ内資格情報を使う。利用者が普段使っている
   コマンドと同じ接続先を向くことが、驚きの少ない挙動である（[research.md](./research.md) D3）。
   設定ファイルが**存在しない**場合は候補が無いものとして次へ進み（FR-023）、
   **存在して解釈できない**場合はエラーで止まる（FR-022）。この区別も client-go の
   挙動をそのまま写したものである。
3. **名前空間の決定は自前で書き、ここだけ client-go に合わせない。** client-go の
   `Namespace()` は、設定ファイルの名前空間が空のときクラスタ内の名前空間へ回り込み、
   それも無ければ `"default"` を補う。しかも「明示された `default`」と区別できない。
   FR-013・FR-014 が禁じる回り込みと推測そのものであるため、解釈前の設定
   （`RawConfig()`）から直接読み、空ならエラーとする（同 D4）。**接続先を取り違えても
   認証が失敗して気づけるが、名前空間を取り違えると別の Secret を読んで成功しうる。**
   誤りの重さが違うため、接続先の選択だけを標準に合わせる。
4. **判定できるものはすべて生成時に寄せる。** 接続情報の不在、候補の失敗、名前空間の
   未決定は `New...` の戻り値のエラーとする。評価時にしか分からないのは Secret の
   内容と権限だけである（[data-model.md](./data-model.md) 第 5 節、SC-006）。

Secret の値の復号は自前で書かない。`corev1.Secret.Data` は `map[string][]byte` であり、
JSON デコードの時点で base64 が復号されている（同 D7）。FR-018 はこの経路で満たされる。

## 技術的な前提

**言語 / バージョン**: Go 1.27（各 `go.mod` は `go 1.27.0`、確認環境は go1.27.1 linux/amd64）

**主要な依存**: `k8s.io/client-go` v0.37.0（`k8s.io/api`、`k8s.io/apimachinery` を伴う）。
`misc/k8s/go.mod` にのみ現れる。**本体（`github.com/iij/dpf-go`）および他の `misc/*` の
`go.mod` は変更しない**

**保存先**: 該当なし（永続データを持たない。入力は Kubernetes API の応答と、
接続情報を置いた環境変数・ファイル）

**テスト**: `go test ./... -race -count=1`（既存の `make test`）。継ぎ目は自前の
1 メソッドのインターフェースで、手書きの差し替えを用いる（[research.md](./research.md) D9）。
接続情報のファイルパスはパッケージ内部で差し替え可能にする
（[contracts/credentials.md](./contracts/credentials.md) 第 3 節）

**対象環境**: Kubernetes クラスタ内の Pod、および開発者端末（Linux / macOS）。
同じコードが両方で動くことが利用シナリオ 2 の要件

**プロジェクト種別**: Go ライブラリ（`misc/` 配下の独立モジュール 1 件を追加）

**性能目標**: 取得処理の評価 1 回につき API サーバへの GET 1 回。生成時はネットワークへ
出ない（候補の判定はファイルの有無と環境変数のみ）。評価回数を抑えたい利用者は
`utils.WithTokenTTL` を使う

**制約**:
- 本体の `go.mod` に依存を追加しない（憲章 原則 V）。追加は `misc/k8s` に閉じる
- ライブラリ内でログを出力しない（同）。`unsafe` を使わない（同）
- 単体テストはネットワークも動作中のクラスタも必要としない（憲章 原則 II、FR-032）
- 探索順序は client-go（`DeferredLoadingClientConfig`）に委ね、独自の順序を持たない
  （research.md D3）。名前空間解決（`Namespace()`）だけは委ねられない。回り込みと
  `"default"` の補完が FR-013・FR-014 に反する（同 D4）
- 名前空間の導出元を決めるため、どの候補が選ばれたかを本パッケージ側でも判定する。
  判定条件は client-go の `Possible()` と一致させる（同 D5）
- `"default"` 名前空間を補わない。`POD_NAMESPACE` を参照しない（FR-014、同 D4・D6）
- エラーに Secret の値を含めない（FR-029）
- 既定値は保守的に定め、変更手段は Option として公開する（憲章 原則 V）

**規模 / 範囲**: モジュール 1 件の追加（リポジトリ内で計 6 件）。公開する関数 6 件
（コンストラクタ 2・アダプタ 1・オプション 3）、公開する型 2 件、番兵エラー 7 件、
import されるパッケージ 54 件（`misc/k8s` の母集団）、要件 36 件（FR）／成功基準 18 件（SC）。
変更する既存ファイルは 5 件（`README.md`、`doc.go`、`utils/doc.go`、`CHANGELOG.md`、
`.github/workflows/sbom.yml`）

**要明確化**: なし。仕様に未解決のマーカーは無い。計画に委ねられていた SDK の選定は
[research.md](./research.md) D1 で、公開 API の形は D10 で決定した。仕様の作成中に
残っていた名前空間の既定値の扱いは、利用者との確認により FR-014 として確定している。

## 憲章への適合確認

*ゲート: フェーズ 0 の調査より前に通過しなければならない。フェーズ 1 の設計後に再確認する。*

憲章 **v2.0.1** に対する評価。フェーズ 1 の設計後に再確認し、判定は変わっていない。

| 原則・節 | 要求 | 本計画での扱い | 判定 |
|---|---|---|---|
| I. 生成コードと手書きコードの分離 | 生成物を直接編集しない。手書きファイルは `.openapi-generator-ignore` に登録する | 生成物を編集しない。`.openapi-generator-ignore` の 37 行目に `misc/**` があり、新規ファイルは既に保護対象。**変更不要**（research.md D11） | PASS |
| II. テストを伴う実装 | 手書きコードに単体テストを付ける。`make test` はネットワーク不要 | 公開挙動すべてに単体テストを付ける。継ぎ目は自前インターフェース、接続情報のパスは差し替え可能にし、実ファイルシステムの固定パスに依存しない（D9、contracts/credentials.md 第 3 節） | PASS |
| II. 統合テストの隔離 | 実 API に依存するテストは `integration` タグで隔離 | 単体テストは実クラスタに触れない。実クラスタでの確認は quickstart.md の手動手順とし、`make test` の対象にしない | PASS |
| II. モジュールの導出 | 対象モジュールを `go.mod` の位置から導出し、固定の列挙を持たない | Makefile の `MODULES` は `find` による導出済み（specs/001）。`misc/k8s/go.mod` を置くだけで全ゲートの対象に入る。**Makefile は変更不要**（FR-005、SC-016） | PASS |
| III. godoc とドキュメント | 公開要素に godoc、各パッケージに `doc.go`、全 Go ファイルに SPDX、記述は日本語 | `misc/k8s` に `doc.go` を置き、公開要素すべてに godoc を付ける。全ファイルに SPDX ヘッダ。記述は日本語 | PASS |
| III. 同時更新 | 利用者に見える挙動の変更は `CHANGELOG.md` と必要なら `README.md` を同じ変更で更新 | 利用者に見える機能追加である。`CHANGELOG.md`、`README.md`（2 か所）、`doc.go`、`utils/doc.go` を同じ変更で更新する（FR-036、D11） | PASS |
| IV. ドメイン名は miekg/dns | ドメイン名操作に `strings` を使わない | 本機能はドメイン名を扱わない。扱うのは名前空間名・Secret 名・キー名であり DNS 名ではない | N/A |
| V. `unsafe` 不使用 | `unsafe` を使わない | 使わない（FR-034） | PASS |
| V. ログ出力の禁止 | ライブラリ内でログを出力しない | 出力しない（FR-035）。異常は error として返す。**client-go は内部で klog を用いるが、本パッケージが出力するものではない**（後述） | PASS |
| V. 既定値と Option | 既定値は保守的に、変更手段は Option として公開 | Secret 名 `dpf-token`、キー `token`、名前空間は認証情報の値を既定とし、いずれも Option で変更可能（FR-010・FR-016・FR-017） | PASS |
| V. 本体への依存追加の禁止 | 本体が依存する外部パッケージを増やさない | 本体の `go.mod` を変更しない。`k8s.io/*` は `misc/k8s` にのみ現れる（FR-002、SC-003） | PASS |
| V. optional な機能の隔離 | 他パッケージに依存する機能は `misc/` 配下の独立した `go.mod` を持つモジュールへ | `misc/k8s` として新設する。他の `misc/*` の SDK を持ち込まない（FR-001・FR-003、SC-004） | PASS |
| セキュリティ | トークンは API リクエストのたびに評価する | 取得処理は評価のたびに問い合わせ、内部に保持しない（FR-007）。保持は `utils.WithTokenTTL` の責務 | PASS |
| セキュリティ | 検出値・秘密をログや出力に出さない | エラーに Secret の値を含めない（FR-029、SC-018）。名前空間・Secret 名・キー名は値ではないため含める | PASS |
| ライセンス: SPDX | すべての Go ファイルの先頭に `// SPDX-License-Identifier: Apache-2.0` | 新規ファイルすべてに付与（FR-033、SC-014）。`make check-headers` が機械的に検証する | PASS |
| ライセンス: LICENSE の同梱 | 独立して取得した利用者へライセンスが届く状態を保つ | `misc/k8s/LICENSE` をルートの複製として置く（FR-004、SC-005）。他 4 モジュールと同じ扱い | PASS |
| ライセンス: 許容リスト | 依存は Apache-2.0 / MIT / BSD-2 / BSD-3 / ISC / MPL-2.0 に限る。判別不能を持ち込まない | **実測済み**（research.md D2）。Apache-2.0 30 / BSD-3-Clause 17 / MIT 6 / ISC 1 の計 54 件、すべて notice 区分。MPL-2.0・GPL 系・判別不能は 0 件。憲章の改訂を要しない | PASS |
| ライセンス: reciprocal の隔離 | ソース提供義務を伴う依存は本体に現れない | `misc/k8s` は MPL-2.0 を 1 件も持ち込まない。`misc/vault` と異なり、この観点の負担が無い（FR-006 相当、SC-003） | PASS |
| ライセンス: 母集団 | 判定は「実際に import されるパッケージ」を母集団とする | D2 の実測は `go-licenses csv ./...`（既存の `make check-licenses` と同じ母集団）で行った | PASS |
| Go コード品質 | `gofmt` / `golangci-lint` / `govulncheck` を基準とし、判断を人の裁量で覆さない | 既存の `.golangci.yml` と `make check-lint` の対象に自動で入る。`govulncheck` は probe で**到達可能な既知脆弱性 0 件**を確認済み（D2） | PASS |
| 品質ゲート（マージ前 7 種） | ビルド / 単体テスト / 整形・静的解析 / 冒頭の規約 / 依存ライセンス / 脆弱性 / シークレット検査 | `MODULES` の導出により 7 ゲートすべてに自動で乗る。ゲート側の変更は無い。検証手順は [quickstart.md](./quickstart.md) 第 1 節 | PASS |
| サービスの呼称 | 「IIJ DNSプラットフォームサービス」「IIJ DNS Platform Service」「DPF」のいずれかを用いる | 新規の文書・godoc・コメントで遵守する。既定の Secret 名 `dpf-token` は利用者が指定した値であり、略称 `DPF` の用法として整合する | PASS |
| 利用者向け文書（README） | サポート範囲の注記とライセンスの明示を含む | 既存の記載を変更しない。追加するのは `misc/k8s` の行のみ | PASS |

**違反なし。** フェーズ 1 の設計後に再評価し、判定は変わっていない。

**`klog` についての補足**: 憲章 原則 V の「ライブラリ内でログを出力してはならない」は
本リポジトリのコードに対する規範である。`k8s.io/client-go` は内部で `k8s.io/klog/v2` を
用いており、一部の経路（`rest.InClusterConfig` が CA を読めなかった場合など）で
標準エラー出力へ書く。これは依存の挙動であり、本パッケージが出力するものではない。
本パッケージ側で `klog` を呼ばず、抑止のための大域的な設定も行わない
（利用者のプロセス全体の設定を書き換えることになり、原則 V の「呼び出し側の環境を
侵さない」に反する）。この事実は `doc.go` に記載し、利用者が自分のプロセスで
`klog` を設定できるようにする。

ただし、**本パッケージが引き起こす経路が 1 つだけあり、これは塞ぐ**。設定ファイルが
すべて欠けている場合、`ClientConfigLoadingRules.Load()` は `Warner` を呼び、
`Warner` が未設定なら `klog.V(1)` へ書く。FR-023 により「設定ファイルが無い」は
正常な経路であるため、警告が出るのは不適切である。`ClientConfigLoadingRules.Warner`
に何もしない関数を設定して黙らせる。大域の `klog` 設定には触れないため、利用者の
プロセスへの副作用は無い。既定の詳細度では `V(1)` は出力されないが、詳細度を上げた
利用者に対しても本パッケージ由来の出力を出さないためである。

**憲章側の追随（本機能の範囲外）**: 憲章 原則 II が参考値として「現在の実数は 5 件
（本体と `misc/` 配下の 4 つ）である」と記している。本機能でモジュールが 6 件になるため
この数値が古くなる。規範は「`go.mod` の位置から導出する」であって固定の数ではないため
違反ではないが、憲章の PATCH 改訂（意味を変えない数値の更新）が望ましい。
実装とは別の Pull Request とする（research.md D11）。

## プロジェクト構成

### 文書 (本機能)

```text
specs/005-k8s-secret-token-provider/
├── plan.md                  # 本ファイル (/speckit-plan の出力)
├── research.md              # フェーズ 0 の出力。D1〜D11
├── data-model.md            # フェーズ 1 の出力
├── quickstart.md            # フェーズ 1 の出力。検証手順
├── contracts/               # フェーズ 1 の出力
│   ├── api.md               # 公開 API の契約
│   └── credentials.md       # 接続情報の選択と名前空間の決定の契約
├── checklists/
│   └── requirements.md      # /speckit-specify が作成した品質チェックリスト
└── tasks.md                 # フェーズ 2 の出力 (/speckit-tasks。/speckit-plan では作られない)
```

### ソースコード (リポジトリルート)

```text
misc/k8s/                        # 新規モジュール
├── go.mod                       # module github.com/iij/dpf-go/misc/k8s
├── go.sum
├── LICENSE                      # ルートの複製 (FR-004)
├── doc.go                       # パッケージ文書 (FR-031)。klog の注記を含む
├── token.go                     # SecretsAPI / NewSecretsAPI / Option / NewTokenProvider
│                                #   / 番兵エラー / Secret からの取り出し
├── token_test.go
├── credentials.go               # 候補の選択 / 名前空間の決定
│                                #   / NewTokenProviderFromEnvironment
└── credentials_test.go

README.md                        # 変更: go get の一覧 (33-36 行) と対応表 (88-91 行) に
                                 #   misc/k8s を追加。186 行「計 5 モジュール」→ 6
doc.go                           # 変更: 103 行の misc 列挙に misc/k8s を追加
utils/doc.go                     # 変更: 67-70 行の misc 列挙に misc/k8s を追加
CHANGELOG.md                     # 変更: [Unreleased] の Added に本機能を追記
.github/workflows/sbom.yml       # 変更: 6 行目のコメント「計 5 つの go.mod」→ 6 つ
                                 #   生成はディレクトリ全体を走査するため、他の変更は不要

Makefile                         # 変更なし。MODULES は go.mod の位置から導出される
.openapi-generator-ignore        # 変更なし。37 行目の misc/** が新規ファイルを保護する
```

**構成の決定**: `misc/k8s` を `misc/{vault,aws,azure,gcp}` と同じ構成
（`doc.go` / `go.mod` / `go.sum` / `LICENSE` + 実装 + テスト）に揃える。パッケージ名は
`k8s`、モジュールパスは `github.com/iij/dpf-go/misc/k8s` とする。既存 4 モジュールが
サービス名をそのままディレクトリ名・パッケージ名にしているのに倣う。

既存 4 モジュールが実装を `token.go` の 1 ファイルに収めているのに対し、本モジュールは
`token.go` と `credentials.go` に分ける。分ける理由は、接続情報の取得と名前空間の決定が
**実行環境（環境変数とファイルパス）を読む唯一の部分**であり、単体テストのために
探索規則とパスを差し替え可能にする必要があるためである
（[contracts/credentials.md](./contracts/credentials.md) 第 3 節）。これを `token.go` に
混ぜると、Secret の取り出しのテストにも環境の準備が要る。既存 4 モジュールは
クライアントを必須の引数に取り、実行環境を読まないため 1 ファイルで足りている。

`NewTokenProvider`（クライアント注入）を `token.go` に、
`NewTokenProviderFromEnvironment`（自動選択）を `credentials.go` に置く。前者は既存 4
モジュールと同形で実行環境に触れず、後者だけが触れる。この境界がファイルの分割と一致する。

## 複雑さの記録

> **憲章への適合確認で、正当化が必要な逸脱があった場合にのみ記入する**

憲章違反なし。記載事項なし。

判断として却下した選択を 4 点記録する（いずれも [research.md](./research.md) に詳細）。

| 却下した選択 | 却下の理由 |
|---|---|
| 独自の探索順序（`KUBECONFIG` → クラスタ内 → `~/.kube/config`）を実装する | 初稿の案。`KUBECONFIG` と `~/.kube/config` の間にクラスタ内資格情報を挟む形は標準的なクライアントに存在せず、同じ環境で利用者のコマンドと本ライブラリが別の接続先を向きうる。利用者との確認により却下し、client-go の順序へ合わせた（D3） |
| client-go の `Namespace()` に委ねる | 決まらないときにクラスタ内の名前空間へ回り込み、最後は黙って `"default"` を返す。明示された `default` とも区別できない。FR-013・FR-014 を満たせない（D4） |
| 公式の fake clientset（`client-go/kubernetes/fake`）でテストする | 継ぎ目が client-go の型に固定され、利用者が自分の実装を差し込めない。既存 4 モジュールの作法から外れ、テスト専用の依存も増える。メソッド 1 つの継ぎ目に対して重い（D9） |
| API サーバへ自前で HTTP リクエストを送り依存を最小化する | 依存 54 件は削れるが、kubeconfig の形式（merge 規則・exec credential plugin・client 証明書）と TLS の組み立てを自前で持つことになる。非互換は「利用者の既存の設定が動かない」形で現れる（D1） |
