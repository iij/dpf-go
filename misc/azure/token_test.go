// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// fakeSecretsClient は SecretsAPI のテスト用実装。
type fakeSecretsClient struct {
	value       *string
	err         error
	calls       int
	lastName    string
	lastVersion string
	// each を指定すると呼び出しごとに異なる値を返す。
	each []string
}

func (f *fakeSecretsClient) GetSecret(_ context.Context, name, version string, _ *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
	f.lastName = name
	f.lastVersion = version
	f.calls++
	if f.err != nil {
		return azsecrets.GetSecretResponse{}, f.err
	}
	v := f.value
	if len(f.each) > 0 {
		s := f.each[(f.calls-1)%len(f.each)]
		v = &s
	}
	return azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: v}}, nil
}

// secretValue はテスト用に文字列のポインタを返す。
func secretValue(s string) *string { return &s }

func TestNewTokenProvider_PlainSecret(t *testing.T) {
	f := &fakeSecretsClient{value: secretValue("  tok  ")}

	p, err := NewTokenProvider(f, "dpf-api-token")
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
	if f.lastName != "dpf-api-token" {
		t.Errorf("name: got %q", f.lastName)
	}
	// バージョン未指定時は空文字（＝最新）を渡す。
	if f.lastVersion != "" {
		t.Errorf("version: got %q, want empty", f.lastVersion)
	}
}

func TestNewTokenProvider_WithVersion(t *testing.T) {
	f := &fakeSecretsClient{value: secretValue("tok")}

	p, err := NewTokenProvider(f, "dpf-api-token", WithVersion("abc123"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); err != nil {
		t.Fatalf("provider: %v", err)
	}
	if f.lastVersion != "abc123" {
		t.Errorf("version: got %q, want %q", f.lastVersion, "abc123")
	}
}

func TestNewTokenProvider_JSONKey(t *testing.T) {
	f := &fakeSecretsClient{value: secretValue(`{"token":"jsontok","other":"x"}`)}

	p, err := NewTokenProvider(f, "dpf-api-token", WithJSONKey("token"))
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
	f := &fakeSecretsClient{value: secretValue(`{"other":"x"}`)}

	p, err := NewTokenProvider(f, "dpf-api-token", WithJSONKey("token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestNewTokenProvider_JSONParseError(t *testing.T) {
	f := &fakeSecretsClient{value: secretValue("not json")}

	p, err := NewTokenProvider(f, "dpf-api-token", WithJSONKey("token"))
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
	f := &fakeSecretsClient{value: secretValue(`{"token":123}`)}

	p, err := NewTokenProvider(f, "dpf-api-token", WithJSONKey("token"))
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
	for _, tt := range []struct {
		name  string
		value *string
	}{
		{name: "nil value", value: nil},
		{name: "empty value", value: secretValue("")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSecretsClient{value: tt.value}
			p, err := NewTokenProvider(f, "dpf-api-token")
			if err != nil {
				t.Fatalf("NewTokenProvider: %v", err)
			}
			if _, err := p(context.Background()); !errors.Is(err, ErrEmptySecret) {
				t.Fatalf("expected ErrEmptySecret, got %v", err)
			}
		})
	}
}

// トークンは呼び出しのたびに取得されることを検証する。
func TestNewTokenProvider_ReadsAtCallTime(t *testing.T) {
	want := []string{"tok-a", "tok-b"}
	f := &fakeSecretsClient{each: want}

	p, err := NewTokenProvider(f, "dpf-api-token")
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
	wantErr := errors.New("forbidden")
	f := &fakeSecretsClient{err: wantErr}

	p, err := NewTokenProvider(f, "dpf-api-token")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	_, err = p(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped wantErr, got %v", err)
	}
}

func TestNewTokenProvider_Validation(t *testing.T) {
	if _, err := NewTokenProvider(nil, "dpf-api-token"); err == nil {
		t.Error("expected error for nil client")
	}
	if _, err := NewTokenProvider(&fakeSecretsClient{}, ""); err == nil {
		t.Error("expected error for empty secret name")
	}
}

// 空文字の Option は無視されることを検証する。
func TestOptions_IgnoreEmpty(t *testing.T) {
	cfg := &config{}
	WithJSONKey("")(cfg)
	WithVersion("")(cfg)
	if cfg.jsonKey != "" || cfg.version != "" {
		t.Errorf("empty options changed config: %+v", cfg)
	}
}
