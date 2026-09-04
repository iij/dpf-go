// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"time"

	dpf "github.com/iij/dpf-go"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// dpf API のデフォルトエンドポイント。
const defaultAPIEndpoint = "https://api.dns-platform.jp/dpf/v1"

// 環境変数名。
const (
	// EnvAPIEndpoint は endpoint が空のときに参照する環境変数名。
	EnvAPIEndpoint = "DPF_API_ENDPOINT"
	// EnvAPIToken はトークンの指定がない場合に、API リクエストのたびに参照する環境変数名。
	EnvAPIToken = "DPF_API_TOKEN"
)

// Client wrapper のデフォルト値。いずれも ClientOption で変更できる。
const (
	// DefaultRateLimit は 1 秒あたりのリクエスト数のデフォルト。
	DefaultRateLimit = rate.Limit(5)
	// DefaultBurst はレート制限のバーストのデフォルト。
	DefaultBurst = 10
	// DefaultMaxConcurrency は最大同時実行数のデフォルト。
	DefaultMaxConcurrency = 5
	// DefaultMaxRetry はリトライ回数のデフォルト。
	DefaultMaxRetry = 3
	// DefaultTimeout は HTTP クライアントのタイムアウトのデフォルト。
	DefaultTimeout = 30 * time.Second
	// DefaultTokenTTL はトークンのキャッシュ期間のデフォルト。
	// 0 はキャッシュしない（リクエストのたびに TokenProvider を実行する）ことを意味する。
	DefaultTokenTTL = time.Duration(0)
)

// modulePath は User-Agent のバージョン取得に用いる本モジュールのパス。
const modulePath = "github.com/iij/dpf-go"

// Client は dpf API client を内包するラッパー。
//
// レート制限と最大同時実行数制限は HTTP の RoundTripper(Transport)層で適用されるため、
// GetAPIClient() で取得したクライアント経由の通常 API リクエストも含め、
// この Client を通じて行われるすべての HTTP リクエストにクライアント全体として効く。
// Operation はそれらに加えてリトライを付与する。
//
// アクセストークンも同じく RoundTripper 層で、リクエストのたびに
// TokenProvider を評価して付与される。詳細は NewClient を参照。
type Client struct {
	api            *dpf.APIClient
	tokens         *tokenCache
	maxConcurrency int64
	maxRetry       int
}

// limitTransport は各 HTTP リクエストにレート制限と同時実行数制限を適用する
// http.RoundTripper。Client 全体で 1 つの limiter / semaphore を共有する。
type limitTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
	sem     *semaphore.Weighted
}

