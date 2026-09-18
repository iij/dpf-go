// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
)

const (
	testZoneID   = "zoneidexample0" // 14文字
	testSOAID    = "soarecordid001" // 14文字
	testZoneName = "example.com."
	testLockName = DefaultLockRecordLabel + "." + testZoneName
)

// testRecord は模擬サーバが保持するレコード。
type testRecord struct {
	id     string
	name   string
	rrtype string
	labels map[string]string
	state  int // 0=反映済み 1=追加予定 2=削除予定 3=更新予定
}

// lockServer はレコードの取得・追加・更新・削除・取消を模擬するテストサーバ。
//
// 追加は同名かつ同 RRTYPE の重複を拒否する。削除予定のレコードは重複判定に含めない
// （specs/006-zone-lock-redesign/research.md D1 の実測どおり）。
type lockServer struct {
	mu sync.Mutex

	records []*testRecord
	nextID  int

	getErr bool // 一覧取得を失敗させる

	// 呼び出しの記録
	posts   int
	patches int
	applies int // ゾーン反映の呼び出し回数。排他の操作では 0 でなければならない

	// patched は直近に PATCH されたラベル（未 PATCH なら nil）
	patched map[string]string

	// postHook は POST の成功直後に呼ばれる。競合の再現に使う
	postHook func(s *lockServer, created *testRecord)
	// cancelErr は DeleteRecordChanges を指定の error_details で失敗させる
	cancelErr [2]string
	// deleteErr は DeleteRecord を指定の error_details で失敗させる
	deleteErr [2]string
}

// newLockServer は SOA レコード 1 件だけを持つ模擬サーバを返す。
func newLockServer(labels map[string]string, state int) *lockServer {
	if labels == nil {
		labels = map[string]string{}
	}
	return &lockServer{
		records: []*testRecord{{
			id:     testSOAID,
			name:   testZoneName,
			rrtype: "SOA",
			labels: labels,
			state:  state,
		}},
	}
}

// addRecord は模擬サーバへレコードを直接追加する（前提条件の作り込み用）。
func (s *lockServer) addRecord(r *testRecord) *testRecord {
	if r.id == "" {
		s.nextID++
		r.id = fmt.Sprintf("lockrecord%04d", s.nextID)
	}
	if r.labels == nil {
		r.labels = map[string]string{}
	}
	s.records = append(s.records, r)
	return r
}

// find は ID でレコードを探す。
func (s *lockServer) find(id string) *testRecord {
	for _, r := range s.records {
		if r.id == id {
			return r
		}
	}
	return nil
}

// lockRecordsOf は専用レコードを返す。
func (s *lockServer) lockRecordsOf() []*testRecord {
	var out []*testRecord
	for _, r := range s.records {
		if r.name == testLockName {
			out = append(out, r)
		}
	}
	return out
}

func (s *lockServer) recordJSON(r *testRecord) map[string]any {
	labels := r.labels
	if labels == nil {
		labels = map[string]string{}
	}
	return map[string]any{
		"id":          r.id,
		"name":        r.name,
		"ttl":         3600,
		"rrtype":      r.rrtype,
		"rdata":       []any{},
		"labels":      labels,
		"state":       r.state,
		"description": "",
		"operator":    nil,
	}
}

func (s *lockServer) listJSON() string {
	results := make([]any, 0, len(s.records))
	for _, r := range s.records {
		results = append(results, s.recordJSON(r))
	}
	b, _ := json.Marshal(map[string]any{"request_id": "req00000000001", "results": results})
	return string(b)
}

func writeParameterError(w http.ResponseWriter, code, attribute string) {
	w.WriteHeader(http.StatusBadRequest)
	b, _ := json.Marshal(map[string]any{
		"request_id":    "req00000000001",
		"error_type":    "ParameterError",
		"error_message": "There are invalid parameters.",
		"error_details": []any{map[string]string{"code": code, "attribute": attribute}},
	})
	_, _ = w.Write(b)
}

