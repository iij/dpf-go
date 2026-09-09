# SPDX-License-Identifier: Apache-2.0

GENERATOR_IMAGE := openapitools/openapi-generator-cli:latest
DOCKER_RUN := docker run --rm -u $(shell id -u):$(shell id -g) -v "$(CURDIR):/local" $(GENERATOR_IMAGE)

# 本リポジトリは複数モジュール構成。misc 以下は他パッケージに依存する
# optional な機能で、それぞれ独立した go.mod を持つ。
#
# 対象は固定で列挙せず、リポジトリ内の go.mod の位置から導出する。
# 列挙にするとモジュールを増やしたときに、ビルド・テスト・各検査の対象から
# 静かに漏れる。vendor と testdata は依存の持ち込みやテスト用の入力であり
# 検査対象ではないため除外する。
# sort は重複を除きつつ順序を安定させる（find の走査順に依存させない）。
MODULES := $(sort $(patsubst ./%,%,$(patsubst %/,%,$(dir $(shell find . \
	-name vendor -prune -o -name testdata -prune -o -name go.mod -print)))))

# 検査ツールの版。ここが唯一の出典であり、CI は自前で版を持たない。
# 手元と CI で版が違うと、指摘が「環境の差」として扱われ、ツールの判断が
# 信頼されなくなる。
GOLANGCI_LINT_VERSION := 2.13.2
GOVULNCHECK_VERSION := v1.7.0
GO_LICENSES_VERSION := v2.0.1
BETTERLEAKS_VERSION := v1.8.1

# シークレット検査のターゲット名。
#
# 一括実行 (make check) がこのターゲットを呼ぶ。名前を変数にしておくことで、
# 検査ツールを入れ替えるときの変更をこの 1 行に閉じられる。
SECRET_SCAN_TARGET := betterleaks

# 検査ゲートの共通規約。
#
# 終了コード:
#   0  違反なし
#   1  違反あり（コードを直す）
#   2  検査できなかった（環境を直す）
# 1 と 2 を分けるのは、対応が違うためである。同じにすると環境の不備を
# 違反として扱ってしまい、原因の切り分けが遅れる。
#
# ゲートは作業ツリーを書き換えない。書き換えるのは fmt のみ。
# ゲートを迂回する・一時的に無効化する・報告のみへ格下げする経路は設けない。
# 止まった場合の対処は、コードまたは依存を直すことである。
#
# ツールが未導入、版が固定値と不一致、版の抽出に失敗した場合はいずれも
# 終了コード 2 で中断する。「版が取れなかった」を「一致した」として扱わない。
#
# 版の取得手段はツールごとに異なる。go-licenses は自身の版を報告する手段を
# 持たないため、ビルド情報 (go version -m) から読む。
# $(1) ツール名 / $(2) 期待する版 / $(3) 版を取得するコマンド
#
# $(3) はシェル変数に入れずコマンド位置へ直接展開する。変数に入れて eval すると、
# awk の $$1 やコマンド置換がシェルの二重展開で失われる。
define require_version
	@command -v $(1) >/dev/null 2>&1 || { \
		echo "ERROR: $(1) が見つかりません。$(2) を導入してください" >&2; \
		exit 2; \
	}
	@got=$$($(3)); \
	if [ -z "$$got" ]; then \
		echo "ERROR: $(1) の版を取得できませんでした（期待 $(2)）" >&2; \
		echo "       版が確認できない状態で検査を通しません" >&2; \
		exit 2; \
	fi; \
	if [ "$$got" != "$(2)" ]; then \
		echo "ERROR: $(1) の版が固定値と一致しません（導入 $$got / 期待 $(2)）" >&2; \
		echo "       Makefile の固定値に合わせて導入し直してください" >&2; \
		exit 2; \
	fi
endef

