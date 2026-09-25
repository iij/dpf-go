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

	// applied は反映済みの内容の控え。編集予定(state=3)になった時点で、
	// その直前のラベルを保存する。公開されているレコードの一覧
	// （GET /records/currents）は、編集予定のレコードについてこの控えを
	// 「更新前の状態」(state=5) として返す。
	applied map[string]string
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
	atomics int // 一括置き換えの呼び出し回数
	renews  int // 延長のための PATCH の回数（ラベルの deadline のみが変わった PATCH）

	// allowApply が false の場合、ゾーン反映と一括置き換えの呼び出しはテストの
	// 失敗として扱う。排他の操作（Lock / Renew / Unlock）はゾーン反映を伴っては
	// ならないためである。一括置き換えを検証するテストだけが true にする。
	allowApply bool

	// atomicBody は直近の一括置き換えのリクエスト。フラグの確認に使う。
	atomicBody *atomicRequest

	// patched は直近に PATCH されたラベル（未 PATCH なら nil）
	patched map[string]string

	// postHook は POST の成功直後に呼ばれる。競合の再現に使う
	postHook func(s *lockServer, created *testRecord)
	// cancelErr は DeleteRecordChanges を指定の error_details で失敗させる
	cancelErr [2]string
	// deleteErr は DeleteRecord を指定の error_details で失敗させる
	deleteErr [2]string
	// currentsErr は公開されているレコードの取得を失敗させる
	currentsErr bool
	// atomicErr は一括置き換えを指定の error_details で失敗させる
	atomicErr [2]string
	// jobFailed は JOB の状態を FAILED で返させる
	jobFailed bool

	// patchHook は PATCH の直前に呼ばれる。延長中の横取りの再現に使う。
	patchHook func(s *lockServer, recordID string, labels map[string]string)

	// patchFailN が正の場合、その回数だけ PATCH を 500 で失敗させる。
	// 延長の一時的な失敗の再現に使う。
	patchFailN int
}

// atomicRequest は一括置き換えのリクエスト。
type atomicRequest struct {
	Records []struct {
		Name        string            `json:"name"`
		Ttl         *int32            `json:"ttl"`
		Rrtype      string            `json:"rrtype"`
		Rdata       []any             `json:"rdata"`
		Description string            `json:"description"`
		Labels      map[string]string `json:"labels"`
	} `json:"records"`
	Description         *string `json:"description"`
	OverwriteSoa        *bool   `json:"overwrite_soa"`
	OverwriteZoneApexNs *bool   `json:"overwrite_zone_apex_ns"`
}

// isApexNS は Zone Apex の NS レコードかを返す。
func (s *lockServer) isApexNS(name, rrtype string) bool {
	return rrtype == "NS" && name == testZoneName
}

// currentsJSON は公開されているレコードの一覧を返す。
//
// 追加予定(state=1)は含めない。編集予定(state=3)は「更新前の状態」(state=5) として
// 反映済みの内容の控えを返す。実 API の挙動に合わせている
// （specs/007-zone-atomic-apply/research.md D1）。
func (s *lockServer) currentsJSON() string {
	results := make([]any, 0, len(s.records))
	for _, r := range s.records {
		switch r.state {
		case 1:
			continue
		case 3:
			snapshot := &testRecord{
				id: r.id, name: r.name, rrtype: r.rrtype,
				labels: r.applied, state: 5,
			}
			results = append(results, s.recordJSON(snapshot))
		default:
			results = append(results, s.recordJSON(r))
		}
	}
	b, _ := json.Marshal(map[string]any{"request_id": "req00000000001", "results": results})
	return string(b)
}

