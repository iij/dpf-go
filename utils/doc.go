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
//   - ゾーン単位ロック: SOA レコードのラベルと一時的な専用レコードを用いた
//     排他制御（Mutex）。etcd などの仕組みへ差し替えられる（Locker）。
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
//   - misc/k8s   : Kubernetes Secret
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
// 複数プログラム間の排他制御を実現する。
//
// 排他の強さは競合相手によって異なる。別ユーザとの間は、DPF-API が編集中のレコードへの
// 他ユーザからの編集を拒否するためサーバ側で保証される。一方、同一ユーザ（同一アクセス
// トークン）からの編集は拒否されないため、ラベルの読み取りから書き込みまでの区間を
// 専用レコードの追加によって排他する。レコードの新規追加に対する同名かつ同 RRTYPE の
// 重複の拒否は、編集者が誰かに依らないためである。この専用レコードは排他が取得できた
// 時点で取り消され、権威サーバへは公開されない。
//
// ロックは再入できない。保持期間を延ばす場合は Renew を使う。取得・延長・解放は
// いずれもゾーン反映を行わない。
//
// 取得と解放の対を自分で書く代わりに、Mutex.Do へ処理を渡せる。実行中は保持期間が
// 自動で延長され、終了時に解放される。排他を他者に奪われた場合は、処理へ渡した
// context が打ち切られる。
//
//	mu := utils.NewMutex(client.RecordsAPI, zoneID)
//	err := mu.Do(ctx, func(ctx context.Context) error {
//		// ここでレコードを編集し、ゾーンへ反映する。
//		return nil
//	})
//
// ゾーン全体を読んで編集し、一括で置き換える場合は ZoneApplier を使う。排他の取得と
// 解放に加えて、取り込みの可否を決めるフラグの固定と、反映によって排他が解かれることの
// 扱いを引き受ける。利用者が書くのは編集の内容だけである。
//
//	ap := utils.NewZoneApplier(client.RecordsAPI, client.ZonesAPI, client.JobsAPI, zoneID)
//	err := ap.Apply(ctx, func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
//		// records を編集して返す。編集しない要素はそのまま返せばよい。
//		return records, nil
//	})
//
// レコードの一括更新とゾーン反映を行う PatchZoneAtomicChanges は、ロックを保持したまま
// 使えない。ロックのラベルは SOA の「未反映の編集」としてのみ存在し、一括更新は未反映の
// 編集を引き継がないためである。overwrite_soa の値は関係しない。詳細は Mutex を参照。
//
// # 排他の仕組みの差し替え
//
// 排他は Locker インターフェースで抽象化されている。既に etcd や Consul で排他を
// 運用している環境では、そちらへ寄せられる。RunLocked に自作の実装を渡すか、
// ZoneApplier に WithLocker で渡す。**どちらも呼び出し方と結果は変わらない。**
//
//	err := utils.RunLocked(ctx, myEtcdLocker, func(ctx context.Context) error {
//		// ここでレコードを編集し、ゾーンへ反映する。
//		return nil
//	})
//
// 自作の実装が契約（Locker の godoc）を満たすかは utils/lockertest で機械的に
// 確かめられる。
//
// # 2 つの方式の比較
//
// 差し替えは等価な交換ではない。**排他の効く範囲が変わる。**
//
//	観点                       レコードを用いる排他（既定）      外部の仕組みを使う排他
//	-------------------------  ------------------------------  --------------------------
//	排他が効く相手             同じゾーンを触るすべての相手      その仕組みを使う
//	                                                             プログラム同士のみ
//	別ユーザ・管理画面からの   止められる                        止められない
//	編集                       （DPF-API が編集を拒否する）
//	外部に必要な運用           無し（DPF-API だけで完結）        その仕組みの運用が要る
//	取得・延長・解放の費用     DPF-API の呼び出し                その仕組み次第
//	                           （レート制限を消費する）          （通常は安い）
//	ゾーン反映との関係         一括置き換えが排他を解く          反映は排他に影響しない
//	                           （本パッケージが扱う）
//	保持期間                   ラベルの期限（既定 15 分）        実装次第
//
// **外部の仕組みを使う排他に替えると、別ユーザや管理画面からの編集を止める効果が
// 失われる。** 既定のレコードを用いる排他は、編集中のレコードへの他ユーザからの編集を
// DPF-API が拒否することによって、本ライブラリを使っていない相手にも効く。外部の
// 仕組みにはその効果が無く、その仕組みを使うプログラム同士しか排他できない。
// 管理画面や他部署のツールが同じゾーンを触る環境では、既定のままにすること。
//
// # 排他とゾーンの対応づけ
//
// **1 つの Locker の値は、1 つのゾーンに対する 1 つの保持者を表す。** どのゾーンに
// 対応するかを決める責任は実装の側にある。本パッケージは対応づけを知らないため、
// 別のゾーンの排他を渡されても検出できず、保護されていないまま処理が走る。
//
// 既定の Mutex は NewMutex に渡した zoneID がそのまま対応づけになる。外部の仕組みを
// 使う場合は、排他の鍵にゾーンを識別できる値（ゾーン ID など）を含めること。複数の
// ゾーンを 1 つの鍵で守ると、無関係なゾーンの操作まで直列になる。
package utils