func writeAccepted(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"request_id":"req00000000001","jobs_url":"http://x/jobs/req00000000001"}`))
}

// newLockClient は模擬サーバへ向けた API クライアントを返す。
func newLockClient(t *testing.T, s *lockServer) *dpf.APIClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		// パスは /zones/{zoneID}/... の形で届く。
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 2 || parts[0] != "zones" || parts[1] != testZoneID {
			http.NotFound(w, r)
			return
		}
		rest := parts[2:]

		switch {
		// ゾーン反映。排他の操作では呼ばれてはならない（FR-026・SC-004）。
		case len(rest) == 1 && (rest[0] == "changes" || rest[0] == "atomic_changes"):
			s.applies++
			t.Errorf("排他の操作でゾーン反映 (%s) が呼ばれた", rest[0])
			writeAccepted(w)

		case len(rest) == 1 && rest[0] == "records" && r.Method == http.MethodGet:
			if s.getErr {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"request_id":"x","error_type":"SystemError","error_message":"e"}`))
				return
			}
			_, _ = w.Write([]byte(s.listJSON()))

		case len(rest) == 1 && rest[0] == "records" && r.Method == http.MethodPost:
			s.posts++
			body, _ := io.ReadAll(r.Body)
			var pr struct {
				Name   string            `json:"name"`
				Rrtype string            `json:"rrtype"`
				Labels map[string]string `json:"labels"`
			}
			if err := json.Unmarshal(body, &pr); err != nil {
				t.Errorf("unmarshal post body: %v", err)
			}
			// 重複判定。削除予定は含めない。
			for _, r0 := range s.records {
				if r0.name == pr.Name && r0.rrtype == pr.Rrtype && r0.state != 2 {
					writeParameterError(w, "duplicated", "record")
					return
				}
			}
			created := s.addRecord(&testRecord{name: pr.Name, rrtype: pr.Rrtype, labels: pr.Labels, state: 1})
			if s.postHook != nil {
				s.postHook(s, created)
			}
			writeAccepted(w)

		case len(rest) == 2 && rest[0] == "records" && r.Method == http.MethodPatch:
			s.patches++
			rec := s.find(rest[1])
			if rec == nil {
				writeParameterError(w, "not_found", "record")
				return
			}
			body, _ := io.ReadAll(r.Body)
			var pr struct {
				Labels map[string]string `json:"labels"`
			}
			if err := json.Unmarshal(body, &pr); err != nil {
				t.Errorf("unmarshal patch body: %v", err)
			}
			s.patched = pr.Labels
			rec.labels = pr.Labels
			if rec.state == 0 {
				rec.state = 3
			}
			writeAccepted(w)

		case len(rest) == 2 && rest[0] == "records" && r.Method == http.MethodDelete:
			if s.deleteErr[0] != "" {
				writeParameterError(w, s.deleteErr[0], s.deleteErr[1])
				return
			}
			rec := s.find(rest[1])
			if rec == nil {
				writeParameterError(w, "not_found", "record")
				return
			}
			if rec.state != 0 {
				writeParameterError(w, "forbidden", "record")
				return
			}
			rec.state = 2
			writeAccepted(w)

		case len(rest) == 3 && rest[0] == "records" && rest[2] == "changes" && r.Method == http.MethodDelete:
			if s.cancelErr[0] != "" {
				writeParameterError(w, s.cancelErr[0], s.cancelErr[1])
				return
			}
			rec := s.find(rest[1])
			if rec == nil {
				writeParameterError(w, "not_found", "record")
				return
			}
			switch rec.state {
			case 1: // 追加予定は取り消しで消える
				kept := make([]*testRecord, 0, len(s.records))
				for _, r0 := range s.records {
					if r0.id != rec.id {
						kept = append(kept, r0)
					}
				}
				s.records = kept
			case 3: // 更新予定は反映済みへ戻る
				rec.state = 0
			default:
				writeParameterError(w, "forbidden", "record")
				return
			}
			writeAccepted(w)

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

// testMutex は now とポーリング間隔を固定したテスト用 Mutex を返す。
func testMutex(c *dpf.APIClient, owner string, now time.Time, opts ...Option) *Mutex {
	opts = append([]Option{WithOwner(owner), WithTTL(time.Hour), WithVerifyTimeout(20 * time.Millisecond)}, opts...)
	m := NewMutex(c.RecordsAPI, testZoneID, opts...)
	m.now = func() time.Time { return now }
	m.pollInterval = time.Millisecond
	return m
}

func fixedNow() time.Time { return time.Unix(1_700_000_000, 0) }

// ---- 土台 (T017) ----

