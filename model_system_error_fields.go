// SPDX-License-Identifier: Apache-2.0

/*
DPF-APIリファレンスマニュアル

# 1. はじめに  ## 1.1 DPF-APIについて IIJ DNSプラットフォームサービスでは、DNSレコードやゾーン情報などを、\\ お客様が用意したプログラムから自動的に操作するためのAPI機能を提供しています。\\ 以降、IIJ DNSプラットフォームサービスを「DPF」、DPFが提供するAPIを「DPF-API」あるいは単に「API」と表記します。\\ DPF-APIの利用には、DPFの契約とIIJ IDサービスの契約が必要となります。  本リファレンスマニュアルは[**OpenAPI**](https://www.openapis.org/)に準拠しています。  このWEBページはopenapi.jsonから自動生成しています。**このWEBページとopenapi.jsonの内容が異なる場合は、openapi.jsonの内容を正とします。** openapi.jsonは、上部のDownloadボタンからダウンロードできます。  ## 1.2 サポート範囲 DPF-APIを呼び出すためのプログラム、及びそのプログラムを稼働させるためのサーバは、お客様にてご用意ください。\\ お客様にご用意いただくプログラムの開発、利用、動作についてのお問い合わせは承ることができません。  以下の事項についてのお問い合わせは、弊社[**サポートセンター**](https://help.iij.ad.jp/)にて承ります。 - DPF-APIの挙動が本リファレンスマニュアルと異なる場合 - DPF-APIがシステムエラーを応答した場合  ## 1.3 参考資料 - IIJ DNSプラットフォームサービス オンラインマニュアル   - [https://manual.iij.jp/dpf/help/](https://manual.iij.jp/dpf/help/)  - IIJ IDサービス オンラインマニュアル   - [https://manual.iij.jp/iid/admin-help/](https://manual.iij.jp/iid/admin-help/)  # 2. 利用方法 DPF-APIは、URLとHTTPリクエストヘッダ、HTTPリクエストボディでパラメータを指定して利用します。\\ また、IIJ IDサービスのアクセストークンと管理対象の権限設定が必要です。  ## 2.1 リクエスト仕様  項目 | 規格 -----|----- プロトコル | HTTP/1.1（https） HTTPメソッド | GET、POST、PATCH、PUT、DELETE フォーマット | JSON 文字コード | UTF-8 タイムアウト | 300秒  - httpでのリクエストは受け付けません。必ずhttpsを使用してください。 - DPF-APIを呼び出すプログラムは、リクエスト先が正当なものであることを確認するため、SSL証明書を検証することを推奨します。 - 短期間に極めて多数のリクエストが行われた場合、サービスの健全性を保つためにリクエストを制限する場合があります。  ### アクセストークン APIリクエストの際にIIJ IDサービスによって発行されたアクセストークンをAuthorizationヘッダに指定する必要があります。\\ 各APIにより必要となるアクセス権の範囲（許可するスコープ）が異なるのでご注意ください。  各APIに表示されるAUTHORIZATIONSの `DPFViewer` / `DPFOperator` / `ContractOperator` は、HTTPヘッダ名ではなくそのAPIの実行に必要な権限を表す security scheme 名です。\\ 実際に送信するHTTPヘッダはすべて共通で `Authorization: Bearer <access_token>` です。  アクセストークン作成時に指定できる「許可するスコープ」は以下のとおりです。  許可するスコープ | 実行できるAPI -----------------|------------ dpf_read | 参照系API dpf_write | 更新系、及び参照系API dpf_contract | 契約系API  発行済のアクセストークンは、[**IIJ IDサービス**](https://www.auth.iij.jp/console/)の「アクセストークンの管理」より確認できます。\\ DPF-APIを利用する場合は「利用するリソースサーバ」の設定で「IIJ DNSサービスAPI」を選択してください。\\ アクセストークン管理方法のマニュアルは[**こちら**](https://manual.iij.jp/iid/admin-help/9054382.html)を参照してください。  ### 管理対象の権限設定 DPFでは、管理対象となる契約単位での参照、編集権限を細かく設定できます。\\ アクセストークンの許可するスコープが適切であっても、管理対象の権限が付与されていない場合はAPIを実行できません。\\ 管理対象の権限設定のマニュアルは[**こちら**](https://manual.iij.jp/dpf/help/19004706.html#IIJID%E3%82%B5%E3%83%BC%E3%83%93%E3%82%B9%E3%81%A8%E9%80%A3%E6%90%BA%E3%81%99%E3%82%8B-IIJID%E3%82%B5%E3%83%BC%E3%83%93%E3%82%B9%E3%82%A2%E3%82%AB%E3%82%A6%E3%83%B3%E3%83%88%E3%81%B8%E7%AE%A1%E7%90%86%E6%A8%A9%E9%99%90%E3%82%92%E4%BB%98%E4%B8%8E%E3%81%99%E3%82%8B)を参照してください。  ## 2.2 HTTPリクエスト  ### 例 ``` <HTTPメソッド> /dpf/<version>/<APIパス> HTTP/1.1 Host: api.dns-platform.jp Authorization: Bearer <access_token> Content-Type: application/json; charset=UTF-8  <HTTPリクエストボディ: JSON形式のAPI固有のパラメータ> ```  ### リクエストパラメータ DPF-APIで指定するパラメータは以下のとおりです。\\ リクエストパラメータに同一のキーが含まれる場合の動作は保証されません。  共通 | 指定箇所 | パラメータ | 意味 -----|----------|------------|----- 共通 | HTTPメソッド | HTTPメソッド | HTTPメソッド（値：GET、POST、PATCH、PUT、DELETE） 共通 | URL | version | DPF-APIバージョン（値：v1） 個別 | URL | APIパス | API名称やAPI個別のパラメータの組み合わせ 共通 | HTTPヘッダー | access_token | IIJ IDアクセストークン（参照：[**IIJ IDサービス**](https://www.auth.iij.jp/console/)） 個別 | HTTPボディ | APIごとに異なる | JSON形式のパラメータ  <a id=\"http-response\"></a>  ## 2.3 HTTPレスポンス  ### 成功レスポンス APIごとにレスポンスが異なります。  ### エラーレスポンス HTTPステータスコード、及びレスポンスボディによってクライアントプログラムにエラーを通知します。  #### 例：アクセストークンが誤っている ``` {   \"request_id\": \"782d746ac3cb46499b31708fa80e8660\",   \"error_type\": \"ParameterError\",   \"error_message\": \"There are invalid parameters.\",   \"error_details\": [     {       \"code\": \"invalid\",       \"attribute\": \"access_token\"     }   ] } ```  #### エラーコード一覧 HTTP Status Code | error_type | error_message | code | attribute | block_id | 説明 | 対処方法 -----------------|------------|---------------|------|-----------|----------|------|--------- 400 | ParameterError | There are invalid parameters. | invalid | access_token | - | 指定したアクセストークンに誤りがあります | アクセストークンを確認してください 400 | ParameterError | JSON parse error occurred. | - | - | - | パラメータとして不正なJSON文字列が指定されました | リクエストのパラメータを確認してください 400 | ParameterError | There are invalid parameters. | （API個別） | （API個別） | - | 不正なパラメータが指定されました | 各APIのエラーコードを確認してください 404 | NotFound | Specified resource not found. | - | - | - | アクセスURLが正しくありません <br> 存在しないAPIが指定されました <br> 指定された以外のHTTPメソッドが指定されました | 左記の内容を確認してください 406 | NotAcceptable | Request was blocked. | - | - | {id} | セキュリティ制御によりリクエストがブロックされました | block_idを添えてサポートセンターにお問い合わせください 429 | TooManyRequests | Too many requests. | - | - | - | 大量のAPIリクエストが送信されました | 単位時間当たりのAPIリクエスト数が多いため、リクエスト数を抑えてください 500 | SystemError | System error occurred. | - | - | - | システム障害が発生しました | [**サポートセンター**](https://help.iij.ad.jp/)へお問い合わせください<br>お問い合わせの際にリクエストの詳細時間及びrequest_idをお伝えください 503 | ServiceUnavailable | Service unavailable. | - | - | - | メンテナンス中もしくはアクセスが集中しています | しばらく待ってから再度リクエストしてください<br>メンテナンス内容についてはサービスオンラインでご確認ください 503 | SystemBusy | The system is busy, Please try again later. | - | - | - | システムが混み合っています | しばらく待ってから再度リクエストしてください 504 | GatewayTimeout | Gateway timeout. | - | - | - | リクエストがタイムアウトしました | しばらく待ってから再度リクエストしてください  ## 2.4 非同期リクエスト  DPF-APIにおけるGET以外のAPIは全て非同期APIです。\\ 非同期APIはリクエストを受け付けると即座にレスポンスを返却しますが、\\ リクエストに対する実際の処理は非同期で行われます。  非同期リクエストの受け付けに成功した場合のHTTPステータスコードは202で、\\ 返却されたレスポンスボディには、処理進捗を確認するためのURL（jobs_url）が含まれます。\\ このjobs_urlに対してGETリクエストをすることで進捗状況を確認できます。  進捗状況を確認した際、非同期処理が正常に終了していた場合は、\\ 返却されたレスポンスボディには、対象リソースを取得するためのURL（resources_url）が含まれます。\\ このresources_urlに対してGETリクエストをすることで実行結果を確認できます。  ### 例 #### 非同期リクエストのレスポンス ``` HTTP/1.1 202 Accepted Date: Mon, 26 Mar 20XX hh:mm:dd GMT Content-Type: application/json; charset=utf-8 〜 略 〜  {   \"request_id\": \"782d746ac3cb46499b31708fa80e8660\",   \"jobs_url\": \"https://api.dns-platform.jp/dpf/<version>/jobs/<request_id>\" } ```  #### GETリクエスト ``` GET /dpf/<version>/jobs/<request_id> HTTP/1.1 Host: api.dns-platform.jp Authorization: Bearer <access_token> Content-Type: application/json; charset=UTF-8  {} ```  #### 進捗状況のレスポンス ``` HTTP/1.1 200 OK Date: Mon, 26 Mar 202X hh:mm:dd GMT Content-Type: application/json; charset=utf-8 〜 略 〜  {   \"request_id\": \"782d746ac3cb46499b31708fa80e8660\",   \"resources_url\": <resources_url>,   \"status\": \"SUCCESSFUL\" } ```  # 3. リリースノート  ## 3.1 バージョン 1.0.0 - APIをリリースしました。  ## 3.2 バージョン 2.0.0 - 対応プロトコルから HTTP/2 を削除しました。  ## 3.3 バージョン 3.7.0 - セキュリティ制御でリクエストがブロックされた場合に HTTP Status 406 がレスポンスされます。レスポンス形式は **[2.3 HTTPレスポンス](#http-response)** を参照してください。 - エラーレスポンス定義に関する軽微な不具合を修正しました。

API version: 3.7.0
*/

