// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// ErrEmptySecret はシークレットに値が含まれていない場合に返される。
var ErrEmptySecret = errors.New("aws: secret value is empty")

// ErrKeyNotFound は WithJSONKey で指定したキーが JSON 内に存在しない場合に返される。
var ErrKeyNotFound = errors.New("aws: key not found in secret")

// SecretsManagerAPI は本パッケージが使用する Secrets Manager の操作。
// *secretsmanager.Client が満たす。テスト時のモック差し替えを容易にするために
// インターフェースとして定義している。
type SecretsManagerAPI interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// config は NewTokenProvider のオプション適用先。
type config struct {
	jsonKey      string
	versionID    string
	versionStage string
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

// WithVersionID は取得するシークレットのバージョン ID を指定する。
// 空文字を渡した場合は無視される。
func WithVersionID(id string) Option {
	return func(c *config) {
		if id != "" {
			c.versionID = id
		}
	}
}

// WithVersionStage は取得するシークレットのステージラベル（AWSCURRENT 等）を
// 指定する。空文字を渡した場合は無視される。
func WithVersionStage(stage string) Option {
	return func(c *config) {
		if stage != "" {
			c.versionStage = stage
		}
	}
}

// NewTokenProvider は Secrets Manager のシークレット secretID から
// トークンを取得する TokenProvider を返す。
//
//   - client   : Secrets Manager クライアント（*secretsmanager.Client 等）
//   - secretID : シークレットの名前または ARN
//
// client が nil、または secretID が空の場合はエラーを返す。
func NewTokenProvider(client SecretsManagerAPI, secretID string, opts ...Option) (func(ctx context.Context) (string, error), error) {
	if client == nil {
		return nil, errors.New("aws: client is required")
	}
	if secretID == "" {
		return nil, errors.New("aws: secret id is required")
	}

	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	in := &secretsmanager.GetSecretValueInput{SecretId: &secretID}
	if cfg.versionID != "" {
		in.VersionId = &cfg.versionID
	}
	if cfg.versionStage != "" {
		in.VersionStage = &cfg.versionStage
	}

	return func(ctx context.Context) (string, error) {
		out, err := client.GetSecretValue(ctx, in)
		if err != nil {
			return "", fmt.Errorf("aws: get secret value %q: %w", secretID, err)
		}
		raw, err := secretValue(out)
		if err != nil {
			return "", err
		}
		return extractToken(raw, cfg.jsonKey)
	}, nil
}

// secretValue はレスポンスから文字列またはバイナリのシークレット値を取り出す。
func secretValue(out *secretsmanager.GetSecretValueOutput) (string, error) {
	if out == nil {
		return "", ErrEmptySecret
	}
	if out.SecretString != nil && *out.SecretString != "" {
		return *out.SecretString, nil
	}
	if len(out.SecretBinary) > 0 {
		return string(out.SecretBinary), nil
	}
	return "", ErrEmptySecret
}

// extractToken は jsonKey が指定されていれば JSON から該当キーを、
// そうでなければ値全体をトークンとして返す。
func extractToken(raw, jsonKey string) (string, error) {
	if jsonKey == "" {
		return strings.TrimSpace(raw), nil
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", fmt.Errorf("aws: parse secret as json: %w", err)
	}
	v, ok := m[jsonKey]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrKeyNotFound, jsonKey)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("aws: key %q is not a string (%T)", jsonKey, v)
	}
	return strings.TrimSpace(s), nil
}
