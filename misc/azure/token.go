// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// ErrEmptySecret はシークレットに値が含まれていない場合に返される。
var ErrEmptySecret = errors.New("azure: secret value is empty")

// ErrKeyNotFound は WithJSONKey で指定したキーが JSON 内に存在しない場合に返される。
var ErrKeyNotFound = errors.New("azure: key not found in secret")

// SecretsAPI は本パッケージが使用する Key Vault の操作。
// *azsecrets.Client が満たす。テスト時のモック差し替えを容易にするために
// インターフェースとして定義している。
type SecretsAPI interface {
	GetSecret(ctx context.Context, name string, version string, options *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
}

// config は NewTokenProvider のオプション適用先。
type config struct {
	jsonKey string
	version string
}

// Option は TokenProvider の任意設定を変更する。
type Option func(*config)

// WithJSONKey はシークレットを JSON オブジェクトとして解釈し、
// 取り出すキー名を指定する。指定しない場合はシークレットの値全体を
// トークンとして扱う。空文字を渡した場合は無視される。
func WithJSONKey(key string) Option {
	return func(c *config) {
		if key != "" {
			c.jsonKey = key
		}
	}
}

// WithVersion は取得するシークレットのバージョンを指定する。
// 指定しない場合は最新バージョンを取得する。空文字を渡した場合は無視される。
func WithVersion(version string) Option {
	return func(c *config) {
		if version != "" {
			c.version = version
		}
	}
}

// NewTokenProvider は Key Vault のシークレット name からトークンを取得する
// TokenProvider を返す。
//
//   - client : Key Vault クライアント（*azsecrets.Client 等）
//   - name   : シークレット名
//
// client が nil、または name が空の場合はエラーを返す。
func NewTokenProvider(client SecretsAPI, name string, opts ...Option) (func(ctx context.Context) (string, error), error) {
	if client == nil {
		return nil, errors.New("azure: client is required")
	}
	if name == "" {
		return nil, errors.New("azure: secret name is required")
	}

	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx context.Context) (string, error) {
		resp, err := client.GetSecret(ctx, name, cfg.version, nil)
		if err != nil {
			return "", fmt.Errorf("azure: get secret %q: %w", name, err)
		}
		if resp.Value == nil || *resp.Value == "" {
			return "", ErrEmptySecret
		}
		return extractToken(*resp.Value, cfg.jsonKey)
	}, nil
}

// extractToken は jsonKey が指定されていれば JSON から該当キーを、
// そうでなければ値全体をトークンとして返す。
func extractToken(raw, jsonKey string) (string, error) {
	if jsonKey == "" {
		return strings.TrimSpace(raw), nil
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", fmt.Errorf("azure: parse secret as json: %w", err)
	}
	v, ok := m[jsonKey]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, jsonKey)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("azure: key %q is not a string (%T)", jsonKey, v)
	}
	return strings.TrimSpace(s), nil
}
