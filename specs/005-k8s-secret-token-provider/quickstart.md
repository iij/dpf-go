# フェーズ 1: 検証手順

**機能**: Kubernetes Secret からのトークン取得 | **日付**: 2026-09-18

**仕様**: [spec.md](./spec.md) | **契約**: [contracts/](./contracts/)

本機能が動いていることを確かめる手順である。実装の内容は
[tasks.md](./tasks.md) (`/speckit-tasks` が作成) と実装フェーズが担う。

---

## 前提

| 必要なもの | 用途 | 備考 |
|---|---|---|
| Go 1.27 以降 | ビルドと単体テスト | 確認環境は go1.27.1 linux/amd64 |
| `make` | 品質ゲートの実行 | |
| `golangci-lint` 2.13.2 / `govulncheck` v1.7.0 / `go-licenses` v2.0.1 / `betterleaks` v1.8.1 | `make check` | 版は Makefile が唯一の出典。不一致なら中断する |
| Kubernetes クラスタ | 手動確認 (任意) | 単体テストには**不要** |

**単体テストはネットワークも動作中のクラスタも必要としない** (FR-032、SC-012)。

---

## 1. 単体テストと品質ゲート

```sh
# 新しいモジュールがゲートの対象に自動で入ることを確認する (FR-005、SC-016)
make build-all
make test

# 依存ライセンスと脆弱性。research.md D2 の実測値と一致することを確認する
make check-licenses
make check-vuln

# マージ前の 7 ゲートをまとめて実行する
make check
```

**期待する結果**:

| 確認 | 期待 |
|---|---|
| `make build-all` / `make test` の対象 | `misc/k8s` が現れる。`MODULES` を書き換えていない |
| `make test` | `-race -count=1` で成功する。ネットワークへ出ない |
| `make check-licenses` | 違反 0 件（終了コード 0）。`misc/k8s` は Apache-2.0 31 / BSD-3-Clause 17 / MIT 6 / ISC 1 の計 55 件。MPL-2.0 と判別不能は 0 件 |
| `make check-vuln` | 到達可能な既知脆弱性 0 件 |
| `make check` | `RESULT: ok`。検査前後で作業ツリーの差分が 0 件 (ゲートは書き換えない) |

`misc/k8s` の件数が上記 (計 55 件) と食い違う場合は、client-go の版が変わったか、
import しているパッケージが設計と違う。差分を確認する。
[research.md](./research.md) D2 には実装前の probe による 54 件も記録してあり、
差の 1 件は `misc/k8s` 自身の `LICENSE` である。

---

## 2. 契約の検証 (単体テスト)

[contracts/api.md](./contracts/api.md) と [contracts/credentials.md](./contracts/credentials.md)
の表が、テストとして固定されていることを確認する。

```sh
go test ./misc/k8s/... -race -count=1 -v
```

**網羅すべき観点** (仕様の受け入れシナリオとの対応):

| 観点 | 対応する受け入れシナリオ | 成功基準 |
|---|---|---|
| Secret から値が返る | 1-1 | — |
| 符号化された値がそのまま返らない | 1-2 | SC-011 |
| 名前空間・Secret 名の既定値が問い合わせに使われる | 1-3, 1-4 | SC-001 |
| 複数回の評価でそのたびに問い合わせる | 1-6 | — |
| Secret 不在 / キー不在 / 値が空 / 権限不足を区別できる | 1-7〜1-10 | SC-010 |
| その他の失敗の原因を辿れる | 1-11 | — |
| エラーに値が含まれない | 1-12 | SC-018 |
| キー名の指定 | 1-13 | — |
| オプションへの空の値が無視される | 1-14 | — |
| `ctx` の打ち切りに応じる | 1-15 | — |
| 候補の選択 8 通り | 2-1〜2-5 | SC-007, SC-008 |
| 標準的なクライアントと同じ接続先を選ぶ | 2-1〜2-4 | SC-008 |
| 壊れた設定ファイルで次へ移らない | 2-6 | — |
| 存在しない設定ファイルは無いものとして次へ進む | 2-7 | — |
| 明示した接続情報で自動選択が起きない | 2-7 | — |
| 名前空間の導出 2 経路 | 2-9, 2-10 | SC-009 |
| 名前空間が決まらない場合に生成時エラー | 2-11, 2-13 | SC-006, SC-009 |
| 候補をまたいで名前空間を回り込まない | 2-12 | SC-009 |
| 明示すればエラーにならない | 2-14 | — |

