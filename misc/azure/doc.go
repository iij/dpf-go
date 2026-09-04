// SPDX-License-Identifier: Apache-2.0

// Package azure は Azure Key Vault から DPF API のアクセストークンを
// 取得する TokenProvider を提供する。
//
// 本パッケージは github.com/iij/dpf-go 本体に依存しない独立モジュールである。
// Azure を使わない利用者に Azure SDK への依存を持ち込まないよう、
// misc 以下は機能ごとに別モジュールとして切り出している。
//
// # 使い方
//
// Key Vault クライアントを渡して TokenProvider を作り、
// utils.WithTokenProvider に渡す。
//
//	cred, err := azidentity.NewDefaultAzureCredential(nil)
//	if err != nil {
//		return err
//	}
//	kv, err := azsecrets.NewClient("https://example.vault.azure.net/", cred, nil)
//	if err != nil {
//		return err
//	}
//	p, err := azure.NewTokenProvider(kv, "dpf-api-token")
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
// 第 1 引数は [SecretsAPI] インターフェースであり、*azsecrets.Client が
// これを満たす。テスト時にはモックへ差し替えられる。
//
// # 設定
//
//   - [WithJSONKey] : シークレットを JSON として解釈し、取り出すキー名を指定する
//   - [WithVersion] : 取得するシークレットのバージョン（未指定なら最新）
//
// WithJSONKey を指定しない場合はシークレットの値全体をトークンとして扱う。
//
// # 取得タイミングとエラー
//
// TokenProvider は呼び出しのたびに Key Vault へ問い合わせる。
// 本パッケージ側ではキャッシュを行わないため、頻度を抑えたい場合は
// utils.WithTokenTTL を併用する。これにより、外部でローテーションされた
// トークンを Client を作り直さずに反映できる。
//
// 取得した値は前後の空白を取り除いて返す。シークレットが空の場合は
// [ErrEmptySecret]、WithJSONKey で指定したキーが存在しない場合は
// [ErrKeyNotFound] を返す。
package azure
