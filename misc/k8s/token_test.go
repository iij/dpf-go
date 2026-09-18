// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeSecretsAPI は SecretsAPI のテスト用実装。
//
// 呼び出された名前空間・Secret 名・オプションを記録する。動作中のクラスタを
// 必要とせずに取得処理を検証するために用いる。
type fakeSecretsAPI struct {
	data map[string][]byte
	err  error

	calls         int
	lastNamespace string
	lastName      string
	lastOpts      metav1.GetOptions

	// each を指定すると、呼び出しごとに異なる data を返す。
	each []map[string][]byte
}

func (f *fakeSecretsAPI) Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	f.calls++
	f.lastNamespace = namespace
	f.lastName = name
	f.lastOpts = opts

	// ctx が取得処理から素通しで渡っていることを確認するため、
	// 打ち切られていれば ctx のエラーを返す。
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}

	data := f.data
	if len(f.each) > 0 {
		data = f.each[(f.calls-1)%len(f.each)]
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Data:       data,
	}, nil
}

// secretsResource は apierrors のコンストラクタへ渡す対象の指定。
var secretsResource = schema.GroupResource{Group: "", Resource: "secrets"}

// newProvider はテスト用に NewTokenProvider を呼ぶ。名前空間は既定で "dns"。
func newProvider(t *testing.T, f *fakeSecretsAPI, opts ...Option) func(context.Context) (string, error) {
	t.Helper()
	opts = append([]Option{WithNamespace("dns")}, opts...)
	p, err := NewTokenProvider(f, opts...)
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	return p
}

// --- T010: オプションへ空文字を渡した場合は既定値が保たれる (FR-011) ---

func TestOptions_EmptyValueIsIgnored(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
	}{
		{"空文字", []Option{WithNamespace(""), WithSecretName(""), WithKey("")}},
		{"空文字を複数回", []Option{WithSecretName(""), WithSecretName(""), WithKey(""), WithKey("")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig(tt.opts...)
			if cfg.namespace != "" {
				t.Errorf("namespace: got %q, want empty", cfg.namespace)
			}
			if cfg.secretName != DefaultSecretName {
				t.Errorf("secretName: got %q, want %q", cfg.secretName, DefaultSecretName)
			}
			if cfg.key != DefaultKey {
				t.Errorf("key: got %q, want %q", cfg.key, DefaultKey)
			}
		})
	}
}

func TestOptions_LastNonEmptyWins(t *testing.T) {
	cfg := newConfig(WithSecretName("a"), WithSecretName(""), WithSecretName("b"))
	if cfg.secretName != "b" {
		t.Errorf("secretName: got %q, want %q", cfg.secretName, "b")
	}
}

// --- T012: 取得の成功と復号 (受け入れシナリオ 1-1, 1-2, SC-010) ---

func TestNewTokenProvider_ReturnsToken(t *testing.T) {
	f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
	p := newProvider(t, f)

	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "tok" {
		t.Errorf("token: got %q, want %q", got, "tok")
	}
}

func TestNewTokenProvider_TrimsSurroundingSpace(t *testing.T) {
	// kubectl create secret --from-file は末尾の改行を含めることがある。
	f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("  tok\n")}}
	p := newProvider(t, f)

	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "tok" {
		t.Errorf("token: got %q, want %q", got, "tok")
	}
}

func TestNewTokenProvider_DoesNotReturnEncodedValue(t *testing.T) {
	// Secret.Data は復号済みのバイト列である。base64 の二重復号を行っていない
	// ことを、base64 として解釈できる平文で確認する。
	const plain = "dG9rZW4="
	f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte(plain)}}
	p := newProvider(t, f)

	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != plain {
		t.Errorf("token: got %q, want %q (二重復号してはならない)", got, plain)
	}
}

// --- T013: 既定値が問い合わせに使われる (受け入れシナリオ 1-3, 1-4, SC-001) ---