func TestNewOwner_UniquePerInstance(t *testing.T) {
	s := newLockServer(nil, 0)
	c := newLockClient(t, s)

	a := NewMutex(c.RecordsAPI, testZoneID)
	b := NewMutex(c.RecordsAPI, testZoneID)

	if a.Owner() == b.Owner() {
		t.Errorf("owner がインスタンス間で同一である: %q", a.Owner())
	}
	for _, owner := range []string{a.Owner(), b.Owner()} {
		if owner == "" {
			t.Fatal("owner が空である")
		}
		if len(owner) > maxLabelValueLen {
			t.Errorf("owner が %d 文字ある（上限 %d）: %q", len(owner), maxLabelValueLen, owner)
		}
		if strings.Contains(owner, ".") {
			t.Errorf("owner に . が含まれる（nodename のみであること）: %q", owner)
		}
		if !strings.Contains(owner, strconv.Itoa(os.Getpid())) {
			t.Errorf("owner に PID が含まれない: %q", owner)
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		nodename := h
		if i := strings.IndexByte(nodename, '.'); i >= 0 {
			nodename = nodename[:i]
		}
		if nodename != "" && !strings.HasPrefix(a.Owner(), nodename[:1]) {
			t.Errorf("owner がホスト名から始まらない: %q (host %q)", a.Owner(), nodename)
		}
	}
}

func TestLockable_IgnoresOwner(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("alice", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if m.lockable(s.records[0].labels) {
		t.Error("owner が自分自身でも、奪ってよい時刻の前は取得できてはならない")
	}
}

func TestLockable_InvalidDeadline(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{
		LockOwnerLabelKey:    "bob",
		LockDeadlineLabelKey: "not-a-number",
	}, 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if !m.lockable(s.records[0].labels) {
		t.Error("奪ってよい時刻が解釈できない場合は取得を許さなければならない")
	}
}

func TestGetSOA_PrefersEditing(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{id: "soaeditingid01", name: testZoneName, rrtype: "SOA",
		labels: lockLabel("bob", now.Unix()+3600), state: 3})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	soa, err := m.getSOA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if soa.State != dpf.RECORDSSTATE__3 {
		t.Errorf("state=%v を返した。編集予定(3)を優先しなければならない", soa.State)
	}
	if soa.Labels[LockOwnerLabelKey] != "bob" {
		t.Errorf("編集中のラベルを読めていない: %v", soa.Labels)
	}
}

func TestGetSOA_Ambiguous(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{id: "soaduplicate01", name: testZoneName, rrtype: "SOA", state: 0})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if _, err := m.getSOA(context.Background()); err == nil {
		t.Error("同じ state の SOA が複数あるときはエラーを返さなければならない")
	}
}

func TestAcquirable_LabelLimit(t *testing.T) {
	now := fixedNow()
	labels := map[string]string{}
	for i := 0; i < 9; i++ {
		labels[fmt.Sprintf("k%d.example", i)] = "v"
	}
	s := newLockServer(labels, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrLabelLimit) {
		t.Fatalf("ErrLabelLimit を期待したが %v", err)
	}
	if s.posts != 0 {
		t.Errorf("ラベル上限では専用レコードを作ってはならない（POST %d 回）", s.posts)
	}
	if s.patches != 0 {
		t.Errorf("ラベル上限では書き込みを試みてはならない（PATCH %d 回）", s.patches)
	}
}

// ---- 利用シナリオ 1: 取得 (T018〜T023) ----

func TestMutexLock_Success(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{"keep.example": "v"}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soa := s.find(testSOAID)
	if got := soa.labels[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("owner: got %q, want alice", got)
	}
	want := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	if got := soa.labels[LockDeadlineLabelKey]; got != want {
		t.Errorf("deadline: got %q, want %q", got, want)
	}
	if soa.state != 3 {
		t.Errorf("SOA が編集予定になっていない: state=%d", soa.state)
	}
	// T023: 既存のラベルが保持される
	if got := soa.labels["keep.example"]; got != "v" {
		t.Errorf("既存のラベルが失われた: %v", soa.labels)
	}
	// 専用レコードは残らない
	if recs := s.lockRecordsOf(); len(recs) != 0 {
		t.Errorf("専用レコードが %d 件残っている", len(recs))
	}
	if s.applies != 0 {
		t.Errorf("ゾーン反映が %d 回呼ばれた", s.applies)
	}
}

func TestMutexLock_NoReentry(t *testing.T) {
	now := fixedNow()
	s := newLockServer(nil, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("1 回目の取得に失敗した: %v", err)
	}
	posts := s.posts

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("再入は ErrStillLock を期待したが %v", err)
	}
	if s.posts != posts {
		t.Errorf("再入の拒否で専用レコードを作ってはならない（POST が %d → %d）", posts, s.posts)
	}
}

