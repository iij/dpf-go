---

description: "Kubernetes Secret からのトークン取得の実装タスク"
---

# タスク: Kubernetes Secret からのトークン取得

**入力**: `/specs/005-k8s-secret-token-provider/` の設計文書

**前提**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**テスト**: **含める。** 憲章 原則 II が「手書きコードは単体テストを伴わなければ完成とみなさない」と定め、仕様も FR-032 で単体テストを要求している。任意ではなく必須である。

**構成**: タスクは利用シナリオごとにまとめる。シナリオ単位で独立して実装・検証できるようにするためである。

## 書式: `[ID] [P?] [シナリオ] 説明`

- **[P]**: 並行して実行できる (異なるファイル、依存なし)
- **[シナリオ]**: このタスクが属する利用シナリオ (US1、US2、US3)
- 説明には正確なファイルパスを含める

## パスの規約

本機能は Go の複数モジュール構成のライブラリである。新規モジュールは `misc/k8s/`
（モジュールパス `github.com/iij/dpf-go/misc/k8s`、パッケージ名 `k8s`）に置く。
既存 4 モジュール（`misc/{vault,aws,azure,gcp}`）と同じ構成に揃える。
テストは実装と同じディレクトリに `*_test.go` として置く（Go の慣習。`tests/` は作らない）。

---

## フェーズ 1: 準備 (共通の基盤)

**目的**: モジュールの器を作り、依存を確定させる

- [X] T001 `misc/k8s/` ディレクトリを作り、`misc/k8s/go.mod` に `module github.com/iij/dpf-go/misc/k8s` と `go 1.27.0` を書く（既存の `misc/azure/go.mod` と同じ形）
- [X] T002 `misc/k8s/` で `go get k8s.io/client-go@v0.37.0` を実行し、`misc/k8s/go.sum` を生成する（research.md D1 で確定した版）
- [X] T003 [P] リポジトリルートの `LICENSE` を `misc/k8s/LICENSE` へ複製する（FR-004、SC-005。他 4 モジュールと同じ扱い）
- [X] T004 [P] `misc/k8s/doc.go` に SPDX ヘッダと `package k8s` の骨格を置き、パッケージがビルドできる状態にする（内容は T047 で完成させる）

**関門**: `cd misc/k8s && go build ./...` が通り、`make build-all` の対象に `misc/k8s` が現れる

---

## フェーズ 2: 土台 (先行必須)

**目的**: US1 と US2 の双方が依存する型・エラー・オプションを用意する

**⚠️ 重要**: このフェーズが完了するまで、利用シナリオの作業は開始できない

- [X] T005 `misc/k8s/token.go` に 7 つの番兵エラー（`ErrNilClient`、`ErrNoCredentials`、`ErrNamespaceUnknown`、`ErrSecretNotFound`、`ErrKeyNotFound`、`ErrEmptySecret`、`ErrForbidden`）を godoc 付きで定義する（contracts/api.md のエラー節）
- [X] T006 `misc/k8s/token.go` に `SecretsAPI` インターフェース（`Get(ctx, namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error)`）を godoc 付きで定義する（FR-009、research.md D9）
- [X] T007 `misc/k8s/token.go` に `NewSecretsAPI(c kubernetes.Interface) SecretsAPI` を実装する。`c.CoreV1().Secrets(ns).Get(...)` へ委譲するだけで判断を持たない（contracts/api.md）
- [X] T008 `misc/k8s/token.go` に非公開の `config` 構造体（`namespace`、`secretName`、`key`）を定義し、既定値を `secretName = "dpf-token"`、`key = "token"` として初期化する関数を置く（FR-016、FR-017、data-model.md 第 1 節）
- [X] T009 `misc/k8s/token.go` に `Option` 型と `WithNamespace` / `WithSecretName` / `WithKey` を godoc 付きで実装する。**いずれも空文字を渡された場合はその指定を無視する**（FR-010、FR-011）
- [X] T010 [P] `misc/k8s/token_test.go` に、3 つのオプションへ空文字を渡したとき既定値が保たれることのテーブル駆動テストを書く（FR-011、受け入れシナリオ 1-14）

**関門**: 土台の型が揃い、US1 と US2 を並行して開始できる

---

## フェーズ 3: 利用シナリオ 1 - Secret に置いたトークンで API を呼べる (優先度: P1) 🎯 MVP

**目標**: 利用者が `SecretsAPI` と名前空間を渡すと、Secret の `token` からトークンを取得する処理が得られる。取得処理は `utils.WithTokenProvider` にそのまま渡せる。