func TestNewTokenProvider_Defaults(t *testing.T) {
	f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
	p := newProvider(t, f)

	if _, err := p(context.Background()); err != nil {
		t.Fatalf("provider: %v", err)
	}
	if f.lastName != DefaultSecretName {
		t.Errorf("secret name: got %q, want %q", f.lastName, DefaultSecretName)
	}
	if f.lastNamespace != "dns" {
		t.Errorf("namespace: got %q, want %q", f.lastNamespace, "dns")
	}
	if f.lastOpts != (metav1.GetOptions{}) {
		t.Errorf("opts: got %+v, want zero value", f.lastOpts)
	}
}

func TestNewTokenProvider_WithSecretName(t *testing.T) {
	f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
	p := newProvider(t, f, WithSecretName("dpf"))

	if _, err := p(context.Background()); err != nil {
		t.Fatalf("provider: %v", err)
	}
	if f.lastName != "dpf" {
		t.Errorf("secret name: got %q, want %q", f.lastName, "dpf")
	}
}

// --- T014: エラーの区別 (受け入れシナリオ 1-7〜1-11, SC-009) ---

func TestNewTokenProvider_ErrorClassification(t *testing.T) {
	otherErr := errors.New("connection refused")

	tests := []struct {
		name    string
		data    map[string][]byte
		apiErr  error
		want    error
		wantRaw error // 番兵ではなく元のエラーが辿れることを期待する場合
	}{
		{
			name:   "Secret が存在しない",
			apiErr: apierrors.NewNotFound(secretsResource, DefaultSecretName),
			want:   ErrSecretNotFound,
		},
		{
			name:   "権限が不足 (403)",
			apiErr: apierrors.NewForbidden(secretsResource, DefaultSecretName, errors.New("denied")),
			want:   ErrForbidden,
		},
		{
			name:   "認証されていない (401)",
			apiErr: apierrors.NewUnauthorized("no credentials"),
			want:   ErrForbidden,
		},
		{
			name:    "その他の失敗",
			apiErr:  otherErr,
			wantRaw: otherErr,
		},
		{
			name: "キーが存在しない",
			data: map[string][]byte{"other": []byte("tok")},
			want: ErrKeyNotFound,
		},
		{
			name: "値が空",
			data: map[string][]byte{"token": []byte("")},
			want: ErrEmptySecret,
		},
		{
			name: "値が空白のみ",
			data: map[string][]byte{"token": []byte(" \n")},
			want: ErrEmptySecret,
		},
		{
			name: "data が nil",
			data: nil,
			want: ErrKeyNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSecretsAPI{data: tt.data, err: tt.apiErr}
			p := newProvider(t, f)

			_, err := p(context.Background())
			if err == nil {
				t.Fatal("エラーが返らなかった")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("errors.Is(err, %v) = false; err = %v", tt.want, err)
			}
			if tt.wantRaw != nil && !errors.Is(err, tt.wantRaw) {
				t.Errorf("元のエラーが辿れない: err = %v", err)
			}
		})
	}
}

func TestNewTokenProvider_ErrorsAreDistinguishable(t *testing.T) {
	// 4 つの原因が互いに区別できること (SC-009)。
	sentinels := []error{ErrSecretNotFound, ErrKeyNotFound, ErrEmptySecret, ErrForbidden}
	cases := []struct {
		data   map[string][]byte
		apiErr error
		want   error
	}{
		{apiErr: apierrors.NewNotFound(secretsResource, DefaultSecretName), want: ErrSecretNotFound},
		{data: map[string][]byte{"other": []byte("x")}, want: ErrKeyNotFound},
		{data: map[string][]byte{"token": []byte("")}, want: ErrEmptySecret},
		{apiErr: apierrors.NewForbidden(secretsResource, DefaultSecretName, errors.New("d")), want: ErrForbidden},
	}

	for _, c := range cases {
		f := &fakeSecretsAPI{data: c.data, err: c.apiErr}
		p := newProvider(t, f)
		_, err := p(context.Background())
		if err == nil {
			t.Fatalf("want %v, got nil", c.want)
		}
		for _, s := range sentinels {
			if errors.Is(err, s) != (s == c.want) {
				t.Errorf("errors.Is(%v, %v) = %v; want %v", err, s, errors.Is(err, s), s == c.want)
			}
		}
	}
}