func TestMutexLock_PrecheckSkipsLockRecord(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if s.posts != 0 {
		t.Errorf("事前判定で終える場合は専用レコードを作ってはならない（POST %d 回）", s.posts)
	}
	if s.patches != 0 {
		t.Errorf("書き込みを試みてはならない（PATCH %d 回）", s.patches)
	}
}

func TestMutexLock_PrecheckAndGuardedDisagree(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	// 専用レコードが作られた直後に、他者が排他を取得した状況を作る。
	s.postHook = func(s *lockServer, _ *testRecord) {
		s.find(testSOAID).labels = lockLabel("bob", now.Unix()+3600)
		s.find(testSOAID).state = 3
	}

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if s.patches != 0 {
		t.Errorf("本判定で取得できない場合は書き込んではならない（PATCH %d 回）", s.patches)
	}
	if got := s.find(testSOAID).labels[LockOwnerLabelKey]; got != "bob" {
		t.Errorf("他者の排他を上書きした: owner=%q", got)
	}
	if recs := s.lockRecordsOf(); len(recs) != 0 {
		t.Errorf("一時ロックが解放されていない（専用レコードが %d 件）", len(recs))
	}
}

func TestMutexLock_LockRecordHeldByOther(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{name: testLockName, rrtype: "TXT",
		labels: lockLabel("bob", now.Unix()+60), state: 1})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if s.patches != 0 {
		t.Errorf("一時ロックを取れない場合は書き込んではならない（PATCH %d 回）", s.patches)
	}
}

func TestMutexLock_LockRecordRaceLowestIDWins(t *testing.T) {
	now := fixedNow()

	// 敗者: 自分の追加予定より小さい ID の追加予定が同時に現れる。
	s := newLockServer(map[string]string{}, 0)
	s.postHook = func(s *lockServer, created *testRecord) {
		created.id = "zzzzzzzzzzzzzz"
		s.addRecord(&testRecord{id: "aaaaaaaaaaaaaa", name: created.name, rrtype: created.rrtype,
			labels: lockLabel("bob", now.Unix()+60), state: 1})
		s.postHook = nil
	}
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("敗者は ErrStillLock を期待したが %v", err)
	}
	for _, r := range s.lockRecordsOf() {
		if r.labels[LockOwnerLabelKey] == "alice" {
			t.Error("敗者は自分の追加予定を取り消さなければならない")
		}
	}

	// 勝者: 自分の追加予定より大きい ID の追加予定が同時に現れる。
	s2 := newLockServer(map[string]string{}, 0)
	s2.postHook = func(s *lockServer, created *testRecord) {
		created.id = "aaaaaaaaaaaaaa"
		s.addRecord(&testRecord{id: "zzzzzzzzzzzzzz", name: created.name, rrtype: created.rrtype,
			labels: lockLabel("bob", now.Unix()+60), state: 1})
		s.postHook = nil
	}
	c2 := newLockClient(t, s2)
	m2 := testMutex(c2, "alice", now)

	if err := m2.Lock(context.Background()); err != nil {
		t.Fatalf("勝者は取得に成功しなければならない: %v", err)
	}
	if got := s2.find(testSOAID).labels[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("勝者の排他が書かれていない: owner=%q", got)
	}
}

func TestMutexLock_StealsExpiredLock(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()-1), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("失効した排他は奪えなければならない: %v", err)
	}
	if got := s.find(testSOAID).labels[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("owner: got %q, want alice", got)
	}
}

// ---- 利用シナリオ 2: 回復 (T029〜T032) ----

func TestMutexLock_ExpiredLockRecordIsCancelled(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	stale := s.addRecord(&testRecord{name: testLockName, rrtype: "TXT",
		labels: lockLabel("bob", now.Unix()-1), state: 1})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("失効した一時ロックは取り消して取得できなければならない: %v", err)
	}
	if s.find(stale.id) != nil {
		t.Error("失効した専用レコードが取り消されていない")
	}
	if recs := s.lockRecordsOf(); len(recs) != 0 {
		t.Errorf("専用レコードが %d 件残っている", len(recs))
	}
	if s.applies != 0 {
		t.Errorf("ゾーン反映が %d 回呼ばれた", s.applies)
	}
}

func TestMutexLock_InvalidLockRecordDeadline(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{name: testLockName, rrtype: "TXT", state: 1,
		labels: map[string]string{LockOwnerLabelKey: "bob", LockDeadlineLabelKey: "broken"}})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("失効時刻が解釈できない専用レコードは取り消して取得できなければならない: %v", err)
	}
}