**独立した検証**: `SecretsAPI` を手書きの差し替えにして取得処理を作り、期待するトークンが返ることを確認する。4 つのエラー原因が `errors.Is` で区別できることを確認する。動作中のクラスタは不要。

### 利用シナリオ 1 のテスト ⚠️

> **注記: これらのテストを先に書き、実装前に失敗することを確認する**

- [X] T011 [P] [US1] `misc/k8s/token_test.go` に `SecretsAPI` の手書きの差し替え（呼び出された `namespace` / `name` / `opts` を記録し、任意の `*corev1.Secret` とエラーを返す）を作る
- [X] T012 [P] [US1] `misc/k8s/token_test.go` に取得の成功のテストを書く。`Data["token"]` の値が返ること、`Data` が復号済みのバイト列として扱われ符号化された文字列がそのまま返らないこと（受け入れシナリオ 1-1、1-2、SC-010）
- [X] T013 [P] [US1] `misc/k8s/token_test.go` に既定値のテストを書く。Secret 名・キーを指定しないとき `dpf-token` と `token` が問い合わせに使われること（受け入れシナリオ 1-3、1-4、SC-001）
- [X] T014 [P] [US1] `misc/k8s/token_test.go` にエラーの区別のテストを書く。`IsNotFound` → `ErrSecretNotFound`、キー欠落 → `ErrKeyNotFound`、長さ 0 → `ErrEmptySecret`、`IsForbidden` / `IsUnauthorized` → `ErrForbidden`、その他 → 元のエラーが `errors.Is` で辿れること（受け入れシナリオ 1-7〜1-11、SC-009）
- [X] T015 [P] [US1] `misc/k8s/token_test.go` にエラーの文言に Secret の値が含まれないことのテストを書く（FR-029、受け入れシナリオ 1-12、SC-018）
- [X] T016 [P] [US1] `misc/k8s/token_test.go` に `WithKey` で `token` 以外のキーから取り出せることのテストを書く（FR-017、受け入れシナリオ 1-13）
- [X] T017 [P] [US1] `misc/k8s/token_test.go` に、取得処理を複数回評価するとそのたびに問い合わせが起き、差し替え側で値を変えると反映されることのテストを書く（FR-007、受け入れシナリオ 1-6）
- [X] T018 [P] [US1] `misc/k8s/token_test.go` に、打ち切った `context` を渡すと評価が打ち切りに応じることのテストを書く（FR-008、受け入れシナリオ 1-15）
- [X] T019 [P] [US1] `misc/k8s/token_test.go` に、`client` が `nil` のとき生成時に `ErrNilClient`、`WithNamespace` が無いとき生成時に `ErrNamespaceUnknown` が返ることのテストを書く（FR-014、SC-006、contracts/api.md）

### 利用シナリオ 1 の実装

- [X] T020 [US1] `misc/k8s/token.go` に Secret から値を取り出す非公開関数を実装する。`Data[key]` を引き、キー欠落と長さ 0 を区別する。`StringData` は参照しない（FR-018、FR-025、FR-026、research.md D7）
- [X] T021 [US1] `misc/k8s/token.go` に API サーバの失敗を分類する非公開関数を実装する。`k8s.io/apimachinery/pkg/api/errors` の `IsNotFound` / `IsForbidden` / `IsUnauthorized` を用い、元のエラーを `%w` で包む（FR-024、FR-027、FR-028、research.md D8 の対応表）
- [X] T022 [US1] `misc/k8s/token.go` に `NewTokenProvider(client SecretsAPI, opts ...Option) (func(ctx context.Context) (string, error), error)` を実装する。`client` が `nil` なら `ErrNilClient`、名前空間が未指定なら `ErrNamespaceUnknown` を**生成時に**返す（FR-006、FR-019、contracts/api.md）
- [X] T023 [US1] `misc/k8s/token.go` の取得処理の本体で、解決済みの名前空間・Secret 名・キーを用いて `client.Get` を呼び、T020・T021 を通して文字列を返す。トークンを内部に保持しない（FR-007、FR-008）
- [X] T024 [US1] `cd misc/k8s && go test ./... -race -count=1` を実行し、T011〜T019 のすべてが通ることを確認する

**関門**: `NewTokenProvider` が単独で完全に機能する。利用者は自分で用意した clientset から
トークン取得処理を得て `utils.WithTokenProvider` へ渡せる。ここまでが MVP である。

