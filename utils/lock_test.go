// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
)

const (
	testZoneID = "zoneidexample0" // 14文字
	testSOAID  = "soarecordid001" // 14文字
)

// lockServer は SOA レコードのラベル取得と PATCH を模擬するテストサーバ。
type lockServer struct {
	mu      sync.Mutex
	labels  map[string]string
	state   int
	getErr  bool
	patched map[string]string // 直近 PATCH されたラベル（未 PATCH なら nil）
}

func (s *lockServer) recordsJSON() string {
	labels := s.labels
	if labels == nil {
		labels = map[string]string{}
	}
	rec := map[string]any{
		"id":          testSOAID,
		"name":        "example.com.",
		"ttl":         3600,
		"rrtype":      "SOA",
		"rdata":       []any{},
		"labels":      labels,
		"state":       s.state,
		"description": "",
		"operator":    nil,
	}
	resp := map[string]any{"request_id": "r", "results": []any{rec}}
	b, _ := json.Marshal(resp)
	return string(b)
}

func newLockClient(t *testing.T, s *lockServer) *dpf.APIClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			var pr struct {
				Labels map[string]string `json:"labels"`
			}
			if err := json.Unmarshal(body, &pr); err != nil {
				t.Errorf("unmarshal patch body: %v", err)
			}
			s.patched = pr.Labels
			s.labels = pr.Labels
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"request_id":"req00000000001","jobs_url":"http://x/jobs/req00000000001"}`))
		case strings.HasSuffix(r.URL.Path, "/records"):
			if s.getErr {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"request_id":"x","error_type":"SystemError","error_message":"e"}`))
				return
			}
			_, _ = w.Write([]byte(s.recordsJSON()))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := dpf.NewConfiguration()
	cfg.Servers = dpf.ServerConfigurations{{URL: srv.URL}}
	return dpf.NewAPIClient(cfg)
}

func lockLabel(owner string, deadline int64) map[string]string {
	return map[string]string{
		LockOwnerLabelKey:    owner,
		LockDeadlineLabelKey: strconv.FormatInt(deadline, 10),
	}
}

// fixedTimeMutex は now を固定したテスト用 Mutex を返す。
func fixedTimeMutex(c *dpf.APIClient, owner string, now time.Time) *Mutex {
	m := NewMutex(c.RecordsAPI, testZoneID, WithOwner(owner), WithTTL(time.Hour))
	m.now = func() time.Time { return now }
	return m
}

func TestMutexLock_NoExistingLock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: map[string]string{}}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := s.patched[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("owner: got %q, want alice", got)
	}
	want := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	if got := s.patched[LockDeadlineLabelKey]; got != want {
		t.Errorf("deadline: got %q, want %q", got, want)
	}
}

func TestMutexLock_HeldByOtherNotExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: lockLabel("bob", now.Unix()+3600)}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("expected ErrStillLock, got %v", err)
	}
	if s.patched != nil {
		t.Errorf("must not PATCH when still locked: %v", s.patched)
	}
}

func TestMutexLock_HeldByOtherExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: lockLabel("bob", now.Unix()-1)}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("expected steal of expired lock, got %v", err)
	}
	if got := s.patched[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("owner: got %q, want alice", got)
	}
}

func TestMutexLock_HeldBySelf(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: lockLabel("alice", now.Unix()+3600)}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("self re-lock should succeed, got %v", err)
	}
}

func TestMutexUnlock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: lockLabel("alice", now.Unix()+3600)}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strconv.FormatInt(now.Unix(), 10)
	if got := s.patched[LockDeadlineLabelKey]; got != want {
		t.Errorf("deadline: got %q, want %q (now)", got, want)
	}
	// owner ラベルは維持される。
	if got := s.patched[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("owner should be preserved: got %q, want alice", got)
	}
}

func TestMutexUnlock_NoLock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: map[string]string{}}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.patched != nil {
		t.Errorf("must not PATCH when no lock exists: %v", s.patched)
	}
}

func TestMutexLock_Defaults(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: map[string]string{}}
	c := newLockClient(t, s)

	// Option 未指定 → デフォルト(ホスト名 nodename / 15分)が使われる。
	m := NewMutex(c.RecordsAPI, testZoneID)
	m.now = func() time.Time { return now }

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := s.patched[LockOwnerLabelKey], defaultOwner(); got != want {
		t.Errorf("owner: got %q, want %q (hostname nodename)", got, want)
	}
	want := strconv.FormatInt(now.Add(DefaultLockTTL).Unix(), 10)
	if got := s.patched[LockDeadlineLabelKey]; got != want {
		t.Errorf("deadline: got %q, want %q", got, want)
	}
}

func TestDefaultOwner(t *testing.T) {
	got := defaultOwner()
	if got == "" {
		t.Fatal("defaultOwner returned empty")
	}
	if strings.Contains(got, ".") {
		t.Errorf("defaultOwner must be nodename only (no dot): %q", got)
	}
}

func TestMutexLockWait_Success(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: map[string]string{}}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	if err := m.LockWait(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMutexLockWait_NonStillLockError(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{getErr: true}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	err := m.LockWait(context.Background(), time.Millisecond)
	if err == nil || errors.Is(err, ErrStillLock) {
		t.Fatalf("expected non-ErrStillLock error, got %v", err)
	}
}

func TestMutexLockWait_CtxCancel(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := &lockServer{labels: lockLabel("bob", now.Unix()+3600)}
	c := newLockClient(t, s)
	m := fixedTimeMutex(c, "alice", now)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := m.LockWait(ctx, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}
