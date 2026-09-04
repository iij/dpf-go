// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// SyncWaitPollInterval は SyncWait が非同期 JOB の状態を確認する間隔。
var SyncWaitPollInterval = 1 * time.Second

// SyncWait は、GET 以外の非同期 API の Execute() の戻り値をそのまま受け取り、
// 非同期 JOB が終了（SUCCESSFUL または FAILED）するまで待つ。
//
//	async, resp, err := apiService.Xxx(ctx, ...).Execute()
//	job, resp, err := client.JobsAPI.SyncWait(async, resp, err)
//
// 戻り値:
//   - *GetJobs : 終了時の job 情報
//   - *http.Response : Execute() の戻り値の resp（JOB が失敗・エラー時もこれを返す）
//   - error : Execute() がエラーを返した場合はそのエラー、
//     非同期 JOB が FAILED で終了した場合はその error_type / error_message を表すエラー、
//     正常終了時は nil
func (a *JobsAPIService) SyncWait(async *AsyncResponse, resp *http.Response, err error) (*GetJobs, *http.Response, error) {
	return a.SyncWaitContext(syncWaitContext(resp), async, resp, err)
}

// SyncWaitContext は SyncWait と同じだが、進捗確認のポーリングに使う context を
// 明示的に指定する。待ち時間に上限を設けたい場合や、呼び出し側のキャンセルを
// 確実に伝播させたい場合に使う。
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
//	defer cancel()
//	async, resp, err := apiService.Xxx(ctx, ...).Execute()
//	job, resp, err := client.JobsAPI.SyncWaitContext(ctx, async, resp, err)
func (a *JobsAPIService) SyncWaitContext(ctx context.Context, async *AsyncResponse, resp *http.Response, err error) (*GetJobs, *http.Response, error) {
	// Execute() がエラーを返していた場合はそのまま返す。
	if err != nil {
		return nil, resp, err
	}
	if async == nil {
		return nil, resp, fmt.Errorf("dpf: SyncWait: async response is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	requestId := async.GetRequestId()
	for {
		job, _, jobErr := a.GetJob(ctx, requestId).Execute()
		if jobErr != nil {
			return job, resp, jobErr
		}

		switch job.GetStatus() {
		case "SUCCESSFUL":
			return job, resp, nil
		case "FAILED":
			return job, resp, fmt.Errorf("dpf: async job failed: %s: %s",
				job.GetErrorType(), job.GetErrorMessage())
		default:
			// RUNNING（またはそれ以外）の場合は待機して再確認する。
		}

		select {
		case <-ctx.Done():
			return job, resp, ctx.Err()
		case <-time.After(SyncWaitPollInterval):
		}
	}
}

// syncWaitContext は SyncWait がポーリングに使う context を導出する。
//
// 元のリクエストの context を引き継ぐが、すでにキャンセルされている場合は
// キャンセルだけを切り離す。net/http は Client.Timeout が非ゼロで、かつ
// *http.Transport 以外の（未知の）Transport が使われている場合、リクエストの
// context を自身が作った期限付きのものに差し替え、レスポンスボディの Close 時に
// それをキャンセルする。生成コードの Execute() はボディを読んだ直後に Close する
// ため、この条件では Execute() から戻った時点で context がキャンセル済みになる。
// そのまま引き継ぐと最初の進捗確認が context canceled で即失敗する。
//
// context.WithoutCancel を使うのは、OpenTelemetry のトレースコンテキストなど
// context が持つ値を落とさないため。context.Background() に置き換えると
// スパンの親子関係が切れてしまう。
//
// context がまだ有効な場合はそのまま使うため、呼び出し側のキャンセルは
// 従来どおり伝播する。キャンセル済みの経路では伝播できないため、待ち時間に
// 上限を設けたい場合は SyncWaitContext を使うこと。
func syncWaitContext(resp *http.Response) context.Context {
	if resp == nil || resp.Request == nil {
		return context.Background()
	}

	ctx := resp.Request.Context()
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
	}
	return ctx
}
