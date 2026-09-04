// SPDX-License-Identifier: Apache-2.0

/*
DPF-APIリファレンスマニュアル

# 1. はじめに  ## 1.1 DPF-APIについて IIJ DNSプラットフォームサービスでは、DNSレコードやゾーン情報などを、\\ お客様が用意したプログラムから自動的に操作するためのAPI機能を提供しています。\\ 以降、IIJ DNSプラットフォームサービスを「DPF」、DPFが提供するAPIを「DPF-API」あるいは単に「API」と表記します。\\ DPF-APIの利用には、DPFの契約とIIJ IDサービスの契約が必要となります。  本リファレンスマニュアルは[**OpenAPI**](https://www.openapis.org/)に準拠しています。  このWEBページはopenapi.jsonから自動生成しています。**このWEBページとopenapi.jsonの内容が異なる場合は、openapi.jsonの内容を正とします。** openapi.jsonは、上部のDownloadボタンからダウンロードできます。  ## 1.2 サポート範囲 DPF-APIを呼び出すためのプログラム、及びそのプログラムを稼働させるためのサーバは、お客様にてご用意ください。\\ お客様にご用意いただくプログラムの開発、利用、動作についてのお問い合わせは承ることができません。  以下の事項についてのお問い合わせは、弊社[**サポートセンター**](https://help.iij.ad.jp/)にて承ります。 - DPF-APIの挙動が本リファレンスマニュアルと異なる場合 - DPF-APIがシステムエラーを応答した場合  ## 1.3 参考資料 - IIJ DNSプラットフォームサービス オンラインマニュアル   - [https://manual.iij.jp/dpf/help/](https://manual.iij.jp/dpf/help/)  - IIJ IDサービス オンラインマニュアル   - [https://manual.iij.jp/iid/admin-help/](https://manual.iij.jp/iid/admin-help/)  # 2. 利用方法 DPF-APIは、URLとHTTPリクエストヘッダ、HTTPリクエストボディでパラメータを指定して利用します。\\ また、IIJ IDサービスのアクセストークンと管理対象の権限設定が必要です。  ## 2.1 リクエスト仕様  項目 | 規格 -----|----- プロトコル | HTTP/1.1（https） HTTPメソッド | GET、POST、PATCH、PUT、DELETE フォーマット | JSON 文字コード | UTF-8 タイムアウト | 300秒  - httpでのリクエストは受け付けません。必ずhttpsを使用してください。 - DPF-APIを呼び出すプログラムは、リクエスト先が正当なものであることを確認するため、SSL証明書を検証することを推奨します。 - 短期間に極めて多数のリクエストが行われた場合、サービスの健全性を保つためにリクエストを制限する場合があります。  ### アクセストークン APIリクエストの際にIIJ IDサービスによって発行されたアクセストークンをAuthorizationヘッダに指定する必要があります。\\ 各APIにより必要となるアクセス権の範囲（許可するスコープ）が異なるのでご注意ください。  各APIに表示されるAUTHORIZATIONSの `DPFViewer` / `DPFOperator` / `ContractOperator` は、HTTPヘッダ名ではなくそのAPIの実行に必要な権限を表す security scheme 名です。\\ 実際に送信するHTTPヘッダはすべて共通で `Authorization: Bearer <access_token>` です。  アクセストークン作成時に指定できる「許可するスコープ」は以下のとおりです。  許可するスコープ | 実行できるAPI -----------------|------------ dpf_read | 参照系API dpf_write | 更新系、及び参照系API dpf_contract | 契約系API  発行済のアクセストークンは、[**IIJ IDサービス**](https://www.auth.iij.jp/console/)の「アクセストークンの管理」より確認できます。\\ DPF-APIを利用する場合は「利用するリソースサーバ」の設定で「IIJ DNSサービスAPI」を選択してください。\\ アクセストークン管理方法のマニュアルは[**こちら**](https://manual.iij.jp/iid/admin-help/9054382.html)を参照してください。  ### 管理対象の権限設定 DPFでは、管理対象となる契約単位での参照、編集権限を細かく設定できます。\\ アクセストークンの許可するスコープが適切であっても、管理対象の権限が付与されていない場合はAPIを実行できません。\\ 管理対象の権限設定のマニュアルは[**こちら**](https://manual.iij.jp/dpf/help/19004706.html#IIJID%E3%82%B5%E3%83%BC%E3%83%93%E3%82%B9%E3%81%A8%E9%80%A3%E6%90%BA%E3%81%99%E3%82%8B-IIJID%E3%82%B5%E3%83%BC%E3%83%93%E3%82%B9%E3%82%A2%E3%82%AB%E3%82%A6%E3%83%B3%E3%83%88%E3%81%B8%E7%AE%A1%E7%90%86%E6%A8%A9%E9%99%90%E3%82%92%E4%BB%98%E4%B8%8E%E3%81%99%E3%82%8B)を参照してください。  ## 2.2 HTTPリクエスト  ### 例 ``` <HTTPメソッド> /dpf/<version>/<APIパス> HTTP/1.1 Host: api.dns-platform.jp Authorization: Bearer <access_token> Content-Type: application/json; charset=UTF-8  <HTTPリクエストボディ: JSON形式のAPI固有のパラメータ> ```  ### リクエストパラメータ DPF-APIで指定するパラメータは以下のとおりです。\\ リクエストパラメータに同一のキーが含まれる場合の動作は保証されません。  共通 | 指定箇所 | パラメータ | 意味 -----|----------|------------|----- 共通 | HTTPメソッド | HTTPメソッド | HTTPメソッド（値：GET、POST、PATCH、PUT、DELETE） 共通 | URL | version | DPF-APIバージョン（値：v1） 個別 | URL | APIパス | API名称やAPI個別のパラメータの組み合わせ 共通 | HTTPヘッダー | access_token | IIJ IDアクセストークン（参照：[**IIJ IDサービス**](https://www.auth.iij.jp/console/)） 個別 | HTTPボディ | APIごとに異なる | JSON形式のパラメータ  <a id=\"http-response\"></a>  ## 2.3 HTTPレスポンス  ### 成功レスポンス APIごとにレスポンスが異なります。  ### エラーレスポンス HTTPステータスコード、及びレスポンスボディによってクライアントプログラムにエラーを通知します。  #### 例：アクセストークンが誤っている ``` {   \"request_id\": \"782d746ac3cb46499b31708fa80e8660\",   \"error_type\": \"ParameterError\",   \"error_message\": \"There are invalid parameters.\",   \"error_details\": [     {       \"code\": \"invalid\",       \"attribute\": \"access_token\"     }   ] } ```  #### エラーコード一覧 HTTP Status Code | error_type | error_message | code | attribute | block_id | 説明 | 対処方法 -----------------|------------|---------------|------|-----------|----------|------|--------- 400 | ParameterError | There are invalid parameters. | invalid | access_token | - | 指定したアクセストークンに誤りがあります | アクセストークンを確認してください 400 | ParameterError | JSON parse error occurred. | - | - | - | パラメータとして不正なJSON文字列が指定されました | リクエストのパラメータを確認してください 400 | ParameterError | There are invalid parameters. | （API個別） | （API個別） | - | 不正なパラメータが指定されました | 各APIのエラーコードを確認してください 404 | NotFound | Specified resource not found. | - | - | - | アクセスURLが正しくありません <br> 存在しないAPIが指定されました <br> 指定された以外のHTTPメソッドが指定されました | 左記の内容を確認してください 406 | NotAcceptable | Request was blocked. | - | - | {id} | セキュリティ制御によりリクエストがブロックされました | block_idを添えてサポートセンターにお問い合わせください 429 | TooManyRequests | Too many requests. | - | - | - | 大量のAPIリクエストが送信されました | 単位時間当たりのAPIリクエスト数が多いため、リクエスト数を抑えてください 500 | SystemError | System error occurred. | - | - | - | システム障害が発生しました | [**サポートセンター**](https://help.iij.ad.jp/)へお問い合わせください<br>お問い合わせの際にリクエストの詳細時間及びrequest_idをお伝えください 503 | ServiceUnavailable | Service unavailable. | - | - | - | メンテナンス中もしくはアクセスが集中しています | しばらく待ってから再度リクエストしてください<br>メンテナンス内容についてはサービスオンラインでご確認ください 503 | SystemBusy | The system is busy, Please try again later. | - | - | - | システムが混み合っています | しばらく待ってから再度リクエストしてください 504 | GatewayTimeout | Gateway timeout. | - | - | - | リクエストがタイムアウトしました | しばらく待ってから再度リクエストしてください  ## 2.4 非同期リクエスト  DPF-APIにおけるGET以外のAPIは全て非同期APIです。\\ 非同期APIはリクエストを受け付けると即座にレスポンスを返却しますが、\\ リクエストに対する実際の処理は非同期で行われます。  非同期リクエストの受け付けに成功した場合のHTTPステータスコードは202で、\\ 返却されたレスポンスボディには、処理進捗を確認するためのURL（jobs_url）が含まれます。\\ このjobs_urlに対してGETリクエストをすることで進捗状況を確認できます。  進捗状況を確認した際、非同期処理が正常に終了していた場合は、\\ 返却されたレスポンスボディには、対象リソースを取得するためのURL（resources_url）が含まれます。\\ このresources_urlに対してGETリクエストをすることで実行結果を確認できます。  ### 例 #### 非同期リクエストのレスポンス ``` HTTP/1.1 202 Accepted Date: Mon, 26 Mar 20XX hh:mm:dd GMT Content-Type: application/json; charset=utf-8 〜 略 〜  {   \"request_id\": \"782d746ac3cb46499b31708fa80e8660\",   \"jobs_url\": \"https://api.dns-platform.jp/dpf/<version>/jobs/<request_id>\" } ```  #### GETリクエスト ``` GET /dpf/<version>/jobs/<request_id> HTTP/1.1 Host: api.dns-platform.jp Authorization: Bearer <access_token> Content-Type: application/json; charset=UTF-8  {} ```  #### 進捗状況のレスポンス ``` HTTP/1.1 200 OK Date: Mon, 26 Mar 202X hh:mm:dd GMT Content-Type: application/json; charset=utf-8 〜 略 〜  {   \"request_id\": \"782d746ac3cb46499b31708fa80e8660\",   \"resources_url\": <resources_url>,   \"status\": \"SUCCESSFUL\" } ```  # 3. リリースノート  ## 3.1 バージョン 1.0.0 - APIをリリースしました。  ## 3.2 バージョン 2.0.0 - 対応プロトコルから HTTP/2 を削除しました。  ## 3.3 バージョン 3.7.0 - セキュリティ制御でリクエストがブロックされた場合に HTTP Status 406 がレスポンスされます。レスポンス形式は **[2.3 HTTPレスポンス](#http-response)** を参照してください。 - エラーレスポンス定義に関する軽微な不具合を修正しました。

API version: 3.7.0
*/

