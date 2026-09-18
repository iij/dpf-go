<!-- SPDX-License-Identifier: Apache-2.0 -->

# Changelog

本ファイルの形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。

> [!NOTE]
> `v1.0.0` に達するまでは、マイナーバージョンの更新に破壊的変更が含まれることがあります。

## [Unreleased]

## [0.2.0] - 2026-09-18

### Added

- Kubernetes の Secret からトークンを取得する `misc/k8s` モジュール。
  `NewTokenProviderFromEnvironment` は引数なしで使え、クラスタ内で動かす場合は
  資格情報に紐づく名前空間の Secret `dpf-token` の `data.token` を読む。
  名前空間・Secret 名・キー名は `WithNamespace` / `WithSecretName` / `WithKey` で
  変更できる。接続情報を自分で用意する場合は `NewTokenProvider` に `SecretsAPI` を渡す
  - 接続情報は Kubernetes の標準的なクライアントと同じ順序で選ぶ。環境変数
    `KUBECONFIG` が指す設定ファイル（未設定なら `~/.kube/config`）を先に見て、
    そこから接続情報が得られない場合にクラスタ内で割り当てられた資格情報を使う。
    どちらからも得られない場合は `ErrNoCredentials` を返す
  - 名前空間は選ばれた認証情報が持つ値のみを用い、決まらない場合は
    `ErrNamespaceUnknown` を返す。`default` を補わず、別の認証情報の名前空間へ
    回り込むこともしない。この点は client-go の挙動と異なる
  - 他のシークレット管理サービス連携と同様、本体（`github.com/iij/dpf-go`）の
    依存は増えない。`k8s.io/client-go` は `misc/k8s` にのみ現れる
- `misc/vault`、`misc/aws`、`misc/azure`、`misc/gcp` の各モジュールに `LICENSE` を追加。
  Go モジュールの配布単位はモジュールのディレクトリ配下であるため、これらを独立に
  取得した利用者にはリポジトリルートの `LICENSE` が届かなかった
- 変更を `main` へ入れる前の検査をまとめて実行する `make check`。ビルド、単体テスト、
  整形・静的解析、ファイル冒頭の規約、依存ライセンス、到達可能な既知脆弱性、
  シークレットの混入を実行する。1 つが失敗しても残りを実行して最後にまとめて報告する
- ファイル冒頭の規約を検査する `make check-headers`。すべての Go ファイル（テスト・
  生成物を含む）の先頭行の SPDX 識別子と、`package` 宣言より前の `nolint`
  （ファイル全体に効く）を検出する
- 依存ライセンスを許容リストと照合する `make check-licenses`。判定の対象は実際に
  import されるパッケージで、許容リストは `licenses-allowlist.txt` に定義する。
  ソース提供義務を伴うライセンス（MPL-2.0）が本体モジュールに現れた場合は失敗する
- 到達可能な既知脆弱性を検査する `make check-vuln`
- リリース公開時に SBOM (`sbom.spdx.json`, SPDX 2.3) を生成し、リリースアセットとして添付する。
  root と `misc/*` の計 6 モジュールの依存を単一の文書に含む
- SBOM に対する SLSA provenance attestation。生成元のワークフローとタグを
  `gh attestation verify` で検証できる（public リポジトリでのみ有効）

### Changed

- リポジトリ内の全 6 モジュールの依存を最新版へ更新。主なものは
  `miekg/dns` v1.1.73、OpenTelemetry v1.46.0、`aws-sdk-go-v2` v1.47.0、
  `azcore` v1.23.1、`google.golang.org/api` v0.298.0、`go-jose/v4` v4.1.5。
  `k8s.io/client-go` は最新の安定版である v0.37.0 を維持している
- `make lint` を `make check-lint` に改名。設定を `.golangci.yml` に固定し、
  整形の検査も同ターゲットが担うようにした。検査は作業ツリーを書き換えない
  （書き換えるのは `make fmt`）
- ビルド・テスト・各検査の対象モジュールを `go.mod` の位置から導出するようにした。
  固定の列挙を持たないため、モジュールを増やしても対象から漏れない
