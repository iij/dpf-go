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
)

// checks if the PutConfigMonitoringsInnerProps type satisfies the MappedNullable interface at compile time
var _ MappedNullable = &PutConfigMonitoringsInnerProps{}

// PutConfigMonitoringsInnerProps struct for PutConfigMonitoringsInnerProps
type PutConfigMonitoringsInnerProps struct {
	// 監視元ロケーション（指定可能な監視種別はping,tcp,http）
	Location *string `json:"location,omitempty"`
	// 監視間隔（s）（指定可能な監視種別はping,tcp,http）
	Interval *int32 `json:"interval,omitempty"`
	// 保留時間（s）（指定可能な監視種別はping,tcp,http）
	Holdtime *int32 `json:"holdtime,omitempty"`
	// タイムアウト（s）（指定可能な監視種別はping,tcp,http）
	Timeout *int32 `json:"timeout,omitempty"`
	// ポート番号（指定可能な監視種別はtcp,http）
	Port *int32 `json:"port,omitempty"`
	// TLS監視（指定可能な監視種別はtcp）
	TlsEnabled *bool `json:"tls_enabled,omitempty"`
	// TLS SNI（指定可能な監視種別はtcp,http）
	TlsSni *string `json:"tls_sni,omitempty"`
	// レスポンスボディマッチ文字列（指定可能な監視種別はhttp）
	ResponseMatch *string `json:"response_match,omitempty"`
	// HTTPS利用（指定可能な監視種別はhttp）
	Https *bool `json:"https,omitempty"`
	// Hostヘッダー（指定可能な監視種別はhttp）
	Headers []PutConfigMonitoringsInnerPropsHeadersInner `json:"headers,omitempty"`
	// パス（先頭に「/」は不要。指定可能な監視種別はhttp）
	Path *string `json:"path,omitempty"`
	// ステータスコード（0-9の3桁の文字列のみ指定可能。指定可能な監視種別はhttp）
	StatusCodes []string `json:"status_codes,omitempty"`
	// 固定値（指定可能な監視種別はstatic）
	Result *string `json:"result,omitempty"`
}

// NewPutConfigMonitoringsInnerProps instantiates a new PutConfigMonitoringsInnerProps object
// This constructor will assign default values to properties that have it defined,
// and makes sure properties required by API are set, but the set of arguments
// will change when the set of required properties is changed
func NewPutConfigMonitoringsInnerProps() *PutConfigMonitoringsInnerProps {
	this := PutConfigMonitoringsInnerProps{}
	return &this
}

// NewPutConfigMonitoringsInnerPropsWithDefaults instantiates a new PutConfigMonitoringsInnerProps object
// This constructor will only assign default values to properties that have it defined,
// but it doesn't guarantee that properties required by API are set
func NewPutConfigMonitoringsInnerPropsWithDefaults() *PutConfigMonitoringsInnerProps {
	this := PutConfigMonitoringsInnerProps{}
	return &this
}

// GetLocation returns the Location field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetLocation() string {
	if o == nil || IsNil(o.Location) {
		var ret string
		return ret
	}
	return *o.Location
}

// GetLocationOk returns a tuple with the Location field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetLocationOk() (*string, bool) {
	if o == nil || IsNil(o.Location) {
		return nil, false
	}
	return o.Location, true
}

// HasLocation returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasLocation() bool {
	if o != nil && !IsNil(o.Location) {
		return true
	}

	return false
}

// SetLocation gets a reference to the given string and assigns it to the Location field.
func (o *PutConfigMonitoringsInnerProps) SetLocation(v string) {
	o.Location = &v
}

// GetInterval returns the Interval field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetInterval() int32 {
	if o == nil || IsNil(o.Interval) {
		var ret int32
		return ret
	}
	return *o.Interval
}

// GetIntervalOk returns a tuple with the Interval field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetIntervalOk() (*int32, bool) {
	if o == nil || IsNil(o.Interval) {
		return nil, false
	}
	return o.Interval, true
}

