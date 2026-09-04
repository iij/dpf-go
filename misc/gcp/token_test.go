// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	gax "github.com/googleapis/gax-go/v2"
)

// fakeSecretManager は SecretManagerAPI のテスト用実装。
type fakeSecretManager struct {
	data     []byte
	err      error
	calls    int
	lastName string
	// each を指定すると呼び出しごとに異なる値を返す。
	each []string
}

func (f *fakeSecretManager) AccessSecretVersion(_ context.Context, req *secretmanagerpb.AccessSecretVersionRequest, _ ...gax.CallOption) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	f.lastName = req.GetName()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	data := f.data
	if len(f.each) > 0 {
		data = []byte(f.each[(f.calls-1)%len(f.each)])
	}
	return &secretmanagerpb.AccessSecretVersionResponse{
		Name:    req.GetName(),
		Payload: &secretmanagerpb.SecretPayload{Data: data},
	}, nil
}

func TestNewTokenProvider_PlainSecret(t *testing.T) {
	f := &fakeSecretManager{data: []byte("  tok  ")}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("my-project"))
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
	want := "projects/my-project/secrets/dpf-api-token/versions/latest"
	if f.lastName != want {
		t.Errorf("name: got %q, want %q", f.lastName, want)
	}
}

// リソース名の組み立てパターンを検証する。
func TestResourceName(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		opts   []Option
		want   string
	}{
		{
			name:   "secret id with project",
			secret: "tok",
			opts:   []Option{WithProject("p")},
			want:   "projects/p/secrets/tok/versions/latest",
		},
		{
			name:   "secret id with explicit version",
			secret: "tok",
			opts:   []Option{WithProject("p"), WithVersion("3")},
			want:   "projects/p/secrets/tok/versions/3",
		},
		{
			name:   "fully qualified without version",
			secret: "projects/p/secrets/tok",
			want:   "projects/p/secrets/tok/versions/latest",
		},
		{
			name:   "fully qualified without version, explicit version",
			secret: "projects/p/secrets/tok",
			opts:   []Option{WithVersion("7")},
			want:   "projects/p/secrets/tok/versions/7",
		},
		{
			// バージョンまで含む場合は WithVersion より優先される。
			name:   "fully qualified with version",
			secret: "projects/p/secrets/tok/versions/2",
			opts:   []Option{WithVersion("9")},
			want:   "projects/p/secrets/tok/versions/2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSecretManager{data: []byte("tok")}
			p, err := NewTokenProvider(f, tt.secret, tt.opts...)
			if err != nil {
				t.Fatalf("NewTokenProvider: %v", err)
			}
			if _, err := p(context.Background()); err != nil {
				t.Fatalf("provider: %v", err)
			}
			if f.lastName != tt.want {
				t.Errorf("name: got %q, want %q", f.lastName, tt.want)
			}
		})
	}
}

func TestNewTokenProvider_JSONKey(t *testing.T) {
	f := &fakeSecretManager{data: []byte(`{"token":"jsontok","other":"x"}`)}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"), WithJSONKey("token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "jsontok" {
		t.Errorf("token: got %q, want %q", got, "jsontok")
	}
}

func TestNewTokenProvider_JSONKeyNotFound(t *testing.T) {
	f := &fakeSecretManager{data: []byte(`{"other":"x"}`)}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"), WithJSONKey("token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestNewTokenProvider_JSONParseError(t *testing.T) {
	f := &fakeSecretManager{data: []byte("not json")}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"), WithJSONKey("token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	_, err = p(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse secret as json") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewTokenProvider_JSONKeyNonString(t *testing.T) {
	f := &fakeSecretManager{data: []byte(`{"token":123}`)}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"), WithJSONKey("token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	_, err = p(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not a string") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewTokenProvider_EmptySecret(t *testing.T) {
	f := &fakeSecretManager{data: nil}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("expected ErrEmptySecret, got %v", err)
	}
}

// トークンは呼び出しのたびに取得されることを検証する。
func TestNewTokenProvider_ReadsAtCallTime(t *testing.T) {
	want := []string{"tok-a", "tok-b"}
	f := &fakeSecretManager{each: want}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	ctx := context.Background()
	for _, w := range want {
		got, err := p(ctx)
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
		if got != w {
			t.Errorf("token: got %q, want %q", got, w)
		}
	}
	if f.calls != len(want) {
		t.Errorf("calls: got %d, want %d", f.calls, len(want))
	}
}

func TestNewTokenProvider_APIError(t *testing.T) {
	wantErr := errors.New("permission denied")
	f := &fakeSecretManager{err: wantErr}

	p, err := NewTokenProvider(f, "dpf-api-token", WithProject("p"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	_, err = p(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped wantErr, got %v", err)
	}
}

func TestNewTokenProvider_Validation(t *testing.T) {
	tests := []struct {
		name   string
		client SecretManagerAPI
		secret string
		opts   []Option
	}{
		{name: "nil client", client: nil, secret: "tok", opts: []Option{WithProject("p")}},
		{name: "empty secret", client: &fakeSecretManager{}, secret: ""},
		// シークレット ID 指定なのに project がない。
		{name: "missing project", client: &fakeSecretManager{}, secret: "tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTokenProvider(tt.client, tt.secret, tt.opts...); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// 空文字の Option は無視されることを検証する。
func TestOptions_IgnoreEmpty(t *testing.T) {
	cfg := &config{version: DefaultVersion}
	WithProject("")(cfg)
	WithVersion("")(cfg)
	WithJSONKey("")(cfg)
	if cfg.project != "" || cfg.version != DefaultVersion || cfg.jsonKey != "" {
		t.Errorf("empty options changed config: %+v", cfg)
	}
}
