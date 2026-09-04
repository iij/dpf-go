// SPDX-License-Identifier: Apache-2.0

// Package utils は IIJ DPF Go SDK（github.com/iij/dpf-go）の上に構築された
// ユーティリティ関数群を提供する。
//
// 生成された SDK の低レベル API を直接呼ぶ代わりに、よくある操作を
// 1 関数で行えるようにラップしている。提供する機能は大きく次の 5 つ。
//
//   - API クライアントラッパー: レート制限・同時実行数制限・リトライ・
//     トークン管理を備えた Client（NewClient）。
//   - ゾーン取得: ドメイン名・ゾーン名・サービスコードから対応するゾーン／ゾーンID
//     を取得する（GetZoneFromName, GetZoneIDFromZonename, GetZoneFromZonename,
//     GetZoneFromServiceCode, GetZoneIdFromServiceCode）。
//   - レコード取得: レコード名と RRTYPE からゾーンとレコードを取得する
//     （GetRecordFromRecordName, GetRecordFromZonename, GetRecordFromZoneID）。
//   - ゾーン単位ロック: SOA レコードのラベルを用いた排他制御（Mutex）。
//   - ジョブID取得: 非同期 API のレスポンスから request_id を取り出す（GetJobID）。
//
// # API クライアントの受け渡し
//
// 多くの関数は具象型ではなく dpf.ZonesApi / dpf.RecordsApi インターフェースを
// 受け取る（テスト時のモック差し替えを容易にするため）。
// 実運用では *dpf.APIClient の ZonesAPI / RecordsAPI フィールドをそのまま渡せる。
//
//	cfg := dpf.NewConfiguration()
//	client := dpf.NewAPIClient(cfg)
//
//	// www.example.jp を含むゾーンを longest match で取得する。
//	zone, err := utils.GetZoneFromName(ctx, client.ZonesAPI, "www.example.jp.", false)
//
// # API クライアントラッパー
//
// エンドポイントは WithEndpoint で指定できるが、既定で環境変数
// DPF_API_ENDPOINT、それも空なら本番エンドポイントが使われるため通常は不要。
//
// Client は dpf API client にレート制限（既定 5 req/s, burst 10）、
// 最大同時実行数（既定 5）、リトライ（既定 3 回）、タイムアウト（既定 30 秒）を
// 付与する。制限は RoundTripper 層で適用されるため、GetAPIClient() で取り出した
// クライアント経由の直接リクエストにも効く。
//
//	c, err := utils.NewClient() // 環境変数 DPF_API_ENDPOINT / DPF_API_TOKEN を使う
//	if err != nil {
//		return err
//	}
//	err = c.Operation(ctx, func() error {
//		zones, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).ExecuteAll()
//		...
//		return err
//	})
//
// # アクセストークン
//
// アクセストークンは TokenProvider から取得する。TokenProvider は API リクエストの
// たびに評価されるため、外部でローテーションされたトークンを Client を作り直さずに
// 反映できる。指定がない場合は環境変数 DPF_API_TOKEN を実行時に参照する。
//
//	utils.NewClient(utils.WithToken("..."))                  // 文字列を直接指定
//	utils.NewClient(utils.WithTokenFile("/etc/dpf/token"))   // ファイルから取得
//	utils.NewClient(utils.WithTokenProvider(myProvider))     // 任意の取得処理
//
// 毎リクエストの評価コストを抑えたい場合は WithTokenTTL でキャッシュ期間を指定する
// （既定はキャッシュなし）。トークンの取得に失敗した場合は *TokenError が返り、
// Operation はこのエラーをリトライしない。
//
// シークレット管理サービスから取得する TokenProvider は、本体に依存を持ち込まない
// よう独立モジュールとして github.com/iij/dpf-go/misc 以下に用意している。
//
//   - misc/vault : HashiCorp Vault (KV シークレットエンジン)
//   - misc/aws   : AWS Secrets Manager
//   - misc/azure : Azure Key Vault
//   - misc/gcp   : Google Secret Manager
//
// いずれも NewTokenProvider が返す関数をそのまま WithTokenProvider に渡せる。
//
// # 名前の正規化
//
// ゾーン名・レコード名の比較は文字列操作ではなく miekg/dns の関数で行う。
// dns.CanonicalName で小文字化・FQDN 化したうえで比較するため、
// "Example.JP." と "example.jp" は同一として扱われる。
// longest match では、dns.IsSubDomain で包含関係を判定し、dns.CountLabel が
// 最も大きい（ラベル数が多い ＝ 最も具体的な）ゾーンが選択される。
//
// # ゾーン単位ロック
//
// Mutex はゾーンの SOA レコードのラベルにロック情報を書き込むことで、
// 複数プログラム間の排他制御を実現する。詳細は Mutex を参照。
package utils
