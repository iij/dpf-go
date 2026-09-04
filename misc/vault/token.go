// SPDX-License-Identifier: Apache-2.0

package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/vault/api"
)

// デフォルト値。Option で変更できる。
const (
	// DefaultMount は KV シークレットエンジンのマウントパスのデフォルト。
	DefaultMount = "secret"
	// DefaultKey はシークレット内でトークンを格納するキー名のデフォルト。
	DefaultKey = "token"
	// DefaultKVVersion は KV シークレットエンジンのバージョンのデフォルト。
	DefaultKVVersion = 2
)

// ErrKeyNotFound はシークレット内に指定したキーが存在しない場合に返される。
var ErrKeyNotFound = errors.New("vault: key not found in secret")

// config は NewTokenProvider のオプション適用先。
type config struct {
	mount     string
	key       string
	kvVersion int
}

// Option は TokenProvider の任意設定を変更する。
type Option func(*config)

// WithMount は KV シークレットエンジンのマウントパスを指定する
// （デフォルト DefaultMount）。空文字を渡した場合は無視される。
func WithMount(mount string) Option {
	return func(c *config) {
		if mount != "" {
			c.mount = mount
		}
	}
}

// WithKey はシークレット内でトークンを格納するキー名を指定する
// （デフォルト DefaultKey）。空文字を渡した場合は無視される。
func WithKey(key string) Option {
	return func(c *config) {
		if key != "" {
			c.key = key
		}
	}
}

// WithKVVersion は KV シークレットエンジンのバージョン（1 または 2）を指定する
// （デフォルト DefaultKVVersion）。
func WithKVVersion(v int) Option {
	return func(c *config) {
		c.kvVersion = v
	}
}

// NewTokenProvider は Vault の KV シークレット path からトークンを取得する
// TokenProvider を返す。
//
//   - client : 認証済みの Vault API クライアント
//   - path   : マウントパスを除いたシークレットのパス（例: "dpf/api"）
//
// マウントパス・キー名・KV バージョンは既定値を使い、変更したい場合のみ
// opts（With... 関数）を渡す。client が nil、path が空、または KV バージョンが
// 1/2 以外の場合はエラーを返す。
func NewTokenProvider(client *api.Client, path string, opts ...Option) (func(ctx context.Context) (string, error), error) {
	if client == nil {
		return nil, errors.New("vault: client is required")
	}
	if path == "" {
		return nil, errors.New("vault: secret path is required")
	}

	cfg := &config{
		mount:     DefaultMount,
		key:       DefaultKey,
		kvVersion: DefaultKVVersion,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.kvVersion != 1 && cfg.kvVersion != 2 {
		return nil, fmt.Errorf("vault: unsupported kv version: %d", cfg.kvVersion)
	}

	return func(ctx context.Context) (string, error) {
		data, err := readSecret(ctx, client, cfg, path)
		if err != nil {
			return "", err
		}
		return tokenFromData(data, cfg.key)
	}, nil
}

// readSecret は KV バージョンに応じてシークレットを読み出し、その中身を返す。
func readSecret(ctx context.Context, client *api.Client, cfg *config, path string) (map[string]any, error) {
	if cfg.kvVersion == 1 {
		s, err := client.KVv1(cfg.mount).Get(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("vault: read secret %q: %w", path, err)
		}
		return s.Data, nil
	}
	s, err := client.KVv2(cfg.mount).Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("vault: read secret %q: %w", path, err)
	}
	return s.Data, nil
}

// tokenFromData はシークレットの中身から key に対応するトークンを取り出す。
func tokenFromData(data map[string]any, key string) (string, error) {
	v, ok := data[key]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("vault: key %q is not a string (%T)", key, v)
	}
	return strings.TrimSpace(s), nil
}