---

## フェーズ 4: 利用シナリオ 2 - 実行環境に応じて接続情報が自動で選ばれる (優先度: P2)

**目標**: 引数なしで `NewTokenProviderFromEnvironment()` を呼ぶと、Kubernetes の標準的な
クライアントと同じ順序で接続情報が選ばれ、選ばれた認証情報の名前空間が既定として使われる。

**独立した検証**: 環境変数と一時ディレクトリのファイルを組み合わせた 8 通りで、選ばれる
候補が一意に定まり標準的なクライアントの選択と一致することを確認する。名前空間が決まらない
場合に生成時エラーになることを確認する。動作中のクラスタは不要。

### 利用シナリオ 2 のテスト ⚠️

- [X] T025 [P] [US2] `misc/k8s/credentials.go` に、テストから差し替えるための非公開の変数（クラスタ内のトークンと名前空間のファイルパス、`ClientConfigLoadingRules` を作る関数）を宣言する。公開 API には出さない（contracts/credentials.md 第 3 節）
- [X] T026 [P] [US2] `misc/k8s/credentials_test.go` に、一時ディレクトリへ kubeconfig・トークンファイル・名前空間ファイルを書き、環境変数を `t.Setenv` で設定する補助関数を作る
- [X] T027 [P] [US2] `misc/k8s/credentials_test.go` に候補の選択 8 通りのテーブル駆動テストを書く。`KUBECONFIG` × `~/.kube/config` × クラスタ内の可否の組み合わせで、選ばれる候補が contracts/credentials.md 第 1 節の表と一致すること（受け入れシナリオ 2-1〜2-5、SC-007、SC-008）
- [X] T028 [P] [US2] `misc/k8s/credentials_test.go` に、`KUBECONFIG` が設定されているとき `~/.kube/config` が読まれないことのテストを書く（受け入れシナリオ 2-1）
- [X] T029 [P] [US2] `misc/k8s/credentials_test.go` に、設定ファイルが壊れている場合はエラーになりクラスタ内の資格情報へ移らないこと、存在しない場合は次の候補へ進むことのテストを書く（FR-022、FR-023、受け入れシナリオ 2-6、2-7）
- [X] T030 [P] [US2] `misc/k8s/credentials_test.go` に、候補が 1 つも無いとき生成時に `ErrNoCredentials` が返ることのテストを書く。`clientcmd.IsEmptyConfig` の判別に依存する箇所である（FR-021、受け入れシナリオ 2-5、SC-006）
- [X] T031 [P] [US2] `misc/k8s/credentials_test.go` に名前空間の導出のテストを書く。設定ファイルが選ばれた場合は選択中の context の `namespace`、クラスタ内が選ばれた場合は名前空間ファイルの値（前後の空白を除去）が使われること（FR-013、受け入れシナリオ 2-9、2-10、SC-009）
- [X] T032 [P] [US2] `misc/k8s/credentials_test.go` に名前空間が決まらない場合のテストを書く。context に `namespace` が無い、名前空間ファイルが読めない、`CurrentContext` が空、`Contexts` に該当が無い、のいずれでも生成時に `ErrNamespaceUnknown` になり `default` を補わないこと（FR-014、受け入れシナリオ 2-11、2-13、SC-009）
- [X] T033 [P] [US2] `misc/k8s/credentials_test.go` に、候補をまたいで名前空間を回り込まないことのテストを書く。設定ファイルを選ばせ、その context に `namespace` を書かず、名前空間ファイルは読める状態にして `ErrNamespaceUnknown` になること（受け入れシナリオ 2-12、research.md D4）
- [X] T034 [P] [US2] `misc/k8s/credentials_test.go` に、`POD_NAMESPACE` を設定しても参照されないことのテストを書く（FR-013、research.md D6）
- [X] T035 [P] [US2] `misc/k8s/credentials_test.go` に、到達しない接続先を指す設定でも生成が成功すること（生成時にネットワークへ出ない）のテストを書く（SC-012）
- [X] T036 [P] [US2] `misc/k8s/credentials_test.go` に、`WithNamespace` を明示すれば認証情報に名前空間の値が無くてもエラーにならないことのテストを書く（受け入れシナリオ 2-14）

### 利用シナリオ 2 の実装

