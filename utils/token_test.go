// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenFromString(t *testing.T) {
	got, err := TokenFromString("tok")(context.Background())
	if err != nil {
		t.Fatalf("TokenFromString: %v", err)
	}
	if got != "tok" {
		t.Errorf("token: got %q, want %q", got, "tok")
	}
}

func TestTokenFromString_Empty(t *testing.T) {
	// provider 自体はエラーにせず空文字を返す。ErrTokenRequired への変換は
	// tokenCache 側の責務とする。
	got, err := TokenFromString("")(context.Background())
	if err != nil {
		t.Fatalf("TokenFromString: %v", err)
	}
	if got != "" {
		t.Errorf("token: got %q, want empty", got)
	}
}

func TestTokenFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	// 末尾の改行・前後の空白が取り除かれることを確認する。
	if err := os.WriteFile(path, []byte("  filetok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := TokenFromFile(path)(context.Background())
	if err != nil {
		t.Fatalf("TokenFromFile: %v", err)
	}
	if got != "filetok" {
		t.Errorf("token: got %q, want %q", got, "filetok")
	}
}

func TestTokenFromFile_Missing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")

	_, err := TokenFromFile(path)(context.Background())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestTokenFromFile_ReadsAtCallTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("tok-a"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	p := TokenFromFile(path)

	if got, err := p(context.Background()); err != nil || got != "tok-a" {
		t.Fatalf("first call: got %q, %v", got, err)
	}

	// 呼び出しのたびに読み直すため、書き換えが次の呼び出しに反映される。
	if err := os.WriteFile(path, []byte("tok-b"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, err := p(context.Background()); err != nil || got != "tok-b" {
		t.Errorf("second call: got %q, %v; want tok-b", got, err)
	}
}

func TestTokenFromEnv_ReadsAtCallTime(t *testing.T) {
	const name = "DPF_TEST_TOKEN"
	p := TokenFromEnv(name)

	t.Setenv(name, "")
	if got, err := p(context.Background()); err != nil || got != "" {
		t.Fatalf("unset env: got %q, %v; want empty", got, err)
	}

	t.Setenv(name, " envtok\n")
	if got, err := p(context.Background()); err != nil || got != "envtok" {
		t.Errorf("set env: got %q, %v; want envtok", got, err)
	}
}

// countingProvider は呼び出し回数を数える TokenProvider を返す。
func countingProvider(token string) (TokenProvider, *int32) {
	var calls int32
	return func(context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return token, nil
	}, &calls
}

func TestTokenCache_NoTTL_CallsProviderEveryTime(t *testing.T) {
	p, calls := countingProvider("tok")
	c := newTokenCache(p, 0)

	for range 3 {
		if got, err := c.get(context.Background()); err != nil || got != "tok" {
			t.Fatalf("get: got %q, %v", got, err)
		}
	}
	if n := atomic.LoadInt32(calls); n != 3 {
		t.Errorf("provider calls: got %d, want 3", n)
	}
}

func TestTokenCache_TTL_Caches(t *testing.T) {
	p, calls := countingProvider("tok")
	c := newTokenCache(p, time.Hour)
	now := time.Now()
	c.now = func() time.Time { return now }

	for range 3 {
		if got, err := c.get(context.Background()); err != nil || got != "tok" {
			t.Fatalf("get: got %q, %v", got, err)
		}
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("provider calls: got %d, want 1", n)
	}
}

func TestTokenCache_TTL_Expires(t *testing.T) {
	p, calls := countingProvider("tok")
	c := newTokenCache(p, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }

	if _, err := c.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	// 期限内は再取得しない。
	now = now.Add(59 * time.Second)
	if _, err := c.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Fatalf("provider calls before expiry: got %d, want 1", n)
	}

	// 期限切れ後は再取得する。
	now = now.Add(2 * time.Second)
	if _, err := c.get(context.Background()); err != nil {
		t.Fatalf("get: %v", err)
	}
	if n := atomic.LoadInt32(calls); n != 2 {
		t.Errorf("provider calls after expiry: got %d, want 2", n)
	}
}

func TestTokenCache_Error_NotCached_NoStale(t *testing.T) {
	var fail atomic.Bool
	wantErr := errors.New("boom")
	c := newTokenCache(func(context.Context) (string, error) {
		if fail.Load() {
			return "", wantErr
		}
		return "tok", nil
	}, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }

	if got, err := c.get(context.Background()); err != nil || got != "tok" {
		t.Fatalf("initial get: got %q, %v", got, err)
	}

	// 期限切れ後に provider が失敗したら、古いトークンを返さずエラーを返す。
	now = now.Add(2 * time.Minute)
	fail.Store(true)
	got, err := c.get(context.Background())
	if err == nil {
		t.Fatalf("expected error, got token %q", got)
	}
	if got != "" {
		t.Errorf("stale token returned: %q", got)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is(err, wantErr) = false; err = %v", err)
	}
	var tokErr *TokenError
	if !errors.As(err, &tokErr) {
		t.Errorf("expected *TokenError, got %T", err)
	}

	// 失敗はキャッシュされないため、次の呼び出しで再試行される。
	fail.Store(false)
	if got, err := c.get(context.Background()); err != nil || got != "tok" {
		t.Errorf("retry after failure: got %q, %v", got, err)
	}
}

func TestTokenCache_Empty_IsErrTokenRequired(t *testing.T) {
	c := newTokenCache(TokenFromString(""), 0)

	_, err := c.get(context.Background())
	if !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("expected ErrTokenRequired, got %v", err)
	}
	var tokErr *TokenError
	if !errors.As(err, &tokErr) {
		t.Errorf("expected *TokenError, got %T", err)
	}
	// 空トークンはキャッシュされない。
	if c.token != "" {
		t.Errorf("empty token was cached: %q", c.token)
	}
}

func TestTokenCache_Concurrent(t *testing.T) {
	// TTL 有効時、並行に呼んでも provider は 1 度しか呼ばれない（-race で検証する）。
	p, calls := countingProvider("tok")
	c := newTokenCache(p, time.Hour)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if got, err := c.get(context.Background()); err != nil || got != "tok" {
				t.Errorf("get: got %q, %v", got, err)
			}
		})
	}
	wg.Wait()

	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("provider calls: got %d, want 1", n)
	}
}

