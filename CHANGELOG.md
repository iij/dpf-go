<!-- SPDX-License-Identifier: Apache-2.0 -->

# Changelog

本ファイルの形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。

> [!NOTE]
> `v1.0.0` に達するまでは、マイナーバージョンの更新に破壊的変更が含まれることがあります。

## [Unreleased]

## [0.3.0] - 2026-09-18

### Added

- `utils.Mutex.Renew`: 保持している排他の奪ってよい時刻を延長する
- `utils.Mutex.Owner`: 排他の保持者を表す値を返す
- `utils.ErrNotLockHolder` / `utils.ErrLabelLimit`
- `utils.WithLockRecordTTL` / `utils.WithVerifyTimeout` /
  `utils.WithLockRecordLabel` / `utils.WithLockRecordContent`
- `utils.DefaultLockRecordTTL`（1 分）/ `utils.DefaultVerifyTimeout`（10 秒）/
  `utils.DefaultLockRecordLabel`（`_dpf-go-lock`）

### Changed

- **破壊的変更**: `utils.Mutex` のゾーン単位ロックを、同一のアクセストークンを用いる
  複数のプログラム間でも排他が成立する方式へ改めた。仕様は
  `specs/006-zone-lock-redesign` を参照
  - `Lock` は再入できなくなった。保持中の再取得は `utils.ErrStillLock` を返す。
    保持期間を延ばす場合は新設の `Renew` を使う
  - 取得の判定は「奪ってよい時刻」のみで行う。owner が自分自身であることは取得の
    条件ではなくなった。奪ってよい時刻が数値として解釈できない場合は、保持者不明として
    取得を許す（以前は誰も取得できなくなった）
  - 既定の owner の形式が「ホスト名の nodename」から「nodename-PID-ランダム値」へ変わり、
    `NewMutex` の呼び出しごとに異なる値になった。`Owner` で取得できる
  - `Unlock` と `Renew` は保持者の確認を伴う。保持者でない場合は他者の排他を変更せず
    `utils.ErrNotLockHolder` を返す
  - 取得の前後で、ゾーンに専用のレコード（既定 `_dpf-go-lock` の TXT）を一時的に
    追加予定の状態で作る。排他が取得できた時点で取り消すため、権威サーバへは公開されない。
    保持中に他者がゾーン反映した場合に公開されうるが、次の取得が削除予定にして
    入れ直すことで自力で回復する
  - 排他は SOA レコードのラベルを 2 つ使う。上限（10 個）を超える場合は
    新設の `utils.ErrLabelLimit` を返し、書き込みを試みない
- **破壊的変更**: `dpf.RecordsApi` に `PostRecord` / `DeleteRecord` /
  `DeleteRecordChanges` を追加した。`client.RecordsAPI` を渡している場合は影響しない。
  このインターフェースを自分で実装している場合は追随が必要

### Fixed

- 同一のアクセストークンを用いる複数のプログラムの間で、ゾーン単位ロックが排他として
  成立していなかった。DPF-API は編集中のレコードへの他ユーザからの編集を拒否するが、
  同一ユーザからの編集は拒否しないため、ラベルの読み取りから書き込みまでの間に競合すると
  両方が取得に成功しうる状態だった
- `Lock` が非同期のレコード更新の完了を待たずに復帰していた。書き込んだ内容が読み出せる
  ことを確認してから復帰する
- SOA レコードの行の選び方を、編集予定（state=3）を優先する形に改めた。編集予定の行と
  反映済み（state=0）の行が同時に返る場合、反映済みを優先するとロックのラベルが付いて
  いない行を読み、保持中の排他を他者へ渡しうる。現行の DPF-API は SOA を 1 行しか
  返さないため実測では発生しないが、応答が変わった場合に備える。一意に決まらない場合は
  エラーを返し、黙って選ばない

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

[Unreleased]: https://github.com/iij/dpf-go/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/iij/dpf-go/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/iij/dpf-go/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/iij/dpf-go/releases/tag/v0.1.0
