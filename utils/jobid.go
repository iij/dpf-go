// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// GetJobID は Execute() が返す *http.Response のレスポンスボディから
// request_id を取得する。取得できない場合は空文字列を返す。
//
// 生成された Execute() はボディを読み込んだ後に再読込可能な形へ復元するため、
// この関数はボディを読み出した後も元に戻し、呼び出し側の再読込を妨げない。
func GetJobID(res *http.Response) string {
	if res == nil || res.Body == nil {
		return ""
	}

	body, err := io.ReadAll(res.Body)
	// 読み出しは完了しているため Close のエラーは無視してよい。
	_ = res.Body.Close()
	// 呼び出し側がボディを再度読めるよう復元する。
	res.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return ""
	}

	var v struct {
		RequestId string `json:"request_id"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return v.RequestId
}
