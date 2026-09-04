<!-- SPDX-License-Identifier: Apache-2.0 -->

# Changelog

本ファイルの形式は [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に、
バージョン番号は [Semantic Versioning](https://semver.org/lang/ja/) に従います。

> [!NOTE]
> `v1.0.0` に達するまでは、マイナーバージョンの更新に破壊的変更が含まれることがあります。

## [Unreleased]

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

[Unreleased]: https://github.com/iij/dpf-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/iij/dpf-go/releases/tag/v0.1.0
