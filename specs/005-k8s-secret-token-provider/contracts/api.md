# 契約: `misc/k8s` の公開 API

**機能**: Kubernetes Secret からのトークン取得 | **日付**: 2026-09-18

**関連**: [spec.md](../spec.md) | [data-model.md](../data-model.md) | [credentials.md](./credentials.md)

本モジュールが利用者へ公開する Go の API の契約である。利用者から見える約束を定める
ものであり、内部の実装の分割は定めない。

---

## 型

### `SecretsAPI`

```go
// SecretsAPI は本パッケージが使用する Kubernetes の操作。
type SecretsAPI interface {
	Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error)
}
```

**契約**:

- 本パッケージは `Get` 以外を呼ばない。書き込み・監視・一覧を行わない (spec の範囲)
- 利用者は自前の実装を渡せる。単体テストが動作中のクラスタを必要としない根拠である
  (FR-009、SC-012)
- 名前空間は引数である。名前空間の決定は本パッケージの責務であり、実装側で
  読み替えてはならない

### `NewSecretsAPI`

```go
// NewSecretsAPI は clientset を SecretsAPI へ適合させる。
func NewSecretsAPI(c kubernetes.Interface) SecretsAPI
```

**契約**: `c.CoreV1().Secrets(namespace).Get(ctx, name, opts)` へ委譲するだけである。
判断を持たない。

### `Option`

```go
type Option func(*config)
```

**契約**: すべてのオプションは、**空の値を渡された場合にその指定を無視する**
(FR-011)。適用順は引数の順であり、同じオプションを複数回渡した場合は最後の
空でない値が効く。

---

## 関数

### `NewTokenProvider`

```go
// NewTokenProvider は、渡された client を通じて Secret からトークンを取得する
// TokenProvider を返す。
func NewTokenProvider(client SecretsAPI, opts ...Option) (func(ctx context.Context) (string, error), error)
```

**契約**:

| 項目 | 約束 |
|---|---|
| 戻り値の形 | `utils.WithTokenProvider` がそのまま受け付ける (FR-006、SC-002) |
| `client` が `nil` | 生成時に `ErrNilClient` を返す。評価まで遅らせない |
| 名前空間 | `WithNamespace` が必須。無ければ生成時に `ErrNamespaceUnknown` (FR-014)。注入された client は名前空間の値を持たないため |
| Secret 名 | 既定 `"dpf-token"` (FR-016) |
| キー | 既定 `"token"` (FR-017) |
| 自動選択 | **行わない** (FR-019) |

### `NewTokenProviderFromEnvironment`

```go
// NewTokenProviderFromEnvironment は、実行環境から接続情報を選び、
// Secret からトークンを取得する TokenProvider を返す。
func NewTokenProviderFromEnvironment(opts ...Option) (func(ctx context.Context) (string, error), error)
```

**契約**:

| 項目 | 約束 |
|---|---|
| 接続情報の選択 | [credentials.md](./credentials.md) の決定表に従う (FR-020) |
| 候補が無い | 生成時に `ErrNoCredentials` (FR-021、SC-006) |
| 候補の失敗 | 生成時にエラー。次の候補へ移らない (FR-022) |
| 名前空間 | `WithNamespace` があればその値。無ければ選ばれた認証情報が持つ値 (FR-013)。導けなければ `ErrNamespaceUnknown` (FR-014) |
| 既定値 | Secret 名・キーは `NewTokenProvider` と同じ |
| 引数なしで成立する条件 | クラスタ内で動作し、Secret 名が `dpf-token`、キーが `token` (SC-001) |

### オプション

```go
func WithNamespace(namespace string) Option  // 既定: 認証情報が持つ値 (FR-013)
func WithSecretName(name string) Option      // 既定: "dpf-token" (FR-016)
func WithKey(key string) Option              // 既定: "token"      (FR-017)
```

---

## エラー

```go
var ErrNilClient        = errors.New("k8s: client is required")
var ErrNoCredentials    = errors.New("k8s: no kubernetes credentials found")
var ErrNamespaceUnknown = errors.New("k8s: namespace could not be determined")
var ErrSecretNotFound   = errors.New("k8s: secret not found")
var ErrKeyNotFound      = errors.New("k8s: key not found in secret")
var ErrEmptySecret      = errors.New("k8s: secret value is empty")
var ErrForbidden        = errors.New("k8s: not permitted to read the secret")
```

**契約**:

| 原因 | 返るもの | 時点 | 要件 |
|---|---|---|---|
| client が `nil` | `ErrNilClient` | 生成 | — |
| 候補が 1 つも無い | `ErrNoCredentials` | 生成 | FR-021 |
| 選ばれた候補が読めない・不正 | 元のエラーを包む | 生成 | FR-022 |
| 名前空間が決まらない | `ErrNamespaceUnknown` | 生成 | FR-014 |
| Secret が無い (`IsNotFound`) | `ErrSecretNotFound` | 評価 | FR-024 |
| キーが無い | `ErrKeyNotFound` | 評価 | FR-025 |
| 値の長さが 0 | `ErrEmptySecret` | 評価 | FR-026 |
| 権限不足 (`IsForbidden` / `IsUnauthorized`) | `ErrForbidden` | 評価 | FR-027 |
| その他 | 元のエラーを包む | 評価 | FR-028 |

**判別の約束**:

- 上表の 4 つの評価時エラーは `errors.Is` で互いに区別できる (SC-010)
- 元のエラーは `%w` で包み、`errors.Is` / `errors.As` で辿れる (FR-028)
- **エラーの文言に Secret の値そのものを含めない** (FR-029、SC-018)。
  名前空間・Secret 名・キー名は値ではないため含めてよい

---

## 利用者から見た使い方

```go
// クラスタ内。既定のまま
tp, err := k8s.NewTokenProviderFromEnvironment()

// 名前空間と Secret 名を指定
tp, err := k8s.NewTokenProviderFromEnvironment(
	k8s.WithNamespace("dns"),
	k8s.WithSecretName("dpf"),
)

// 接続情報を自分で用意する
tp, err := k8s.NewTokenProvider(
	k8s.NewSecretsAPI(clientset),
	k8s.WithNamespace("dns"),
)

// いずれも そのまま渡せる
c, err := utils.NewClient(utils.WithTokenProvider(tp))
```

---

## 契約の不変条件

| 不変条件 | 根拠 |
|---|---|
| 取得処理は評価のたびに問い合わせる。内部にトークンを保持しない | FR-007。保持は `utils.WithTokenTTL` の責務 |
| 取得処理は `ctx` の打ち切りに応じる | FR-008 |
| 本パッケージはログを出力しない | FR-035、憲章 原則 V |
| 本パッケージは `unsafe` を使わない | FR-034、憲章 原則 V |
| 公開要素はすべて godoc を持つ | FR-030、憲章 原則 III |
| 自前の base64 復号を行わない (`Secret.Data` が復号済み) | FR-018、[research.md](../research.md) D7 |
