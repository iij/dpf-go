// SPDX-License-Identifier: Apache-2.0

// Package dpf は IIJ DNS プラットフォームサービス（DPF）が提供する
// DPF-API の Go クライアントである。
//
// 本パッケージの api_*.go / model_*.go / client.go / configuration.go /
// response.go / utils.go は openapi.json から OpenAPI Generator で生成される。
// 直接編集せず、openapi.json を更新して make generate を実行すること。
//
// # クライアントの生成
//
// Configuration にサーバ URL などを設定して APIClient を作る。
// エンドポイントを指定しない場合は本番の https://api.dns-platform.jp/dpf/v1 を使う。
//
//	cfg := dpf.NewConfiguration()
//	client := dpf.NewAPIClient(cfg)
//
// レート制限・リトライ・トークン管理まで含めて扱いたい場合は、
// 本パッケージを直接使うのではなく [github.com/iij/dpf-go/utils] の
// Client ラッパーを使うほうが簡単である。
//
// # 認証
//
// DPF-API は IIJ ID サービスが発行したアクセストークンを
// Authorization ヘッダに Bearer 形式で指定する。
// context に [ContextAccessToken] を入れるとリクエストごとに付与される。
//
//	ctx := context.WithValue(context.Background(), dpf.ContextAccessToken, token)
//	zones, resp, err := client.ZonesAPI.GetZoneList(ctx).Execute()
//
// 必要なスコープは API ごとに異なる（dpf_read / dpf_write / dpf_contract）。
// 各 API の AUTHORIZATIONS に表示される DPFViewer / DPFOperator /
// ContractOperator は HTTP ヘッダ名ではなく、実行に必要な権限を表す
// security scheme 名である。
//
// # 一覧取得とページング
//
// 一覧系 API の Execute() は limit / offset によるページャであり、
// 1 回の呼び出しでは全件を取得できない。全件を取得するには、
// 生成された ExecuteAll() を使う。
//
//	zones, resp, err := client.ZonesAPI.GetZoneList(ctx).ExecuteAll()
//
// ExecuteAll() は 1 ページあたり通常の一覧 API では 10000 件、
// log 系 API では 100 件を取得し、全ページを連結して返す。
//
// # 非同期 API
//
// GET 以外の API はすべて非同期であり、リクエストが受け付けられると
// HTTP 202 と共に処理進捗を確認するための jobs_url を含む
// [AsyncResponse] が返る。処理の完了を待つには [JobsAPIService.SyncWait] を使う。
//
//	commit := dpf.PatchZoneCommit{Description: dpf.PtrString("apply")}
//	job, resp, err := client.JobsAPI.SyncWait(
//		client.ZonesAPI.PatchZoneChanges(ctx, zoneID).PatchZoneCommit(commit).Execute(),
//	)
//
// SyncWait は Execute() の戻り値をそのまま受け取り、ジョブの完了まで
// ポーリングする。ジョブが失敗した場合はその内容をエラーとして返す。
//
// ポーリングに使う context を明示したい場合（待ち時間に上限を設けたい、
// 呼び出し側のキャンセルを確実に伝播させたい場合など）は
// [JobsAPIService.SyncWaitContext] を使う。
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
//	defer cancel()
//	job, resp, err := client.JobsAPI.SyncWaitContext(ctx,
//		client.ZonesAPI.PatchZoneChanges(ctx, zoneID).PatchZoneCommit(commit).Execute(),
//	)
//
// # エラー
//
// HTTP レスポンスが得られたうえでのエラーは [GenericOpenAPIError] として返る。
// Model() で、そのステータスコードに対応するエラーモデル
// （ParameterErrorResponse、NotFoundError、TooManyRequestsError、
// SystemError など）を取り出せる。
//
//	if apiErr, ok := err.(*dpf.GenericOpenAPIError); ok {
//		switch m := apiErr.Model().(type) {
//		case dpf.ParameterErrorResponse:
//			// リクエストパラメータの誤り
//		}
//	}
//
// レスポンスが得られなかった通信エラーは、これとは別に生の error として返る。
//
// # OpenTelemetry
//
// APIClient は各 API リクエストに対して OpenTelemetry のスパンを生成する。
// 計装スコープ名は "github.com/iij/dpf-go" である。
// グローバルな TracerProvider が設定されていない場合は何も記録されない。
//
// # 関連パッケージ
//
// 本パッケージの上に、次の補助パッケージを用意している。
//
//   - [github.com/iij/dpf-go/utils] : API クライアントラッパー（レート制限・
//     リトライ・トークン管理）、ゾーン／レコード取得、ゾーン単位ロック、
//     ジョブ ID 取得。
//   - github.com/iij/dpf-go/misc/... : シークレット管理サービスから
//     トークンを取得する TokenProvider。他パッケージへの依存を本体に
//     持ち込まないよう、独立したモジュールとして提供する
//     （misc/vault、misc/aws、misc/azure、misc/gcp）。
package dpf
