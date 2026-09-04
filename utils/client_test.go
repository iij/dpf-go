// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"golang.org/x/time/rate"
)

// newTestWrapper はネットワークを使わない Operation テスト用の Client を返す。
func newTestWrapper(t *testing.T, opts ...ClientOption) *Client {
	t.Helper()
	opts = append([]ClientOption{WithRateLimit(rate.Inf, 1), WithToken("tok")}, opts...)
	c, err := NewClient(append([]ClientOption{WithEndpoint("http://example.invalid")}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewClient_TokenRequired(t *testing.T) {
	t.Setenv(EnvAPIEndpoint, "")
	t.Setenv(EnvAPIToken, "")
	if _, err := NewClient(); !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("expected ErrTokenRequired, got %v", err)
	}
}

func TestNewClient_EnvFallbackAndHeaders(t *testing.T) {
	var gotAuth, gotUA string
	var gotAuthCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAuthCount = len(r.Header.Values("Authorization"))
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"r","results":[]}`))
	}))
	defer srv.Close()

	t.Setenv(EnvAPIEndpoint, srv.URL)
	t.Setenv(EnvAPIToken, "envtok")

	c, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// エンドポイント・トークン・UA が設定されていることを実リクエストで確認する。
	_, _, err = c.GetAPIClient().ZonesAPI.GetZoneList(context.Background()).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotAuth != "Bearer envtok" {
		t.Errorf("Authorization: got %q, want %q", gotAuth, "Bearer envtok")
	}
	// Authorization が重複して付与されないことを確認する。
	if gotAuthCount != 1 {
		t.Errorf("Authorization header count: got %d, want 1", gotAuthCount)
	}
	if len(gotUA) < len("dpf-go/") || gotUA[:len("dpf-go/")] != "dpf-go/" {
		t.Errorf("User-Agent: got %q, want prefix dpf-go/", gotUA)
	}
}

func TestNewClient_Defaults(t *testing.T) {
	c := newTestWrapper(t)
	if c.maxRetry != DefaultMaxRetry {
		t.Errorf("maxRetry: got %d, want %d", c.maxRetry, DefaultMaxRetry)
	}
	if c.maxConcurrency != DefaultMaxConcurrency {
		t.Errorf("maxConcurrency: got %d, want %d", c.maxConcurrency, DefaultMaxConcurrency)
	}
}

// sentinelRT は WithHTTPClient テストで下位 Transport を識別するためのダミー。
type sentinelRT struct{}

func (sentinelRT) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("sentinel")
}

func TestNewClient_WithHTTPClient(t *testing.T) {
	sentinel := &sentinelRT{}
	hc := &http.Client{Timeout: 7 * time.Second, Transport: sentinel}

	c, err := NewClient(WithEndpoint("http://example.invalid"), WithToken("tok"), WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got := c.GetAPIClient().GetConfig().HTTPClient
	// タイムアウトは渡した http.Client から引き継がれる。
	if got.Timeout != 7*time.Second {
		t.Errorf("timeout: got %v, want 7s", got.Timeout)
	}
	// Transport は制限付き→認証の順にラップされ、最下位は元の Transport を保持する。
	lt, ok := got.Transport.(*limitTransport)
	if !ok {
		t.Fatalf("transport not wrapped by limitTransport: %T", got.Transport)
	}
	at, ok := lt.base.(*authTransport)
	if !ok {
		t.Fatalf("transport not wrapped by authTransport: %T", lt.base)
	}
	if at.base != sentinel {
		t.Errorf("base transport not preserved: %T", at.base)
	}
	// 呼び出し側の http.Client は変更されない。
	if hc.Transport != http.RoundTripper(sentinel) {
		t.Errorf("caller http.Client was mutated")
	}
}

func TestOperation_Success(t *testing.T) {
	c := newTestWrapper(t)
	var calls int32
	err := c.Operation(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls: got %d, want 1", calls)
	}
}

func TestOperation_RetryTransportError(t *testing.T) {
	c := newTestWrapper(t, WithMaxRetry(2))
	var calls int32
	wantErr := errors.New("connection refused")
	err := c.Operation(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	// 初回 + リトライ2回 = 3 回。
	if calls != 3 {
		t.Errorf("calls: got %d, want 3", calls)
	}
}

func TestOperation_NoRetryOnAPIError(t *testing.T) {
	c := newTestWrapper(t, WithMaxRetry(3))
	var calls int32
	apiErr := &dpf.GenericOpenAPIError{} // HTTP レスポンスが得られた API エラー。
	err := c.Operation(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return apiErr
	})
	if !errors.As(err, new(*dpf.GenericOpenAPIError)) {
		t.Fatalf("expected GenericOpenAPIError, got %v", err)
	}
	if calls != 1 {
		t.Errorf("calls: got %d, want 1 (no retry)", calls)
	}
}

// serverClient は指定ハンドラのテストサーバに接続する Client を返す。
func serverClient(t *testing.T, handler http.HandlerFunc, opts ...ClientOption) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(append([]ClientOption{WithEndpoint(srv.URL), WithToken("tok")}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// 同時実行数制限が Operation を介さない通常の API リクエストにも効くことを検証する。
func TestClient_MaxConcurrency_AllRequests(t *testing.T) {
	var cur, max int32
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&cur, 1)
		for {
			m := atomic.LoadInt32(&max)
			if n <= m || atomic.CompareAndSwapInt32(&max, m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"r","results":[]}`))
	}, WithMaxConcurrency(2), WithRateLimit(rate.Inf, 1))

	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			// Operation を介さない直接の API リクエスト。
			_, _, _ = c.GetAPIClient().ZonesAPI.GetZoneList(context.Background()).Execute()
		})
	}
	wg.Wait()
	if max > 2 {
		t.Errorf("observed concurrency %d, want <= 2", max)
	}
}

