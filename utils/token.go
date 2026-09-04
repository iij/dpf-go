// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrTokenRequired はトークンが取得できなかった場合に返される。
// TokenProvider が空文字列を返した場合、および WithToken / WithTokenFile /
// WithTokenProvider のいずれも指定せず環境変数 DPF_API_TOKEN も空の場合が該当する。
var ErrTokenRequired = errors.New("dpf: api token is required (use WithToken/WithTokenFile/WithTokenProvider or set " + EnvAPIToken + ")")

// TokenError はアクセストークンの取得に失敗したことを表す。
//
// トークンを取得できない状態は同じリクエストを繰り返しても解消しないため、
// Operation はこのエラーをリトライしない。
// Unwrap により errors.Is(err, ErrTokenRequired) や、TokenProvider が返した
// 元のエラー（os.ErrNotExist など）の判定がそのまま行える。
type TokenError struct {
	// Err はトークン取得に失敗した原因。
	Err error
}

// Error はエラーメッセージを返す。
func (e *TokenError) Error() string {
	return fmt.Sprintf("dpf: failed to get api token: %v", e.Err)
}

// Unwrap は原因となったエラーを返す。
func (e *TokenError) Unwrap() error { return e.Err }

// TokenProvider は API リクエストのたびに呼ばれ、アクセストークンを返す関数。
//
// 返す値はトークンそのものであり、"Bearer " 接頭辞は付けない
// （Authorization ヘッダの組み立てはライブラリ側で行う）。
// 空文字列を返した場合は ErrTokenRequired として扱われる。
//
// リクエストごとに評価されるため、外部でローテーションされたトークンを
// Client を作り直さずに反映できる。呼び出し頻度を抑えたい場合は
// WithTokenTTL でキャッシュ期間を指定する。
type TokenProvider func(ctx context.Context) (string, error)

// TokenFromString は固定のトークン文字列を返す TokenProvider を返す。
func TokenFromString(token string) TokenProvider {
	return func(context.Context) (string, error) {
		return token, nil
	}
}

// TokenFromFile は path のファイルを読み、前後の空白を取り除いた内容を
// トークンとして返す TokenProvider を返す。
//
// ファイルは呼び出しのたびに読み込まれるため、外部プロセスがファイルを
// 書き換えれば次のリクエストから新しいトークンが使われる。
// 読み込みに失敗した場合はそのエラーを返す（ファイルの内容はエラーに含めない）。
func TokenFromFile(path string) TokenProvider {
	return func(context.Context) (string, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
}

// TokenFromEnv は環境変数 name の値をトークンとして返す TokenProvider を返す。
//
// 環境変数は呼び出しのたびに参照される。未設定の場合は空文字列を返すため、
// 呼び出し側では ErrTokenRequired として扱われる。
func TokenFromEnv(name string) TokenProvider {
	return func(context.Context) (string, error) {
		return strings.TrimSpace(os.Getenv(name)), nil
	}
}

// tokenCache は TokenProvider の結果を ttl の間だけ保持する。
//
// ttl が 0 以下の場合はキャッシュせず、毎回 provider を呼ぶ（ロックも取らない）。
// ttl が正の場合は排他制御のもとで provider を 1 度だけ呼び、期限まで使い回す。
type tokenCache struct {
	provider TokenProvider
	ttl      time.Duration

	// now は現在時刻を返す。テスト差し替え用。
	now func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// newTokenCache は provider の結果を ttl の間キャッシュする tokenCache を返す。
func newTokenCache(provider TokenProvider, ttl time.Duration) *tokenCache {
	return &tokenCache{
		provider: provider,
		ttl:      ttl,
		now:      time.Now,
	}
}

// get はトークンを返す。キャッシュが有効ならそれを、なければ provider を呼ぶ。
//
// provider が失敗した場合はキャッシュを更新せず、期限切れの古いトークンも返さない。
// 失敗はキャッシュされないため、次の呼び出しで再試行される。
func (c *tokenCache) get(ctx context.Context) (string, error) {
	// キャッシュ無効時はロックを取らずに provider を呼ぶ。
	if c.ttl <= 0 {
		return c.fetch(ctx)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// ロック待ちの間に ctx がキャンセルされた場合は即座に返す。
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}

	token, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}
	c.token = token
	c.expires = c.now().Add(c.ttl)
	return token, nil
}

// fetch は provider を呼び、結果を検証して返す。
// エラーおよび空トークンは *TokenError に包んで返す。
func (c *tokenCache) fetch(ctx context.Context) (string, error) {
	token, err := c.provider(ctx)
	if err != nil {
		return "", &TokenError{Err: err}
	}
	if token == "" {
		return "", &TokenError{Err: ErrTokenRequired}
	}
	return token, nil
}

// authTransport は各 HTTP リクエストに Authorization ヘッダを付与する
// http.RoundTripper。トークンはリクエストのたびに tokenCache 経由で取得される。
type authTransport struct {
	base http.RoundTripper
	tok  *tokenCache

	// host は API エンドポイントのホスト。別ホストへのリダイレクト時に
	// トークンを漏らさないための判定に使う。空の場合は判定しない。
	host string
}

// RoundTrip はトークンを取得して Authorization ヘッダを設定し、下位 Transport を呼ぶ。
//
// 以下の場合はトークンを付与せず、そのまま下位 Transport に委譲する。
//   - すでに Authorization ヘッダが設定されている場合
//     （dpf.ContextAccessToken でリクエスト単位に指定した場合など）
//   - リクエスト先がエンドポイントと別ホストの場合
//     （net/http はリダイレクト時に Authorization を落とすが、Transport 層で
//     付け直すとその保護が無効になるため）
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	if req.Header.Get("Authorization") != "" {
		return base.RoundTrip(req)
	}
	if t.host != "" && req.URL != nil && req.URL.Host != t.host {
		return base.RoundTrip(req)
	}

	token, err := t.tok.get(req.Context())
	if err != nil {
		// req.Body は http.Client 側で閉じられるため、ここでは閉じない。
		return nil, err
	}

	// http.RoundTripper は渡された Request を変更してはならない。
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return base.RoundTrip(r)
}