// Code generated by OpenAPI Generator (https://openapi-generator.tech); DO NOT EDIT.

package dpf

import (
	"encoding/json"
	"fmt"
)

// ContractsLogsOperation 詳細説明は[**こちら**](#tag/logs-(contracts))
type ContractsLogsOperation string

// List of ContractsLogsOperation
const (
	CONTRACTSLOGSOPERATION_ADD_CC_PRIMARY                ContractsLogsOperation = "add_cc_primary"
	CONTRACTSLOGSOPERATION_UPDATE_CC_PRIMARY             ContractsLogsOperation = "update_cc_primary"
	CONTRACTSLOGSOPERATION_DELETE_CC_PRIMARY             ContractsLogsOperation = "delete_cc_primary"
	CONTRACTSLOGSOPERATION_ADD_CC_SEC_NOTIFIED_SERVER    ContractsLogsOperation = "add_cc_sec_notified_server"
	CONTRACTSLOGSOPERATION_UPDATE_CC_SEC_NOTIFIED_SERVER ContractsLogsOperation = "update_cc_sec_notified_server"
	CONTRACTSLOGSOPERATION_DELETE_CC_SEC_NOTIFIED_SERVER ContractsLogsOperation = "delete_cc_sec_notified_server"
	CONTRACTSLOGSOPERATION_ADD_CC_SEC_TRANSFER_ACL       ContractsLogsOperation = "add_cc_sec_transfer_acl"
	CONTRACTSLOGSOPERATION_UPDATE_CC_SEC_TRANSFER_ACL    ContractsLogsOperation = "update_cc_sec_transfer_acl"
	CONTRACTSLOGSOPERATION_DELETE_CC_SEC_TRANSFER_ACL    ContractsLogsOperation = "delete_cc_sec_transfer_acl"
	CONTRACTSLOGSOPERATION_ADD_NOTIFICATION_ACCOUNT      ContractsLogsOperation = "add_notification_account"
	CONTRACTSLOGSOPERATION_UPDATE_NOTIFICATION_ACCOUNT   ContractsLogsOperation = "update_notification_account"
	CONTRACTSLOGSOPERATION_DELETE_NOTIFICATION_ACCOUNT   ContractsLogsOperation = "delete_notification_account"
	CONTRACTSLOGSOPERATION_MAIL_NOTIFICATION             ContractsLogsOperation = "mail_notification"
	CONTRACTSLOGSOPERATION_PHONE_NOTIFICATION            ContractsLogsOperation = "phone_notification"
	CONTRACTSLOGSOPERATION_CREATE_COMMON_CONFIG          ContractsLogsOperation = "create_common_config"
	CONTRACTSLOGSOPERATION_SWITCH_DEFAULT_COMMON_CONFIG  ContractsLogsOperation = "switch_default_common_config"
	CONTRACTSLOGSOPERATION_UPDATE_COMMON_CONFIG          ContractsLogsOperation = "update_common_config"
	CONTRACTSLOGSOPERATION_DELETE_COMMON_CONFIG          ContractsLogsOperation = "delete_common_config"
	CONTRACTSLOGSOPERATION_COPY_COMMON_CONFIG            ContractsLogsOperation = "copy_common_config"
	CONTRACTSLOGSOPERATION_UPDATE_MANAGED_DNS_STATE      ContractsLogsOperation = "update_managed_dns_state"
	CONTRACTSLOGSOPERATION_UPDATE_CONTRACT_DESCRIPTION   ContractsLogsOperation = "update_contract_description"
	CONTRACTSLOGSOPERATION_UPDATE_CONTRACT_FAVORITE      ContractsLogsOperation = "update_contract_favorite"
	CONTRACTSLOGSOPERATION_UPDATE_CONTRACT_LABELS        ContractsLogsOperation = "update_contract_labels"
	CONTRACTSLOGSOPERATION_APPLY_COMMON_CONFIG           ContractsLogsOperation = "apply_common_config"
	CONTRACTSLOGSOPERATION_CREATE_CONTRACT_PARTNERSHIP   ContractsLogsOperation = "create_contract_partnership"
	CONTRACTSLOGSOPERATION_DELETE_CONTRACT_PARTNERSHIP   ContractsLogsOperation = "delete_contract_partnership"
	CONTRACTSLOGSOPERATION_CREATE_TSIG                   ContractsLogsOperation = "create_tsig"
	CONTRACTSLOGSOPERATION_UPDATE_TSIG                   ContractsLogsOperation = "update_tsig"
	CONTRACTSLOGSOPERATION_DELETE_TSIG                   ContractsLogsOperation = "delete_tsig"
)