# 各ツールの版を取得するコマンド。
#
# 遅延展開 (=) にする。即時展開 (:=) にすると $$( ) が make の変数参照として
# 解釈され、コマンド置換が消える。
#
# 取得手段はツールごとに異なる。go-licenses は --version / -version /
# version のいずれも持たず、自身の版を報告できないため、ビルド情報から読む。
# betterleaks は version サブコマンドを持つが、go install で導入した場合は
# "dev" を返して固定値と比較できないため、同じくビルド情報から読む。
GOLANGCI_LINT_PROBE = golangci-lint --version | sed -n 's/.*has version \([^ ]*\) .*/\1/p'
GOVULNCHECK_PROBE = govulncheck --version | sed -n 's/^Scanner: govulncheck@//p'
GO_LICENSES_PROBE = go version -m "$$(command -v go-licenses)" | awk '$$1=="mod"{print $$3; exit}'
BETTERLEAKS_PROBE = go version -m "$$(command -v betterleaks)" | awk '$$1=="mod"{print $$3; exit}'

.PHONY: all generate clean templates executeall fmt tidy build build-all test test-integration betterleaks install-hooks uninstall-hooks tidy-all check check-headers check-licenses check-lint check-vuln

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

# 整形と静的解析（統合前ゲート）。
#
# 設定はリポジトリルートの .golangci.yml。版は Makefile が唯一の出典であり、
# CI は自前で版を持たない。同一コミットに対する手元と CI の指摘が一致する
# ことが要件である。
#
# --build-tags=integration を付けて、ビルドタグによって既定のビルド対象から
# 外れる internal/integration も検査対象に含める。
#
# 整形の検査も本ターゲットが担う（.golangci.yml の formatters）。
# 検査のみを行い、ファイルを書き換えない。書き換えるのは fmt だけである。
check-lint:
	$(call require_version,golangci-lint,$(GOLANGCI_LINT_VERSION),$(GOLANGCI_LINT_PROBE))
	@status=0; \
	for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && golangci-lint run --build-tags=integration ./...) || status=1; \
	done; \
	exit $$status

# Makefile の変数を読み出す（CI が版の固定値を参照するために使う）。
#
# CI 側に版を書かないための入口である。make print-GOLANGCI_LINT_VERSION の
# ように呼ぶ。
print-%:
	@echo "$($*)"

# 変更を main へ入れる前に満たすべきすべてのゲートをまとめて実行する。
#
# 対象は憲章「開発ワークフローと品質ゲート」のマージ前の表そのものであり、
# 本リポジトリで新設した 4 種の検査だけではない。既存のビルド・単体テスト・
# シークレット検査も含む。コントリビューターが提出前に実行する単一の入口。
# 既存の検査自体は変更せず、ここから呼ぶだけである。
#
# 1 つが失敗しても残りを実行し、最後にまとめて報告する。1 つ直すたびに
# 全部を実行し直す手戻りを避けるため。
#
# 先にツールの版をまとめて検証する。「検査できなかった」(2) と「違反があった」
# (1) は対応が違うためで、環境の不備は 1 件でもあればここで止める。これを
# 通した後の失敗はすべて違反である。
#
# 【注意】GNU make はレシピが失敗すると必ず 2 で終了する。したがって
# make 自身の終了コードで 1 と 2 を区別することはできない。区別は本ターゲットが
# 出す RESULT 行と各ゲートの出力で判断すること。
check:
	@echo "=== ツールの版を検証する ==="
	$(call require_version,golangci-lint,$(GOLANGCI_LINT_VERSION),$(GOLANGCI_LINT_PROBE))
	$(call require_version,go-licenses,$(GO_LICENSES_VERSION),$(GO_LICENSES_PROBE))
	$(call require_version,govulncheck,$(GOVULNCHECK_VERSION),$(GOVULNCHECK_PROBE))
	$(call require_version,betterleaks,$(BETTERLEAKS_VERSION),$(BETTERLEAKS_PROBE))
	@failed=""; \
	for t in build-all test check-lint check-headers check-licenses check-vuln $(SECRET_SCAN_TARGET); do \
		echo ""; \
		echo "=== $$t ==="; \
		$(MAKE) --no-print-directory $$t || failed="$$failed $$t"; \
	done; \
	echo ""; \
	echo "=== 結果 ==="; \
	if [ -z "$$failed" ]; then \
		echo "RESULT: ok（統合前のすべてのゲートを満たしています）"; \
		exit 0; \
	fi; \
	for t in $$failed; do echo "  FAILED  $$t"; done; \
	echo "RESULT: violated（上のゲートを満たしていません）"; \
	exit 1

