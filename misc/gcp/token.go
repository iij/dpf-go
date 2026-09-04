// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	gax "github.com/googleapis/gax-go/v2"
)

// DefaultVersion は取得するシークレットバージョンのデフォルト。
const DefaultVersion = "latest"

// resourcePrefix は完全修飾リソース名の接頭辞。
const resourcePrefix = "projects/"

// ErrEmptySecret はシークレットに値が含まれていない場合に返される。
var ErrEmptySecret = errors.New("gcp: secret payload is empty")

// ErrKeyNotFound は WithJSONKey で指定したキーが JSON 内に存在しない場合に返される。
var ErrKeyNotFound = errors.New("gcp: key not found in secret")

// SecretManagerAPI は本パッケージが使用する Secret Manager の操作。
// *secretmanager.Client が満たす。テスト時のモック差し替えを容易にするために
// インターフェースとして定義している。
type SecretManagerAPI interface {
	AccessSecretVersion(ctx context.Context, req *secretmanagerpb.AccessSecretVersionRequest, opts ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error)
}

// config は NewTokenProvider のオプション適用先。
type config struct {
	project string
	version string
	jsonKey string
}

// Option は TokenProvider の任意設定を変更する。
type Option func(*config)

// WithProject はシークレットが属するプロジェクト ID を指定する。
// secret に完全修飾リソース名を渡す場合は不要。空文字を渡した場合は無視される。
func WithProject(project string) Option {
	return func(c *config) {
		if project != "" {
			c.project = project
		}
	}
}

// WithVersion は取得するシークレットバージョンを指定する
// （デフォルト DefaultVersion）。secret に "/versions/" を含む完全修飾リソース名を
// 渡した場合は無視される。空文字を渡した場合もデフォルトのままとする。
func WithVersion(version string) Option {
	return func(c *config) {
		if version != "" {
			c.version = version
		}
	}
}

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

// NewTokenProvider は Secret Manager のシークレットからトークンを取得する
// TokenProvider を返す。
//
//   - client : Secret Manager クライアント（*secretmanager.Client 等）
//   - secret : シークレット ID（この場合 WithProject が必須）、または
//     "projects/{project}/secrets/{secret}" 形式の完全修飾リソース名。
//     "projects/{project}/secrets/{secret}/versions/{version}" のように
//     バージョンまで含めて指定することもできる。
//
// client が nil、secret が空、またはシークレット ID 指定で WithProject が
// ない場合はエラーを返す。
func NewTokenProvider(client SecretManagerAPI, secret string, opts ...Option) (func(ctx context.Context) (string, error), error) {
	if client == nil {
		return nil, errors.New("gcp: client is required")
	}
	if secret == "" {
		return nil, errors.New("gcp: secret is required")
	}

	cfg := &config{version: DefaultVersion}
	for _, opt := range opts {
		opt(cfg)
	}

	name, err := resourceName(secret, cfg)
	if err != nil {
		return nil, err
	}
	req := &secretmanagerpb.AccessSecretVersionRequest{Name: name}

	return func(ctx context.Context) (string, error) {
		resp, err := client.AccessSecretVersion(ctx, req)
		if err != nil {
			return "", fmt.Errorf("gcp: access secret version %q: %w", name, err)
		}
		if resp == nil || resp.GetPayload() == nil || len(resp.GetPayload().GetData()) == 0 {
			return "", ErrEmptySecret
		}
		return extractToken(string(resp.GetPayload().GetData()), cfg.jsonKey)
	}, nil
}

// resourceName はアクセス対象のバージョンの完全修飾リソース名を組み立てる。
func resourceName(secret string, cfg *config) (string, error) {
	if !strings.HasPrefix(secret, resourcePrefix) {
		if cfg.project == "" {
			return "", errors.New("gcp: project is required (use WithProject or pass a fully qualified resource name)")
		}
		return fmt.Sprintf("projects/%s/secrets/%s/versions/%s", cfg.project, secret, cfg.version), nil
	}
	// バージョンまで含む完全修飾名はそのまま使う。
	if strings.Contains(secret, "/versions/") {
		return secret, nil
	}
	return secret + "/versions/" + cfg.version, nil
}

// extractToken は jsonKey が指定されていれば JSON から該当キーを、
// そうでなければ値全体をトークンとして返す。
func extractToken(raw, jsonKey string) (string, error) {
	if jsonKey == "" {
		return strings.TrimSpace(raw), nil
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", fmt.Errorf("gcp: parse secret as json: %w", err)
	}
	v, ok := m[jsonKey]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, jsonKey)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("gcp: key %q is not a string (%T)", jsonKey, v)
	}
	return strings.TrimSpace(s), nil
}