// applyAtomic は一括置き換えを適用する。
//
// 実 API の挙動を再現する。すなわち、(1) 未反映の編集を破棄し、(2) リクエストの
// レコードでゾーンを置き換える。ただし取り込まないフラグが立っている SOA と
// Zone Apex の NS は、既存の反映済みのものを維持する
// （specs/007-zone-atomic-apply/research.md D1・D1b）。
func (s *lockServer) applyAtomic(req *atomicRequest) {
	overwriteSoa := req.OverwriteSoa == nil || *req.OverwriteSoa
	overwriteNS := req.OverwriteZoneApexNs == nil || *req.OverwriteZoneApexNs

	// (1) 未反映の編集を破棄する。編集予定は反映済みの控えへ戻し、追加予定は消える。
	kept := make([]*testRecord, 0, len(s.records))
	for _, r := range s.records {
		switch r.state {
		case 1:
			continue
		case 3:
			r.labels = r.applied
			r.state = 0
		case 2:
			r.state = 0
		}
		kept = append(kept, r)
	}

	// (2) リクエストのレコードで置き換える。取り込まないフラグの対象は除く。
	next := make([]*testRecord, 0, len(req.Records))
	for _, rr := range req.Records {
		if !overwriteSoa && rr.Rrtype == "SOA" {
			continue
		}
		if !overwriteNS && s.isApexNS(rr.Name, rr.Rrtype) {
			continue
		}
		rec := &testRecord{name: rr.Name, rrtype: rr.Rrtype, labels: rr.Labels, state: 0}
		for _, old := range kept {
			if old.name == rr.Name && old.rrtype == rr.Rrtype {
				rec.id = old.id
				break
			}
		}
		if rec.id == "" {
			s.nextID++
			rec.id = fmt.Sprintf("atomicrec%04d", s.nextID)
		}
		next = append(next, rec)
	}

	// 取り込まなかった SOA と Zone Apex の NS は、既存の反映済みのものを維持する。
	for _, old := range kept {
		if (!overwriteSoa && old.rrtype == "SOA") || (!overwriteNS && s.isApexNS(old.name, old.rrtype)) {
			next = append(next, old)
		}
	}
	s.records = next
}

// newLockServer は SOA レコード 1 件だけを持つ模擬サーバを返す。
func newLockServer(labels map[string]string, state int) *lockServer {
	if labels == nil {
		labels = map[string]string{}
	}
	return &lockServer{
		records: []*testRecord{{
			id:      testSOAID,
			name:    testZoneName,
			rrtype:  "SOA",
			labels:  labels,
			applied: labels,
			state:   state,
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
	if r.applied == nil {
		r.applied = r.labels
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

// testRequestID は非同期レスポンスの request_id。生成コードは 32 文字以上を要求する。
const testRequestID = "req00000000000000000000000000001"

func writeAccepted(w http.ResponseWriter) {
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"request_id":"` + testRequestID +
		`","jobs_url":"http://x/jobs/` + testRequestID + `"}`))
}

// newLockClient は模擬サーバへ向けた API クライアントを返す。
func newLockClient(t *testing.T, s *lockServer) *dpf.APIClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

		// JOB の状態取得。SyncWaitContext は jobs_url ではなく
		// GET /jobs/{request_id} を叩く。
		if len(parts) == 2 && parts[0] == "jobs" && r.Method == http.MethodGet {
			status := "SUCCESSFUL"
			body := map[string]any{"request_id": parts[1], "status": status,
				"resources_url": "http://x/resources"}
			if s.jobFailed {
				body = map[string]any{"request_id": parts[1], "status": "FAILED",
					"error_type": "SystemError", "error_message": "job failed"}
			}
			b, _ := json.Marshal(body)
			_, _ = w.Write(b)
			return
		}

		// 残りは /zones/{zoneID}/... の形で届く。
		if len(parts) < 2 || parts[0] != "zones" || parts[1] != testZoneID {
			http.NotFound(w, r)
			return
		}
		rest := parts[2:]

		switch {
		// ゾーン反映。排他の操作では呼ばれてはならない（006 FR-026・SC-004）。
		case len(rest) == 1 && rest[0] == "changes":
			s.applies++
			if !s.allowApply {
				t.Errorf("排他の操作でゾーン反映 (changes) が呼ばれた")
			}
			writeAccepted(w)

		// 一括置き換えと反映。
		case len(rest) == 1 && rest[0] == "atomic_changes" && r.Method == http.MethodPatch:
			s.atomics++
			if !s.allowApply {
				t.Errorf("排他の操作で一括置き換え (atomic_changes) が呼ばれた")
			}
			if s.atomicErr[0] != "" {
				writeParameterError(w, s.atomicErr[0], s.atomicErr[1])
				return
			}
			body, _ := io.ReadAll(r.Body)
			var req atomicRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal atomic_changes body: %v", err)
			}
			s.atomicBody = &req
			if !s.jobFailed {
				// JOB が失敗する場合は適用しない。一括置き換えはアトミックであり、
				// 失敗した反映が中途半端に適用されることはない。
				s.applyAtomic(&req)
			}
			writeAccepted(w)

		// 公開されているレコードの一覧。
		case len(rest) == 2 && rest[0] == "records" && rest[1] == "currents" && r.Method == http.MethodGet:
			if s.currentsErr {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"request_id":"x","error_type":"SystemError","error_message":"e"}`))
				return
			}
			_, _ = w.Write([]byte(s.currentsJSON()))

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
			if s.patchFailN > 0 {
				s.patchFailN--
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"request_id":"x","error_type":"SystemError","error_message":"e"}`))
				return
			}
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
			if s.patchHook != nil {
				s.patchHook(s, rec.id, pr.Labels)
			}
			// owner が変わらず deadline だけが変わった PATCH を延長として数える。
			if rec.labels[LockOwnerLabelKey] == pr.Labels[LockOwnerLabelKey] &&
				rec.labels[LockDeadlineLabelKey] != pr.Labels[LockDeadlineLabelKey] {
				s.renews++
			}
			s.patched = pr.Labels
			if rec.state == 0 {
				// 反映済みから編集予定へ移る時点の内容を控える。
				rec.applied = rec.labels
				rec.state = 3
			}
			rec.labels = pr.Labels
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