// HasInterval returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasInterval() bool {
	if o != nil && !IsNil(o.Interval) {
		return true
	}

	return false
}

// SetInterval gets a reference to the given int32 and assigns it to the Interval field.
func (o *PutConfigMonitoringsInnerProps) SetInterval(v int32) {
	o.Interval = &v
}

// GetHoldtime returns the Holdtime field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetHoldtime() int32 {
	if o == nil || IsNil(o.Holdtime) {
		var ret int32
		return ret
	}
	return *o.Holdtime
}

// GetHoldtimeOk returns a tuple with the Holdtime field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetHoldtimeOk() (*int32, bool) {
	if o == nil || IsNil(o.Holdtime) {
		return nil, false
	}
	return o.Holdtime, true
}

// HasHoldtime returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasHoldtime() bool {
	if o != nil && !IsNil(o.Holdtime) {
		return true
	}

	return false
}

// SetHoldtime gets a reference to the given int32 and assigns it to the Holdtime field.
func (o *PutConfigMonitoringsInnerProps) SetHoldtime(v int32) {
	o.Holdtime = &v
}

// GetTimeout returns the Timeout field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetTimeout() int32 {
	if o == nil || IsNil(o.Timeout) {
		var ret int32
		return ret
	}
	return *o.Timeout
}

// GetTimeoutOk returns a tuple with the Timeout field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetTimeoutOk() (*int32, bool) {
	if o == nil || IsNil(o.Timeout) {
		return nil, false
	}
	return o.Timeout, true
}

// HasTimeout returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasTimeout() bool {
	if o != nil && !IsNil(o.Timeout) {
		return true
	}

	return false
}

// SetTimeout gets a reference to the given int32 and assigns it to the Timeout field.
func (o *PutConfigMonitoringsInnerProps) SetTimeout(v int32) {
	o.Timeout = &v
}

// GetPort returns the Port field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetPort() int32 {
	if o == nil || IsNil(o.Port) {
		var ret int32
		return ret
	}
	return *o.Port
}

// GetPortOk returns a tuple with the Port field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetPortOk() (*int32, bool) {
	if o == nil || IsNil(o.Port) {
		return nil, false
	}
	return o.Port, true
}

// HasPort returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasPort() bool {
	if o != nil && !IsNil(o.Port) {
		return true
	}

	return false
}

// SetPort gets a reference to the given int32 and assigns it to the Port field.
func (o *PutConfigMonitoringsInnerProps) SetPort(v int32) {
	o.Port = &v
}

// GetTlsEnabled returns the TlsEnabled field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetTlsEnabled() bool {
	if o == nil || IsNil(o.TlsEnabled) {
		var ret bool
		return ret
	}
	return *o.TlsEnabled
}

// GetTlsEnabledOk returns a tuple with the TlsEnabled field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetTlsEnabledOk() (*bool, bool) {
	if o == nil || IsNil(o.TlsEnabled) {
		return nil, false
	}
	return o.TlsEnabled, true
}

// HasTlsEnabled returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasTlsEnabled() bool {
	if o != nil && !IsNil(o.TlsEnabled) {
		return true
	}

	return false
}

// SetTlsEnabled gets a reference to the given bool and assigns it to the TlsEnabled field.
func (o *PutConfigMonitoringsInnerProps) SetTlsEnabled(v bool) {
	o.TlsEnabled = &v
}

// GetTlsSni returns the TlsSni field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetTlsSni() string {
	if o == nil || IsNil(o.TlsSni) {
		var ret string
		return ret
	}
	return *o.TlsSni
}

// GetTlsSniOk returns a tuple with the TlsSni field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetTlsSniOk() (*string, bool) {
	if o == nil || IsNil(o.TlsSni) {
		return nil, false
	}
	return o.TlsSni, true
}

// HasTlsSni returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasTlsSni() bool {
	if o != nil && !IsNil(o.TlsSni) {
		return true
	}

	return false
}

