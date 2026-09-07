<!-- SPDX-License-Identifier: Apache-2.0 -->

# IIJ DNS Platform Service (DPF) Go Client

[![Go Reference](https://pkg.go.dev/badge/github.com/iij/dpf-go.svg)](https://pkg.go.dev/github.com/iij/dpf-go)
[![Go Version](https://img.shields.io/badge/go-1.27%2B-00ADD8)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

[IIJ DNSプラットフォームサービス API (DPF-API)](https://manual.iij.jp/dpf/dpfapi/) の Go 言語クライアントライブラリです。

> [!IMPORTANT]
> 本ライブラリは IIJ DNS プラットフォームサービスのサポート対象外です。
> バグ報告や機能追加の要望は、サポートセンターではなく GitHub の Issue へお願いします。

## Requirements

- Go 1.27 以降
- 対応 DPF-API バージョン: 3.7.0

## インストール

```bash
go get github.com/iij/dpf-go
```

> [!NOTE]
> `v1.0.0` に達するまでは、マイナーバージョンの更新に破壊的変更が含まれることがあります。
> 変更点は [CHANGELOG.md](CHANGELOG.md) を参照してください。

シークレット管理サービス連携は依存を持ち込まないよう別モジュールに分けています。必要なものだけ追加してください。

```bash
go get github.com/iij/dpf-go/misc/vault   # HashiCorp Vault
go get github.com/iij/dpf-go/misc/aws     # AWS Secrets Manager
go get github.com/iij/dpf-go/misc/azure   # Azure Key Vault
go get github.com/iij/dpf-go/misc/gcp     # Google Secret Manager
```

## クイックスタート

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/iij/dpf-go/utils"
)

func main() {
	// トークンとエンドポイントは環境変数から取得される。
	client, err := utils.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// ExecuteAll() は全ページを取得する（Execute() は 1 ページのみ）。
	zones, _, err := client.GetAPIClient().ZonesAPI.GetZoneList(ctx).ExecuteAll()
	if err != nil {
		log.Fatal(err)
	}

	for _, zone := range zones.GetResults() {
		fmt.Println(zone.GetServiceCode(), zone.GetName())
	}
}
```

アクセストークンは [IIJ ID サービス](https://www.auth.iij.jp/console/) で発行します。API ごとに必要なスコープ（`dpf_read` / `dpf_write` / `dpf_contract`）が異なります。

```bash
export DPF_API_TOKEN="your-token"
go run .
```

## ドキュメント

使い方の詳細は各パッケージの godoc を参照してください。

| パッケージ | 内容 |
|---|---|
| [`dpf`](https://pkg.go.dev/github.com/iij/dpf-go) | 生成された API クライアント。認証、`ExecuteAll()`、`SyncWait()`、エラーの扱い、OpenTelemetry |
| [`dpf/utils`](https://pkg.go.dev/github.com/iij/dpf-go/utils) | クライアントラッパー（レート制限・リトライ・トークン管理）、ゾーン／レコード取得、ゾーン単位ロック |
| [`misc/vault`](https://pkg.go.dev/github.com/iij/dpf-go/misc/vault) | HashiCorp Vault (KV v1/v2) からトークンを取得 |
| [`misc/aws`](https://pkg.go.dev/github.com/iij/dpf-go/misc/aws) | AWS Secrets Manager からトークンを取得 |
| [`misc/azure`](https://pkg.go.dev/github.com/iij/dpf-go/misc/azure) | Azure Key Vault からトークンを取得 |
| [`misc/gcp`](https://pkg.go.dev/github.com/iij/dpf-go/misc/gcp) | Google Secret Manager からトークンを取得 |

DPF-API 自体の仕様は [DPF-API リファレンスマニュアル](https://manual.iij.jp/dpf/dpfapi/) を参照してください。

## 開発

```bash
make install-hooks # git hook を有効化する（clone 直後に一度）
make build-all     # 全モジュールのビルド
make test          # 全モジュールの単体テスト（ネットワーク不要）
make lint          # golangci-lint
make gitleaks      # シークレットの混入検査（履歴と作業ツリー）
make generate      # openapi.json から api/model を再生成（Docker が必要）
```

### シークレットの混入検査

[gitleaks](https://github.com/gitleaks/gitleaks) で三段構えに検査しています。

| 段 | 実体 | 走査対象 |
|---|---|---|
| コミット時 | [.githooks/pre-commit](.githooks/pre-commit) | ステージした変更 |
| push 時 | [.githooks/pre-push](.githooks/pre-push) | リモートにまだ無いコミットの履歴 |
| CI | [.github/workflows/gitleaks.yml](.github/workflows/gitleaks.yml) | 履歴全体と作業ツリー |

git は clone で hook を持ってこないため、clone 直後に一度だけ有効化してください。
`core.hooksPath` をリポジトリ管理の `.githooks/` に向けます（グローバルの
`core.hooksPath` より優先されます。戻すには `make uninstall-hooks`）。

```bash
make install-hooks
```

hook は最後の砦なので、gitleaks が入っていなければ検査せず通すのではなく
中断します。急ぎで飛ばす必要があれば `git commit --no-verify` /
`git push --no-verify` を使えますが、その場合も CI で必ず検査されます。
`main` へのマージには CI の `gitleaks` の成功が必須です。

CI が落ちた場合は、手元で `make gitleaks` を実行して内容を確認してください。

検出されたものが本物だった場合は、**まずトークンを失効・再発行してください**。
履歴から消しても、一度 push された値は漏洩したものとして扱う必要があります。

誤検知の除外は [.gitleaks.toml](.gitleaks.toml) に定義しています。
除外を追加する場合は、なぜシークレットでないのかを `description` に必ず記載してください。

実際の DPF-API と権威 DNS サーバを使う統合テストは、`integration` ビルドタグで
隔離しており `make test` では実行されない。実行方法と必要な環境変数は
[internal/integration/README.md](internal/integration/README.md) を参照。

```bash
make test-integration
```

## ライセンス

[Apache License 2.0](LICENSE)
