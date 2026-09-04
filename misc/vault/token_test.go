// SPDX-License-Identifier: Apache-2.0

package vault

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/vault/api"
)

// newTestClient は指定ハンドラのテストサーバに接続する Vault クライアントを返す。
func newTestClient(t *testing.T, handler http.HandlerFunc) *api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := api.DefaultConfig()
	cfg.Address = srv.URL
	c, err := api.NewClient(cfg)
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	c.SetToken("test-vault-token")
	return c
}

// kvv2Response は KV v2 の読み出しレスポンスを組み立てる。
func kvv2Response(body string) string {
	return `{"request_id":"r","data":{"data":` + body +
		`,"metadata":{"created_time":"2024-01-01T00:00:00Z","deletion_time":"","destroyed":false,"version":1}}}`
}

func TestNewTokenProvider_KVv2(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kvv2Response(`{"token":"  tok  "}`)))
	})

	p, err := NewTokenProvider(c, "dpf/api")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	// 前後の空白は取り除かれる。
	if got != "tok" {
		t.Errorf("token: got %q, want %q", got, "tok")
	}
	if want := "/v1/secret/data/dpf/api"; gotPath != want {
		t.Errorf("path: got %q, want %q", gotPath, want)
	}
}

func TestNewTokenProvider_KVv1(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"r","data":{"token":"tok1"}}`))
	})

	p, err := NewTokenProvider(c, "dpf/api", WithKVVersion(1))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "tok1" {
		t.Errorf("token: got %q, want %q", got, "tok1")
	}
	if want := "/v1/secret/dpf/api"; gotPath != want {
		t.Errorf("path: got %q, want %q", gotPath, want)
	}
}

func TestNewTokenProvider_MountAndKey(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kvv2Response(`{"dpf_token":"custom"}`)))
	})

	p, err := NewTokenProvider(c, "dpf/api", WithMount("kv"), WithKey("dpf_token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "custom" {
		t.Errorf("token: got %q, want %q", got, "custom")
	}
	if want := "/v1/kv/data/dpf/api"; gotPath != want {
		t.Errorf("path: got %q, want %q", gotPath, want)
	}
}

// トークンは呼び出しのたびに取得されることを検証する。
func TestNewTokenProvider_ReadsAtCallTime(t *testing.T) {
	tokens := []string{"tok-a", "tok-b"}
	var i int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kvv2Response(`{"token":"` + tokens[i] + `"}`)))
		i++
	})

	p, err := NewTokenProvider(c, "dpf/api")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	ctx := context.Background()
	for _, want := range tokens {
		got, err := p(ctx)
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		if got != want {
			t.Errorf("token: got %q, want %q", got, want)
		}
	}
}

func TestNewTokenProvider_KeyNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kvv2Response(`{"other":"x"}`)))
	})

	p, err := NewTokenProvider(c, "dpf/api")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestNewTokenProvider_NonStringValue(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kvv2Response(`{"token":123}`)))
	})

	p, err := NewTokenProvider(c, "dpf/api")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	_, err = p(context.Background())
	if err == nil {
		t.Fatal("expected error for non-string value")
	}
	if !strings.Contains(err.Error(), "not a string") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewTokenProvider_ReadError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
	})

	p, err := NewTokenProvider(c, "dpf/api")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewTokenProvider_Validation(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})

	tests := []struct {
		name   string
		client *api.Client
		path   string
		opts   []Option
	}{
		{name: "nil client", client: nil, path: "dpf/api"},
		{name: "empty path", client: c, path: ""},
		{name: "bad kv version", client: c, path: "dpf/api", opts: []Option{WithKVVersion(3)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTokenProvider(tt.client, tt.path, tt.opts...); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// 空文字の Option は無視され、デフォルトのままになることを検証する。
func TestOptions_IgnoreEmpty(t *testing.T) {
	cfg := &config{mount: DefaultMount, key: DefaultKey, kvVersion: DefaultKVVersion}
	WithMount("")(cfg)
	WithKey("")(cfg)
	if cfg.mount != DefaultMount || cfg.key != DefaultKey {
		t.Errorf("empty options changed config: %+v", cfg)
	}
}
