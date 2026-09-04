// SPDX-License-Identifier: Apache-2.0

package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// fakeSecretsManager は SecretsManagerAPI のテスト用実装。
type fakeSecretsManager struct {
	out   *secretsmanager.GetSecretValueOutput
	err   error
	calls int
	last  *secretsmanager.GetSecretValueInput
	// each を指定すると呼び出しごとに異なるレスポンスを返す。
	each []string
}

func (f *fakeSecretsManager) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.last = in
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.each) > 0 {
		s := f.each[(f.calls-1)%len(f.each)]
		return &secretsmanager.GetSecretValueOutput{SecretString: &s}, nil
	}
	return f.out, nil
}

// stringSecret は SecretString を持つレスポンスを組み立てる。
func stringSecret(s string) *secretsmanager.GetSecretValueOutput {
	return &secretsmanager.GetSecretValueOutput{SecretString: &s}
}

func TestNewTokenProvider_PlainSecret(t *testing.T) {
	f := &fakeSecretsManager{out: stringSecret("  tok  ")}

	p, err := NewTokenProvider(f, "dpf/api-token")
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
	if f.last.SecretId == nil || *f.last.SecretId != "dpf/api-token" {
		t.Errorf("SecretId: got %v", f.last.SecretId)
	}
	if f.last.VersionId != nil || f.last.VersionStage != nil {
		t.Errorf("unexpected version fields: %v %v", f.last.VersionId, f.last.VersionStage)
	}
}

func TestNewTokenProvider_JSONKey(t *testing.T) {
	f := &fakeSecretsManager{out: stringSecret(`{"token":"jsontok","other":"x"}`)}

	p, err := NewTokenProvider(f, "dpf/api-token", WithJSONKey("token"))
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
	f := &fakeSecretsManager{out: stringSecret(`{"other":"x"}`)}

	p, err := NewTokenProvider(f, "dpf/api-token", WithJSONKey("token"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestNewTokenProvider_JSONParseError(t *testing.T) {
	f := &fakeSecretsManager{out: stringSecret("not json")}

	p, err := NewTokenProvider(f, "dpf/api-token", WithJSONKey("token"))
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
	f := &fakeSecretsManager{out: stringSecret(`{"token":123}`)}

	p, err := NewTokenProvider(f, "dpf/api-token", WithJSONKey("token"))
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

func TestNewTokenProvider_SecretBinary(t *testing.T) {
	f := &fakeSecretsManager{out: &secretsmanager.GetSecretValueOutput{SecretBinary: []byte("bintok")}}

	p, err := NewTokenProvider(f, "dpf/api-token")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "bintok" {
		t.Errorf("token: got %q, want %q", got, "bintok")
	}
}

func TestNewTokenProvider_EmptySecret(t *testing.T) {
	f := &fakeSecretsManager{out: &secretsmanager.GetSecretValueOutput{}}

	p, err := NewTokenProvider(f, "dpf/api-token")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); !errors.Is(err, ErrEmptySecret) {
		t.Fatalf("expected ErrEmptySecret, got %v", err)
	}
}

func TestNewTokenProvider_VersionOptions(t *testing.T) {
	f := &fakeSecretsManager{out: stringSecret("tok")}

	p, err := NewTokenProvider(f, "dpf/api-token",
		WithVersionID("v1"), WithVersionStage("AWSPREVIOUS"))
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	if _, err := p(context.Background()); err != nil {
		t.Fatalf("provider: %v", err)
	}
	if f.last.VersionId == nil || *f.last.VersionId != "v1" {
		t.Errorf("VersionId: got %v, want v1", f.last.VersionId)
	}
	if f.last.VersionStage == nil || *f.last.VersionStage != "AWSPREVIOUS" {
		t.Errorf("VersionStage: got %v, want AWSPREVIOUS", f.last.VersionStage)
	}
}

// トークンは呼び出しのたびに取得されることを検証する。
func TestNewTokenProvider_ReadsAtCallTime(t *testing.T) {
	want := []string{"tok-a", "tok-b"}
	f := &fakeSecretsManager{each: want}

	p, err := NewTokenProvider(f, "dpf/api-token")
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
	wantErr := errors.New("access denied")
	f := &fakeSecretsManager{err: wantErr}

	p, err := NewTokenProvider(f, "dpf/api-token")
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	_, err = p(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped wantErr, got %v", err)
	}
}

func TestNewTokenProvider_Validation(t *testing.T) {
	if _, err := NewTokenProvider(nil, "dpf/api-token"); err == nil {
		t.Error("expected error for nil client")
	}
	if _, err := NewTokenProvider(&fakeSecretsManager{}, ""); err == nil {
		t.Error("expected error for empty secret id")
	}
}

// 空文字の Option は無視されることを検証する。
func TestOptions_IgnoreEmpty(t *testing.T) {
	cfg := &config{}
	WithJSONKey("")(cfg)
	WithVersionID("")(cfg)
	WithVersionStage("")(cfg)
	if cfg.jsonKey != "" || cfg.versionID != "" || cfg.versionStage != "" {
		t.Errorf("empty options changed config: %+v", cfg)
	}
}
