// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/client-go/tools/clientcmd"
)

// credentialSource は接続情報がどこから得られたかを表す。
//
// 名前空間の導出元を決めるためだけに用いる。接続情報の選択そのものは
// client-go の規則に従う。
type credentialSource int

const (
	// sourceKubeconfig は設定ファイル (KUBECONFIG が指すもの、
	// 無ければ利用者の既定の設定ファイル) から得られたことを表す。
	sourceKubeconfig credentialSource = iota + 1
	// sourceInCluster はクラスタ内で割り当てられた資格情報から
	// 得られたことを表す。
	sourceInCluster
)

// String は credentialSource の名前を返す。テストの出力に用いる。
func (s credentialSource) String() string {
	switch s {
	case sourceKubeconfig:
		return "kubeconfig"
	case sourceInCluster:
		return "in-cluster"
	default:
		return "unknown"
	}
}

// 以下は実行環境を読む位置であり、単体テストから差し替える。
//
// 実ファイルシステムの固定パスに依存すると、候補の選択と名前空間の導出を
// 動作中のクラスタ無しに検証できない。公開 API には出さない。
var (
	// inClusterTokenFile はクラスタ内で割り当てられる資格情報の位置。
	// client-go の rest.InClusterConfig が読む位置と同じである。
	inClusterTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// inClusterNamespaceFile はクラスタ内で割り当てられる名前空間の位置。
	inClusterNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	// kubeconfigHomeFile は KUBECONFIG が未設定のときに読む設定ファイル。
	kubeconfigHomeFile = clientcmd.RecommendedHomeFile

	// inClusterConfig はクラスタ内の資格情報から接続設定を組み立てる。
	// 組み立ては client-go の責務であるため、既定では rest.InClusterConfig を用いる。
	inClusterConfig = rest.InClusterConfig
)

// credentials は選ばれた接続情報。
type credentials struct {
	// restConfig は組み立てられた接続設定。
	restConfig *rest.Config
	// source はどの候補が選ばれたか。
	source credentialSource
	// namespace は選ばれた認証情報が持つ名前空間の値。
	// 得られなかった場合は空である。エラーにはしない。
	// WithNamespace が指定されていれば、そちらが優先されるためである。
	namespace string
}

// newLoadingRules は設定ファイルの探索規則を返す。
//
// clientcmd.NewDefaultClientConfigLoadingRules と同じ規則である。すなわち
// 環境変数 KUBECONFIG が設定されていればその値を、設定されていなければ
// 利用者の既定の設定ファイルを読む。この 2 つは 1 つの候補であり、
// 前者が設定されていれば後者は読まれない。
//
// 既定の規則と異なる点は 2 つある。
//
// 1 つは Warner に何もしない関数を置くことである。設定ファイルが欠けている
// ことは正常な経路 (クラスタ内の資格情報へ進む) であり、警告にあたらない。
// 未設定のままにすると client-go が klog へ書く。大域の klog 設定には
// 触れないため、呼び出し側のプロセスへの副作用は無い。
//
// もう 1 つは、既定の設定ファイルの位置を差し替え可能にすることである。
// 単体テストのためであり、挙動は変わらない。
func newLoadingRules() *clientcmd.ClientConfigLoadingRules {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.Warner = func(error) {}

	// KUBECONFIG が未設定のとき、既定の Precedence は既定の設定ファイル 1 件
	// である。判定条件は clientcmd.NewDefaultClientConfigLoadingRules と同じ。
	if os.Getenv(clientcmd.RecommendedConfigPathEnvVar) == "" {
		rules.Precedence = []string{kubeconfigHomeFile}
	}
	return rules
}

// inClusterPossible はクラスタ内の資格情報が利用可能かを返す。
//
// 判定条件は client-go の inClusterClientConfig.Possible と同じである。
// すなわち資格情報のファイルが存在し (ディレクトリではなく)、かつ
// 接続先を示す 2 つの環境変数がいずれも空でないことである。
// 環境変数が無ければ問い合わせ先が存在しないため、資格情報として使えない。
func inClusterPossible() bool {
	fi, err := os.Stat(inClusterTokenFile)
	return os.Getenv("KUBERNETES_SERVICE_HOST") != "" &&
		os.Getenv("KUBERNETES_SERVICE_PORT") != "" &&
		err == nil && !fi.IsDir()
}