// レート制限が Operation を介さない通常の API リクエストにも効くことを検証する。
func TestClient_RateLimit_AllRequests(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"r","results":[]}`))
	}, WithRateLimit(rate.Every(time.Hour), 1)) // burst 1、以降は実質補充されない。

	// 1 回目はバーストトークンを消費して成功する。
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(context.Background()).Execute(); err != nil {
		t.Fatalf("first request: %v", err)
	}

	// 2 回目はレート制限で待たされる。短い ctx 期限で打ち切られることを確認する。
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err == nil {
		t.Fatal("expected rate-limited request to fail under short ctx deadline")
	}
}

func TestOperation_CtxCanceled(t *testing.T) {
	c := newTestWrapper(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls int32
	err := c.Operation(ctx, func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err == nil {
		t.Fatal("expected error for canceled ctx")
	}
	if calls != 0 {
		t.Errorf("operation must not run on canceled ctx, calls=%d", calls)
	}
}

// recordAuthServer は各リクエストの Authorization ヘッダを記録するテストサーバを返す。
func recordAuthServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, strings.Join(r.Header.Values("Authorization"), " | "))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"r","results":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

// トークンがリクエストのたびに解決され、最新の値が使われることを検証する。
// これが新仕様の中核。
func TestNewClient_TokenResolvedPerRequest(t *testing.T) {
	srv, auths := recordAuthServer(t)

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("tok-a"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c, err := NewClient(WithEndpoint(srv.URL), WithTokenFile(path), WithRateLimit(rate.Inf, 1))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err != nil {
		t.Fatalf("first request: %v", err)
	}

	// Client を作り直さずにトークンを差し替える。
	if err := os.WriteFile(path, []byte("tok-b"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err != nil {
		t.Fatalf("second request: %v", err)
	}

	want := []string{"Bearer tok-a", "Bearer tok-b"}
	if got := auths(); !slices.Equal(got, want) {
		t.Errorf("Authorization headers: got %v, want %v", got, want)
	}
}

// 環境変数版。指定がない場合の既定の TokenProvider も実行時に評価される。
func TestNewClient_EnvTokenResolvedPerRequest(t *testing.T) {
	srv, auths := recordAuthServer(t)

	t.Setenv(EnvAPIToken, "env-a")
	c, err := NewClient(WithEndpoint(srv.URL), WithRateLimit(rate.Inf, 1))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err != nil {
		t.Fatalf("first request: %v", err)
	}

	t.Setenv(EnvAPIToken, "env-b")
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err != nil {
		t.Fatalf("second request: %v", err)
	}

	want := []string{"Bearer env-a", "Bearer env-b"}
	if got := auths(); !slices.Equal(got, want) {
		t.Errorf("Authorization headers: got %v, want %v", got, want)
	}
}

// WithTokenTTL 指定時は TTL の間 TokenProvider が再実行されないことを検証する。
func TestNewClient_WithTokenTTL_Caches(t *testing.T) {
	srv, auths := recordAuthServer(t)

	var calls int32
	c, err := NewClient(WithEndpoint(srv.URL),
		WithTokenProvider(func(context.Context) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "tok", nil
		}),
		WithTokenTTL(time.Hour),
		WithRateLimit(rate.Inf, 1),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	for range 3 {
		if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}

	// NewClient の検証時の 1 回だけで、リクエストでは再取得されない。
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("provider calls: got %d, want 1", n)
	}
	if got := auths(); len(got) != 3 {
		t.Fatalf("requests: got %d, want 3", len(got))
	}
	for i, a := range auths() {
		if a != "Bearer tok" {
			t.Errorf("request %d Authorization: got %q, want %q", i, a, "Bearer tok")
		}
	}
}

func TestNewClient_TokenOptionLastWins(t *testing.T) {
	srv, auths := recordAuthServer(t)

	c, err := NewClient(WithEndpoint(srv.URL),
		WithToken("first"),
		WithTokenProvider(TokenFromString("last")),
		WithRateLimit(rate.Inf, 1),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(context.Background()).Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := auths(); len(got) != 1 || got[0] != "Bearer last" {
		t.Errorf("Authorization: got %v, want [Bearer last]", got)
	}
}

// トークンファイルが存在しない場合、NewClient が失敗することを検証する。
func TestNewClient_TokenFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := NewClient(WithEndpoint("http://example.invalid"), WithTokenFile(path))
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
	var tokErr *TokenError
	if !errors.As(err, &tokErr) {
		t.Errorf("expected *TokenError, got %T", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

// TokenProvider が空文字を返す場合は ErrTokenRequired になることを検証する。
func TestNewClient_ProviderEmptyToken(t *testing.T) {
	_, err := NewClient(WithEndpoint("http://example.invalid"), WithTokenProvider(TokenFromString("")))
	if !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("expected ErrTokenRequired, got %v", err)
	}
}

// ctx で dpf.ContextAccessToken を指定した場合、そちらが優先され
// Authorization が重複しないことを検証する。
func TestNewClient_ContextAccessTokenWins(t *testing.T) {
	srv, auths := recordAuthServer(t)

	c, err := NewClient(WithEndpoint(srv.URL), WithToken("clienttok"), WithRateLimit(rate.Inf, 1))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.WithValue(context.Background(), dpf.ContextAccessToken, "ctxtok")
	if _, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// " | " で連結しているので、重複していれば検出できる。
	if got := auths(); len(got) != 1 || got[0] != "Bearer ctxtok" {
		t.Errorf("Authorization: got %v, want [Bearer ctxtok]", got)
	}
}

// トークン取得失敗はリトライされないことを検証する。
func TestOperation_NoRetryOnTokenError(t *testing.T) {
	srv, auths := recordAuthServer(t)

	var fail atomic.Bool
	c, err := NewClient(WithEndpoint(srv.URL),
		WithTokenProvider(func(context.Context) (string, error) {
			if fail.Load() {
				return "", errors.New("token unavailable")
			}
			return "tok", nil
		}),
		WithRateLimit(rate.Inf, 1),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// 構築後にトークン取得を失敗させる。
	fail.Store(true)

	ctx := context.Background()
	var calls int32
	opErr := c.Operation(ctx, func() error {
		atomic.AddInt32(&calls, 1)
		_, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).Execute()
		return err
	})

	if opErr == nil {
		t.Fatal("expected error")
	}
	var tokErr *TokenError
	if !errors.As(opErr, &tokErr) {
		t.Errorf("expected *TokenError, got %T (%v)", opErr, opErr)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("operation calls: got %d, want 1 (no retry)", n)
	}
	if got := auths(); len(got) != 0 {
		t.Errorf("server hits: got %d, want 0", len(got))
	}
}

func TestIsRetryable_TokenError(t *testing.T) {
	if isRetryable(&TokenError{Err: ErrTokenRequired}) {
		t.Error("TokenError should not be retryable")
	}
	// url.Error 越しでも検出できる（http.Client が RoundTrip のエラーを包むため）。
	wrapped := &url.Error{Op: "Get", URL: "http://example.invalid", Err: &TokenError{Err: ErrTokenRequired}}
	if isRetryable(wrapped) {
		t.Error("TokenError wrapped in *url.Error should not be retryable")
	}
}

// エンドポイントの決定順（WithEndpoint > 環境変数 > 既定値）を検証する。
func TestNewClient_EndpointPrecedence(t *testing.T) {
	// 実リクエストは行わないので、設定された Servers を直接確認する。
	serverURL := func(t *testing.T, c *Client) string {
		t.Helper()
		servers := c.GetAPIClient().GetConfig().Servers
		if len(servers) != 1 {
			t.Fatalf("servers: got %d, want 1", len(servers))
		}
		return servers[0].URL
	}

	t.Run("option wins over env", func(t *testing.T) {
		t.Setenv(EnvAPIEndpoint, "http://from-env.invalid")
		c, err := NewClient(WithEndpoint("http://from-option.invalid"), WithToken("tok"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if got := serverURL(t, c); got != "http://from-option.invalid" {
			t.Errorf("endpoint: got %q, want option value", got)
		}
	})

	t.Run("env wins over default", func(t *testing.T) {
		t.Setenv(EnvAPIEndpoint, "http://from-env.invalid")
		c, err := NewClient(WithToken("tok"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if got := serverURL(t, c); got != "http://from-env.invalid" {
			t.Errorf("endpoint: got %q, want env value", got)
		}
	})

	t.Run("default when unset", func(t *testing.T) {
		t.Setenv(EnvAPIEndpoint, "")
		c, err := NewClient(WithToken("tok"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if got := serverURL(t, c); got != defaultAPIEndpoint {
			t.Errorf("endpoint: got %q, want %q", got, defaultAPIEndpoint)
		}
	})

	t.Run("empty option is ignored", func(t *testing.T) {
		t.Setenv(EnvAPIEndpoint, "http://from-env.invalid")
		c, err := NewClient(WithEndpoint(""), WithToken("tok"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if got := serverURL(t, c); got != "http://from-env.invalid" {
			t.Errorf("endpoint: got %q, want env value", got)
		}
	})
}

// TestClient_SyncWaitThroughWrapper は、NewClient が構築するクライアント
// （レート制限用のカスタム Transport + Timeout 付き http.Client）経由で
// 非同期 API を実行し、その戻り値をそのまま SyncWait に渡しても
// 正常に JOB の完了を待てることを検証する。
//
// net/http は Client.Timeout が非ゼロで、かつ *http.Transport 以外の
// Transport が使われている場合、レスポンスボディの Close 時にリクエストの
// context をキャンセルする。limitTransport はまさにその条件に該当するため、
// SyncWait がキャンセル済みの context を引き継ぐと即座に失敗する。
// 本テストは、この組み合わせが将来にわたって壊れないことを保証する。
func TestClient_SyncWaitThroughWrapper(t *testing.T) {
	dpf.SyncWaitPollInterval = 1 * time.Millisecond

	const requestID = "req50000000000000000000000000005"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/jobs/") {
			_, _ = w.Write([]byte(`{"request_id":"` + requestID + `","status":"SUCCESSFUL"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"request_id":"` + requestID + `","jobs_url":"/jobs/` + requestID + `"}`))
	}))
	defer srv.Close()

	c, err := NewClient(
		WithEndpoint(srv.URL),
		WithToken("tok"),
		WithRateLimit(rate.Inf, 1),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	api := c.GetAPIClient()
	job, _, err := api.JobsAPI.SyncWait(
		api.ZonesAPI.DeleteZoneChanges(context.Background(), "zone1234567890").Execute(),
	)
	if err != nil {
		t.Fatalf("SyncWait through wrapper failed: %v", err)
	}
	if job == nil || job.GetStatus() != "SUCCESSFUL" {
		t.Fatalf("expected SUCCESSFUL job, got %+v", job)
	}
}
