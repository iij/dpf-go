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
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

// RecordsAPIService RecordsAPI service
type RecordsAPIService service

type ApiDeleteRecordRequest struct {
	ctx        context.Context
	ApiService *RecordsAPIService
	zoneId     string
	recordId   string
}

func (r ApiDeleteRecordRequest) Execute() (*AsyncResponse, *http.Response, error) {
	return r.ApiService.DeleteRecordExecute(r)
}

/*
DeleteRecord レコードの削除

指定したRecordIdのレコードを削除します。\
[**編集中レコードのゾーン反映**](#tag/zones/operation/patchZoneChanges)を実行するまでは権威サーバには反映されません。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@param recordId ID
	@return ApiDeleteRecordRequest
*/
func (a *RecordsAPIService) DeleteRecord(ctx context.Context, zoneId string, recordId string) ApiDeleteRecordRequest {
	return ApiDeleteRecordRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
		recordId:   recordId,
	}
}

// Execute executes the request
//
//	@return AsyncResponse
func (a *RecordsAPIService) DeleteRecordExecute(r ApiDeleteRecordRequest) (*AsyncResponse, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodDelete
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *AsyncResponse
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.DeleteRecord")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/{RecordId}"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)
	localVarPath = strings.Replace(localVarPath, "{"+"RecordId"+"}", url.PathEscape(parameterValueToString(r.recordId, "recordId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if strlen(r.recordId) < 14 {
		return localVarReturnValue, nil, reportError("recordId must have at least 14 elements")
	}
	if strlen(r.recordId) > 14 {
		return localVarReturnValue, nil, reportError("recordId must have less than 14 elements")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiDeleteRecordChangesRequest struct {
	ctx        context.Context
	ApiService *RecordsAPIService
	zoneId     string
	recordId   string
}

func (r ApiDeleteRecordChangesRequest) Execute() (*AsyncResponse, *http.Response, error) {
	return r.ApiService.DeleteRecordChangesExecute(r)
}

/*
DeleteRecordChanges 編集中レコードの取消

指定したRecordIdのレコードの操作を取消します。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@param recordId ID
	@return ApiDeleteRecordChangesRequest
*/
func (a *RecordsAPIService) DeleteRecordChanges(ctx context.Context, zoneId string, recordId string) ApiDeleteRecordChangesRequest {
	return ApiDeleteRecordChangesRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
		recordId:   recordId,
	}
}

// Execute executes the request
//
//	@return AsyncResponse
func (a *RecordsAPIService) DeleteRecordChangesExecute(r ApiDeleteRecordChangesRequest) (*AsyncResponse, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodDelete
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *AsyncResponse
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.DeleteRecordChanges")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/{RecordId}/changes"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)
	localVarPath = strings.Replace(localVarPath, "{"+"RecordId"+"}", url.PathEscape(parameterValueToString(r.recordId, "recordId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if strlen(r.recordId) < 14 {
		return localVarReturnValue, nil, reportError("recordId must have at least 14 elements")
	}
	if strlen(r.recordId) > 14 {
		return localVarReturnValue, nil, reportError("recordId must have less than 14 elements")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetDsRecordsFromCdsRequest struct {
	ctx        context.Context
	ApiService *RecordsAPIService
	zoneId     string
	recordId   string
}

func (r ApiGetDsRecordsFromCdsRequest) Execute() (*GetDsRecordsFromCds, *http.Response, error) {
	return r.ApiService.GetDsRecordsFromCdsExecute(r)
}

/*
GetDsRecordsFromCds CDS経由のDSレコードの取得

指定したRecordIdのレコードによって取り込まれるCDS経由のDSレコードを取得します。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@param recordId ID
	@return ApiGetDsRecordsFromCdsRequest
*/
func (a *RecordsAPIService) GetDsRecordsFromCds(ctx context.Context, zoneId string, recordId string) ApiGetDsRecordsFromCdsRequest {
	return ApiGetDsRecordsFromCdsRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
		recordId:   recordId,
	}
}

// Execute executes the request
//
//	@return GetDsRecordsFromCds
func (a *RecordsAPIService) GetDsRecordsFromCdsExecute(r ApiGetDsRecordsFromCdsRequest) (*GetDsRecordsFromCds, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetDsRecordsFromCds
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetDsRecordsFromCds")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/{RecordId}/ds_records"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)
	localVarPath = strings.Replace(localVarPath, "{"+"RecordId"+"}", url.PathEscape(parameterValueToString(r.recordId, "recordId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if strlen(r.recordId) < 14 {
		return localVarReturnValue, nil, reportError("recordId must have at least 14 elements")
	}
	if strlen(r.recordId) > 14 {
		return localVarReturnValue, nil, reportError("recordId must have less than 14 elements")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordRequest struct {
	ctx        context.Context
	ApiService *RecordsAPIService
	zoneId     string
	recordId   string
}

func (r ApiGetRecordRequest) Execute() (*GetRecord, *http.Response, error) {
	return r.ApiService.GetRecordExecute(r)
}

/*
GetRecord レコードの取得

指定したRecordIdのレコードを取得します。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@param recordId ID
	@return ApiGetRecordRequest
*/
func (a *RecordsAPIService) GetRecord(ctx context.Context, zoneId string, recordId string) ApiGetRecordRequest {
	return ApiGetRecordRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
		recordId:   recordId,
	}
}

// Execute executes the request
//
//	@return GetRecord
func (a *RecordsAPIService) GetRecordExecute(r ApiGetRecordRequest) (*GetRecord, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetRecord
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecord")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/{RecordId}"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)
	localVarPath = strings.Replace(localVarPath, "{"+"RecordId"+"}", url.PathEscape(parameterValueToString(r.recordId, "recordId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if strlen(r.recordId) < 14 {
		return localVarReturnValue, nil, reportError("recordId must have at least 14 elements")
	}
	if strlen(r.recordId) > 14 {
		return localVarReturnValue, nil, reportError("recordId must have less than 14 elements")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordCurrentsRequest struct {
	ctx                 context.Context
	ApiService          *RecordsAPIService
	zoneId              string
	type_               *SearchType
	offset              *int32
	limit               *int32
	keywordsFullText    *[]string
	keywordsName        *[]string
	keywordsTtl         *int32
	keywordsRrtype      *RecordsRrtype
	keywordsRdata       *[]string
	keywordsDescription *[]string
	keywordsOperator    *[]string
	keywordsLabel       *[]string
}

func (r ApiGetRecordCurrentsRequest) Type_(type_ SearchType) ApiGetRecordCurrentsRequest {
	r.type_ = &type_
	return r
}

func (r ApiGetRecordCurrentsRequest) Offset(offset int32) ApiGetRecordCurrentsRequest {
	r.offset = &offset
	return r
}

func (r ApiGetRecordCurrentsRequest) Limit(limit int32) ApiGetRecordCurrentsRequest {
	r.limit = &limit
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsFullText(keywordsFullText []string) ApiGetRecordCurrentsRequest {
	r.keywordsFullText = &keywordsFullText
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsName(keywordsName []string) ApiGetRecordCurrentsRequest {
	r.keywordsName = &keywordsName
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsTtl(keywordsTtl int32) ApiGetRecordCurrentsRequest {
	r.keywordsTtl = &keywordsTtl
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsRrtype(keywordsRrtype RecordsRrtype) ApiGetRecordCurrentsRequest {
	r.keywordsRrtype = &keywordsRrtype
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsRdata(keywordsRdata []string) ApiGetRecordCurrentsRequest {
	r.keywordsRdata = &keywordsRdata
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsDescription(keywordsDescription []string) ApiGetRecordCurrentsRequest {
	r.keywordsDescription = &keywordsDescription
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsOperator(keywordsOperator []string) ApiGetRecordCurrentsRequest {
	r.keywordsOperator = &keywordsOperator
	return r
}

func (r ApiGetRecordCurrentsRequest) KeywordsLabel(keywordsLabel []string) ApiGetRecordCurrentsRequest {
	r.keywordsLabel = &keywordsLabel
	return r
}

func (r ApiGetRecordCurrentsRequest) Execute() (*GetRecords, *http.Response, error) {
	return r.ApiService.GetRecordCurrentsExecute(r)
}

/*
GetRecordCurrents DNS反映済レコードの一覧取得

現在DNSに登録されているレコードの一覧を取得します。\
"_keywords" から始まるパラメータは、合計で30個まで指定可能です。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiGetRecordCurrentsRequest
*/
func (a *RecordsAPIService) GetRecordCurrents(ctx context.Context, zoneId string) ApiGetRecordCurrentsRequest {
	return ApiGetRecordCurrentsRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return GetRecords
func (a *RecordsAPIService) GetRecordCurrentsExecute(r ApiGetRecordCurrentsRequest) (*GetRecords, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetRecords
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecordCurrents")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/currents"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}

	if r.type_ != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", r.type_, "form", "")
	} else {
		var defaultValue SearchType = "AND"
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", defaultValue, "form", "")
		r.type_ = &defaultValue
	}
	if r.offset != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "offset", r.offset, "form", "")
	} else {
		var defaultValue int32 = 0
		parameterAddToHeaderOrQuery(localVarQueryParams, "offset", defaultValue, "form", "")
		r.offset = &defaultValue
	}
	if r.limit != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "limit", r.limit, "form", "")
	} else {
		var defaultValue int32 = 100
		parameterAddToHeaderOrQuery(localVarQueryParams, "limit", defaultValue, "form", "")
		r.limit = &defaultValue
	}
	if r.keywordsFullText != nil {
		t := *r.keywordsFullText
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", defaultValue, "form", "multi")
		r.keywordsFullText = &defaultValue
	}
	if r.keywordsName != nil {
		t := *r.keywordsName
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", defaultValue, "form", "multi")
		r.keywordsName = &defaultValue
	}
	if r.keywordsTtl != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_ttl[]", r.keywordsTtl, "form", "")
	}
	if r.keywordsRrtype != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rrtype[]", r.keywordsRrtype, "form", "")
	}
	if r.keywordsRdata != nil {
		t := *r.keywordsRdata
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", defaultValue, "form", "multi")
		r.keywordsRdata = &defaultValue
	}
	if r.keywordsDescription != nil {
		t := *r.keywordsDescription
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", defaultValue, "form", "multi")
		r.keywordsDescription = &defaultValue
	}
	if r.keywordsOperator != nil {
		t := *r.keywordsOperator
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", defaultValue, "form", "multi")
		r.keywordsOperator = &defaultValue
	}
	if r.keywordsLabel != nil {
		t := *r.keywordsLabel
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", defaultValue, "form", "multi")
		r.keywordsLabel = &defaultValue
	}
	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordCurrentsCountRequest struct {
	ctx                 context.Context
	ApiService          *RecordsAPIService
	zoneId              string
	type_               *SearchType
	keywordsFullText    *[]string
	keywordsName        *[]string
	keywordsTtl         *int32
	keywordsRrtype      *RecordsRrtype
	keywordsRdata       *[]string
	keywordsDescription *[]string
	keywordsOperator    *[]string
	keywordsLabel       *[]string
}

func (r ApiGetRecordCurrentsCountRequest) Type_(type_ SearchType) ApiGetRecordCurrentsCountRequest {
	r.type_ = &type_
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsFullText(keywordsFullText []string) ApiGetRecordCurrentsCountRequest {
	r.keywordsFullText = &keywordsFullText
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsName(keywordsName []string) ApiGetRecordCurrentsCountRequest {
	r.keywordsName = &keywordsName
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsTtl(keywordsTtl int32) ApiGetRecordCurrentsCountRequest {
	r.keywordsTtl = &keywordsTtl
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsRrtype(keywordsRrtype RecordsRrtype) ApiGetRecordCurrentsCountRequest {
	r.keywordsRrtype = &keywordsRrtype
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsRdata(keywordsRdata []string) ApiGetRecordCurrentsCountRequest {
	r.keywordsRdata = &keywordsRdata
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsDescription(keywordsDescription []string) ApiGetRecordCurrentsCountRequest {
	r.keywordsDescription = &keywordsDescription
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsOperator(keywordsOperator []string) ApiGetRecordCurrentsCountRequest {
	r.keywordsOperator = &keywordsOperator
	return r
}

func (r ApiGetRecordCurrentsCountRequest) KeywordsLabel(keywordsLabel []string) ApiGetRecordCurrentsCountRequest {
	r.keywordsLabel = &keywordsLabel
	return r
}

func (r ApiGetRecordCurrentsCountRequest) Execute() (*GetCount, *http.Response, error) {
	return r.ApiService.GetRecordCurrentsCountExecute(r)
}

/*
GetRecordCurrentsCount DNS反映済レコードの件数取得

現在DNSに登録されているレコードの件数を取得します。\
"_keywords" から始まるパラメータは、合計で30個まで指定可能です。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiGetRecordCurrentsCountRequest
*/
func (a *RecordsAPIService) GetRecordCurrentsCount(ctx context.Context, zoneId string) ApiGetRecordCurrentsCountRequest {
	return ApiGetRecordCurrentsCountRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return GetCount
func (a *RecordsAPIService) GetRecordCurrentsCountExecute(r ApiGetRecordCurrentsCountRequest) (*GetCount, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetCount
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecordCurrentsCount")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/currents/count"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}

	if r.type_ != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", r.type_, "form", "")
	} else {
		var defaultValue SearchType = "AND"
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", defaultValue, "form", "")
		r.type_ = &defaultValue
	}
	if r.keywordsFullText != nil {
		t := *r.keywordsFullText
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", defaultValue, "form", "multi")
		r.keywordsFullText = &defaultValue
	}
	if r.keywordsName != nil {
		t := *r.keywordsName
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", defaultValue, "form", "multi")
		r.keywordsName = &defaultValue
	}
	if r.keywordsTtl != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_ttl[]", r.keywordsTtl, "form", "")
	}
	if r.keywordsRrtype != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rrtype[]", r.keywordsRrtype, "form", "")
	}
	if r.keywordsRdata != nil {
		t := *r.keywordsRdata
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", defaultValue, "form", "multi")
		r.keywordsRdata = &defaultValue
	}
	if r.keywordsDescription != nil {
		t := *r.keywordsDescription
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", defaultValue, "form", "multi")
		r.keywordsDescription = &defaultValue
	}
	if r.keywordsOperator != nil {
		t := *r.keywordsOperator
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", defaultValue, "form", "multi")
		r.keywordsOperator = &defaultValue
	}
	if r.keywordsLabel != nil {
		t := *r.keywordsLabel
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", defaultValue, "form", "multi")
		r.keywordsLabel = &defaultValue
	}
	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordDiffsRequest struct {
	ctx                 context.Context
	ApiService          *RecordsAPIService
	zoneId              string
	type_               *SearchType
	offset              *int32
	limit               *int32
	keywordsFullText    *[]string
	keywordsName        *[]string
	keywordsTtl         *int32
	keywordsRrtype      *RecordsRrtype
	keywordsRdata       *[]string
	keywordsDescription *[]string
	keywordsLabel       *[]string
}

func (r ApiGetRecordDiffsRequest) Type_(type_ SearchType) ApiGetRecordDiffsRequest {
	r.type_ = &type_
	return r
}

func (r ApiGetRecordDiffsRequest) Offset(offset int32) ApiGetRecordDiffsRequest {
	r.offset = &offset
	return r
}

func (r ApiGetRecordDiffsRequest) Limit(limit int32) ApiGetRecordDiffsRequest {
	r.limit = &limit
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsFullText(keywordsFullText []string) ApiGetRecordDiffsRequest {
	r.keywordsFullText = &keywordsFullText
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsName(keywordsName []string) ApiGetRecordDiffsRequest {
	r.keywordsName = &keywordsName
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsTtl(keywordsTtl int32) ApiGetRecordDiffsRequest {
	r.keywordsTtl = &keywordsTtl
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsRrtype(keywordsRrtype RecordsRrtype) ApiGetRecordDiffsRequest {
	r.keywordsRrtype = &keywordsRrtype
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsRdata(keywordsRdata []string) ApiGetRecordDiffsRequest {
	r.keywordsRdata = &keywordsRdata
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsDescription(keywordsDescription []string) ApiGetRecordDiffsRequest {
	r.keywordsDescription = &keywordsDescription
	return r
}

func (r ApiGetRecordDiffsRequest) KeywordsLabel(keywordsLabel []string) ApiGetRecordDiffsRequest {
	r.keywordsLabel = &keywordsLabel
	return r
}

func (r ApiGetRecordDiffsRequest) Execute() (*GetRecordsDiffs, *http.Response, error) {
	return r.ApiService.GetRecordDiffsExecute(r)
}

/*
GetRecordDiffs レコードの編集差分の一覧取得

現在DNSに登録されているレコードと反映予定のレコードの差分一覧を取得します。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiGetRecordDiffsRequest
*/
func (a *RecordsAPIService) GetRecordDiffs(ctx context.Context, zoneId string) ApiGetRecordDiffsRequest {
	return ApiGetRecordDiffsRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return GetRecordsDiffs
func (a *RecordsAPIService) GetRecordDiffsExecute(r ApiGetRecordDiffsRequest) (*GetRecordsDiffs, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetRecordsDiffs
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecordDiffs")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/diffs"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}

	if r.type_ != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", r.type_, "form", "")
	} else {
		var defaultValue SearchType = "AND"
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", defaultValue, "form", "")
		r.type_ = &defaultValue
	}
	if r.offset != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "offset", r.offset, "form", "")
	} else {
		var defaultValue int32 = 0
		parameterAddToHeaderOrQuery(localVarQueryParams, "offset", defaultValue, "form", "")
		r.offset = &defaultValue
	}
	if r.limit != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "limit", r.limit, "form", "")
	} else {
		var defaultValue int32 = 100
		parameterAddToHeaderOrQuery(localVarQueryParams, "limit", defaultValue, "form", "")
		r.limit = &defaultValue
	}
	if r.keywordsFullText != nil {
		t := *r.keywordsFullText
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", defaultValue, "form", "multi")
		r.keywordsFullText = &defaultValue
	}
	if r.keywordsName != nil {
		t := *r.keywordsName
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", defaultValue, "form", "multi")
		r.keywordsName = &defaultValue
	}
	if r.keywordsTtl != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_ttl[]", r.keywordsTtl, "form", "")
	}
	if r.keywordsRrtype != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rrtype[]", r.keywordsRrtype, "form", "")
	}
	if r.keywordsRdata != nil {
		t := *r.keywordsRdata
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", defaultValue, "form", "multi")
		r.keywordsRdata = &defaultValue
	}
	if r.keywordsDescription != nil {
		t := *r.keywordsDescription
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", defaultValue, "form", "multi")
		r.keywordsDescription = &defaultValue
	}
	if r.keywordsLabel != nil {
		t := *r.keywordsLabel
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", defaultValue, "form", "multi")
		r.keywordsLabel = &defaultValue
	}
	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordDiffsCountRequest struct {
	ctx                 context.Context
	ApiService          *RecordsAPIService
	zoneId              string
	type_               *SearchType
	keywordsFullText    *[]string
	keywordsName        *[]string
	keywordsTtl         *int32
	keywordsRrtype      *RecordsRrtype
	keywordsRdata       *[]string
	keywordsDescription *[]string
	keywordsLabel       *[]string
}

func (r ApiGetRecordDiffsCountRequest) Type_(type_ SearchType) ApiGetRecordDiffsCountRequest {
	r.type_ = &type_
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsFullText(keywordsFullText []string) ApiGetRecordDiffsCountRequest {
	r.keywordsFullText = &keywordsFullText
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsName(keywordsName []string) ApiGetRecordDiffsCountRequest {
	r.keywordsName = &keywordsName
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsTtl(keywordsTtl int32) ApiGetRecordDiffsCountRequest {
	r.keywordsTtl = &keywordsTtl
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsRrtype(keywordsRrtype RecordsRrtype) ApiGetRecordDiffsCountRequest {
	r.keywordsRrtype = &keywordsRrtype
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsRdata(keywordsRdata []string) ApiGetRecordDiffsCountRequest {
	r.keywordsRdata = &keywordsRdata
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsDescription(keywordsDescription []string) ApiGetRecordDiffsCountRequest {
	r.keywordsDescription = &keywordsDescription
	return r
}

func (r ApiGetRecordDiffsCountRequest) KeywordsLabel(keywordsLabel []string) ApiGetRecordDiffsCountRequest {
	r.keywordsLabel = &keywordsLabel
	return r
}

func (r ApiGetRecordDiffsCountRequest) Execute() (*GetCount, *http.Response, error) {
	return r.ApiService.GetRecordDiffsCountExecute(r)
}

/*
GetRecordDiffsCount レコードの編集差分の件数取得

現在DNSに登録されているレコードと反映予定のレコードの差分数を取得します。\
"_keywords" から始まるパラメータは、合計で30個まで指定可能です。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiGetRecordDiffsCountRequest
*/
func (a *RecordsAPIService) GetRecordDiffsCount(ctx context.Context, zoneId string) ApiGetRecordDiffsCountRequest {
	return ApiGetRecordDiffsCountRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return GetCount
func (a *RecordsAPIService) GetRecordDiffsCountExecute(r ApiGetRecordDiffsCountRequest) (*GetCount, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetCount
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecordDiffsCount")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/diffs/count"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}

	if r.type_ != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", r.type_, "form", "")
	} else {
		var defaultValue SearchType = "AND"
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", defaultValue, "form", "")
		r.type_ = &defaultValue
	}
	if r.keywordsFullText != nil {
		t := *r.keywordsFullText
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", defaultValue, "form", "multi")
		r.keywordsFullText = &defaultValue
	}
	if r.keywordsName != nil {
		t := *r.keywordsName
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", defaultValue, "form", "multi")
		r.keywordsName = &defaultValue
	}
	if r.keywordsTtl != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_ttl[]", r.keywordsTtl, "form", "")
	}
	if r.keywordsRrtype != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rrtype[]", r.keywordsRrtype, "form", "")
	}
	if r.keywordsRdata != nil {
		t := *r.keywordsRdata
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", defaultValue, "form", "multi")
		r.keywordsRdata = &defaultValue
	}
	if r.keywordsDescription != nil {
		t := *r.keywordsDescription
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", defaultValue, "form", "multi")
		r.keywordsDescription = &defaultValue
	}
	if r.keywordsLabel != nil {
		t := *r.keywordsLabel
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", defaultValue, "form", "multi")
		r.keywordsLabel = &defaultValue
	}
	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordListRequest struct {
	ctx                 context.Context
	ApiService          *RecordsAPIService
	zoneId              string
	type_               *SearchType
	offset              *int32
	limit               *int32
	keywordsFullText    *[]string
	keywordsName        *[]string
	keywordsTtl         *int32
	keywordsRrtype      *RecordsRrtype
	keywordsRdata       *[]string
	keywordsDescription *[]string
	keywordsOperator    *[]string
	keywordsLabel       *[]string
}

func (r ApiGetRecordListRequest) Type_(type_ SearchType) ApiGetRecordListRequest {
	r.type_ = &type_
	return r
}

func (r ApiGetRecordListRequest) Offset(offset int32) ApiGetRecordListRequest {
	r.offset = &offset
	return r
}

func (r ApiGetRecordListRequest) Limit(limit int32) ApiGetRecordListRequest {
	r.limit = &limit
	return r
}

func (r ApiGetRecordListRequest) KeywordsFullText(keywordsFullText []string) ApiGetRecordListRequest {
	r.keywordsFullText = &keywordsFullText
	return r
}

func (r ApiGetRecordListRequest) KeywordsName(keywordsName []string) ApiGetRecordListRequest {
	r.keywordsName = &keywordsName
	return r
}

func (r ApiGetRecordListRequest) KeywordsTtl(keywordsTtl int32) ApiGetRecordListRequest {
	r.keywordsTtl = &keywordsTtl
	return r
}

func (r ApiGetRecordListRequest) KeywordsRrtype(keywordsRrtype RecordsRrtype) ApiGetRecordListRequest {
	r.keywordsRrtype = &keywordsRrtype
	return r
}

func (r ApiGetRecordListRequest) KeywordsRdata(keywordsRdata []string) ApiGetRecordListRequest {
	r.keywordsRdata = &keywordsRdata
	return r
}

func (r ApiGetRecordListRequest) KeywordsDescription(keywordsDescription []string) ApiGetRecordListRequest {
	r.keywordsDescription = &keywordsDescription
	return r
}

func (r ApiGetRecordListRequest) KeywordsOperator(keywordsOperator []string) ApiGetRecordListRequest {
	r.keywordsOperator = &keywordsOperator
	return r
}

func (r ApiGetRecordListRequest) KeywordsLabel(keywordsLabel []string) ApiGetRecordListRequest {
	r.keywordsLabel = &keywordsLabel
	return r
}

func (r ApiGetRecordListRequest) Execute() (*GetRecords, *http.Response, error) {
	return r.ApiService.GetRecordListExecute(r)
}

/*
GetRecordList レコードの一覧取得

レコード情報の一覧を取得します。\
"_keywords" から始まるパラメータは、合計で30個まで指定可能です。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiGetRecordListRequest
*/
func (a *RecordsAPIService) GetRecordList(ctx context.Context, zoneId string) ApiGetRecordListRequest {
	return ApiGetRecordListRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return GetRecords
func (a *RecordsAPIService) GetRecordListExecute(r ApiGetRecordListRequest) (*GetRecords, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetRecords
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecordList")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}

	if r.type_ != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", r.type_, "form", "")
	} else {
		var defaultValue SearchType = "AND"
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", defaultValue, "form", "")
		r.type_ = &defaultValue
	}
	if r.offset != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "offset", r.offset, "form", "")
	} else {
		var defaultValue int32 = 0
		parameterAddToHeaderOrQuery(localVarQueryParams, "offset", defaultValue, "form", "")
		r.offset = &defaultValue
	}
	if r.limit != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "limit", r.limit, "form", "")
	} else {
		var defaultValue int32 = 100
		parameterAddToHeaderOrQuery(localVarQueryParams, "limit", defaultValue, "form", "")
		r.limit = &defaultValue
	}
	if r.keywordsFullText != nil {
		t := *r.keywordsFullText
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", defaultValue, "form", "multi")
		r.keywordsFullText = &defaultValue
	}
	if r.keywordsName != nil {
		t := *r.keywordsName
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", defaultValue, "form", "multi")
		r.keywordsName = &defaultValue
	}
	if r.keywordsTtl != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_ttl[]", r.keywordsTtl, "form", "")
	}
	if r.keywordsRrtype != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rrtype[]", r.keywordsRrtype, "form", "")
	}
	if r.keywordsRdata != nil {
		t := *r.keywordsRdata
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", defaultValue, "form", "multi")
		r.keywordsRdata = &defaultValue
	}
	if r.keywordsDescription != nil {
		t := *r.keywordsDescription
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", defaultValue, "form", "multi")
		r.keywordsDescription = &defaultValue
	}
	if r.keywordsOperator != nil {
		t := *r.keywordsOperator
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", defaultValue, "form", "multi")
		r.keywordsOperator = &defaultValue
	}
	if r.keywordsLabel != nil {
		t := *r.keywordsLabel
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", defaultValue, "form", "multi")
		r.keywordsLabel = &defaultValue
	}
	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiGetRecordListCountRequest struct {
	ctx                 context.Context
	ApiService          *RecordsAPIService
	zoneId              string
	type_               *SearchType
	keywordsFullText    *[]string
	keywordsName        *[]string
	keywordsTtl         *int32
	keywordsRrtype      *RecordsRrtype
	keywordsRdata       *[]string
	keywordsDescription *[]string
	keywordsOperator    *[]string
	keywordsLabel       *[]string
}

func (r ApiGetRecordListCountRequest) Type_(type_ SearchType) ApiGetRecordListCountRequest {
	r.type_ = &type_
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsFullText(keywordsFullText []string) ApiGetRecordListCountRequest {
	r.keywordsFullText = &keywordsFullText
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsName(keywordsName []string) ApiGetRecordListCountRequest {
	r.keywordsName = &keywordsName
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsTtl(keywordsTtl int32) ApiGetRecordListCountRequest {
	r.keywordsTtl = &keywordsTtl
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsRrtype(keywordsRrtype RecordsRrtype) ApiGetRecordListCountRequest {
	r.keywordsRrtype = &keywordsRrtype
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsRdata(keywordsRdata []string) ApiGetRecordListCountRequest {
	r.keywordsRdata = &keywordsRdata
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsDescription(keywordsDescription []string) ApiGetRecordListCountRequest {
	r.keywordsDescription = &keywordsDescription
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsOperator(keywordsOperator []string) ApiGetRecordListCountRequest {
	r.keywordsOperator = &keywordsOperator
	return r
}

func (r ApiGetRecordListCountRequest) KeywordsLabel(keywordsLabel []string) ApiGetRecordListCountRequest {
	r.keywordsLabel = &keywordsLabel
	return r
}

func (r ApiGetRecordListCountRequest) Execute() (*GetCount, *http.Response, error) {
	return r.ApiService.GetRecordListCountExecute(r)
}

/*
GetRecordListCount レコードの件数取得

レコード情報の件数を取得します。\
"_keywords" から始まるパラメータは、合計で30個まで指定可能です。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiGetRecordListCountRequest
*/
func (a *RecordsAPIService) GetRecordListCount(ctx context.Context, zoneId string) ApiGetRecordListCountRequest {
	return ApiGetRecordListCountRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return GetCount
func (a *RecordsAPIService) GetRecordListCountExecute(r ApiGetRecordListCountRequest) (*GetCount, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodGet
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *GetCount
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.GetRecordListCount")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/count"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}

	if r.type_ != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", r.type_, "form", "")
	} else {
		var defaultValue SearchType = "AND"
		parameterAddToHeaderOrQuery(localVarQueryParams, "type", defaultValue, "form", "")
		r.type_ = &defaultValue
	}
	if r.keywordsFullText != nil {
		t := *r.keywordsFullText
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_full_text[]", defaultValue, "form", "multi")
		r.keywordsFullText = &defaultValue
	}
	if r.keywordsName != nil {
		t := *r.keywordsName
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_name[]", defaultValue, "form", "multi")
		r.keywordsName = &defaultValue
	}
	if r.keywordsTtl != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_ttl[]", r.keywordsTtl, "form", "")
	}
	if r.keywordsRrtype != nil {
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rrtype[]", r.keywordsRrtype, "form", "")
	}
	if r.keywordsRdata != nil {
		t := *r.keywordsRdata
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_rdata[]", defaultValue, "form", "multi")
		r.keywordsRdata = &defaultValue
	}
	if r.keywordsDescription != nil {
		t := *r.keywordsDescription
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_description[]", defaultValue, "form", "multi")
		r.keywordsDescription = &defaultValue
	}
	if r.keywordsOperator != nil {
		t := *r.keywordsOperator
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_operator[]", defaultValue, "form", "multi")
		r.keywordsOperator = &defaultValue
	}
	if r.keywordsLabel != nil {
		t := *r.keywordsLabel
		if reflect.TypeOf(t).Kind() == reflect.Slice {
			s := reflect.ValueOf(t)
			for i := 0; i < s.Len(); i++ {
				parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", s.Index(i).Interface(), "form", "multi")
			}
		} else {
			parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", t, "form", "multi")
		}
	} else {
		var defaultValue []string = []string{}
		parameterAddToHeaderOrQuery(localVarQueryParams, "_keywords_label[]", defaultValue, "form", "multi")
		r.keywordsLabel = &defaultValue
	}
	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiPatchRecordRequest struct {
	ctx         context.Context
	ApiService  *RecordsAPIService
	zoneId      string
	recordId    string
	patchRecord *PatchRecord
}

func (r ApiPatchRecordRequest) PatchRecord(patchRecord PatchRecord) ApiPatchRecordRequest {
	r.patchRecord = &patchRecord
	return r
}

func (r ApiPatchRecordRequest) Execute() (*AsyncResponse, *http.Response, error) {
	return r.ApiService.PatchRecordExecute(r)
}

/*
PatchRecord レコードの更新

指定したRecordIdのレコードを更新します。\
[**編集中レコードのゾーン反映**](#tag/zones/operation/patchZoneChanges)を実行するまでは権威サーバには反映されません。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@param recordId ID
	@return ApiPatchRecordRequest
*/
func (a *RecordsAPIService) PatchRecord(ctx context.Context, zoneId string, recordId string) ApiPatchRecordRequest {
	return ApiPatchRecordRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
		recordId:   recordId,
	}
}

// Execute executes the request
//
//	@return AsyncResponse
func (a *RecordsAPIService) PatchRecordExecute(r ApiPatchRecordRequest) (*AsyncResponse, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodPatch
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *AsyncResponse
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.PatchRecord")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records/{RecordId}"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)
	localVarPath = strings.Replace(localVarPath, "{"+"RecordId"+"}", url.PathEscape(parameterValueToString(r.recordId, "recordId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if strlen(r.recordId) < 14 {
		return localVarReturnValue, nil, reportError("recordId must have at least 14 elements")
	}
	if strlen(r.recordId) > 14 {
		return localVarReturnValue, nil, reportError("recordId must have less than 14 elements")
	}
	if r.patchRecord == nil {
		return localVarReturnValue, nil, reportError("patchRecord is required and must be specified")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{"application/json"}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	// body params
	localVarPostBody = r.patchRecord
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiPostRecordRequest struct {
	ctx        context.Context
	ApiService *RecordsAPIService
	zoneId     string
	postRecord *PostRecord
}

func (r ApiPostRecordRequest) PostRecord(postRecord PostRecord) ApiPostRecordRequest {
	r.postRecord = &postRecord
	return r
}

func (r ApiPostRecordRequest) Execute() (*AsyncResponse, *http.Response, error) {
	return r.ApiService.PostRecordExecute(r)
}

/*
PostRecord レコードの作成

新しくレコードを作成します。\
[**編集中レコードのゾーン反映**](#tag/zones/operation/patchZoneChanges)を実行するまでは権威サーバには反映されません。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiPostRecordRequest
*/
func (a *RecordsAPIService) PostRecord(ctx context.Context, zoneId string) ApiPostRecordRequest {
	return ApiPostRecordRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return AsyncResponse
func (a *RecordsAPIService) PostRecordExecute(r ApiPostRecordRequest) (*AsyncResponse, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodPost
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *AsyncResponse
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.PostRecord")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if r.postRecord == nil {
		return localVarReturnValue, nil, reportError("postRecord is required and must be specified")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{"application/json"}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	// body params
	localVarPostBody = r.postRecord
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}

type ApiPutRecordRequest struct {
	ctx        context.Context
	ApiService *RecordsAPIService
	zoneId     string
	putRecord  *PutRecord
}

func (r ApiPutRecordRequest) PutRecord(putRecord PutRecord) ApiPutRecordRequest {
	r.putRecord = &putRecord
	return r
}

func (r ApiPutRecordRequest) Execute() (*AsyncResponse, *http.Response, error) {
	return r.ApiService.PutRecordExecute(r)
}

/*
PutRecord レコードの一括更新

レコードを一括更新します。\
[**編集中レコードのゾーン反映**](#tag/zones/operation/patchZoneChanges)を実行するまでは権威サーバには反映されません。

	@param ctx context.Context - for authentication, logging, cancellation, deadlines, tracing, etc. Passed from http.Request or context.Background().
	@param zoneId ID
	@return ApiPutRecordRequest
*/
func (a *RecordsAPIService) PutRecord(ctx context.Context, zoneId string) ApiPutRecordRequest {
	return ApiPutRecordRequest{
		ApiService: a,
		ctx:        ctx,
		zoneId:     zoneId,
	}
}

// Execute executes the request
//
//	@return AsyncResponse
func (a *RecordsAPIService) PutRecordExecute(r ApiPutRecordRequest) (*AsyncResponse, *http.Response, error) {
	var (
		localVarHTTPMethod  = http.MethodPut
		localVarPostBody    interface{}
		formFiles           []formFile
		localVarReturnValue *AsyncResponse
	)

	localBasePath, err := a.client.cfg.ServerURLWithContext(r.ctx, "RecordsAPIService.PutRecord")
	if err != nil {
		return localVarReturnValue, nil, &GenericOpenAPIError{error: err.Error()}
	}

	localVarPath := localBasePath + "/zones/{ZoneId}/records"
	localVarPath = strings.Replace(localVarPath, "{"+"ZoneId"+"}", url.PathEscape(parameterValueToString(r.zoneId, "zoneId")), -1)

	localVarHeaderParams := make(map[string]string)
	localVarQueryParams := url.Values{}
	localVarFormParams := url.Values{}
	if strlen(r.zoneId) < 14 {
		return localVarReturnValue, nil, reportError("zoneId must have at least 14 elements")
	}
	if strlen(r.zoneId) > 14 {
		return localVarReturnValue, nil, reportError("zoneId must have less than 14 elements")
	}
	if r.putRecord == nil {
		return localVarReturnValue, nil, reportError("putRecord is required and must be specified")
	}

	// to determine the Content-Type header
	localVarHTTPContentTypes := []string{"application/json"}

	// set Content-Type header
	localVarHTTPContentType := selectHeaderContentType(localVarHTTPContentTypes)
	if localVarHTTPContentType != "" {
		localVarHeaderParams["Content-Type"] = localVarHTTPContentType
	}

	// to determine the Accept header
	localVarHTTPHeaderAccepts := []string{"application/json"}

	// set Accept header
	localVarHTTPHeaderAccept := selectHeaderAccept(localVarHTTPHeaderAccepts)
	if localVarHTTPHeaderAccept != "" {
		localVarHeaderParams["Accept"] = localVarHTTPHeaderAccept
	}
	// body params
	localVarPostBody = r.putRecord
	req, err := a.client.prepareRequest(r.ctx, localVarPath, localVarHTTPMethod, localVarPostBody, localVarHeaderParams, localVarQueryParams, localVarFormParams, formFiles)
	if err != nil {
		return localVarReturnValue, nil, err
	}

	localVarHTTPResponse, err := a.client.callAPI(req)
	if err != nil || localVarHTTPResponse == nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
	localVarHTTPResponse.Body.Close()
	localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))
	if err != nil {
		return localVarReturnValue, localVarHTTPResponse, err
	}

	if localVarHTTPResponse.StatusCode >= 300 {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: localVarHTTPResponse.Status,
		}
		if localVarHTTPResponse.StatusCode == 400 {
			var v ParameterErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 404 {
			var v NotFoundError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 406 {
			var v NotAcceptableError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 429 {
			var v TooManyRequestsError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 500 {
			var v SystemError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 503 {
			var v ServiceUnavailableOrBusyErrorResponse
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
			return localVarReturnValue, localVarHTTPResponse, newErr
		}
		if localVarHTTPResponse.StatusCode == 504 {
			var v GatewayTimeoutError
			err = a.client.decode(&v, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
			if err != nil {
				newErr.error = err.Error()
				return localVarReturnValue, localVarHTTPResponse, newErr
			}
			newErr.error = formatErrorMessage(localVarHTTPResponse.Status, &v)
			newErr.model = v
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	err = a.client.decode(&localVarReturnValue, localVarBody, localVarHTTPResponse.Header.Get("Content-Type"))
	if err != nil {
		newErr := &GenericOpenAPIError{
			body:  localVarBody,
			error: err.Error(),
		}
		return localVarReturnValue, localVarHTTPResponse, newErr
	}

	return localVarReturnValue, localVarHTTPResponse, nil
}