- [X] T037 [US2] `misc/k8s/credentials.go` に接続設定を得る非公開関数を実装する。`clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{})` の `ClientConfig()` を用い、選択と組み立てを client-go へ委ねる。`clientcmd.IsEmptyConfig(err)` を `ErrNoCredentials` へ、それ以外のエラーは包んで返す（FR-020、FR-021、FR-022、research.md D3）
- [X] T038 [US2] `misc/k8s/credentials.go` で `ClientConfigLoadingRules.Warner` に何もしない関数を設定する。設定ファイルがすべて欠けている場合は FR-023 により正常な経路であり、client-go が `klog` へ警告を書くのを防ぐ。大域の `klog` 設定には触れない（FR-035、憲章 原則 V）
- [X] T039 [US2] `misc/k8s/credentials.go` に、どの候補が選ばれたかを判定する非公開関数を実装する。判定条件は client-go の `inClusterClientConfig.Possible()`（トークンファイルが存在しディレクトリでない、かつ `KUBERNETES_SERVICE_HOST` と `KUBERNETES_SERVICE_PORT` がいずれも空でない）と一致させる（research.md D5）
- [X] T040 [US2] `misc/k8s/credentials.go` に名前空間を導出する非公開関数を実装する。設定ファイルが選ばれた場合は `RawConfig()` の `Contexts[CurrentContext].Namespace` を直接読み、クラスタ内が選ばれた場合は名前空間ファイルを読む。空・読めない場合は `ErrNamespaceUnknown` とし、`Namespace()` は使わず `default` も補わない。候補をまたいで回り込まない（FR-013、FR-014、research.md D4）
- [X] T041 [US2] `misc/k8s/credentials.go` に `NewTokenProviderFromEnvironment(opts ...Option) (func(ctx context.Context) (string, error), error)` を実装する。T037 で接続設定を得て clientset を作り、T007 の `NewSecretsAPI` で包み、T039・T040 で名前空間を決めてから T022 の生成処理へ渡す（contracts/api.md、contracts/credentials.md 第 2 節の決定の順序）
- [X] T042 [US2] `cd misc/k8s && go test ./... -race -count=1` を実行し、T027〜T036 のすべてが通ることを確認する

**関門**: 引数なしの `NewTokenProviderFromEnvironment()` がクラスタ内と手元の双方で動作する。
US1 と US2 の双方が独立して機能している。

---

## フェーズ 5: 利用シナリオ 3 - 使わない利用者に依存を持ち込まない (優先度: P3)

**目標**: 本体モジュールに Kubernetes の SDK が現れず、本モジュールに他のシークレット管理
サービスの SDK が現れない。モジュールの追加でゲートの対象一覧を書き換える必要がない。

**独立した検証**: `go list -deps` による依存の確認、`LICENSE` の存在、独立したビルドとテスト、
および `make build-all` / `make test` の対象に自動で含まれることの確認。

- [X] T043 [P] [US3] リポジトリルートで `go list -deps ./...` を実行し、`k8s.io/` で始まるパッケージが 0 件であることを確認する。ルートの `go.mod` に差分が無いことも確認する（FR-002、SC-003）
- [X] T044 [P] [US3] `cd misc/k8s && go list -deps ./...` を実行し、HashiCorp・AWS・Azure・Google Cloud の SDK が 0 件であることを確認する（FR-003、SC-004）
- [X] T045 [P] [US3] `cd misc/k8s && go build ./... && go test ./... -race -count=1` を実行し、独立して成功しネットワークも動作中のクラスタも要さないことを確認する（FR-032、SC-011、SC-014）
- [X] T046 [US3] `make build-all` と `make test` を実行し、`MODULES` を書き換えずに `misc/k8s` が対象に含まれることを確認する（FR-005、SC-016）

**関門**: すべての利用シナリオが独立して機能し、分離が保たれている

---

## フェーズ 6: 仕上げと横断的な事項

**目的**: 憲章が要求する文書・ゲート・呼称の整合を取る