// SetTlsSni gets a reference to the given string and assigns it to the TlsSni field.
func (o *PutConfigMonitoringsInnerProps) SetTlsSni(v string) {
	o.TlsSni = &v
}

// GetResponseMatch returns the ResponseMatch field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetResponseMatch() string {
	if o == nil || IsNil(o.ResponseMatch) {
		var ret string
		return ret
	}
	return *o.ResponseMatch
}

// GetResponseMatchOk returns a tuple with the ResponseMatch field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetResponseMatchOk() (*string, bool) {
	if o == nil || IsNil(o.ResponseMatch) {
		return nil, false
	}
	return o.ResponseMatch, true
}

// HasResponseMatch returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasResponseMatch() bool {
	if o != nil && !IsNil(o.ResponseMatch) {
		return true
	}

	return false
}

// SetResponseMatch gets a reference to the given string and assigns it to the ResponseMatch field.
func (o *PutConfigMonitoringsInnerProps) SetResponseMatch(v string) {
	o.ResponseMatch = &v
}

// GetHttps returns the Https field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetHttps() bool {
	if o == nil || IsNil(o.Https) {
		var ret bool
		return ret
	}
	return *o.Https
}

// GetHttpsOk returns a tuple with the Https field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetHttpsOk() (*bool, bool) {
	if o == nil || IsNil(o.Https) {
		return nil, false
	}
	return o.Https, true
}

// HasHttps returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasHttps() bool {
	if o != nil && !IsNil(o.Https) {
		return true
	}

	return false
}

// SetHttps gets a reference to the given bool and assigns it to the Https field.
func (o *PutConfigMonitoringsInnerProps) SetHttps(v bool) {
	o.Https = &v
}

// GetHeaders returns the Headers field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetHeaders() []PutConfigMonitoringsInnerPropsHeadersInner {
	if o == nil || IsNil(o.Headers) {
		var ret []PutConfigMonitoringsInnerPropsHeadersInner
		return ret
	}
	return o.Headers
}

// GetHeadersOk returns a tuple with the Headers field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetHeadersOk() ([]PutConfigMonitoringsInnerPropsHeadersInner, bool) {
	if o == nil || IsNil(o.Headers) {
		return nil, false
	}
	return o.Headers, true
}

// HasHeaders returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasHeaders() bool {
	if o != nil && !IsNil(o.Headers) {
		return true
	}

	return false
}

// SetHeaders gets a reference to the given []PutConfigMonitoringsInnerPropsHeadersInner and assigns it to the Headers field.
func (o *PutConfigMonitoringsInnerProps) SetHeaders(v []PutConfigMonitoringsInnerPropsHeadersInner) {
	o.Headers = v
}

// GetPath returns the Path field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetPath() string {
	if o == nil || IsNil(o.Path) {
		var ret string
		return ret
	}
	return *o.Path
}

// GetPathOk returns a tuple with the Path field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetPathOk() (*string, bool) {
	if o == nil || IsNil(o.Path) {
		return nil, false
	}
	return o.Path, true
}

// HasPath returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasPath() bool {
	if o != nil && !IsNil(o.Path) {
		return true
	}

	return false
}

// SetPath gets a reference to the given string and assigns it to the Path field.
func (o *PutConfigMonitoringsInnerProps) SetPath(v string) {
	o.Path = &v
}

// GetStatusCodes returns the StatusCodes field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetStatusCodes() []string {
	if o == nil || IsNil(o.StatusCodes) {
		var ret []string
		return ret
	}
	return o.StatusCodes
}

// GetStatusCodesOk returns a tuple with the StatusCodes field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetStatusCodesOk() ([]string, bool) {
	if o == nil || IsNil(o.StatusCodes) {
		return nil, false
	}
	return o.StatusCodes, true
}

// HasStatusCodes returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasStatusCodes() bool {
	if o != nil && !IsNil(o.StatusCodes) {
		return true
	}

	return false
}