func TestMutexLock_RecoversFromPublishedLockRecord(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	published := s.addRecord(&testRecord{name: testLockName, rrtype: "TXT",
		labels: lockLabel("bob", now.Unix()-1), state: 0})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("公開済みの専用レコードからは回復しなければならない: %v", err)
	}
	if got := s.find(testSOAID).labels[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("排他が書かれていない: owner=%q", got)
	}

	// 公開済みのレコードは削除予定として残り、取り消されない。
	rec := s.find(published.id)
	if rec == nil {
		t.Fatal("公開済みのレコードが消えている。削除予定として残さなければならない")
	}
	if rec.state != 2 {
		t.Errorf("公開済みのレコードが削除予定になっていない: state=%d", rec.state)
	}
	// 自分が作った追加予定は解放済みである。
	for _, r := range s.lockRecordsOf() {
		if r.state == 1 {
			t.Error("追加予定の専用レコードが残っている")
		}
	}
	if s.applies != 0 {
		t.Errorf("回復でゾーン反映を行ってはならない（%d 回）", s.applies)
	}
}

func TestMutexLock_RecoveryToleratesAlreadyGone(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{name: testLockName, rrtype: "TXT",
		labels: lockLabel("bob", now.Unix()-1), state: 1})
	// 他者が先に取り消していた状況。取り消しは not_found で拒否される。
	s.cancelErr = [2]string{"not_found", "record"}
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	err := m.Lock(context.Background())
	// 取り消しの拒否は失敗として扱わず追加へ進む。追加は既存の追加予定があるため
	// 重複で拒否され、他者が先に取得したものとして ErrStillLock になる。
	if !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if s.posts < 2 {
		t.Errorf("取り消しの拒否のあと追加をやり直していない（POST %d 回）", s.posts)
	}
}

func TestMutexLock_RetriesOnlyOnce(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{name: testLockName, rrtype: "TXT",
		labels: lockLabel("bob", now.Unix()-1), state: 1})
	// 取り消しが拒否され、既存の追加予定が残り続ける状況。
	s.cancelErr = [2]string{"not_found", "record"}
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if s.posts > 2 {
		t.Errorf("やり直しは 1 回までであること（POST %d 回）", s.posts)
	}
}

// ---- 利用シナリオ 3: 延長と解放 (T036〜T038) ----

func TestMutexRenew(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("alice", now.Unix()+60), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Renew(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	if got := s.find(testSOAID).labels[LockDeadlineLabelKey]; got != want {
		t.Errorf("deadline: got %q, want %q", got, want)
	}
	if s.posts != 0 {
		t.Errorf("延長で一時ロックを取ってはならない（POST %d 回）", s.posts)
	}
	if s.applies != 0 {
		t.Errorf("ゾーン反映が %d 回呼ばれた", s.applies)
	}
}

func TestMutexRenew_NotHolder(t *testing.T) {
	now := fixedNow()

	// 他者が保持している。
	s := newLockServer(lockLabel("bob", now.Unix()+60), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)
	if err := m.Renew(context.Background()); !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("ErrNotLockHolder を期待したが %v", err)
	}
	if s.patches != 0 {
		t.Errorf("保持者でない場合は書き込んではならない（PATCH %d 回）", s.patches)
	}

	// 排他が存在しない。
	s2 := newLockServer(map[string]string{}, 0)
	c2 := newLockClient(t, s2)
	m2 := testMutex(c2, "alice", now)
	if err := m2.Renew(context.Background()); !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("ErrNotLockHolder を期待したが %v", err)
	}
}

func TestMutexRenew_Stolen(t *testing.T) {
	now := fixedNow()
	// 保持中に他者へ奪われた状態。
	s := newLockServer(lockLabel("bob", now.Unix()+60), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Renew(context.Background()); !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("奪われていた場合は ErrNotLockHolder を期待したが %v", err)
	}
	if s.patches != 0 {
		t.Errorf("奪われていた場合は書き込んではならない（PATCH %d 回）", s.patches)
	}
}

