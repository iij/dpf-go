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
//   - ゾーン単位ロック: ゾーンのラベルと一時的な専用レコードを用いた
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
// Mutex はゾーンのラベルにロック情報を書き込むことで、複数プログラム間の排他制御を
// 実現する。ゾーンのラベルは**未反映の編集の概念を持たない**ため、取得・延長・解放の
// いずれもゾーンに未反映の編集を作らず、ゾーン反映を必要としない。
//
// **2 者が同時に取得することはない。** ラベルの読み取りから書き込みまでの区間を専用
// レコードの追加によって排他しており、レコードの新規追加に対する同名かつ同 RRTYPE の
// 重複の拒否は編集者が誰かに依らないためである。この拒否は**ユーザをまたいで機械的に
// 効く。** 専用レコードは排他が取得できた時点で取り消され、権威サーバへは公開されない。
//
// **一方、保持中に他ユーザや管理画面からレコードを編集することは止められない。** 保持中の
// 尊重は、奪ってよい時刻を読んで譲るという協調によって成り立つ。
//
// 排他はゾーンのラベルを 1 つ消費する。**解放してもラベルは残る**（奪ってよい時刻を現在
// 時刻へ更新するだけである）。利用者がそのゾーンへ付けられるラベルは 9 個までになる。
//
// ロックは再入できない。保持期間を延ばす場合は Renew を使う。
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
// 読んだ結果、反映が不要だと分かった場合は ErrSkipApply を返す。一括置き換えは行われず、
// Apply は nil を返す。
//
//	return nil, utils.ErrSkipApply // 変える必要が無かった
//
// **空の一覧を返すことと混同しないこと。** 空は ErrNoRecords であり失敗として扱う。
// ゾーンの全削除と区別できないため、黙って何もしないことはしない。
//
// 省略できることで、台帳と定期的に突き合わせて必要なときだけ反映する使い方が現実的になる。
// ただし短い間隔で回すと排他の競合が増えるため、取得できるまで待つ場合は WithHoldOptions へ
// WithLockWait を渡すこと。
//
// レコードの一括更新とゾーン反映を行う PatchZoneAtomicChanges は、**排他を保持したまま
// 使える。** 置き換えの対象はレコードであり、排他の状態を持つゾーンのラベルは別のリソース
// であるためである。反映の後も排他は保持され、通常どおり解放できる。詳細は Mutex を参照。
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
//	観点                       ゾーンのラベルを用いる排他        外部の仕組みを使う排他
//	                           （既定）
//	-------------------------  ------------------------------  --------------------------
//	2 者の同時取得             DPF-API の重複の拒否によって      その仕組み次第
//	                           機械的に防がれる
//	                           （ユーザをまたいで効く）
//	別ユーザ・管理画面からの   止められない                      止められない
//	編集
//	外部に必要な運用           無し（DPF-API だけで完結）        その仕組みの運用が要る
//	取得・延長・解放の費用     DPF-API の呼び出し                その仕組み次第
//	                           （レート制限を消費する）          （通常は安い）
//	ゾーン反映との関係         影響しない                        影響しない
//	保持中のゾーンの状態       未反映の編集は残らない            何も残らない
//	ゾーンのラベルの消費       1 個（解放後も残る）              無し
//	保持期間                   ラベルの期限（既定 15 分）        実装次第
//
// **どちらの方式も、保持中に他ユーザや管理画面からレコードを編集することは止められない。**
// 保持中の尊重は協調的である。違いは「2 者の同時取得を何が防ぐか」であり、既定の排他は
// DPF-API の重複の拒否によってユーザをまたいで防ぐ。外部の仕組みでは、その仕組みを使う
// プログラム同士でしか防げない。管理画面や他部署のツールが同じゾーンを触る環境では、
// **どちらの方式でも編集の衝突は起こりうる**ことを前提に運用すること。
//
// # 旧版との混在
//
// **v0.5.0 以前は排他の状態を SOA レコードのラベルへ書いていた。** 新旧が同じゾーンを
// 触ると互いの排他を認識できず、両方が取得に成功しうる。ライブラリはこの混在を検出
// できないため、**同じゾーンを触るプログラムはまとめて更新すること。** 旧版が SOA
// レコードへ残したラベルと未反映の編集は掃除されないため、必要であれば利用者の側で
// 取り除く。
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