// SetStatusCodes gets a reference to the given []string and assigns it to the StatusCodes field.
func (o *PutConfigMonitoringsInnerProps) SetStatusCodes(v []string) {
	o.StatusCodes = v
}

// GetResult returns the Result field value if set, zero value otherwise.
func (o *PutConfigMonitoringsInnerProps) GetResult() string {
	if o == nil || IsNil(o.Result) {
		var ret string
		return ret
	}
	return *o.Result
}

// GetResultOk returns a tuple with the Result field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *PutConfigMonitoringsInnerProps) GetResultOk() (*string, bool) {
	if o == nil || IsNil(o.Result) {
		return nil, false
	}
	return o.Result, true
}

// HasResult returns a boolean if a field has been set.
func (o *PutConfigMonitoringsInnerProps) HasResult() bool {
	if o != nil && !IsNil(o.Result) {
		return true
	}

	return false
}

// SetResult gets a reference to the given string and assigns it to the Result field.
func (o *PutConfigMonitoringsInnerProps) SetResult(v string) {
	o.Result = &v
}

func (o PutConfigMonitoringsInnerProps) MarshalJSON() ([]byte, error) {
	toSerialize, err := o.ToMap()
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(toSerialize)
}

func (o PutConfigMonitoringsInnerProps) ToMap() (map[string]interface{}, error) {
	toSerialize := map[string]interface{}{}
	if !IsNil(o.Location) {
		toSerialize["location"] = o.Location
	}
	if !IsNil(o.Interval) {
		toSerialize["interval"] = o.Interval
	}
	if !IsNil(o.Holdtime) {
		toSerialize["holdtime"] = o.Holdtime
	}
	if !IsNil(o.Timeout) {
		toSerialize["timeout"] = o.Timeout
	}
	if !IsNil(o.Port) {
		toSerialize["port"] = o.Port
	}
	if !IsNil(o.TlsEnabled) {
		toSerialize["tls_enabled"] = o.TlsEnabled
	}
	if !IsNil(o.TlsSni) {
		toSerialize["tls_sni"] = o.TlsSni
	}
	if !IsNil(o.ResponseMatch) {
		toSerialize["response_match"] = o.ResponseMatch
	}
	if !IsNil(o.Https) {
		toSerialize["https"] = o.Https
	}
	if !IsNil(o.Headers) {
		toSerialize["headers"] = o.Headers
	}
	if !IsNil(o.Path) {
		toSerialize["path"] = o.Path
	}
	if !IsNil(o.StatusCodes) {
		toSerialize["status_codes"] = o.StatusCodes
	}
	if !IsNil(o.Result) {
		toSerialize["result"] = o.Result
	}
	return toSerialize, nil
}

type NullablePutConfigMonitoringsInnerProps struct {
	value *PutConfigMonitoringsInnerProps
	isSet bool
}

func (v NullablePutConfigMonitoringsInnerProps) Get() *PutConfigMonitoringsInnerProps {
	return v.value
}

func (v *NullablePutConfigMonitoringsInnerProps) Set(val *PutConfigMonitoringsInnerProps) {
	v.value = val
	v.isSet = true
}

func (v NullablePutConfigMonitoringsInnerProps) IsSet() bool {
	return v.isSet
}

func (v *NullablePutConfigMonitoringsInnerProps) Unset() {
	v.value = nil
	v.isSet = false
}

func NewNullablePutConfigMonitoringsInnerProps(val *PutConfigMonitoringsInnerProps) *NullablePutConfigMonitoringsInnerProps {
	return &NullablePutConfigMonitoringsInnerProps{value: val, isSet: true}
}

func (v NullablePutConfigMonitoringsInnerProps) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

func (v *NullablePutConfigMonitoringsInnerProps) UnmarshalJSON(src []byte) error {
	v.isSet = true
	return json.Unmarshal(src, &v.value)
}