---

## 3. 分離の確認 (FR-002・FR-003)

```sh
# 本体モジュールの依存に Kubernetes の SDK が現れないこと (SC-003)
go list -deps ./... | grep -c '^k8s\.io/' ; # 0 を期待する

# misc/k8s の依存に他のシークレット管理サービスの SDK が現れないこと (SC-004)
cd misc/k8s && go list -deps ./... | grep -Ec 'hashicorp|aws-sdk-go|azure-sdk-for-go|cloud\.google\.com' ; # 0 を期待する

# ライセンスの同梱 (SC-005)
test -f misc/k8s/LICENSE && echo "LICENSE ok"

# 独立してビルド・テストできること (SC-015)
cd misc/k8s && go build ./... && go test ./... -race -count=1
```

`grep -c` が 0 のとき終了コードは 1 になる。件数の表示が目的なので、
`|| true` を付けるか件数だけを読むこと。

---

## 4. 手動確認 (任意。実クラスタ)

単体テストは差し替えで行うため、client-go を介した実際の読み出しは 1 度は手で
確かめておく価値がある。憲章は実 API に触れる確認を SHOULD としている。

```sh
# 1. Secret を作る (既定の名前で)
kubectl -n dns create secret generic dpf-token --from-literal=token="$DPF_API_TOKEN"

# 2. 手元から確認する (候補 1: 設定ファイルが選ばれる)
#    名前空間を明示しない場合は、kubeconfig の context に namespace が必要
kubectl config set-context --current --namespace=dns
go run ./path/to/example   # NewTokenProviderFromEnvironment() を呼ぶ小さなプログラム

# 2b. 標準的なクライアントと同じ接続先を選ぶことを確認する (SC-008)
kubectl config current-context   # ここで表示される接続先と一致すること

# 3. クラスタ内から確認する (候補 2 が選ばれる)
#    ServiceAccount に Secret の読み取り権限が必要
kubectl -n dns create role dpf-token-reader \
  --verb=get --resource=secrets --resource-name=dpf-token
kubectl -n dns create rolebinding dpf-token-reader \
  --role=dpf-token-reader --serviceaccount=dns:default
```

**期待する結果**:

| 確認 | 期待 |
|---|---|
| 手元から (候補 1: 設定ファイル) | トークンが取得できる。context の `namespace` が使われる |
| context に `namespace` が無い状態 | 生成時に `ErrNamespaceUnknown`。`default` を読みにいかない |
| クラスタ内から (候補 2: 割り当てられた資格情報) | トークンが取得できる。名前空間はファイルの値が使われる。**Pod に設定ファイルが無いことが前提**である。設定ファイルがあるとそちらが選ばれる |
| RoleBinding を外した状態 | `ErrForbidden` が返る |
| Secret を消した状態 | `ErrSecretNotFound` が返る |
| Secret のキーを `token` 以外にした状態 | `ErrKeyNotFound` が返る。`WithKey` で指定すれば取得できる |
| Secret を更新する | プログラムを再起動せず、次の評価で新しい値になる |

**注意**: 手動確認で本物のトークンを扱う。`kubectl` の履歴とシェルの履歴に値が
残りうる。確認後は Secret を削除し、必要ならトークンを再発行する
(憲章「一度 push された値は漏洩したものとして扱う」に準じた扱い)。

---

## 5. 文書の確認 (FR-036)

```sh
grep -n "misc/k8s" README.md doc.go utils/doc.go CHANGELOG.md
```

**期待する結果**: 4 ファイルすべてに現れる。加えて `README.md` の
「root と `misc/*` の計 5 モジュール」が「計 6 モジュール」になっていること
([research.md](./research.md) D11)。