// resolveCredentials は実行環境から接続情報を選ぶ。
//
// 順序は client-go の DeferredLoadingClientConfig と同じである。設定ファイル
// から接続情報が得られればそれを使い、得られない場合にクラスタ内の資格情報を
// 使う。どちらも得られなければ ErrNoCredentials を返す。
//
// 設定ファイルが存在しない場合は、その候補が無いものとして次へ進む。
// 存在して解釈できない場合はエラーとし、次の候補へ移らない。壊れた設定を
// 黙って迂回すると、意図した接続先とは別の場所へ問い合わせることになる。
func resolveCredentials() (*credentials, error) {
	rules := newLoadingRules()

	// 設定ファイルを読む。存在しないファイルは読み飛ばされ、
	// 解釈できないファイルはここでエラーになる。
	raw, err := rules.Load()
	if err != nil {
		return nil, fmt.Errorf("k8s: load kubeconfig: %w", err)
	}

	// 設定ファイル単独で接続情報が得られるかを見る。
	direct := clientcmd.NewNonInteractiveClientConfig(*raw, "", &clientcmd.ConfigOverrides{}, rules)
	kubeConfig, kubeErr := direct.ClientConfig()
	switch {
	case kubeErr != nil && !clientcmd.IsEmptyConfig(kubeErr):
		// 設定ファイルはあるが接続情報として使えない。次の候補へ移らない。
		return nil, fmt.Errorf("k8s: use kubeconfig: %w", kubeErr)
	case kubeErr == nil && !rules.IsDefaultConfig(kubeConfig):
		return &credentials{
			restConfig: kubeConfig,
			source:     sourceKubeconfig,
			namespace:  namespaceFromKubeconfig(raw),
		}, nil
	}

	// 設定ファイルから得られなかった。クラスタ内の資格情報を見る。
	if inClusterPossible() {
		restConfig, err := inClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s: use in-cluster credentials: %w", err)
		}
		return &credentials{
			restConfig: restConfig,
			source:     sourceInCluster,
			namespace:  namespaceFromInCluster(),
		}, nil
	}

	return nil, ErrNoCredentials
}

// namespaceFromKubeconfig は設定ファイルの選択中の接続先に記された
// 名前空間の値を返す。得られない場合は空を返す。
//
// clientcmd の Namespace は、値が無いときに "default" を返し、しかも
// 明示された "default" と区別できない。本パッケージは名前空間を推測しない
// ため、解釈前の設定から直接読む。
func namespaceFromKubeconfig(raw *clientcmdapi.Config) string {
	if raw.CurrentContext == "" {
		return ""
	}
	ctx, ok := raw.Contexts[raw.CurrentContext]
	if !ok || ctx == nil {
		return ""
	}
	return strings.TrimSpace(ctx.Namespace)
}

// namespaceFromInCluster はクラスタ内で割り当てられた名前空間の値を返す。
// 読めない場合は空を返す。
//
// 環境変数 POD_NAMESPACE は参照しない。Downward API で利用者が任意に注入する
// 値であり、資格情報が持つ値ではない。トークンを読む権限は資格情報の名前空間に
// 紐づくため、食い違った場合は権限不足で失敗する。名前空間を変えたい場合は
// WithNamespace を用いること。
func namespaceFromInCluster() string {
	b, err := os.ReadFile(inClusterNamespaceFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// NewTokenProviderFromEnvironment は、実行環境から接続情報を選び、
// Kubernetes の Secret からトークンを取得する TokenProvider を返す。
//
// 返る関数は func(ctx context.Context) (string, error) であり、
// utils.WithTokenProvider にそのまま渡せる。
//
// 接続情報は Kubernetes の標準的なクライアントと同じ順序で選ばれる。
// 環境変数 KUBECONFIG が設定されていればそれが指す設定ファイルを、
// 設定されていなければ利用者の既定の設定ファイル (~/.kube/config) を読み、
// そこから接続情報が得られない場合にクラスタ内で割り当てられた資格情報を
// 使う。どちらからも得られない場合は [ErrNoCredentials] を返す。
//
// 名前空間は [WithNamespace] があればその値、無ければ選ばれた認証情報が
// 持つ値を用いる。設定ファイルが選ばれた場合は選択中の接続先に記された
// 名前空間、クラスタ内の資格情報が選ばれた場合は資格情報と同じ位置に置かれた
// 名前空間の値である。いずれも得られない場合は [ErrNamespaceUnknown] を返す。
// "default" などの推測で補うことはない。
//
// 判定できるものはすべて生成の時点で返る。生成の時点では Kubernetes へ
// 問い合わせないため、接続先へ到達できるかどうかは生成の成否に影響しない。
func NewTokenProviderFromEnvironment(opts ...Option) (func(ctx context.Context) (string, error), error) {
	cfg := newConfig(opts...)

	creds, err := resolveCredentials()
	if err != nil {
		return nil, err
	}

	if cfg.namespace == "" {
		cfg.namespace = creds.namespace
	}
	if cfg.namespace == "" {
		return nil, ErrNamespaceUnknown
	}

	clientset, err := kubernetes.NewForConfig(creds.restConfig)
	if err != nil {
		return nil, fmt.Errorf("k8s: build clientset: %w", err)
	}

	return newProviderFunc(NewSecretsAPI(clientset), cfg), nil
}
