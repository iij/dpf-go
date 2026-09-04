// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient は指定 URL を基底とする APIClient を返す。
func newTestClient(baseURL string) *APIClient {
	cfg := NewConfiguration()
	cfg.Servers = ServerConfigurations{{URL: baseURL}}
	return NewAPIClient(cfg)
}

// TestSyncWait_Success は、JOB が RUNNING を経て SUCCESSFUL になるケースを検証する。
func TestSyncWait_Success(t *testing.T) {
	SyncWaitPollInterval = 1 * time.Millisecond

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if n < 2 {
			_, _ = w.Write([]byte(`{"request_id":"req10000000000000000000000000001","status":"RUNNING"}`))
			return
		}
		_, _ = w.Write([]byte(`{"request_id":"req10000000000000000000000000001","status":"SUCCESSFUL","resources_url":"https://example/resources"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	async := NewAsyncResponse("req10000000000000000000000000001", srv.URL+"/jobs/req10000000000000000000000000001")

	job, _, err := c.JobsAPI.SyncWait(async, &http.Response{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job == nil || job.GetStatus() != "SUCCESSFUL" {
		t.Fatalf("expected SUCCESSFUL job, got %+v", job)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected polling (>=2 calls), got %d", calls)
	}
}

// TestSyncWait_Failed は、JOB が FAILED で終了した場合に error を返すことを検証する。
func TestSyncWait_Failed(t *testing.T) {
	SyncWaitPollInterval = 1 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"req20000000000000000000000000002","status":"FAILED","error_type":"ParameterError","error_message":"bad request"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	async := NewAsyncResponse("req20000000000000000000000000002", srv.URL+"/jobs/req20000000000000000000000000002")

	job, _, err := c.JobsAPI.SyncWait(async, &http.Response{}, nil)
	if err == nil {
		t.Fatal("expected error for FAILED job, got nil")
	}
	if job == nil || job.GetStatus() != "FAILED" {
		t.Fatalf("expected FAILED job returned, got %+v", job)
	}
}

// TestSyncWait_PassthroughError は、Execute() のエラーをそのまま返すことを検証する。
func TestSyncWait_PassthroughError(t *testing.T) {
	c := newTestClient("http://example.invalid")
	wantErr := errors.New("execute failed")

	job, _, err := c.JobsAPI.SyncWait(nil, nil, wantErr)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected passthrough error %v, got %v", wantErr, err)
	}
	if job != nil {
		t.Fatalf("expected nil job, got %+v", job)
	}
}

// TestSyncWait_NilAsync は、err が nil かつ async が nil の場合にエラーを返すことを検証する。
func TestSyncWait_NilAsync(t *testing.T) {
	c := newTestClient("http://example.invalid")

	_, _, err := c.JobsAPI.SyncWait(nil, &http.Response{}, nil)
	if err == nil {
		t.Fatal("expected error for nil async response, got nil")
	}
}

// TestSyncWait_ContextCancel は、待機中に context がキャンセルされた場合に終了することを検証する。
func TestSyncWait_ContextCancel(t *testing.T) {
	SyncWaitPollInterval = 50 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"req30000000000000000000000000003","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	async := NewAsyncResponse("req30000000000000000000000000003", srv.URL+"/jobs/req30000000000000000000000000003")

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
	resp := &http.Response{Request: req}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, _, err := c.JobsAPI.SyncWait(async, resp, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// ctxKey は context の値伝播を確認するテスト用のキー型。
type ctxKey string

// recordingTransport は net/http が「未知の Transport」と判定する RoundTripper。
// utils.Client の limitTransport と同じ条件を作り出し、あわせて各リクエストの
// context が保持している値を記録する。
type recordingTransport struct {
	base http.RoundTripper
	key  ctxKey

	mu   sync.Mutex
	seen []any
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.seen = append(t.seen, req.Context().Value(t.key))
	t.mu.Unlock()

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// lastSeen は最後に観測した context の値を返す。
func (t *recordingTransport) lastSeen() any {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.seen) == 0 {
		return nil
	}
	return t.seen[len(t.seen)-1]
}

// asyncJobServer は非同期 API と JOB 参照の両方に応答するテストサーバを返す。
// status には JOB の状態("RUNNING" 等)を指定する。
func asyncJobServer(t *testing.T, status string) *httptest.Server {
	t.Helper()
	const requestID = "req40000000000000000000000000004"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/jobs/") {
			_, _ = w.Write([]byte(`{"request_id":"` + requestID + `","status":"` + status + `"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"request_id":"` + requestID + `","jobs_url":"/jobs/` + requestID + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTimeoutClient は Timeout 付き http.Client とカスタム Transport を持つ
// APIClient を返す。utils.NewClient が構築するものと同じ条件。
func newTimeoutClient(baseURL string, rt http.RoundTripper) *APIClient {
	cfg := NewConfiguration()
	cfg.Servers = ServerConfigurations{{URL: baseURL}}
	cfg.HTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: rt,
	}
	return NewAPIClient(cfg)
}

// TestSyncWait_ThroughRealHTTPClient は、Timeout 付きの http.Client と
// カスタム Transport（utils.NewClient と同じ条件）で実際に API を実行し、
// その戻り値をそのまま SyncWait に渡しても正常に待機できることを検証する。
//
// net/http は「未知の Transport」かつ Client.Timeout が非ゼロの場合、
// リクエストの context を差し替え、レスポンスボディの Close 時にそれを
// キャンセルする。SyncWait がそのキャンセル済み context をそのまま
// 引き継ぐと、最初の GetJob が context canceled で即失敗する。
func TestSyncWait_ThroughRealHTTPClient(t *testing.T) {
	SyncWaitPollInterval = 1 * time.Millisecond

	srv := asyncJobServer(t, "SUCCESSFUL")
	rt := &recordingTransport{}
	c := newTimeoutClient(srv.URL, rt)

	// 非同期 API を実際に実行し、その 3 つの戻り値をそのまま SyncWait に渡す。
	job, _, err := c.JobsAPI.SyncWait(
		c.ZonesAPI.DeleteZoneChanges(context.Background(), "zone1234567890").Execute(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job == nil || job.GetStatus() != "SUCCESSFUL" {
		t.Fatalf("expected SUCCESSFUL job, got %+v", job)
	}
}

// TestSyncWait_PreservesContextValues は、リクエストの context が
// キャンセル済みでも、その値（OpenTelemetry のトレースコンテキスト等）が
// ポーリング側に引き継がれることを検証する。
func TestSyncWait_PreservesContextValues(t *testing.T) {
	SyncWaitPollInterval = 1 * time.Millisecond

	const key ctxKey = "trace-like-value"
	srv := asyncJobServer(t, "SUCCESSFUL")
	rt := &recordingTransport{key: key}
	c := newTimeoutClient(srv.URL, rt)

	ctx := context.WithValue(context.Background(), key, "carried")

	if _, _, err := c.JobsAPI.SyncWait(
		c.ZonesAPI.DeleteZoneChanges(ctx, "zone1234567890").Execute(),
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 最後のリクエストは SyncWait のポーリング（GET /jobs/...）。
	if got := rt.lastSeen(); got != "carried" {
		t.Fatalf("context value not propagated to polling request: got %v, want %q", got, "carried")
	}
}

// TestSyncWaitContext_Deadline は、SyncWaitContext に渡した context の
// 期限でポーリングを打ち切ることを検証する。JOB が RUNNING のままでも
// 無限に待ち続けないことの確認。
func TestSyncWaitContext_Deadline(t *testing.T) {
	SyncWaitPollInterval = 1 * time.Millisecond

	srv := asyncJobServer(t, "RUNNING")
	c := newTimeoutClient(srv.URL, &recordingTransport{})

	async, resp, err := c.ZonesAPI.DeleteZoneChanges(context.Background(), "zone1234567890").Execute()
	if err != nil {
		t.Fatalf("unexpected error from Execute: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, _, err := c.JobsAPI.SyncWaitContext(ctx, async, resp, err); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}