// --- T015: エラーに Secret の値を含めない (FR-029, SC-018) ---

func TestNewTokenProvider_ErrorDoesNotLeakSecretValue(t *testing.T) {
	const secret = "s3cr3t-token-value"

	tests := []struct {
		name string
		data map[string][]byte
		opts []Option
	}{
		{
			name: "キーが見つからない",
			data: map[string][]byte{"other": []byte(secret)},
		},
		{
			name: "キーが見つからない (別のキーを指定)",
			data: map[string][]byte{"token": []byte(secret)},
			opts: []Option{WithKey("missing")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSecretsAPI{data: tt.data}
			p := newProvider(t, f, tt.opts...)

			_, err := p(context.Background())
			if err == nil {
				t.Fatal("エラーが返らなかった")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("エラーに Secret の値が含まれている: %v", err)
			}
		})
	}
}

// --- T016: WithKey で別のキーから取り出す (FR-017, 受け入れシナリオ 1-13) ---

func TestNewTokenProvider_WithKey(t *testing.T) {
	f := &fakeSecretsAPI{data: map[string][]byte{
		"token":     []byte("wrong"),
		"api-token": []byte("right"),
	}}
	p := newProvider(t, f, WithKey("api-token"))

	got, err := p(context.Background())
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got != "right" {
		t.Errorf("token: got %q, want %q", got, "right")
	}
}

// --- T017: 評価のたびに問い合わせる (FR-007, 受け入れシナリオ 1-6) ---

func TestNewTokenProvider_QueriesEveryCall(t *testing.T) {
	f := &fakeSecretsAPI{each: []map[string][]byte{
		{"token": []byte("first")},
		{"token": []byte("second")},
	}}
	p := newProvider(t, f)

	for i, want := range []string{"first", "second"} {
		got, err := p(context.Background())
		if err != nil {
			t.Fatalf("provider (%d): %v", i, err)
		}
		if got != want {
			t.Errorf("token (%d): got %q, want %q", i, got, want)
		}
	}
	if f.calls != 2 {
		t.Errorf("calls: got %d, want 2 (評価のたびに問い合わせること)", f.calls)
	}
}

// --- T018: 打ち切りに応じる (FR-008, 受け入れシナリオ 1-15) ---

func TestNewTokenProvider_RespectsContextCancellation(t *testing.T) {
	f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
	p := newProvider(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p(ctx)
	if err == nil {
		t.Fatal("打ち切られた context でエラーが返らなかった")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
}

// --- T019: 生成時の検証 (FR-014, SC-006) ---

func TestNewTokenProvider_GenerationTimeValidation(t *testing.T) {
	t.Run("client が nil", func(t *testing.T) {
		_, err := NewTokenProvider(nil, WithNamespace("dns"))
		if !errors.Is(err, ErrNilClient) {
			t.Errorf("errors.Is(err, ErrNilClient) = false; err = %v", err)
		}
	})

	t.Run("名前空間が未指定", func(t *testing.T) {
		f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
		_, err := NewTokenProvider(f)
		if !errors.Is(err, ErrNamespaceUnknown) {
			t.Errorf("errors.Is(err, ErrNamespaceUnknown) = false; err = %v", err)
		}
	})

	t.Run("名前空間が空文字", func(t *testing.T) {
		f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
		_, err := NewTokenProvider(f, WithNamespace(""))
		if !errors.Is(err, ErrNamespaceUnknown) {
			t.Errorf("errors.Is(err, ErrNamespaceUnknown) = false; err = %v", err)
		}
	})

	t.Run("生成時に問い合わせない", func(t *testing.T) {
		f := &fakeSecretsAPI{data: map[string][]byte{"token": []byte("tok")}}
		if _, err := NewTokenProvider(f, WithNamespace("dns")); err != nil {
			t.Fatalf("NewTokenProvider: %v", err)
		}
		if f.calls != 0 {
			t.Errorf("calls: got %d, want 0 (生成時に問い合わせてはならない)", f.calls)
		}
	})
}
