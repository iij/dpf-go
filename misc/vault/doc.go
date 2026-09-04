// SPDX-License-Identifier: Apache-2.0

// Package vault は HashiCorp Vault の KV シークレットエンジンから
// DPF API のアクセストークンを取得する TokenProvider を提供する。
//
// 本パッケージは github.com/iij/dpf-go 本体に依存しない独立モジュールである。
// Vault を使わない利用者に Vault SDK への依存を持ち込まないよう、
// misc 以下は機能ごとに別モジュールとして切り出している。
//
// # 使い方
//
// 認証済みの Vault クライアントを渡して TokenProvider を作り、
// utils.WithTokenProvider に渡す。
//
//	vc, err := api.NewClient(api.DefaultConfig())
//	if err != nil {
//		return err
//	}
//	p, err := vault.NewTokenProvider(vc, "dpf/api")
//	if err != nil {
//		return err
//	}
//	c, err := utils.NewClient(
//		utils.WithTokenProvider(p),
//		utils.WithTokenTTL(5*time.Minute), // 毎リクエストの Vault アクセスを避ける
//	)
//
// [NewTokenProvider] が返すのは func(ctx context.Context) (string, error) であり、
// utils.TokenProvider にそのまま代入できる。本パッケージが utils を
// import しないのはこのためである。
//
// # 設定
//
// マウントパス・キー名・KV バージョンは Option で変更する。
//
//   - [WithMount]     : KV シークレットエンジンのマウントパス（既定 [DefaultMount]）
//   - [WithKey]       : シークレット内でトークンを格納するキー名（既定 [DefaultKey]）
//   - [WithKVVersion] : KV のバージョン 1 または 2（既定 [DefaultKVVersion]）
//
// # 取得タイミングとエラー
//
// TokenProvider は呼び出しのたびに Vault へ問い合わせる。本パッケージ側では
// キャッシュを行わないため、頻度を抑えたい場合は utils.WithTokenTTL を併用する。
// これにより、外部でローテーションされたトークンを Client を作り直さずに
// 反映できる。
//
// 取得した値は前後の空白を取り除いて返す。シークレット内に指定したキーが
// 存在しない場合は [ErrKeyNotFound] を返す。
package vault
