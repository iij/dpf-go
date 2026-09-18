// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DefaultSecretName は WithSecretName を指定しなかった場合に読み出す Secret 名。
const DefaultSecretName = "dpf-token"

// DefaultKey は WithKey を指定しなかった場合に読み出す Secret 内のキー名。
const DefaultKey = "token"

// ErrNilClient は NewTokenProvider に client が渡されなかった場合に返される。
var ErrNilClient = errors.New("k8s: client is required")

// ErrNoCredentials は Kubernetes への接続情報が 1 つも見つからない場合に返される。
//
// NewTokenProviderFromEnvironment が、環境変数 KUBECONFIG が指す設定ファイル、
// 利用者の既定の設定ファイル (~/.kube/config)、クラスタ内で割り当てられた
// 資格情報のいずれからも接続情報を得られなかったことを表す。
var ErrNoCredentials = errors.New("k8s: no kubernetes credentials found")

// ErrNamespaceUnknown は名前空間を決められない場合に返される。
//
// WithNamespace が指定されておらず、かつ接続に用いた認証情報が名前空間の値を
// 持たない場合が該当する。本パッケージは名前空間を推測しないため、
// "default" などの既定値で補うことはない。WithNamespace で明示すれば解消する。
var ErrNamespaceUnknown = errors.New("k8s: namespace could not be determined")

// ErrSecretNotFound は指定した Secret が存在しない場合に返される。
var ErrSecretNotFound = errors.New("k8s: secret not found")

// ErrKeyNotFound は Secret の data に指定したキーが存在しない場合に返される。
var ErrKeyNotFound = errors.New("k8s: key not found in secret")

// ErrEmptySecret は Secret の値が空の場合に返される。
var ErrEmptySecret = errors.New("k8s: secret value is empty")

// ErrForbidden は Secret を読む権限が不足している場合に返される。
//
// API サーバが 401 と 403 のいずれを返した場合も本エラーになる。
// どちらも利用者の対処は権限の設定であり、区別が対処を変えないためである。
// 区別が必要な場合は errors.As で元のエラーを取り出すこと。
var ErrForbidden = errors.New("k8s: not permitted to read the secret")

// SecretsAPI は本パッケージが使用する Kubernetes の操作。
//
// 本パッケージは Get 以外を呼ばない。名前空間は引数として渡される。
// NewSecretsAPI が clientset を本インターフェースへ適合させる。
// テスト時のモック差し替えを容易にするためにインターフェースとして定義している。
type SecretsAPI interface {
	Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error)
}

// clientsetAPI は kubernetes.Interface を SecretsAPI へ適合させる。
type clientsetAPI struct {
	c kubernetes.Interface
}

// Get は clientset の CoreV1().Secrets(namespace).Get へ委譲する。
func (a clientsetAPI) Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	return a.c.CoreV1().Secrets(namespace).Get(ctx, name, opts)
}

// NewSecretsAPI は clientset を SecretsAPI へ適合させる。
//
// *kubernetes.Clientset が kubernetes.Interface を満たす。
// 委譲のみを行い、名前空間や Secret 名に関する判断は持たない。
func NewSecretsAPI(c kubernetes.Interface) SecretsAPI {
	return clientsetAPI{c: c}
}

// config は NewTokenProvider / NewTokenProviderFromEnvironment のオプション適用先。
type config struct {
	namespace  string
	secretName string
	key        string
}

// newConfig は既定値を適用した config を返す。
//
// namespace の既定値は空である。空のままであれば、接続に用いた認証情報が持つ
// 名前空間の値を用い、それも得られなければ ErrNamespaceUnknown となる。
func newConfig(opts ...Option) *config {
	cfg := &config{
		secretName: DefaultSecretName,
		key:        DefaultKey,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Option は TokenProvider の任意設定を変更する。
//
// いずれのオプションも、空文字を渡した場合はその指定を無視し既定値を保つ。
type Option func(*config)

// WithNamespace は Secret が属する名前空間を指定する。
//
// 指定しない場合は、接続に用いた認証情報が持つ名前空間の値を用いる。
// 空文字を渡した場合は無視される。
func WithNamespace(namespace string) Option {
	return func(c *config) {
		if namespace != "" {
			c.namespace = namespace
		}
	}
}

// WithSecretName は読み出す Secret の名前を指定する。
//
// 指定しない場合は [DefaultSecretName] を用いる。
// 空文字を渡した場合は無視される。
func WithSecretName(name string) Option {
	return func(c *config) {
		if name != "" {
			c.secretName = name
		}
	}
}

// WithKey は Secret の data のうちトークンを保持するキー名を指定する。
//
// 指定しない場合は [DefaultKey] を用いる。
// 空文字を渡した場合は無視される。
func WithKey(key string) Option {
	return func(c *config) {
		if key != "" {
			c.key = key
		}
	}
}

// NewTokenProvider は、渡された client を通じて Kubernetes の Secret から
// トークンを取得する TokenProvider を返す。
//
// 返る関数は func(ctx context.Context) (string, error) であり、
// utils.WithTokenProvider にそのまま渡せる。
//
// 名前空間は [WithNamespace] で指定しなければならない。渡された client は
// 名前空間の値を持たないため、指定が無い場合は生成時に [ErrNamespaceUnknown]
// を返す。実行環境から名前空間を含めて自動的に決めたい場合は
// [NewTokenProviderFromEnvironment] を用いること。
//
// client が nil の場合は生成時に [ErrNilClient] を返す。生成の時点では
// Kubernetes へ問い合わせない。
func NewTokenProvider(client SecretsAPI, opts ...Option) (func(ctx context.Context) (string, error), error) {
	if client == nil {
		return nil, ErrNilClient
	}

	cfg := newConfig(opts...)
	if cfg.namespace == "" {
		return nil, ErrNamespaceUnknown
	}

	return newProviderFunc(client, cfg), nil
}

// newProviderFunc は解決済みの設定から取得処理を組み立てる。
//
// 取得処理はトークンを保持せず、評価のたびに Secret へ問い合わせる。
func newProviderFunc(client SecretsAPI, cfg *config) func(ctx context.Context) (string, error) {
	namespace, name, key := cfg.namespace, cfg.secretName, cfg.key

	return func(ctx context.Context) (string, error) {
		secret, err := client.Get(ctx, namespace, name, metav1.GetOptions{})
		if err != nil {
			return "", classifyGetError(err, namespace, name)
		}
		return extractToken(secret, key)
	}
}

// classifyGetError は API サーバの失敗を本パッケージのエラーへ対応づける。
//
// 元のエラーは常に包んで返すため、errors.As で取り出せる。
// 名前空間と Secret 名は Secret の値ではないため、文言に含めてよい。
func classifyGetError(err error, namespace, name string) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%w: %s/%s: %w", ErrSecretNotFound, namespace, name, err)
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("%w: %s/%s: %w", ErrForbidden, namespace, name, err)
	default:
		return fmt.Errorf("k8s: get secret %s/%s: %w", namespace, name, err)
	}
}

// extractToken は Secret の data から key の値をトークンとして取り出す。
//
// Secret.Data は API の JSON デコードの時点で base64 が復号されているため、
// 本パッケージで復号は行わない。前後の空白は取り除く。
// StringData は書き込み専用であり API の応答に含まれないため参照しない。
//
// エラーの文言に値そのものを含めない。
func extractToken(secret *corev1.Secret, key string) (string, error) {
	raw, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, key)
	}

	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("%w: %q", ErrEmptySecret, key)
	}
	return token, nil
}