// All allowed values of ContractsLogsOperation enum
var AllowedContractsLogsOperationEnumValues = []ContractsLogsOperation{
	"add_cc_primary",
	"update_cc_primary",
	"delete_cc_primary",
	"add_cc_sec_notified_server",
	"update_cc_sec_notified_server",
	"delete_cc_sec_notified_server",
	"add_cc_sec_transfer_acl",
	"update_cc_sec_transfer_acl",
	"delete_cc_sec_transfer_acl",
	"add_notification_account",
	"update_notification_account",
	"delete_notification_account",
	"mail_notification",
	"phone_notification",
	"create_common_config",
	"switch_default_common_config",
	"update_common_config",
	"delete_common_config",
	"copy_common_config",
	"update_managed_dns_state",
	"update_contract_description",
	"update_contract_favorite",
	"update_contract_labels",
	"apply_common_config",
	"create_contract_partnership",
	"delete_contract_partnership",
	"create_tsig",
	"update_tsig",
	"delete_tsig",
}

func (v *ContractsLogsOperation) UnmarshalJSON(src []byte) error {
	var value string
	err := json.Unmarshal(src, &value)
	if err != nil {
		return err
	}
	enumTypeValue := ContractsLogsOperation(value)
	for _, existing := range AllowedContractsLogsOperationEnumValues {
		if existing == enumTypeValue {
			*v = enumTypeValue
			return nil
		}
	}

	return fmt.Errorf("%+v is not a valid ContractsLogsOperation", value)
}