func TestMutexUnlock(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("alice", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strconv.FormatInt(now.Unix(), 10)
	if got := s.patched[LockDeadlineLabelKey]; got != want {
		t.Errorf("deadline: got %q, want %q (now)", got, want)
	}
	if got := s.patched[LockOwnerLabelKey]; got != "alice" {
		t.Errorf("owner ラベルは維持されること: got %q", got)
	}
	if s.applies != 0 {
		t.Errorf("ゾーン反映が %d 回呼ばれた", s.applies)
	}
}

func TestMutexUnlock_NoLock(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); err != nil {
		t.Fatalf("排他が無い場合は何もせず成功すること: %v", err)
	}
	if s.patched != nil {
		t.Errorf("PATCH してはならない: %v", s.patched)
	}
}

func TestMutexUnlock_NotHolder(t *testing.T) {
	now := fixedNow()
	before := lockLabel("bob", now.Unix()+3600)
	s := newLockServer(before, 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("ErrNotLockHolder を期待したが %v", err)
	}
	if s.patched != nil {
		t.Errorf("他者の排他を変更してはならない: %v", s.patched)
	}
	if got := s.find(testSOAID).labels[LockDeadlineLabelKey]; got != before[LockDeadlineLabelKey] {
		t.Errorf("他者の deadline が変わっている: %q", got)
	}
}

// ---- LockWait ----

func TestMutexLockWait_Success(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.LockWait(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMutexLockWait_NonStillLockError(t *testing.T) {
	now := fixedNow()
	s := newLockServer(nil, 0)
	s.getErr = true
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	err := m.LockWait(context.Background(), time.Millisecond)
	if err == nil || errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock 以外のエラーを期待したが %v", err)
	}
}

func TestMutexLockWait_CtxCancel(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := m.LockWait(ctx, 10*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context.DeadlineExceeded を期待したが %v", err)
	}
}

func TestMutexLockWait_OneCallPerAttempt(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_ = m.LockWait(ctx, 10*time.Millisecond)

	if s.posts != 0 {
		t.Errorf("待機中に専用レコードを作ってはならない（POST %d 回）", s.posts)
	}
	if s.patches != 0 {
		t.Errorf("待機中に書き込んではならない（PATCH %d 回）", s.patches)
	}
}

// ---- 既定値 ----

func TestMutexDefaults(t *testing.T) {
	s := newLockServer(nil, 0)
	c := newLockClient(t, s)
	m := NewMutex(c.RecordsAPI, testZoneID)

	if m.ttl != DefaultLockTTL {
		t.Errorf("ttl: got %v, want %v", m.ttl, DefaultLockTTL)
	}
	if m.lockRecordTTL != DefaultLockRecordTTL {
		t.Errorf("lockRecordTTL: got %v, want %v", m.lockRecordTTL, DefaultLockRecordTTL)
	}
	if m.verifyTimeout != DefaultVerifyTimeout {
		t.Errorf("verifyTimeout: got %v, want %v", m.verifyTimeout, DefaultVerifyTimeout)
	}
	if m.lockRecordLabel != DefaultLockRecordLabel {
		t.Errorf("lockRecordLabel: got %q, want %q", m.lockRecordLabel, DefaultLockRecordLabel)
	}
	if m.lockRecordRrtype != defaultLockRecordRrtype {
		t.Errorf("lockRecordRrtype: got %v, want %v", m.lockRecordRrtype, defaultLockRecordRrtype)
	}

	// 0 値・空値は無視して既定のままとする。
	m2 := NewMutex(c.RecordsAPI, testZoneID,
		WithOwner(""), WithTTL(0), WithLockRecordTTL(-1),
		WithVerifyTimeout(0), WithLockRecordLabel(""),
		WithLockRecordContent(dpf.RECORDSRRTYPEWITHOUTSOA_A, nil))
	if m2.owner == "" || m2.ttl != DefaultLockTTL || m2.lockRecordTTL != DefaultLockRecordTTL ||
		m2.verifyTimeout != DefaultVerifyTimeout || m2.lockRecordLabel != DefaultLockRecordLabel ||
		m2.lockRecordRrtype != defaultLockRecordRrtype {
		t.Error("0 値・空値の Option で既定値が壊れた")
	}
}

func TestMutexLock_CustomLockRecord(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now,
		WithLockRecordLabel("_mylock"),
		WithLockRecordContent(dpf.RECORDSRRTYPEWITHOUTSOA_TXT, []string{`"mine"`}))

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 専用レコードの名前はゾーン名と結合される。
	if got := m.lockRecordName(&dpf.Record{Name: testZoneName}); got != "_mylock."+testZoneName {
		t.Errorf("専用レコード名: got %q", got)
	}
}
