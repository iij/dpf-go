// SPDX-License-Identifier: Apache-2.0

// Package gcp は Google Secret Manager から DPF API のアクセストークンを
// 取得する TokenProvider を提供する。
//
// 本パッケージは github.com/iij/dpf-go 本体に依存しない独立モジュールである。
// GCP を使わない利用者に Google Cloud SDK への依存を持ち込まないよう、
// misc 以下は機能ごとに別モジュールとして切り出している。
//
// # 使い方
//
// Secret Manager クライアントを渡して TokenProvider を作り、
// utils.WithTokenProvider に渡す。
//
//	sm, err := secretmanager.NewClient(ctx)
//	if err != nil {
//		return err
//	}
//	defer sm.Close()
//
//	p, err := gcp.NewTokenProvider(sm, "dpf-api-token", gcp.WithProject("my-project"))
//	if err != nil {
//		return err
//	}
//	c, err := utils.NewClient(
//		utils.WithTokenProvider(p),
//		utils.WithTokenTTL(5*time.Minute), // 毎リクエストの API 呼び出しを避ける
//	)
//
// [NewTokenProvider] が返すのは func(ctx context.Context) (string, error) であり、
// utils.TokenProvider にそのまま代入できる。本パッケージが utils を
// import しないのはこのためである。
//
// 第 1 引数は [SecretManagerAPI] インターフェースであり、
// *secretmanager.Client がこれを満たす。テスト時にはモックへ差し替えられる。
//
// # シークレットの指定方法
//
// 第 2 引数にはシークレット ID か完全修飾リソース名を渡せる。
//
//   - シークレット ID（"dpf-api-token"）: [WithProject] が必須。
//     projects/{project}/secrets/{secret}/versions/{version} を組み立てる。
//   - "projects/{project}/secrets/{secret}": [WithVersion] のバージョンを付け足す。
//   - "projects/{project}/secrets/{secret}/versions/{version}": そのまま使う
//     （[WithVersion] より優先される）。
//
// バージョンの既定値は [DefaultVersion]（"latest"）である。
//
// # 設定
//
//   - [WithProject] : シークレットが属するプロジェクト ID
//   - [WithVersion] : 取得するシークレットバージョン（既定 [DefaultVersion]）
//   - [WithJSONKey] : シークレットを JSON として解釈し、取り出すキー名を指定する
//
// WithJSONKey を指定しない場合はシークレットの値全体をトークンとして扱う。
//
// # 取得タイミングとエラー
//
// TokenProvider は呼び出しのたびに Secret Manager へ問い合わせる。
// 本パッケージ側ではキャッシュを行わないため、頻度を抑えたい場合は
// utils.WithTokenTTL を併用する。これにより、外部でローテーションされた
// トークンを Client を作り直さずに反映できる。
//
// 取得した値は前後の空白を取り除いて返す。ペイロードが空の場合は
// [ErrEmptySecret]、WithJSONKey で指定したキーが存在しない場合は
// [ErrKeyNotFound] を返す。
package gcp
