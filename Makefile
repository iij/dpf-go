# SPDX-License-Identifier: Apache-2.0

GENERATOR_IMAGE := openapitools/openapi-generator-cli:latest
DOCKER_RUN := docker run --rm -u $(shell id -u):$(shell id -g) -v "$(CURDIR):/local" $(GENERATOR_IMAGE)

# 本リポジトリは複数モジュール構成。misc 以下は他パッケージに依存する
# optional な機能で、それぞれ独立した go.mod を持つ。
MODULES := . misc/vault misc/aws misc/azure misc/gcp

.PHONY: all generate clean templates executeall fmt tidy build build-all test test-integration lint tidy-all

all: generate

# api,model を再生成する。
# 既存の生成物を削除してから生成するため、openapi.json から消えた
# スキーマ/APIの古いファイルは残らない。
generate: clean
	$(DOCKER_RUN) generate -c /local/openapi-generator-config.yaml
	$(MAKE) executeall
	$(MAKE) fmt
	$(MAKE) tidy
	$(MAKE) build

# 生成物のみを削除する（手書きファイルは残す）。
clean:
	rm -f $(CURDIR)/api_*.go $(CURDIR)/model_*.go
	rm -f $(CURDIR)/client.go $(CURDIR)/configuration.go $(CURDIR)/response.go $(CURDIR)/utils.go
	rm -f $(CURDIR)/executeall_gen.go

# Pager 対応の Execute() を持つ API へ ExecuteAll() を生成する。
executeall:
	go run ./tools/genexecuteall

# 生成物を gofmt で整形する。
#
# openapi-generator の出力は gofmt 準拠ではない（構造体フィールドが整列されず、
# 余分な空行やコメントのインデント差異が残る）ため、生成のたびに整形する。
# これを省くと gofmt -l に大量のファイルが並び、手書きコードの整形漏れが埋もれる。
fmt:
	gofmt -w $(CURDIR)

# OpenTelemetry など依存を解決する。
tidy:
	go mod tidy

build:
	go build ./...

# 全モジュールをビルドする。
build-all:
	@for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && go build ./...) || exit 1; \
	done

# 全モジュールのテストを実行する。
test:
	@for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && go test ./... -race -count=1) || exit 1; \
	done

# 実 API と実 DNS サーバを使う統合テスト。
#
# 必要な環境変数:
#   DPF_TOKEN_RO          参照系(dpf_read)トークン。未設定なら全テストがスキップされる
#   DPF_TOKEN_RW          更新系(dpf_write)トークン。未設定なら書き込み系がスキップされる
#   DPF_TEST_SERVICE_CODE 書き込み対象ゾーンのサービスコード
#   DPF_TEST_DNS_TIMEOUT  権威サーバへの反映確認の上限（任意、既定 10m）
#
# -parallel 1 と t.Parallel() 不使用で API 呼び出しを直列化する
# （-p はパッケージ間の並列度なので、これだけでは足りない）。
# 既定の -timeout 10m では確実に足りないため 45m を指定する。
# -race は並行処理が無く実行時間が延びるだけなので付けない。
test-integration:
	go test ./internal/integration/... -tags=integration -count=1 -parallel 1 -timeout 45m -v

# 全モジュールに golangci-lint をかける。
lint:
	@for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && golangci-lint run --build-tags=integration ./...) || exit 1; \
	done

# 全モジュールの go.mod / go.sum を整理する。
tidy-all:
	@for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && go mod tidy) || exit 1; \
	done

# generator のテンプレートを再抽出する（カスタマイズの土台更新用）。
#
# 【警告】templates/ を generator 公式のテンプレートで上書きする。
# 本リポジトリの templates/ には以下の独自カスタマイズが入っており、
# 実行するとすべて失われる。実行後は再適用してから make generate すること。
#
#   - templates/partial_header.mustache
#       先頭に「// SPDX-License-Identifier: Apache-2.0」と空行を追加。
#       生成される api_*.go / model_*.go / client.go / configuration.go /
#       response.go / utils.go すべてのライセンス表記がこれに依存する。
#   - templates/client.mustache
#       OpenTelemetry 対応。otel / attribute / codes / propagation / trace の
#       import、計装スコープ名の定数 tracerName、および callAPI での
#       クライアントスパン生成とトレースコンテキストの伝播。
#
# 再適用せずに make generate すると、SPDX 表記のない・トレースもしない
# コードが生成される。通常の再生成に本ターゲットは不要。
templates:
	$(DOCKER_RUN) author template -g go -o /local/templates