- [X] T047 [P] `misc/k8s/doc.go` を完成させる。用途・使い方・関連パッケージ、既定値（`dpf-token` / `token` / 認証情報の名前空間）、接続情報の選択順序、`utils.WithTokenProvider` への渡し方を記述する。**client-go が内部で `klog` へ書く経路があること**と、本パッケージは `klog` を呼ばず大域設定も変えないことを注記する（FR-031、憲章 原則 III、plan.md の klog の補足）
- [X] T048 [P] `misc/k8s/*.go` のすべてのファイル先頭に `// SPDX-License-Identifier: Apache-2.0` があることを確認し、公開された型・関数・エラー値すべてに godoc があることを確認する（FR-030、FR-033、SC-012、SC-013）
- [X] T049 [P] `README.md` の 33-36 行の `go get` 一覧に `github.com/iij/dpf-go/misc/k8s` を追加する（FR-036）
- [X] T050 [P] `README.md` の 88-91 行の対応表に `misc/k8s` の行（Kubernetes Secret からトークンを取得）を追加する（FR-036）
- [X] T051 [P] `README.md` の 186 行「root と `misc/*` の計 5 モジュール」を「計 6 モジュール」へ改める（research.md D11）
- [X] T052 [P] `.github/workflows/sbom.yml` の 6 行目のコメント「root と misc/* の計 5 つの go.mod」を「計 6 つ」へ改める。SBOM の生成自体はディレクトリ全体を走査するため、コメント以外の変更は不要
- [X] T053 [P] `doc.go` の 103 行の misc の列挙（`misc/vault`、`misc/aws`、`misc/azure`、`misc/gcp`）に `misc/k8s` を追加する（FR-036、憲章 原則 III）
- [X] T054 [P] `utils/doc.go` の 67-70 行の misc の一覧に `misc/k8s : Kubernetes Secret` の行を追加する（FR-036）
- [X] T055 `CHANGELOG.md` の `[Unreleased]` の `Added` に本機能を追記する。取得元の追加、既定値、接続情報の選択順序が標準的なクライアントに従うことを含める（FR-036、憲章 原則 III）
- [X] T056 `misc/k8s/doc.go`、`README.md`、`doc.go`、`utils/doc.go`、`CHANGELOG.md`、`misc/k8s/*.go` のコメントで、サービスの呼称が「IIJ DNSプラットフォームサービス」「IIJ DNS Platform Service」「DPF」のいずれかであることを確認する（憲章「サービスの呼称」）
- [X] T057 `make check-licenses` を実行し、`misc/k8s` の依存が research.md D2 の実測（Apache-2.0 30 / BSD-3-Clause 17 / MIT 6 / ISC 1、MPL-2.0 と判別不能は 0 件）と一致することを確認する（SC-005、quickstart.md 第 1 節）
- [X] T058 `make check-vuln` を実行し、到達可能な既知脆弱性が 0 件であることを確認する
- [X] T059 `make check` を実行し、マージ前の 7 ゲートすべてが通ること（`RESULT: ok`）と、検査前後で作業ツリーの差分が 0 件であることを確認する（憲章「開発ワークフローと品質ゲート」）
> **T060 は未実施。** 実クラスタを要するため、本セッションでは実行していない。
> 単体テストと 7 ゲートはすべて通過している。

- [ ] T060 [quickstart.md](./quickstart.md) 第 4 節の手動確認を実クラスタで実行する。手元からの取得、クラスタ内からの取得、`ErrForbidden` / `ErrSecretNotFound` / `ErrKeyNotFound` の発生、Secret 更新の反映、および標準的なクライアントと同じ接続先を選ぶこと（SC-008）を確認する。**確認後は Secret を削除し、必要ならトークンを再発行する**

---

## 依存関係と実行順序

### フェーズ間の依存

- **準備 (フェーズ 1)**: 依存なし。ただちに開始できる
- **土台 (フェーズ 2)**: 準備の完了に依存する。すべての利用シナリオを塞ぐ
- **利用シナリオ (フェーズ 3〜5)**: いずれも土台の完了に依存する
- **仕上げ (フェーズ 6)**: 実施すると決めたすべての利用シナリオの完了に依存する

### 利用シナリオ間の依存

- **US1 (P1)**: 土台の後に開始できる。他のシナリオに依存しない。**単独で MVP として成立する**
- **US2 (P2)**: 土台の後に開始できる。ただし T041（`NewTokenProviderFromEnvironment`）は US1 の T022 が作る生成処理を呼ぶため、**T041 のみ T022 に依存する**。それ以外の US2 のタスクは US1 と並行できる
- **US3 (P3)**: 土台の後に開始できる。ただし検証の対象が US1・US2 の実装であるため、**実質的には両方の完了後に行う**。T043・T044・T046 は T001〜T002 の直後にも先行して実行でき、依存の混入を早期に検出できる

### 各利用シナリオの内部

- テストを先に書き、実装前に失敗することを確認する（憲章 原則 II）
- 土台の型（T005〜T009）は、US1・US2 の実装より先
- `credentials.go` の差し替え用の変数（T025）は、US2 のテスト（T026 以降）より先
- 分類・取り出しの非公開関数（T020・T021）は、取得処理の本体（T023）より先
- 接続設定の取得（T037）と候補の判定（T039）・名前空間の導出（T040）は、`NewTokenProviderFromEnvironment`（T041）より先

### 並行できる箇所

- T003・T004 は並行できる（異なるファイル）
- **US1 のテスト T011〜T019 はすべて並行できる**が、いずれも `misc/k8s/token_test.go` の同一ファイルへ書くため、1 人で進める場合は順に追記する。複数人で分担する場合はファイルを分ける（例: `token_read_test.go` / `token_error_test.go`）
- **US2 のテスト T026〜T036 はすべて並行できる**。同様に `misc/k8s/credentials_test.go` の同一ファイルである点に注意する
- US3 の T043〜T045 は並行できる
- 仕上げの T047〜T054 は並行できる（異なるファイル）。T055（`CHANGELOG.md`）は内容が他の変更に依存するため最後に寄せる
- T057〜T059 は逐次実行する（いずれもリポジトリ全体を対象とし、実行時間が長い）

---

## 並行実行の例: 利用シナリオ 1

```bash
# 土台の完了後、US1 のテストをまとめて書き始める:
Task: "SecretsAPI の手書きの差し替えを misc/k8s/token_test.go に作る"          # T011
Task: "取得の成功と復号のテストを misc/k8s/token_test.go に書く"                # T012
Task: "既定値のテストを misc/k8s/token_test.go に書く"                          # T013
Task: "エラーの区別のテストを misc/k8s/token_test.go に書く"                    # T014

# 同時に、US2 の差し替え用の変数と補助関数を進められる:
Task: "差し替え用の変数を misc/k8s/credentials.go に宣言する"                   # T025
Task: "一時ディレクトリの補助関数を misc/k8s/credentials_test.go に作る"        # T026

# 同時に、US3 の依存の確認を先行して実行できる:
Task: "ルートの依存に k8s.io/ が現れないことを確認する"                          # T043
Task: "misc/k8s の依存に他サービスの SDK が現れないことを確認する"               # T044
```

---

## 実装の進め方

### まず MVP (利用シナリオ 1 のみ)

1. フェーズ 1 (準備、T001〜T004) を完了する
2. フェーズ 2 (土台、T005〜T010) を完了する（重要 — すべてのシナリオを塞ぐ）
3. フェーズ 3 (US1、T011〜T024) を完了する
4. **いったん止めて確認する**: `cd misc/k8s && go test ./... -race -count=1` が通り、
   利用者が自前の clientset からトークン取得処理を得られる
5. この時点で、既存 4 モジュールと同じ作法の取得元が 1 つ増えている。
   自動選択が無いため、利用者は接続情報を自分で用意する必要がある

### 増分で届ける

1. 準備 + 土台 → 土台が整う
2. US1 を追加 → 単独で検証 → **MVP**（クライアント注入による取得）
3. US2 を追加 → 単独で検証 → 引数なしで使える（クラスタ内・手元の双方）
4. US3 を検証 → 分離が保たれていることを確認
5. 仕上げ → 文書・ゲート・呼称の整合。**ここまで完了して初めて憲章の完成条件を満たす**

### 複数人で並行する場合

1. 準備 + 土台をまず 1 人が完了させる（型の定義が競合するため分担に向かない）
2. 土台が終わったら:
   - 担当者 A: US1（`token.go` / `token_test.go`）
   - 担当者 B: US2 のうち T025〜T040（`credentials.go` / `credentials_test.go`）
   - T041 のみ A の T022 完了を待つ
3. US3 の検証と仕上げは、両者の完了後に合流して行う

---

## 補足

- [P] のタスク = 異なるファイル、依存なし
- **同一ファイルへの [P] タスクに注意**: US1 と US2 のテストは、それぞれ 1 つの
  `*_test.go` に集まる。複数人で分担する場合はファイルを分けること
- 各利用シナリオは、単独で完了・検証できること
- 実装前にテストが失敗することを確認する（憲章 原則 II）
- タスクごと、あるいは論理的なまとまりごとにコミットする
- 避けること: 生成物の直接編集（憲章 原則 I）、テストなき手書きコードの追加（同 II）、
  本体への依存追加（同 V）
- **本機能の範囲外**: 憲章 原則 II の参考値「現在の実数は 5 件」が 6 件になるため、
  憲章の PATCH 改訂が望ましい。実装とは別の Pull Request とする（research.md D11）