// RoundTrip はレート制限を待ち、同時実行スロットを取得してから下位 Transport を呼ぶ。
func (t *limitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// レート制限（リクエストの開始ペースを制御する）。
	if err := t.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	// 同時実行数制限（実際に飛んでいるリクエスト数を制限する）。
	if err := t.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer t.sem.Release(1)

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// clientConfig は NewClient のオプション適用先。
type clientConfig struct {
	endpoint       string
	rateLimit      rate.Limit
	burst          int
	maxConcurrency int
	maxRetry       int
	timeout        time.Duration
	userAgent      string
	httpClient     *http.Client
	tokenProvider  TokenProvider
	tokenTTL       time.Duration
}

// ClientOption は Client の任意設定を変更する。
type ClientOption func(*clientConfig)

// WithEndpoint は dpf API のエンドポイント URL を指定する。
//
// 指定しない場合は環境変数 DPF_API_ENDPOINT、それも空の場合は
// 本番エンドポイント（defaultAPIEndpoint）を使う。
// 空文字を渡した場合は無視される。
func WithEndpoint(endpoint string) ClientOption {
	return func(c *clientConfig) {
		if endpoint != "" {
			c.endpoint = endpoint
		}
	}
}

// WithRateLimit はレート制限（1 秒あたりのリクエスト数 r とバースト burst）を指定する。
// デフォルトは DefaultRateLimit / DefaultBurst。
func WithRateLimit(r rate.Limit, burst int) ClientOption {
	return func(c *clientConfig) {
		if r > 0 {
			c.rateLimit = r
		}
		if burst > 0 {
			c.burst = burst
		}
	}
}

// WithMaxConcurrency は最大同時実行数を指定する（デフォルト DefaultMaxConcurrency）。
func WithMaxConcurrency(n int) ClientOption {
	return func(c *clientConfig) {
		if n > 0 {
			c.maxConcurrency = n
		}
	}
}

// WithMaxRetry はリトライ回数を指定する（デフォルト DefaultMaxRetry）。
// 0 を指定するとリトライしない。
func WithMaxRetry(n int) ClientOption {
	return func(c *clientConfig) {
		if n >= 0 {
			c.maxRetry = n
		}
	}
}

// WithTimeout は HTTP クライアントのタイムアウトを指定する（デフォルト DefaultTimeout）。
// WithHTTPClient を併用した場合は、そのクライアントのタイムアウトが優先され、本指定は無視される。
func WithTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithHTTPClient は dpf API client が使用する *http.Client を指定する。
// プロキシ・TLS 設定・カスタム Transport 等を差し込みたい場合に使う。
// 指定した場合、そのクライアントがそのまま使われ、WithTimeout は無視される。
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *clientConfig) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithUserAgent は User-Agent を指定する（デフォルト "dpf-go/<version>"）。
func WithUserAgent(ua string) ClientOption {
	return func(c *clientConfig) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithToken は固定のアクセストークンを指定する。
// WithTokenProvider(TokenFromString(token)) と等価。
// 空文字を渡した場合は無視される。
//
// トークン系オプション（WithToken / WithTokenFile / WithTokenProvider）を
// 複数指定した場合は、最後に指定したものが有効になる。
func WithToken(token string) ClientOption {
	return func(c *clientConfig) {
		if token != "" {
			c.tokenProvider = TokenFromString(token)
		}
	}
}

// WithTokenFile はアクセストークンを読み込むファイルを指定する。
// WithTokenProvider(TokenFromFile(path)) と等価。
// 空文字を渡した場合は無視される。
//
// ファイルは API リクエストのたびに読み込まれるため、外部でトークンが
// 更新されれば Client を作り直さずに反映される。読み込み頻度を抑えたい場合は
// WithTokenTTL を併用する。
func WithTokenFile(path string) ClientOption {
	return func(c *clientConfig) {
		if path != "" {
			c.tokenProvider = TokenFromFile(path)
		}
	}
}

// WithTokenProvider はアクセストークンを取得する TokenProvider を指定する
// （デフォルト: TokenFromEnv(EnvAPIToken)）。
// nil を渡した場合は無視される。
func WithTokenProvider(p TokenProvider) ClientOption {
	return func(c *clientConfig) {
		if p != nil {
			c.tokenProvider = p
		}
	}
}

// WithTokenTTL はトークンのキャッシュ期間を指定する（デフォルト DefaultTokenTTL）。
//
// 0 以下を渡した場合はデフォルトのまま、つまりキャッシュせずリクエストのたびに
// TokenProvider を実行する。正の値を指定すると、その期間は前回取得した
// トークンを使い回す。
func WithTokenTTL(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		if d > 0 {
			c.tokenTTL = d
		}
	}
}

