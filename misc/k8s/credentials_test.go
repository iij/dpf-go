// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// --- T026: 環境を組み立てる補助 ---

// writeKubeconfig は最小の kubeconfig を書き、そのパスを返す。
//
// namespace が空の場合は context に namespace を書かない。名前空間が
// 決まらない場合の挙動を確かめるために必要である。
func writeKubeconfig(t *testing.T, dir, name, namespace, server string) string {
	t.Helper()

	nsLine := ""
	if namespace != "" {
		nsLine = "\n    namespace: " + namespace
	}
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: ctx1
clusters:
- name: c1
  cluster:
    server: %s
contexts:
- name: ctx1
  context:
    cluster: c1
    user: u1%s
users:
- name: u1
  user:
    token: kubeconfig-bearer-token
`, server, nsLine)

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// unreachableServer はテスト用の接続先。到達しないアドレスを用いることで、
// 生成時にネットワークへ出ていないことを確かめられる (RFC 5737 TEST-NET-1)。
const unreachableServer = "https://192.0.2.1:6443"

// env はテストが用意する実行環境。
type env struct {
	// kubeconfigEnv が空でなければ KUBECONFIG に設定する。
	kubeconfigEnv string
	// homeFile が空でなければ ~/.kube/config の位置として差し替える。
	homeFile string
	// inCluster が真であれば、クラスタ内の資格情報を利用可能にする。
	inCluster bool
	// inClusterNamespace はクラスタ内の名前空間ファイルへ書く値。
	// 空文字ならファイルを作らない。
	inClusterNamespace string
}

// setup は env のとおりに環境変数とパッケージ内の差し替え変数を整える。
func setup(t *testing.T, e env) {
	t.Helper()
	dir := t.TempDir()

	t.Setenv("KUBECONFIG", e.kubeconfigEnv)

	// ~/.kube/config の位置。存在しないパスを既定にしておく。
	origHome := kubeconfigHomeFile
	kubeconfigHomeFile = filepath.Join(dir, "absent-home-kubeconfig")
	if e.homeFile != "" {
		kubeconfigHomeFile = e.homeFile
	}
	t.Cleanup(func() { kubeconfigHomeFile = origHome })

	origToken, origNS := inClusterTokenFile, inClusterNamespaceFile
	origConfig := inClusterConfig
	t.Cleanup(func() {
		inClusterTokenFile, inClusterNamespaceFile = origToken, origNS
		inClusterConfig = origConfig
	})

	// 既定では「クラスタ内ではない」状態にする。
	inClusterTokenFile = filepath.Join(dir, "absent-token")
	inClusterNamespaceFile = filepath.Join(dir, "absent-namespace")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	if e.inCluster {
		inClusterTokenFile = filepath.Join(dir, "sa-token")
		if err := os.WriteFile(inClusterTokenFile, []byte("in-cluster-token"), 0o600); err != nil {
			t.Fatalf("write token file: %v", err)
		}
		t.Setenv("KUBERNETES_SERVICE_HOST", "192.0.2.2")
		t.Setenv("KUBERNETES_SERVICE_PORT", "443")

		// rest.InClusterConfig は固定パスを読むため差し替える。
		// 選択と名前空間の決定が本パッケージの責務であり、接続設定の
		// 組み立ては client-go の責務であるため、ここでは前者だけを検証する。
		inClusterConfig = func() (*rest.Config, error) {
			return &rest.Config{
				Host:            "https://192.0.2.2:443",
				BearerTokenFile: inClusterTokenFile,
			}, nil
		}
	}

	if e.inClusterNamespace != "" {
		inClusterNamespaceFile = filepath.Join(dir, "sa-namespace")
		if err := os.WriteFile(inClusterNamespaceFile, []byte(e.inClusterNamespace), 0o600); err != nil {
			t.Fatalf("write namespace file: %v", err)
		}
	}
}

// --- T027, T028: 候補の選択 8 通り (SC-007) ---

func TestResolveCredentials_Selection(t *testing.T) {
	tests := []struct {
		name       string
		kubeEnv    bool // KUBECONFIG に有効な設定を置く
		home       bool // ~/.kube/config に有効な設定を置く
		inCluster  bool
		wantSource credentialSource
		wantErr    error
	}{
		{name: "1 どれも無い", wantErr: ErrNoCredentials},
		{name: "2 クラスタ内のみ", inCluster: true, wantSource: sourceInCluster},
		{name: "3 home のみ", home: true, wantSource: sourceKubeconfig},
		{name: "4 home とクラスタ内 → home", home: true, inCluster: true, wantSource: sourceKubeconfig},
		{name: "5 KUBECONFIG のみ", kubeEnv: true, wantSource: sourceKubeconfig},
		{name: "6 KUBECONFIG とクラスタ内 → KUBECONFIG", kubeEnv: true, inCluster: true, wantSource: sourceKubeconfig},
		{name: "7 KUBECONFIG と home → KUBECONFIG", kubeEnv: true, home: true, wantSource: sourceKubeconfig},
		{name: "8 すべて → KUBECONFIG", kubeEnv: true, home: true, inCluster: true, wantSource: sourceKubeconfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			e := env{inCluster: tt.inCluster, inClusterNamespace: "sa-ns"}
			if tt.kubeEnv {
				e.kubeconfigEnv = writeKubeconfig(t, dir, "env-kubeconfig", "env-ns", unreachableServer)
			}
			if tt.home {
				e.homeFile = writeKubeconfig(t, dir, "home-kubeconfig", "home-ns", unreachableServer)
			}
			setup(t, e)

			creds, err := resolveCredentials()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("errors.Is(err, %v) = false; err = %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCredentials: %v", err)
			}
			if creds.source != tt.wantSource {
				t.Errorf("source: got %v, want %v", creds.source, tt.wantSource)
			}
		})
	}
}

func TestResolveCredentials_KubeconfigEnvWinsOverHome(t *testing.T) {
	dir := t.TempDir()
	setup(t, env{
		kubeconfigEnv: writeKubeconfig(t, dir, "env-kubeconfig", "env-ns", unreachableServer),
		homeFile:      writeKubeconfig(t, dir, "home-kubeconfig", "home-ns", unreachableServer),
	})

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	// KUBECONFIG 側の namespace が使われる。~/.kube/config は読まれない。
	if creds.namespace != "env-ns" {
		t.Errorf("namespace: got %q, want %q (~/.kube/config を読んではならない)", creds.namespace, "env-ns")
	}
}

// --- SC-008: client-go の選択と一致する ---

func TestResolveCredentials_MatchesClientGo(t *testing.T) {
	// 設定ファイルに関する選択が、client-go の DeferredLoadingClientConfig の
	// 選択と一致することを確かめる。本パッケージが client-go の分岐を
	// 写していることの歯止めである。
	//
	// クラスタ内の資格情報を模擬した組み合わせは比較できない。client-go の
	// Possible は固定パス (/var/run/secrets/...) を読むため、テストが用意した
	// 資格情報を見られないためである。クラスタ内が絡む順序は
	// TestResolveCredentials_Selection の 2・4・6・8 が受け持つ。
	tests := []struct {
		name    string
		kubeEnv bool
		home    bool
		broken  bool // 設定ファイルはあるが接続情報として使えない
	}{
		{name: "どちらも無い"},
		{name: "home のみ", home: true},
		{name: "KUBECONFIG のみ", kubeEnv: true},
		{name: "KUBECONFIG と home", kubeEnv: true, home: true},
		{name: "使えない設定ファイル", kubeEnv: true, broken: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			var e env
			switch {
			case tt.broken:
				// current-context が存在しない名前を指す設定。
				p := filepath.Join(dir, "unusable-kubeconfig")
				content := `apiVersion: v1
kind: Config
current-context: missing-ctx
clusters:
- name: c1
  cluster:
    server: ` + unreachableServer + `
contexts:
- name: ctx1
  context:
    cluster: c1
    user: u1
users:
- name: u1
  user:
    token: t
`
				if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
				e.kubeconfigEnv = p
			default:
				if tt.kubeEnv {
					e.kubeconfigEnv = writeKubeconfig(t, dir, "env-kubeconfig", "env-ns", unreachableServer)
				}
				if tt.home {
					e.homeFile = writeKubeconfig(t, dir, "home-kubeconfig", "home-ns", "https://192.0.2.3:6443")
				}
			}
			setup(t, e)

			creds, ourErr := resolveCredentials()

			// client-go 自身に同じ探索規則で選ばせる。
			dlc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				newLoadingRules(), &clientcmd.ConfigOverrides{})
			theirs, theirErr := dlc.ClientConfig()

			if (ourErr != nil) != (theirErr != nil) {
				t.Fatalf("エラーの有無が食い違う: ours = %v, theirs = %v", ourErr, theirErr)
			}
			if ourErr != nil {
				return
			}
			if creds.restConfig.Host != theirs.Host {
				t.Errorf("接続先が食い違う: ours = %q, theirs = %q", creds.restConfig.Host, theirs.Host)
			}
		})
	}
}

// --- T029: 設定ファイルの状態ごとの扱い (FR-022, FR-023) ---

func TestResolveCredentials_BrokenKubeconfigDoesNotFallThrough(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken-kubeconfig")
	if err := os.WriteFile(broken, []byte("{{ this is not yaml"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setup(t, env{kubeconfigEnv: broken, inCluster: true, inClusterNamespace: "sa-ns"})

	creds, err := resolveCredentials()
	if err == nil {
		t.Fatalf("壊れた設定ファイルでエラーが返らなかった (source = %v)", creds.source)
	}
	if errors.Is(err, ErrNoCredentials) {
		t.Errorf("ErrNoCredentials ではなく解釈の失敗を返すこと: %v", err)
	}
}

func TestResolveCredentials_MissingKubeconfigFallsThrough(t *testing.T) {
	dir := t.TempDir()
	setup(t, env{
		kubeconfigEnv:      filepath.Join(dir, "does-not-exist"),
		inCluster:          true,
		inClusterNamespace: "sa-ns",
	})

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("存在しない設定ファイルは無いものとして扱うこと: %v", err)
	}
	if creds.source != sourceInCluster {
		t.Errorf("source: got %v, want %v", creds.source, sourceInCluster)
	}
}

func TestResolveCredentials_MissingKubeconfigAndNoInCluster(t *testing.T) {
	dir := t.TempDir()
	setup(t, env{kubeconfigEnv: filepath.Join(dir, "does-not-exist")})

	if _, err := resolveCredentials(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("errors.Is(err, ErrNoCredentials) = false; err = %v", err)
	}
}

// --- T030: 候補が無い (FR-021, SC-006) ---

func TestResolveCredentials_NoCredentials(t *testing.T) {
	setup(t, env{})

	if _, err := resolveCredentials(); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("errors.Is(err, ErrNoCredentials) = false; err = %v", err)
	}
}

// --- T031: 名前空間の導出 (FR-013, SC-009) ---

func TestResolveCredentials_NamespaceFromKubeconfig(t *testing.T) {
	dir := t.TempDir()
	setup(t, env{homeFile: writeKubeconfig(t, dir, "home-kubeconfig", "from-context", unreachableServer)})

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if creds.namespace != "from-context" {
		t.Errorf("namespace: got %q, want %q", creds.namespace, "from-context")
	}
}

func TestResolveCredentials_NamespaceFromInCluster(t *testing.T) {
	setup(t, env{inCluster: true, inClusterNamespace: "  from-file\n"})

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	// 前後の空白は取り除く。
	if creds.namespace != "from-file" {
		t.Errorf("namespace: got %q, want %q", creds.namespace, "from-file")
	}
}

// --- T032: 名前空間が決まらない (FR-014, SC-009) ---

// TestResolveCredentials_ContextProblems は、選択中の接続先そのものに問題が
// ある設定ファイルの扱いを固定する。
//
// これらは名前空間の決定に至る前に判定される。context が解決できない設定は
// 接続情報として使えないため FR-022 のエラーとなり、current-context が無い
// 設定は接続情報が空であるため FR-023 により次の候補へ進む。
func TestResolveCredentials_ContextProblems(t *testing.T) {
	danglingContext := `apiVersion: v1
kind: Config
current-context: missing-ctx
clusters:
- name: c1
  cluster:
    server: ` + unreachableServer + `
contexts:
- name: ctx1
  context:
    cluster: c1
    user: u1
users:
- name: u1
  user:
    token: t
`
	noCurrentContext := `apiVersion: v1
kind: Config
clusters:
- name: c1
  cluster:
    server: ` + unreachableServer + `
contexts:
- name: ctx1
  context:
    cluster: c1
    user: u1
    namespace: dns
users:
- name: u1
  user:
    token: t
`

	t.Run("current-context が存在しない名前を指す", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "kubeconfig")
		if err := os.WriteFile(p, []byte(danglingContext), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		// クラスタ内の資格情報があっても、そちらへ移ってはならない (FR-022)。
		setup(t, env{homeFile: p, inCluster: true, inClusterNamespace: "sa-ns"})

		creds, err := resolveCredentials()
		if err == nil {
			t.Fatalf("エラーが返らなかった (source = %v)", creds.source)
		}
		if errors.Is(err, ErrNoCredentials) || errors.Is(err, ErrNamespaceUnknown) {
			t.Errorf("接続情報として使えないことを表すエラーであること: %v", err)
		}
	})

	t.Run("current-context が無い", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "kubeconfig")
		if err := os.WriteFile(p, []byte(noCurrentContext), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		// 接続情報が空であるため、次の候補へ進む (FR-023)。
		setup(t, env{homeFile: p, inCluster: true, inClusterNamespace: "sa-ns"})

		creds, err := resolveCredentials()
		if err != nil {
			t.Fatalf("次の候補へ進むこと: %v", err)
		}
		if creds.source != sourceInCluster {
			t.Errorf("source: got %v, want %v", creds.source, sourceInCluster)
		}
	})

	t.Run("current-context が無く、次の候補も無い", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "kubeconfig")
		if err := os.WriteFile(p, []byte(noCurrentContext), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		setup(t, env{homeFile: p})

		if _, err := resolveCredentials(); !errors.Is(err, ErrNoCredentials) {
			t.Errorf("errors.Is(err, ErrNoCredentials) = false; err = %v", err)
		}
	})
}

func TestNewTokenProviderFromEnvironment_NamespaceUnknown(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) env
	}{
		{
			name: "context に namespace が無い",
			build: func(t *testing.T) env {
				dir := t.TempDir()
				return env{homeFile: writeKubeconfig(t, dir, "home-kubeconfig", "", unreachableServer)}
			},
		},
		{
			name:  "クラスタ内で名前空間ファイルが読めない",
			build: func(t *testing.T) env { return env{inCluster: true} },
		},
		{
			name: "クラスタ内で名前空間ファイルが空",
			build: func(t *testing.T) env {
				return env{inCluster: true, inClusterNamespace: "   \n"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.build(t)
			setup(t, e)

			_, err := NewTokenProviderFromEnvironment()
			if !errors.Is(err, ErrNamespaceUnknown) {
				t.Errorf("errors.Is(err, ErrNamespaceUnknown) = false; err = %v", err)
			}
		})
	}
}

// --- T033: 候補をまたいで回り込まない (受け入れシナリオ 2-12) ---

func TestNewTokenProviderFromEnvironment_NamespaceDoesNotCrossCredentials(t *testing.T) {
	dir := t.TempDir()
	// 設定ファイルが選ばれる。その context に namespace は無い。
	// クラスタ内の名前空間ファイルは読める状態にしておく。
	setup(t, env{
		homeFile:           writeKubeconfig(t, dir, "home-kubeconfig", "", unreachableServer),
		inCluster:          true,
		inClusterNamespace: "should-not-be-used",
	})

	_, err := NewTokenProviderFromEnvironment()
	if !errors.Is(err, ErrNamespaceUnknown) {
		t.Fatalf("別の認証情報の名前空間へ回り込んではならない: err = %v", err)
	}
}

// --- T034: POD_NAMESPACE を参照しない (research.md D6) ---

func TestNewTokenProviderFromEnvironment_IgnoresPodNamespace(t *testing.T) {
	setup(t, env{inCluster: true})
	t.Setenv("POD_NAMESPACE", "from-downward-api")

	_, err := NewTokenProviderFromEnvironment()
	if !errors.Is(err, ErrNamespaceUnknown) {
		t.Errorf("POD_NAMESPACE を参照してはならない: err = %v", err)
	}
}

// --- T035: 生成時にネットワークへ出ない (SC-012) ---

func TestNewTokenProviderFromEnvironment_NoNetworkAtCreation(t *testing.T) {
	dir := t.TempDir()
	setup(t, env{homeFile: writeKubeconfig(t, dir, "home-kubeconfig", "dns", unreachableServer)})

	// 到達しない接続先を指していても、生成は成功する。
	p, err := NewTokenProviderFromEnvironment()
	if err != nil {
		t.Fatalf("NewTokenProviderFromEnvironment: %v", err)
	}
	if p == nil {
		t.Fatal("取得処理が nil")
	}
}

// --- T036: WithNamespace の明示 (受け入れシナリオ 2-14) ---

func TestNewTokenProviderFromEnvironment_ExplicitNamespace(t *testing.T) {
	dir := t.TempDir()
	// 認証情報に名前空間の値が無い状態。
	setup(t, env{homeFile: writeKubeconfig(t, dir, "home-kubeconfig", "", unreachableServer)})

	if _, err := NewTokenProviderFromEnvironment(WithNamespace("explicit")); err != nil {
		t.Errorf("WithNamespace を明示すればエラーにならないこと: %v", err)
	}
}

// --- T038: 設定ファイルが欠けていても警告を出さない (FR-035) ---

func TestNewLoadingRules_SilencesWarner(t *testing.T) {
	setup(t, env{})

	rules := newLoadingRules()
	if rules.Warner == nil {
		t.Fatal("Warner が未設定である。client-go が klog へ警告を書く")
	}
	// 呼んでも何も起きないこと (panic しないこと) を確認する。
	rules.Warner.Warn(errors.New("missing config"))
}