// NewContractsLogsOperationFromValue returns a pointer to a valid ContractsLogsOperation
// for the value passed as argument, or an error if the value passed is not allowed by the enum
func NewContractsLogsOperationFromValue(v string) (*ContractsLogsOperation, error) {
	ev := ContractsLogsOperation(v)
	if ev.IsValid() {
		return &ev, nil
	} else {
		return nil, fmt.Errorf("invalid value '%v' for ContractsLogsOperation: valid values are %v", v, AllowedContractsLogsOperationEnumValues)
	}
}

// IsValid return true if the value is valid for the enum, false otherwise
func (v ContractsLogsOperation) IsValid() bool {
	for _, existing := range AllowedContractsLogsOperationEnumValues {
		if existing == v {
			return true
		}
	}
	return false
}

// Ptr returns reference to ContractsLogsOperation value
func (v ContractsLogsOperation) Ptr() *ContractsLogsOperation {
	return &v
}

type NullableContractsLogsOperation struct {
	value *ContractsLogsOperation
	isSet bool
}

func (v NullableContractsLogsOperation) Get() *ContractsLogsOperation {
	return v.value
}

func (v *NullableContractsLogsOperation) Set(val *ContractsLogsOperation) {
	v.value = val
	v.isSet = true
}

func (v NullableContractsLogsOperation) IsSet() bool {
	return v.isSet
}

func (v *NullableContractsLogsOperation) Unset() {
	v.value = nil
	v.isSet = false
}

func NewNullableContractsLogsOperation(val *ContractsLogsOperation) *NullableContractsLogsOperation {
	return &NullableContractsLogsOperation{value: val, isSet: true}
}

func (v NullableContractsLogsOperation) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

func (v *NullableContractsLogsOperation) UnmarshalJSON(src []byte) error {
	v.isSet = true
	return json.Unmarshal(src, &v.value)
}