func TestTokenCache_CtxCanceled(t *testing.T) {
	p, calls := countingProvider("tok")
	c := newTokenCache(p, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.get(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if n := atomic.LoadInt32(calls); n != 0 {
		t.Errorf("provider calls: got %d, want 0", n)
	}
}

// recordingRT は RoundTrip に渡された Request を記録するダミー Transport。
type recordingRT struct {
	req *http.Request
}

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

// newAuthTransport はテスト用の authTransport と記録用の下位 Transport を返す。
func newAuthTransport(t *testing.T, p TokenProvider, host string) (*authTransport, *recordingRT) {
	t.Helper()
	rec := &recordingRT{}
	return &authTransport{base: rec, tok: newTokenCache(p, 0), host: host}, rec
}

func TestAuthTransport_InjectsBearer(t *testing.T) {
	at, rec := newAuthTransport(t, TokenFromString("tok"), "api.example.jp")

	req, err := http.NewRequest(http.MethodGet, "https://api.example.jp/zones", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := at.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if got := rec.req.Header.Values("Authorization"); len(got) != 1 || got[0] != "Bearer tok" {
		t.Errorf("Authorization: got %v, want [Bearer tok]", got)
	}
}

func TestAuthTransport_ExistingAuthorizationWins(t *testing.T) {
	at, rec := newAuthTransport(t, TokenFromString("tok"), "api.example.jp")

	req, err := http.NewRequest(http.MethodGet, "https://api.example.jp/zones", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer ctxtok")
	if _, err := at.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// 既存の値が尊重され、重複もしない。
	if got := rec.req.Header.Values("Authorization"); len(got) != 1 || got[0] != "Bearer ctxtok" {
		t.Errorf("Authorization: got %v, want [Bearer ctxtok]", got)
	}
}

func TestAuthTransport_DoesNotMutateRequest(t *testing.T) {
	at, _ := newAuthTransport(t, TokenFromString("tok"), "api.example.jp")

	req, err := http.NewRequest(http.MethodGet, "https://api.example.jp/zones", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := at.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// http.RoundTripper の契約上、渡された Request は変更されてはならない。
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("original request was mutated: Authorization = %q", got)
	}
}

func TestAuthTransport_SkipsOtherHost(t *testing.T) {
	// 別ホストへのリダイレクト時にトークンを漏らさない。
	at, rec := newAuthTransport(t, TokenFromString("tok"), "api.example.jp")

	req, err := http.NewRequest(http.MethodGet, "https://evil.example.com/zones", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := at.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if got := rec.req.Header.Get("Authorization"); got != "" {
		t.Errorf("token leaked to other host: Authorization = %q", got)
	}
}

func TestAuthTransport_EmptyHostInjectsAlways(t *testing.T) {
	// host が不明な場合はホスト判定を行わない。
	at, rec := newAuthTransport(t, TokenFromString("tok"), "")

	req, err := http.NewRequest(http.MethodGet, "https://other.example.com/zones", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if _, err := at.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if got := rec.req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer tok")
	}
}

func TestAuthTransport_ProviderError(t *testing.T) {
	wantErr := errors.New("boom")
	at, rec := newAuthTransport(t, func(context.Context) (string, error) {
		return "", wantErr
	}, "api.example.jp")

	req, err := http.NewRequest(http.MethodGet, "https://api.example.jp/zones", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := at.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error")
	}
	if resp != nil {
		t.Errorf("expected nil response, got %v", resp)
	}
	var tokErr *TokenError
	if !errors.As(err, &tokErr) {
		t.Errorf("expected *TokenError, got %T", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is(err, wantErr) = false; err = %v", err)
	}
	// 下位 Transport は呼ばれない。
	if rec.req != nil {
		t.Error("base transport was called despite token failure")
	}
}
