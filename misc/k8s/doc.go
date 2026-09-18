// SPDX-License-Identifier: Apache-2.0

// Package k8s は Kubernetes の Secret から DPF API のアクセストークンを
// 取得する TokenProvider を提供する。
//
// DPF は IIJ DNSプラットフォームサービスの略称である。
//
// 本パッケージは github.com/iij/dpf-go 本体に依存しない独立モジュールである。
// Kubernetes を使わない利用者に client-go への依存を持ち込まないよう、
// misc 以下は機能ごとに別モジュールとして切り出している。
//
// # 使い方
//
// クラスタ内で動かす場合、引数は要らない。
//
//	p, err := k8s.NewTokenProviderFromEnvironment()
//	if err != nil {
//		return err
//	}
//	c, err := utils.NewClient(
//		utils.WithTokenProvider(p),
//		utils.WithTokenTTL(5*time.Minute), // 毎リクエストの API 呼び出しを避ける
//	)
//
// 既定では、資格情報に紐づく名前空間の Secret "dpf-token" の data の
// "token" を読む。あらかじめ次のように作っておけばよい。
//
//	kubectl -n dns create secret generic dpf-token --from-literal=token="$DPF_API_TOKEN"
//
// [NewTokenProviderFromEnvironment] と [NewTokenProvider] が返すのは
// func(ctx context.Context) (string, error) であり、utils.TokenProvider に
// そのまま代入できる。本パッケージが utils を import しないのはこのためである。
//
// 接続情報を自分で用意する場合は [NewTokenProvider] に [SecretsAPI] を渡す。
// *kubernetes.Clientset は [NewSecretsAPI] で適合させられる。この経路では
// 名前空間を [WithNamespace] で指定しなければならない。渡されたクライアントは
// 名前空間の値を持たないためである。
//
//	p, err := k8s.NewTokenProvider(
//		k8s.NewSecretsAPI(clientset),
//		k8s.WithNamespace("dns"),
//	)
//
// [SecretsAPI] はインターフェースであり、テスト時にはモックへ差し替えられる。
//
// # 設定
//
//   - [WithNamespace]  : Secret が属する名前空間（未指定なら認証情報が持つ値）
//   - [WithSecretName] : Secret の名前（未指定なら [DefaultSecretName]）
//   - [WithKey]        : data のうちトークンを保持するキー名（未指定なら [DefaultKey]）
//
// いずれのオプションも、空文字を渡した場合はその指定を無視し既定値を保つ。
//
// # 接続情報の選択
//
// [NewTokenProviderFromEnvironment] は、Kubernetes の標準的なクライアントと
// 同じ順序で接続情報を選ぶ。利用者が普段使っているコマンドと同じ接続先を
// 向くようにするためである。
//
//  1. 設定ファイル。環境変数 KUBECONFIG が設定されていればそれが指すもの、
//     設定されていなければ ~/.kube/config を読む。この 2 つは 1 つの候補で
//     あり、前者が設定されていれば後者は読まれない。
//  2. クラスタ内で割り当てられた資格情報
//     (/var/run/secrets/kubernetes.io/serviceaccount/token)。候補 1 から
//     接続情報が得られない場合に用いる。
//
// どちらからも得られない場合は [ErrNoCredentials] を返す。
//
// 設定ファイルが存在しない場合は、その候補が無いものとして次へ進む。
// 存在して解釈できない場合、および選択中の接続先が解決できない場合は
// エラーとし、次の候補へ移らない。壊れた設定を黙って迂回すると、
// 意図した接続先とは別の場所へ問い合わせることになるためである。
//
// # 名前空間の決定
//
// [WithNamespace] を指定しない場合、名前空間は**選ばれた認証情報が持つ値**を
// 用いる。設定ファイルが選ばれた場合は選択中の接続先に記された namespace、
// クラスタ内の資格情報が選ばれた場合は資格情報と同じ位置に置かれた
// /var/run/secrets/kubernetes.io/serviceaccount/namespace である。
//
// 得られない場合は [ErrNamespaceUnknown] を返す。"default" などの推測で
// 補うことはなく、別の認証情報の名前空間へ回り込むこともしない。接続先を
// 取り違えた場合は認証が失敗して気づけるが、名前空間を取り違えた場合は
// 別の Secret を読んで成功しうるためである。環境変数 POD_NAMESPACE も
// 参照しない。名前空間を変えたい場合は [WithNamespace] を用いること。
//
// なお、この点は client-go の挙動と異なる。client-go は設定ファイルの
// namespace が空のときクラスタ内の名前空間へ回り込み、最後は "default" を
// 補う。接続情報の選択は client-go に合わせているが、名前空間の決定は
// 合わせていない。
//
// # 取得タイミングとエラー
//
// TokenProvider は呼び出しのたびに Secret へ問い合わせる。本パッケージ側では
// キャッシュを行わないため、頻度を抑えたい場合は utils.WithTokenTTL を
// 併用する。これにより、外部でローテーションされたトークンを Client を
// 作り直さずに反映できる。
//
// Secret.Data は API の応答を解釈した時点で base64 が復号されているため、
// 本パッケージで復号は行わない。取得した値は前後の空白を取り除いて返す。
// StringData は書き込み専用であり応答に含まれないため参照しない。
//
// 判定できるものはすべて生成の時点で返る。生成の時点では Kubernetes へ
// 問い合わせないため、接続先へ到達できるかどうかは生成の成否に影響しない。
// 評価の時点でしか分からないのは Secret の内容と権限である。
//
//   - [ErrNilClient]        : [NewTokenProvider] に client が渡されていない（生成時）
//   - [ErrNoCredentials]    : 接続情報が 1 つも見つからない（生成時）
//   - [ErrNamespaceUnknown] : 名前空間を決められない（生成時）
//   - [ErrSecretNotFound]   : Secret が存在しない（評価時）
//   - [ErrKeyNotFound]      : data に指定したキーが無い（評価時）
//   - [ErrEmptySecret]      : 値が空（評価時）
//   - [ErrForbidden]        : Secret を読む権限が不足（評価時）
//
// これらは errors.Is で互いに区別できる。元のエラーは常に包んで返すため、
// errors.As で取り出せる。エラーの文言に Secret の値そのものは含めない。
//
// Secret を読むには対象の名前空間で get 権限が必要である。
//
//	kubectl -n dns create role dpf-token-reader \
//	  --verb=get --resource=secrets --resource-name=dpf-token
//	kubectl -n dns create rolebinding dpf-token-reader \
//	  --role=dpf-token-reader --serviceaccount=dns:default
//
// # ログ出力について
//
// 本パッケージはログを出力しない。ただし依存する client-go は内部で
// k8s.io/klog/v2 を用いており、一部の経路（クラスタ内の資格情報で CA を
// 読めなかった場合など）で標準エラー出力へ書く。これは依存の挙動であり、
// 本パッケージが出力するものではない。
//
// 本パッケージは klog を呼ばず、抑止のための大域的な設定も行わない。
// 呼び出し側のプロセス全体の設定を書き換えることになるためである。
// 出力を制御したい場合は、利用者のプロセスで klog を設定すること。
package k8s