- 検査ツール（`golangci-lint`、`go-licenses`、`govulncheck`、`betterleaks`）の版を
  `Makefile` で固定。CI は自前で版を持たず、同じ値を参照する。導入済みの版が異なる場合、
  検査は省略されず中断する
- シークレットの混入検査を `gitleaks` から
  [`betterleaks`](https://github.com/betterleaks/betterleaks) v1.8.1 へ移行。
  三段構え（`pre-commit` / `pre-push` / CI）の構成と検査対象は変わらない。
  `make gitleaks` は `make betterleaks` に、設定ファイルは `.gitleaks.toml` /
  `.gitleaksignore` から `.betterleaks.toml` / `.betterleaksignore` に、
  CI のワークフローは `gitleaks` から `betterleaks` に改名した

### Fixed

- `internal/integration` の 8 ファイルに欠けていた `// SPDX-License-Identifier: Apache-2.0` を付与
- `misc/gcp` の `google.golang.org/grpc` を v1.82.0 から v1.84.0 へ更新
  （GO-2026-6061、GO-2026-6348）
- `misc/vault` の `github.com/go-jose/go-jose/v4` を v4.1.1 から v4.1.5 へ更新（GO-2026-4945）

## [0.1.0] - 2026-08-28

初回リリース。

### Added

#### API クライアント（`github.com/iij/dpf-go`）

- DPF-API 3.7.0 に対応した API クライアント。`openapi.json` から OpenAPI Generator で生成
- 一覧 API の全ページを取得する `ExecuteAll()`。16 個の API に生成され、1 ページあたりの取得件数は通常の一覧 API が 10000 件、log 系 API が 100 件
- 非同期ジョブの完了を待つ `JobsAPIService.SyncWait()`
- OpenTelemetry によるトレース。API リクエストごとにクライアントスパンを生成し、トレースコンテキストを伝播する（計装スコープ名 `github.com/iij/dpf-go`）

#### ユーティリティ（`github.com/iij/dpf-go/utils`）

- `Client` — レート制限（既定 5 req/s、バースト 10）、最大同時実行数（既定 5）、リトライ（既定 3 回）、タイムアウト（既定 30 秒）、User-Agent を備えたクライアントラッパー。制限は HTTP Transport 層で適用されるため、`GetAPIClient()` 経由の直接リクエストにも効く
- トークン管理 — `TokenProvider` を API リクエストのたびに評価し、外部でローテーションされたトークンをクライアント再生成なしで反映する。`TokenFromString` / `TokenFromFile` / `TokenFromEnv`、`WithTokenTTL` によるキャッシュ、`TokenError` / `ErrTokenRequired`。別ホストへのリダイレクト時にはトークンを付与しない
- ゾーン取得 — `GetZoneFromName`（longest match）、`GetZoneIDFromZonename`、`GetZoneFromZonename`、`GetZoneFromServiceCode`、`GetZoneIdFromServiceCode`。名前の比較は miekg/dns で正規化して行う
- レコード取得 — `GetRecordFromRecordName`、`GetRecordFromZonename`、`GetRecordFromZoneID`
- `Mutex` — SOA レコードのラベルを用いたゾーン単位のロック（既定 TTL 15 分）。`Lock` / `Unlock` / `LockWait`、`ErrStillLock`
- `GetJobID` — レスポンスから `request_id` を取得

#### シークレット管理サービス連携

本体に依存しない独立モジュールとして提供する。`NewTokenProvider` が返す関数は `utils.WithTokenProvider` にそのまま渡せる。

- `github.com/iij/dpf-go/misc/vault` — HashiCorp Vault (KV v1/v2)
- `github.com/iij/dpf-go/misc/aws` — AWS Secrets Manager
- `github.com/iij/dpf-go/misc/azure` — Azure Key Vault
- `github.com/iij/dpf-go/misc/gcp` — Google Secret Manager

#### その他

- Apache License 2.0 のもとで公開。全ファイルに SPDX ライセンス識別子を付与
- セキュリティポリシー（[`.github/SECURITY.md`](.github/SECURITY.md)）

[Unreleased]: https://github.com/iij/dpf-go/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/iij/dpf-go/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/iij/dpf-go/releases/tag/v0.1.0
