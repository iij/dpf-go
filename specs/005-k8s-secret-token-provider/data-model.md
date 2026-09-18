# フェーズ 1: データモデル

**機能**: Kubernetes Secret からのトークン取得 | **日付**: 2026-09-18

**仕様**: [spec.md](./spec.md) | **調査**: [research.md](./research.md) | **契約**: [contracts/](./contracts/)

本機能は永続データを持たない。ここで定義するのは、取得処理を組み立てるときに存在する
値と、その検証規則である。仕様の「主要な実体」に対応する。

---

## 1. 取り出しの設定 (`config`)

`NewTokenProvider` / `NewTokenProviderFromEnvironment` のオプション適用先。非公開。

| フィールド | 型 | 既定値 | 由来 | 検証 |
|---|---|---|---|---|
| `namespace` | `string` | 空 (後段で解決) | `WithNamespace` | 空文字の指定は無視 (FR-011)。解決後に空であれば生成時エラー (FR-014) |
| `secretName` | `string` | `"dpf-token"` | `WithSecretName` | 空文字の指定は無視 (FR-011・FR-016) |
| `key` | `string` | `"token"` | `WithKey` | 空文字の指定は無視 (FR-011・FR-017) |

**既定値の位置**: `config` の初期値として置く。オプションは「空でなければ上書きする」
だけを行う。これにより FR-011 (空の値は無視) が 1 か所で成立する。既存 4 モジュールの
`WithJSONKey` / `WithKey` と同じ形である。

**状態遷移**: なし。`config` は生成時に確定し、以後変わらない。取得処理の評価は
`config` を読むだけである (FR-007: 内部に状態を持たない)。

---

## 2. Secret の位置 (解決後の値)

生成時に確定し、取得処理の評価ごとに問い合わせへ渡される。

| 値 | 決まり方 |
|---|---|
| 名前空間 | `WithNamespace` があればその値。無ければ認証情報が持つ値 (下表)。どちらも無ければ生成時エラー |
| Secret 名 | `WithSecretName` があればその値。無ければ `"dpf-token"` |
| キー | `WithKey` があればその値。無ければ `"token"` |

### 名前空間の決定表 (FR-013・FR-014)

| 経路 | 導出元 | 導出元が空・読めない場合 |
|---|---|---|
| `WithNamespace` で明示 | その値 | (空文字は無視され、以下の行へ落ちる) |
| 候補 1 (設定ファイル) | `RawConfig()` の `Contexts[CurrentContext].Namespace` | `ErrNamespaceUnknown` |
| 候補 2 (クラスタ内の資格情報) | `/var/run/secrets/kubernetes.io/serviceaccount/namespace` | `ErrNamespaceUnknown` |
| `NewTokenProvider` (クライアント注入) | なし | `ErrNamespaceUnknown` |

`"default"` を補う経路は無い ([research.md](./research.md) D4)。
`POD_NAMESPACE` は参照しない (同 D6)。
**候補をまたいで回り込まない。** 候補 1 が選ばれて名前空間が空のとき、クラスタ内の
名前空間が読める状態にあってもそれを使わない。接続先の選択は client-go に合わせるが
(同 D3)、名前空間の決定は選ばれた認証情報に紐づける。

---

## 3. 接続情報 (`credentials`)

`NewTokenProviderFromEnvironment` が実行環境から選ぶ。非公開。

| フィールド | 型 | 内容 |
|---|---|---|
| `source` | 列挙 | どの候補が選ばれたか。`kubeconfig` / `inCluster` |
| `restConfig` | `*rest.Config` | client-go が組み立てた接続設定 |
| `namespace` | `string` | 選ばれた認証情報が持つ名前空間の値。得られなければ空 |

### 候補の判定表 (FR-020)

選択は client-go に委ねる ([research.md](./research.md) D3)。`source` は名前空間の
導出元を決めるためだけに判定する。