# ファイル冒頭の規約を検査する（統合前ゲート）。
#
# 先頭行の SPDX 識別子と、package 宣言より前の nolint（ファイル全体に効く）を
# 見る。対象はリポジトリ内のすべての Go ファイルで、テスト・ビルドタグ付き・
# 生成物を含む。外部ツールに依存しないため版の検証は要らない。
#
# 検査のみを行い、ファイルを書き換えない。
check-headers:
	@go run ./tools/checkheaders .

# 依存ライセンスを許容リストと照合する（統合前ゲート）。
#
# 母集団は「実際に import されるパッケージ」である。go-licenses の既定の
# 挙動がこれと一致する。モジュールグラフ全体（go list -m all 相当）は
# 用いない。GPL 系を排除する根拠が「依存が利用者のバイナリへ静的リンク
# されること」であり、リンクされない依存は利用者の配布物に条件を及ぼさない。
#
# モジュールごとに実行するのは、どのモジュールの依存かを判定に使うため
# である（本体モジュールにソース提供義務を伴う依存が入ることを禁じる）。
#
# 検査のみを行い、ファイルを書き換えない。
check-licenses:
	$(call require_version,go-licenses,$(GO_LICENSES_VERSION),$(GO_LICENSES_PROBE))
	@status=0; \
	for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && go-licenses csv ./... 2>/dev/null) \
			| go run ./tools/checklicenses -module "$$m" -allowlist licenses-allowlist.txt \
			|| status=$$?; \
	done; \
	exit $$status

# 到達可能な既知脆弱性を検査する（統合前ゲート）。
#
# 到達可能な報告がある状態ではマージできない。リリースは main から行うため、
# ここで止めればリリース時点の条件も満たされる。マージ時点に置くのは main を
# 常にリリース可能な状態に保つためで、リリース直前に発見すると、そのとき
# 初めて依存の更新を迫られリリース自体が遅れる。
#
# 新しい脆弱性の公表はコード変更と無関係に起きるため、脆弱性を含まない変更が
# 止まることがある。その場合の対処は依存の更新、または脆弱な経路へ到達しない
# 形への修正である。本ターゲットに検査を飛ばす・無効化する・報告のみへ
# 格下げする入力は設けない。
#
# govulncheck は既知脆弱性のうち実際にコードから到達しうるものだけを報告する。
# 到達しない脆弱性では失敗しない。
#
# 検査のみを行い、ファイルを書き換えない。
check-vuln:
	$(call require_version,govulncheck,$(GOVULNCHECK_VERSION),$(GOVULNCHECK_PROBE))
	@status=0; \
	for m in $(MODULES); do \
		echo "==> $$m"; \
		(cd $$m && govulncheck ./...) || status=1; \
	done; \
	exit $$status

# シークレットの混入を検査する（履歴と作業ツリーの両方）。
#
# --redact を付けて、検出した値そのものが端末やログに残らないようにする。
# --verbose が無いと「leaks found: N」しか出ず、場所が分からない。
# 手元での実行なので詳細を出す（CI では付けない。CI が落ちた時の内容確認は
# このターゲットで行う）。
# 誤検知の除外は .betterleaks.toml に定義する。理由を書かずに除外しないこと。
betterleaks:
	$(call require_version,betterleaks,$(BETTERLEAKS_VERSION),$(BETTERLEAKS_PROBE))
	betterleaks git . --redact --verbose --no-banner --exit-code 1
	betterleaks dir . --redact --verbose --no-banner --exit-code 1

# .githooks/ の git hook を有効化する（clone 直後に一度実行する）。
#
# git は clone で hook を持ってこないため、リポジトリ管理の .githooks を
# core.hooksPath に向ける形で有効化する。パスは相対のまま設定する。
# 絶対パスにすると、リポジトリを別の場所へ移した時点で hook が動かなくなる。
#
# 【注意】core.hooksPath はグローバル設定より優先される。グローバルの
# core.hooksPath に独自の hook を置いている場合、このリポジトリでは
# それらが動かなくなる。必要なら .githooks/ 側に移すこと。
install-hooks:
	git config core.hooksPath .githooks
	@echo "core.hooksPath = $$(git config core.hooksPath)"
	@ls -l .githooks

uninstall-hooks:
	git config --unset core.hooksPath

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
