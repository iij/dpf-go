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
make check         # 変更を main へ入れる前に満たすべき検査をすべて実行する
make generate      # openapi.json から api/model を再生成（Docker が必要）
```

### 統合前の検査

`make check` が、変更を `main` へ入れる前に満たすべき検査をまとめて実行します。
提出前にこれ 1 つを実行してください。1 つが失敗しても残りを実行し、最後にまとめて
報告します。個別に実行することもできます。

| 検査 | コマンド | 対象 |
|---|---|---|
| ビルド | `make build-all` | リポジトリ内のすべてのモジュール |
| 単体テスト | `make test` | 同（`-race -count=1`、ネットワーク不要） |
| 整形・静的解析 | `make check-lint` | 同（`--build-tags=integration`） |
| ファイル冒頭の規約 | `make check-headers` | すべての Go ファイル（テスト・生成物を含む） |
| 依存ライセンス | `make check-licenses` | 同モジュールの依存 |
| 到達可能な既知脆弱性 | `make check-vuln` | 同モジュールの依存 |
| シークレットの混入 | `make betterleaks` | 履歴と作業ツリー |

検査対象のモジュールは `go.mod` の位置から導出しています。モジュールを増やしても
`Makefile` を書き換える必要はありません。

必要なツールは `golangci-lint`、`go-licenses`、`govulncheck` です。版は `Makefile`
の変数が唯一の出典で、CI も同じ値を使います。導入済みの版が違う場合、検査は
省略されず中断します。手元と CI で指摘が食い違うと、指摘が「環境の差」として
扱われてツールの判断が信頼されなくなるためです。

検査はいずれも作業ツリーを書き換えません。整形の是正は `make fmt` で行います。

**ファイル冒頭の規約**は 2 つあります。すべての Go ファイルの先頭行が
`// SPDX-License-Identifier: Apache-2.0` であること、および `package` 宣言より前に
`nolint` を置かないこと（この位置の指示はファイル全体に効くため）です。

**依存ライセンス**は [licenses-allowlist.txt](licenses-allowlist.txt) の許容リストと
照合します。判定の対象は実際に import されるパッケージです。リストに無いライセンス、
判別できないライセンス、およびソース提供義務を伴うライセンス（MPL-2.0）が本体
モジュールに現れた場合は失敗します。リストへの追加は個別の判断で行わないでください。

**到達可能な既知脆弱性**があると `main` へマージできません。新しい脆弱性の公表は
コード変更と無関係に起きるため、脆弱性を含まない変更が止まることがあります。その
場合の対処は依存の更新（`go get <module>@<修正版>` のあと `go mod tidy`）、または
脆弱な経路へ到達しない形への修正です。検査を飛ばす手段は用意していません。

### シークレットの混入検査

[betterleaks](https://github.com/betterleaks/betterleaks) で三段構えに検査しています。
`make check` からも実行されます。

| 段 | 実体 | 走査対象 |
|---|---|---|
| コミット時 | [.githooks/pre-commit](.githooks/pre-commit) | ステージした変更 |
| push 時 | [.githooks/pre-push](.githooks/pre-push) | リモートにまだ無いコミットの履歴 |
| CI | [.github/workflows/betterleaks.yml](.github/workflows/betterleaks.yml) | 履歴全体と作業ツリー |

git は clone で hook を持ってこないため、clone 直後に一度だけ有効化してください。
`core.hooksPath` をリポジトリ管理の `.githooks/` に向けます（グローバルの
`core.hooksPath` より優先されます。戻すには `make uninstall-hooks`）。

```bash
make install-hooks
```

hook は最後の砦なので、betterleaks が入っていなければ検査せず通すのではなく
中断します。急ぎで飛ばす必要があれば `git commit --no-verify` /
`git push --no-verify` を使えますが、その場合も CI で必ず検査されます。
`main` へのマージには CI の `betterleaks` の成功が必須です。

CI が落ちた場合は、手元で `make betterleaks` を実行して内容を確認してください。

検出されたものが本物だった場合は、**まずトークンを失効・再発行してください**。
履歴から消しても、一度 push された値は漏洩したものとして扱う必要があります。

誤検知の除外は [.betterleaks.toml](.betterleaks.toml) に定義しています。
除外を追加する場合は、なぜシークレットでないのかを `description` に必ず記載してください。

実際の DPF-API と権威 DNS サーバを使う統合テストは、`integration` ビルドタグで
隔離しており `make test` では実行されない。実行方法と必要な環境変数は
[internal/integration/README.md](internal/integration/README.md) を参照。

```bash
make test-integration
```

## SBOM と provenance

各リリースには SBOM (`sbom.spdx.json`, SPDX 2.3) がアセットとして添付されます。
root と `misc/*` の計 5 モジュールの依存を単一の文書に含みます。
生成は [.github/workflows/sbom.yml](.github/workflows/sbom.yml) が
[syft](https://github.com/anchore/syft) で行います。

```bash
gh release download v0.1.0 -p sbom.spdx.json --repo iij/dpf-go
```

SBOM には SLSA provenance attestation が付いており、確かに本リポジトリの
リリースワークフローがそのタグから生成したものであることを検証できます
（[gh](https://cli.github.com/) 2.49 以降が必要です）。

```bash
gh attestation verify sbom.spdx.json --repo iij/dpf-go \
  --signer-workflow iij/dpf-go/.github/workflows/sbom.yml
```

`--signer-workflow` は省略しないでください。省略すると「本リポジトリの
いずれかのワークフロー」までしか絞れず、別のワークフローが署名した文書を
受け入れてしまいます。

なお、この attestation が保証するのは SBOM 文書そのものが差し替えられて
いないことです。**コード自体の完全性は `go get` が
[sum.golang.org](https://sum.golang.org) のチェックサムデータベースに対して
検証します**。リリースアセットは Go の取得経路には入りません。

## ライセンス

[Apache License 2.0](LICENSE)