| 順 | 候補 | 選ばれる条件 | 名前空間の導出元 |
|---|---|---|---|
| 1 | 設定ファイル (`KUBECONFIG` が指すもの、無ければ `~/.kube/config`) | そこから接続情報が得られる | 選択中の context の `namespace` |
| 2 | クラスタ内の資格情報 | 候補 1 から接続情報が得られず、**かつ** トークンファイルが存在し `KUBERNETES_SERVICE_HOST` と `KUBERNETES_SERVICE_PORT` がいずれも空でない | `.../serviceaccount/namespace` |
| — | どちらも不可 | — | `ErrNoCredentials` (生成時。FR-021) |

`KUBECONFIG` と `~/.kube/config` は 1 つの候補であり、前者が設定されていれば後者は
読まれない。設定ファイルが存在しない場合は候補が無いものとして候補 2 へ進み (FR-023)、
存在して解釈できない場合はエラーとして止まる (FR-022)。8 通りの組み合わせは
[contracts/credentials.md](./contracts/credentials.md) 第 1 節にある。

---

## 4. トークンの取得処理

`func(ctx context.Context) (string, error)`。`utils.WithTokenProvider` がそのまま
受け付ける形である (FR-006)。

**保持する状態**: 解決済みの Secret の位置と `SecretsAPI` のみ。トークンは保持しない
(FR-007)。

**評価の手順**:

1. `api.Get(ctx, namespace, secretName, metav1.GetOptions{})` を呼ぶ
2. 失敗を分類する ([contracts/api.md](./contracts/api.md) のエラー表)
3. `secret.Data[key]` を引く。無ければ `ErrKeyNotFound`
4. 長さが 0 なら `ErrEmptySecret`
5. `string(値)` を返す

`ctx` はそのまま渡す。打ち切りは client-go が応じる (FR-008)。

---

## 5. 検証規則の一覧

生成時 (`New...` の戻り値のエラー) と評価時 (取得処理の戻り値のエラー) を分ける。
**判定できるものはすべて生成時に寄せる** (FR-014・FR-021、SC-006)。

| 規則 | 時点 | エラー | 要件 |
|---|---|---|---|
| クライアントが `nil` | 生成時 | `ErrNilClient` | 004 の FR-009 に揃える |
| 接続情報の候補が 1 つも無い | 生成時 | `ErrNoCredentials` | FR-021 |
| 設定ファイルの解釈に失敗 | 生成時 | 包んで返す | FR-022 |
| 名前空間が決まらない | 生成時 | `ErrNamespaceUnknown` | FR-014 |
| Secret が存在しない | 評価時 | `ErrSecretNotFound` | FR-024 |
| キーが無い | 評価時 | `ErrKeyNotFound` | FR-025 |
| 値が空 | 評価時 | `ErrEmptySecret` | FR-026 |
| 権限が不足 | 評価時 | `ErrForbidden` | FR-027 |
| その他の失敗 | 評価時 | 包んで返す | FR-028 |

評価時のものを生成時へ寄せられない理由: Secret の存在・内容・権限は、生成時に
問い合わせれば分かるが、それは FR-007 (評価のたびに問い合わせる) の趣旨に反し、
生成を I/O に依存させる。既存 4 モジュールも同じ境界を採っている。

---

## 6. 追加モジュール

| 項目 | 値 |
|---|---|
| モジュールパス | `github.com/iij/dpf-go/misc/k8s` |
| ディレクトリ | `misc/k8s/` |
| パッケージ名 | `k8s` |
| 直接の依存 | `k8s.io/client-go`、`k8s.io/api`、`k8s.io/apimachinery` |
| 同梱物 | `LICENSE` (ルートの複製。FR-004) |

`misc/{vault,aws,azure,gcp}` と同じ構成 (`doc.go` / `token.go` / `token_test.go` /
`go.mod` / `go.sum` / `LICENSE`) に揃える。