// timeString は Unixtime の 10 進表記を返す。ラベルの比較に使う。
func timeString(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

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

// ---- 土台: Do (T011) ----

func TestMutexDo_RunsUnderLock(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	var ownerDuringFn string
	err := m.Do(context.Background(), func(ctx context.Context) error {
		s.mu.Lock()
		ownerDuringFn = s.find(testSOAID).labels[LockOwnerLabelKey]
		s.mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ownerDuringFn != "alice" {
		t.Errorf("処理の実行中に排他が保持されていない: owner=%q", ownerDuringFn)
	}
	// 終了時に解放される（奪ってよい時刻が現在時刻になる）。
	want := strconv.FormatInt(now.Unix(), 10)
	if got := s.find(testSOAID).labels[LockDeadlineLabelKey]; got != want {
		t.Errorf("解放されていない: deadline=%q, want %q", got, want)
	}
	if s.applies != 0 || s.atomics != 0 {
		t.Errorf("ゾーン反映が呼ばれた: changes=%d atomic=%d", s.applies, s.atomics)
	}
}

func TestMutexDo_ContextIsDerived(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "v")
	var got any
	if err := m.Do(parent, func(ctx context.Context) error {
		got = ctx.Value(key{})
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "v" {
		t.Errorf("処理へ渡る context が呼び出し側から派生していない: %v", got)
	}
}

func TestMutexDo_NotCalledWhenLocked(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	called := false
	err := m.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if called {
		t.Error("排他を取得できていないのに処理が実行された")
	}
}

func TestMutexDo_ReturnsFnError(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	sentinel := errors.New("処理のエラー")
	err := m.Do(context.Background(), func(ctx context.Context) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("処理のエラーがそのまま返らない: %v", err)
	}
	// 失敗しても解放される。
	want := strconv.FormatInt(now.Unix(), 10)
	if got := s.find(testSOAID).labels[LockDeadlineLabelKey]; got != want {
		t.Errorf("失敗時に解放されていない: deadline=%q", got)
	}
}

// ---- 利用シナリオ 1: 自動延長と打ち切り (T012〜T018) ----

// doMutex は延長の検証向けに、間隔を短くした Mutex を返す。
func doMutex(c *dpf.APIClient, owner string, now time.Time, opts ...Option) *Mutex {
	opts = append([]Option{WithRenewInterval(10 * time.Millisecond)}, opts...)
	return testMutex(c, owner, now, opts...)
}

// patchCount は模擬サーバの PATCH 回数を返す。
func patchCount(s *lockServer) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.patches
}

// waitPatchAbove は PATCH 回数が n を超えるまで待つ。超えなければ false。
func waitPatchAbove(s *lockServer, n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if patchCount(s) > n {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func TestMutexDo_RenewsWhileRunning(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	err := m.Do(context.Background(), func(ctx context.Context) error {
		after := patchCount(s) // 取得の書き込みまでを含む
		if !waitPatchAbove(s, after, 2*time.Second) {
			t.Error("処理の実行中に延長が走っていない")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMutexDo_RenewIntervalDefault(t *testing.T) {
	s := newLockServer(nil, 0)
	c := newLockClient(t, s)

	m := NewMutex(c.RecordsAPI, testZoneID)
	if want := DefaultLockTTL / renewIntervalDivisor; m.renewInterval != want {
		t.Errorf("既定の延長の間隔が %v（想定 %v）", m.renewInterval, want)
	}

	// 保持期間を変えると比が保たれる。
	m2 := NewMutex(c.RecordsAPI, testZoneID, WithTTL(30*time.Minute))
	if want := 30 * time.Minute / renewIntervalDivisor; m2.renewInterval != want {
		t.Errorf("保持期間 30 分での延長の間隔が %v（想定 %v）", m2.renewInterval, want)
	}

	// 明示すればその値になる。順序にも依存しない。
	m3 := NewMutex(c.RecordsAPI, testZoneID, WithRenewInterval(time.Second), WithTTL(time.Hour))
	if m3.renewInterval != time.Second {
		t.Errorf("WithRenewInterval が効いていない: %v", m3.renewInterval)
	}

	// 0 以下は無視される。
	m4 := NewMutex(c.RecordsAPI, testZoneID, WithRenewInterval(0))
	if want := DefaultLockTTL / renewIntervalDivisor; m4.renewInterval != want {
		t.Errorf("0 が無視されていない: %v", m4.renewInterval)
	}
}

func TestMutexDo_StolenCancelsFn(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	cancelled := false
	err := m.Do(context.Background(), func(ctx context.Context) error {
		// 他者が排他を奪った状況を作る。
		s.mu.Lock()
		s.find(testSOAID).labels = lockLabel("bob", now.Unix()+3600)
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			cancelled = true
		case <-time.After(2 * time.Second):
		}
		return nil
	})

	if !cancelled {
		t.Error("排他を奪われたのに処理の context が打ち切られていない")
	}
	if !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("ErrNotLockHolder を期待したが %v", err)
	}
	// 解放の ErrNotLockHolder を重ねて報告しない。
	if err.Error() != ErrNotLockHolder.Error() {
		t.Errorf("エラーが重複している: %v", err)
	}
}

func TestMutexDo_StolenJoinsFnError(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	sentinel := errors.New("処理のエラー")
	err := m.Do(context.Background(), func(ctx context.Context) error {
		s.mu.Lock()
		s.find(testSOAID).labels = lockLabel("bob", now.Unix()+3600)
		s.mu.Unlock()

		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
		return sentinel
	})

	if !errors.Is(err, ErrNotLockHolder) {
		t.Errorf("ErrNotLockHolder に一致しない: %v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("処理のエラーに一致しない: %v", err)
	}
}

func TestMutexDo_TransientRenewFailure(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	// 取得の書き込みの後、延長の 2 回を失敗させる。
	err := m.Do(context.Background(), func(ctx context.Context) error {
		s.mu.Lock()
		s.patchFailN = 2
		s.mu.Unlock()

		// 失敗の後に成功する延長を待つ。
		after := patchCount(s)
		if !waitPatchAbove(s, after+2, 2*time.Second) {
			t.Error("一時的な失敗の後に延長が再試行されていない")
		}
		select {
		case <-ctx.Done():
			t.Error("一時的な失敗で処理が打ち切られた")
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("一時的な失敗で Do が失敗した: %v", err)
	}
}

func TestMutexDo_RenewLoopStopped(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	// doHold を直接呼び、保持の状態を取り出す。Do が復帰した時点で延長の
	// goroutine が終了していることを、done が閉じていることで確かめる。
	var h *hold
	err := runLockedHold(context.Background(), m, func(ctx context.Context, hh *hold) error {
		h = hh
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-h.done:
	default:
		t.Error("復帰時に延長の goroutine が終了していない")
	}

	// 復帰後は延長の書き込みが起きない。
	stable := patchCount(s)
	time.Sleep(50 * time.Millisecond) // 延長の間隔 10ms の 5 倍
	if got := patchCount(s); got != stable {
		t.Errorf("復帰後に延長が走っている: PATCH が %d → %d", stable, got)
	}
}

// ---- 利用シナリオ 3: 取得の待機 (T039〜T040) ----

func TestMutexDo_LockWaitRetries(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	// 少し経ってから他者の排他が期限切れになる状況を作る。
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.mu.Lock()
		s.find(testSOAID).labels = lockLabel("bob", now.Unix()-1)
		s.mu.Unlock()
	}()

	called := false
	if err := m.Do(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	}, WithLockWait(5*time.Millisecond)); err != nil {
		t.Fatalf("待機の指定があれば取得できるまで繰り返すこと: %v", err)
	}
	if !called {
		t.Error("処理が実行されていない")
	}
}

func TestMutexDo_LockWaitHonorsCancel(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Unix()+3600), 3)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	called := false
	err := m.Do(ctx, func(ctx context.Context) error {
		called = true
		return nil
	}, WithLockWait(5*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context.DeadlineExceeded を期待したが %v", err)
	}
	if called {
		t.Error("取得できていないのに処理が実行された")
	}
	if s.patches != 0 {
		t.Errorf("待機中に書き込みが発生した（PATCH %d 回）", s.patches)
	}
}