// NewClient は dpf API client を内包する Client を生成する。
//
// エンドポイントは WithEndpoint で指定する。指定しない場合は環境変数
// DPF_API_ENDPOINT、それも空の場合は本番エンドポイントを使うため、
// 通常は指定不要である。
//
// アクセストークンは TokenProvider から取得する。WithToken / WithTokenFile /
// WithTokenProvider のいずれも指定しない場合は環境変数 DPF_API_TOKEN を使う。
// TokenProvider は API リクエストのたびに評価されるため、外部でローテーション
// されたトークンを Client を作り直さずに反映できる。評価頻度を抑えたい場合は
// WithTokenTTL でキャッシュ期間を指定する。
//
// 設定ミスを早期に検出するため、NewClient は TokenProvider を 1 度実行して検証する。
// トークンが空、または取得に失敗した場合は *TokenError を返す
// （前者は errors.Is(err, ErrTokenRequired) で判定できる）。
//
// レート制限・最大同時実行数・リトライ回数・タイムアウト・User-Agent は
// 既定値を使い、変更したい場合のみ opts（With... 関数）を渡す。
func NewClient(opts ...ClientOption) (*Client, error) {
	cc := &clientConfig{
		rateLimit:      DefaultRateLimit,
		burst:          DefaultBurst,
		maxConcurrency: DefaultMaxConcurrency,
		maxRetry:       DefaultMaxRetry,
		timeout:        DefaultTimeout,
		userAgent:      "dpf-go/" + moduleVersion(),
		tokenProvider:  TokenFromEnv(EnvAPIToken),
		tokenTTL:       DefaultTokenTTL,
	}
	for _, opt := range opts {
		opt(cc)
	}

	endpoint := cc.endpoint
	if endpoint == "" {
		endpoint = os.Getenv(EnvAPIEndpoint)
	}
	if endpoint == "" {
		endpoint = defaultAPIEndpoint
	}

	tokens := newTokenCache(cc.tokenProvider, cc.tokenTTL)

	// 設定ミスを早期に検出するため、構築時に 1 度だけトークンを取得して検証する。
	// TTL が正の場合はこの結果がキャッシュされ、最初のリクエストで再取得されない。
	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()
	if _, err := tokens.get(ctx); err != nil {
		return nil, err
	}

	// 元の http.Client を変更しないようシャローコピーし、Transport を
	// 認証・レート制限・同時実行数制限付きのものに差し替える。
	var httpClient http.Client
	if cc.httpClient != nil {
		httpClient = *cc.httpClient
	} else {
		httpClient.Timeout = cc.timeout
	}
	// 認証を内側に置くことで、レート制限・同時実行数制限の待ちを抜けた
	// 直後にトークンを取得する。待っている間に古くなったトークンを使わない。
	httpClient.Transport = &limitTransport{
		base: &authTransport{
			base: httpClient.Transport,
			tok:  tokens,
			host: endpointHost(endpoint),
		},
		limiter: rate.NewLimiter(cc.rateLimit, cc.burst),
		sem:     semaphore.NewWeighted(int64(cc.maxConcurrency)),
	}

	cfg := dpf.NewConfiguration()
	cfg.Servers = dpf.ServerConfigurations{{URL: endpoint}}
	cfg.UserAgent = cc.userAgent
	cfg.HTTPClient = &httpClient

	return &Client{
		api:            dpf.NewAPIClient(cfg),
		tokens:         tokens,
		maxConcurrency: int64(cc.maxConcurrency),
		maxRetry:       cc.maxRetry,
	}, nil
}

// endpointHost は endpoint のホスト部を返す。
// 解析できない場合は空文字を返し、その場合ホストによる判定は行われない。
func endpointHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Host
}

// GetAPIClient は内包する dpf API client を返す。
func (c *Client) GetAPIClient() *dpf.APIClient {
	return c.api
}

// Operation は operation を実行し、レスポンスを得られずに失敗した場合
// （トランスポートエラー等）は maxRetry 回までリトライする。
// HTTP レスポンスが得られたエラー（*dpf.GenericOpenAPIError）はリトライせず即座に返す。
//
// レート制限・最大同時実行数制限は Transport 層で全リクエストに適用されるため、
// operation 内の各 API リクエスト（リトライ時の再実行も含む）にも自動的に効く。
func (c *Client) Operation(ctx context.Context, operation func() error) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetry; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = operation()
		if lastErr == nil {
			return nil
		}
		if !isRetryable(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// isRetryable はリトライすべきエラーかを判定する。
// HTTP レスポンスが得られた API エラー(*dpf.GenericOpenAPIError)はリトライしない。
// トークン取得の失敗(*TokenError)も繰り返して解消しないためリトライしない。
// それ以外（レスポンスが得られなかったトランスポートエラー等）はリトライする。
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var tokErr *TokenError
	if errors.As(err, &tokErr) {
		return false
	}
	var apiErr *dpf.GenericOpenAPIError
	return !errors.As(err, &apiErr)
}

// moduleVersion は本モジュール(github.com/iij/dpf-go)のバージョンを返す。
// 取得できない場合は "unknown" を返す。
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if bi.Main.Path == modulePath && bi.Main.Version != "" {
		return bi.Main.Version
	}
	for _, dep := range bi.Deps {
		if dep.Path == modulePath && dep.Version != "" {
			return dep.Version
		}
	}
	return "unknown"
}