// Code generated by OpenAPI Generator (https://openapi-generator.tech); DO NOT EDIT.

package dpf

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// checks if the SystemErrorFields type satisfies the MappedNullable interface at compile time
var _ MappedNullable = &SystemErrorFields{}

// SystemErrorFields struct for SystemErrorFields
type SystemErrorFields struct {
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`
}

type _SystemErrorFields SystemErrorFields

// NewSystemErrorFields instantiates a new SystemErrorFields object
// This constructor will assign default values to properties that have it defined,
// and makes sure properties required by API are set, but the set of arguments
// will change when the set of required properties is changed
func NewSystemErrorFields(errorType string, errorMessage string) *SystemErrorFields {
	this := SystemErrorFields{}
	this.ErrorType = errorType
	this.ErrorMessage = errorMessage
	return &this
}

// NewSystemErrorFieldsWithDefaults instantiates a new SystemErrorFields object
// This constructor will only assign default values to properties that have it defined,
// but it doesn't guarantee that properties required by API are set
func NewSystemErrorFieldsWithDefaults() *SystemErrorFields {
	this := SystemErrorFields{}
	return &this
}

// GetErrorType returns the ErrorType field value
func (o *SystemErrorFields) GetErrorType() string {
	if o == nil {
		var ret string
		return ret
	}

	return o.ErrorType
}

// GetErrorTypeOk returns a tuple with the ErrorType field value
// and a boolean to check if the value has been set.
func (o *SystemErrorFields) GetErrorTypeOk() (*string, bool) {
	if o == nil {
		return nil, false
	}
	return &o.ErrorType, true
}

// SetErrorType sets field value
func (o *SystemErrorFields) SetErrorType(v string) {
	o.ErrorType = v
}

// GetErrorMessage returns the ErrorMessage field value
func (o *SystemErrorFields) GetErrorMessage() string {
	if o == nil {
		var ret string
		return ret
	}

	return o.ErrorMessage
}

// GetErrorMessageOk returns a tuple with the ErrorMessage field value
// and a boolean to check if the value has been set.
func (o *SystemErrorFields) GetErrorMessageOk() (*string, bool) {
	if o == nil {
		return nil, false
	}
	return &o.ErrorMessage, true
}

// SetErrorMessage sets field value
func (o *SystemErrorFields) SetErrorMessage(v string) {
	o.ErrorMessage = v
}

func (o SystemErrorFields) MarshalJSON() ([]byte, error) {
	toSerialize, err := o.ToMap()
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(toSerialize)
}

func (o SystemErrorFields) ToMap() (map[string]interface{}, error) {
	toSerialize := map[string]interface{}{}
	toSerialize["error_type"] = o.ErrorType
	toSerialize["error_message"] = o.ErrorMessage
	return toSerialize, nil
}

func (o *SystemErrorFields) UnmarshalJSON(data []byte) (err error) {
	// This validates that all required properties are included in the JSON object
	// by unmarshalling the object into a generic map with string keys and checking
	// that every required field exists as a key in the generic map.
	requiredProperties := []string{
		"error_type",
		"error_message",
	}

	allProperties := make(map[string]interface{})

	err = json.Unmarshal(data, &allProperties)

	if err != nil {
		return err
	}

	for _, requiredProperty := range requiredProperties {
		if _, exists := allProperties[requiredProperty]; !exists {
			return fmt.Errorf("no value given for required property %v", requiredProperty)
		}
	}

	varSystemErrorFields := _SystemErrorFields{}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&varSystemErrorFields)

	if err != nil {
		return err
	}

	*o = SystemErrorFields(varSystemErrorFields)

	return err
}

type NullableSystemErrorFields struct {
	value *SystemErrorFields
	isSet bool
}

func (v NullableSystemErrorFields) Get() *SystemErrorFields {
	return v.value
}

func (v *NullableSystemErrorFields) Set(val *SystemErrorFields) {
	v.value = val
	v.isSet = true
}

func (v NullableSystemErrorFields) IsSet() bool {
	return v.isSet
}

func (v *NullableSystemErrorFields) Unset() {
	v.value = nil
	v.isSet = false
}

func NewNullableSystemErrorFields(val *SystemErrorFields) *NullableSystemErrorFields {
	return &NullableSystemErrorFields{value: val, isSet: true}
}

func (v NullableSystemErrorFields) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

func (v *NullableSystemErrorFields) UnmarshalJSON(src []byte) error {
	v.isSet = true
	return json.Unmarshal(src, &v.value)
}
