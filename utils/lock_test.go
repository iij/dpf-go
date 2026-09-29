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
	"unicode/utf8"

	dpf "github.com/iij/dpf-go"
	"github.com/miekg/dns"
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

	// zoneLabels はゾーンのラベル。**PUT はマップ全体の置き換えとして扱う**
	// （渡されなかったラベルは消える）。実 API と同じ振る舞いにすることで、
	// 他のラベルを書き戻さない誤りをテストが検出できる。
	zoneLabels map[string]string
	// zoneLabelGets / zoneLabelPuts はゾーンのラベルの呼び出し回数。
	zoneLabelGets int
	zoneLabelPuts int
	// zoneLabelGetErr はゾーンのラベルの取得を失敗させる。
	zoneLabelGetErr bool
	// zoneLabelPutFailN はゾーンのラベルの更新を指定回数だけ失敗させる。
	zoneLabelPutFailN int
	// zoneLabelPutHook は PUT の直前に呼ばれる。延長中の横取りの再現に使う。
	zoneLabelPutHook func(s *lockServer, labels map[string]string)

	// recordGets はレコード一覧の取得の回数。ゾーン名の取得が 2 回目以降
	// 行われないことの確認に使う。
	recordGets int

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
		zoneLabels: map[string]string{},
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
		// ゾーンのラベルの取得。
		case len(rest) == 1 && rest[0] == "labels" && r.Method == http.MethodGet:
			s.zoneLabelGets++
			if s.zoneLabelGetErr {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"request_id":"x","error_type":"SystemError","error_message":"e"}`))
				return
			}
			b, _ := json.Marshal(map[string]any{
				"request_id": "x",
				"result":     map[string]any{"labels": s.zoneLabels},
			})
			_, _ = w.Write(b)

		// ゾーンのラベルの更新。**マップ全体の置き換えである。**
		case len(rest) == 1 && rest[0] == "labels" && r.Method == http.MethodPut:
			s.zoneLabelPuts++
			if s.zoneLabelPutFailN > 0 {
				s.zoneLabelPutFailN--
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"request_id":"x","error_type":"SystemError","error_message":"e"}`))
				return
			}
			body, _ := io.ReadAll(r.Body)
			var pr struct {
				Labels map[string]string `json:"labels"`
			}
			if err := json.Unmarshal(body, &pr); err != nil {
				t.Errorf("unmarshal put labels body: %v", err)
			}
			if s.zoneLabelPutHook != nil {
				s.zoneLabelPutHook(s, pr.Labels)
			}
			if pr.Labels == nil {
				pr.Labels = map[string]string{}
			}
			// 保持者が変わらず奪ってよい時刻だけが変わった PUT を延長として数える。
			oldOwner, oldDeadline, oldOK := parseLockValue(s.zoneLabels[ZoneLockLabelKey])
			newOwner, newDeadline, newOK := parseLockValue(pr.Labels[ZoneLockLabelKey])
			if oldOK && newOK && oldOwner == newOwner && oldDeadline != newDeadline {
				s.renews++
			}
			s.zoneLabels = pr.Labels
			writeAccepted(w)

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
			s.recordGets++
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

// lockLabel は排他の状態を表すゾーンのラベルを 1 つ持つマップを返す。
func lockLabel(owner string, deadline int64) map[string]string {
	return map[string]string{ZoneLockLabelKey: lockValue(owner, time.Unix(deadline, 0))}
}

// zoneLockOwner はゾーンのラベルに書かれた排他の保持者を返す。
func zoneLockOwner(s *lockServer) string {
	owner, _, _ := parseLockValue(zoneLabelOf(s, ZoneLockLabelKey))
	return owner
}

// zoneLockDeadline はゾーンのラベルに書かれた奪ってよい時刻を 10 進表記で返す。
// 排他のラベルが無い場合、および形式を満たさない場合は空文字を返す。
func zoneLockDeadline(s *lockServer) string {
	_, d, ok := parseLockValue(zoneLabelOf(s, ZoneLockLabelKey))
	if !ok {
		return ""
	}
	return strconv.FormatInt(d, 10)
}

// lockedServer は排他が保持されている状態の模擬サーバを返す。
//
// 排他の状態は**ゾーンのラベル**に置く。SOA のラベルには何も置かない。SOA の state も
// 排他の判定には関係しない（ゾーンのラベルは未反映の編集の概念を持たない）。
func lockedServer(owner string, deadline int64) *lockServer {
	s := newLockServer(nil, 0)
	s.zoneLabels = lockLabel(owner, deadline)
	return s
}

// testMutex は now とポーリング間隔を固定したテスト用 Mutex を返す。
func testMutex(c *dpf.APIClient, owner string, now time.Time, opts ...Option) *Mutex {
	opts = append([]Option{WithOwner(owner), WithTTL(time.Hour), WithVerifyTimeout(20 * time.Millisecond)}, opts...)
	m := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID, opts...)
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

	a := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID)
	b := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID)

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
	s := lockedServer("alice", now.Unix()+3600)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if m.lockable(zoneLabelsOf(s)) {
		t.Error("owner が自分自身でも、奪ってよい時刻の前は取得できてはならない")
	}
}

// 形式を満たさない値は「排他が成立していない」として扱う。解釈できない値に
// 引きずられて、ゾーンが永久に取得できない状態にしないためである
// （009 contracts/zone-label.md 第 5 節）。
func TestLockable_InvalidValue(t *testing.T) {
	now := fixedNow()
	for _, v := range []string{
		"",                    // 空
		"bob",                 // 区切りも時刻も無い
		"bob-notanumber",      // 末尾が数字でない
		"bob-179031925",       // 9 桁（1 桁足りない）
		"bob_1790319252",      // 区切りが違う
		"-1790319252",         // 保持者が空
		"bob-1790319252extra", // 末尾が時刻でない
	} {
		t.Run(v, func(t *testing.T) {
			s := newLockServer(map[string]string{}, 0)
			setZoneLabels(s, map[string]string{ZoneLockLabelKey: v})
			c := newLockClient(t, s)
			m := testMutex(c, "alice", now)

			if !m.lockable(zoneLabelsOf(s)) {
				t.Errorf("形式を満たさない値 %q では取得を許さなければならない", v)
			}
			if err := m.Lock(context.Background()); err != nil {
				t.Errorf("形式を満たさない値 %q で取得できなかった: %v", v, err)
			}
		})
	}
}

// getSOA はゾーン名を得るためだけに使う。排他の判定には使わないため、state を
// 問わず名前が得られればよい（009）。
func TestGetSOA_AnyState(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{id: "soaeditingid01", name: testZoneName, rrtype: "SOA", state: 3})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	soa, err := m.getSOA(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dns.CanonicalName(soa.Name) != dns.CanonicalName(testZoneName) {
		t.Errorf("ゾーン名を得られていない: got %q", soa.Name)
	}
}

// 編集予定と反映済みの SOA が並存していてもゾーン名は一意に決まる。
func TestGetSOA_EditingAndAppliedCoexist(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.addRecord(&testRecord{id: "soaduplicate01", name: testZoneName, rrtype: "SOA", state: 3})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	soa, err := m.getSOA(context.Background())
	if err != nil {
		t.Fatalf("並存していてもゾーン名は得られること: %v", err)
	}
	if dns.CanonicalName(soa.Name) != dns.CanonicalName(testZoneName) {
		t.Errorf("ゾーン名を得られていない: got %q", soa.Name)
	}
}

// SOA が 1 件も無い場合は ErrRecordNotFound。
func TestGetSOA_NotFound(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	s.records = nil
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if _, err := m.getSOA(context.Background()); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("ErrRecordNotFound を期待したが %v", err)
	}
}

// ラベルの上限は**ゾーンのラベル**で判定する（009 FR-006）。
func TestAcquirable_LabelLimit(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	labels := map[string]string{}
	for i := 0; i < maxZoneLabels; i++ {
		labels[fmt.Sprintf("k%d.example", i)] = "v"
	}
	setZoneLabels(s, labels)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrLabelLimit) {
		t.Fatalf("ErrLabelLimit を期待したが %v", err)
	}
	if s.posts != 0 {
		t.Errorf("ラベル上限では専用レコードを作ってはならない（POST %d 回）", s.posts)
	}
	if n := zoneLabelPutCount(s); n != 0 {
		t.Errorf("ラベル上限では書き込みを試みてはならない（PUT %d 回）", n)
	}
}

// 上限の 1 つ手前（9 個）なら取得できる。排他が消費するのは 1 個である（SC-009）。
func TestAcquirable_LabelLimitBoundary(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	labels := map[string]string{}
	for i := 0; i < maxZoneLabels-1; i++ {
		labels[fmt.Sprintf("k%d.example", i)] = "v"
	}
	setZoneLabels(s, labels)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("ラベルが 9 個なら取得できること: %v", err)
	}
	if got := len(zoneLabelsOf(s)); got != maxZoneLabels {
		t.Errorf("取得後のゾーンのラベルが %d 個。排他は 1 個だけ消費すること", got)
	}
}

// ---- 利用シナリオ 1: 取得 (T018〜T023) ----

func TestMutexLock_Success(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	setZoneLabels(s, map[string]string{"keep.example": "v"})
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := zoneLockOwner(s); got != "alice" {
		t.Errorf("owner: got %q, want alice", got)
	}
	want := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	if got := zoneLockDeadline(s); got != want {
		t.Errorf("deadline: got %q, want %q", got, want)
	}
	// **レコードのラベルは触らない**（009 FR-001・SC-005）。
	soa := s.find(testSOAID)
	if len(soa.labels) != 0 {
		t.Errorf("SOA のラベルが書かれている: %v", soa.labels)
	}
	if soa.state != 0 {
		t.Errorf("SOA が編集予定になっている: state=%d。排他はレコードを触らないこと", soa.state)
	}
	if n := s.patches; n != 0 {
		t.Errorf("レコードの更新が %d 回。排他はレコードを触らないこと", n)
	}
	// 利用者のゾーンのラベルが保持される（書き込みはマップ全体の置き換えである）
	if got := zoneLabelsOf(s)["keep.example"]; got != "v" {
		t.Errorf("利用者のゾーンのラベルが失われた: %v", zoneLabelsOf(s))
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
	s := lockedServer("bob", now.Unix()+3600)
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
		s.zoneLabels = lockLabel("bob", now.Unix()+3600)
		s.find(testSOAID).state = 3
	}

	if err := m.Lock(context.Background()); !errors.Is(err, ErrStillLock) {
		t.Fatalf("ErrStillLock を期待したが %v", err)
	}
	if s.patches != 0 {
		t.Errorf("本判定で取得できない場合は書き込んではならない（PATCH %d 回）", s.patches)
	}
	if got := zoneLockOwner(s); got != "bob" {
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
	if got := zoneLockOwner(s2); got != "alice" {
		t.Errorf("勝者の排他が書かれていない: owner=%q", got)
	}
}

func TestMutexLock_StealsExpiredLock(t *testing.T) {
	now := fixedNow()
	s := lockedServer("bob", now.Unix()-1)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("失効した排他は奪えなければならない: %v", err)
	}
	if got := zoneLockOwner(s); got != "alice" {
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
	if got := zoneLockOwner(s); got != "alice" {
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
	s := lockedServer("alice", now.Unix()+60)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Renew(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strconv.FormatInt(now.Add(time.Hour).Unix(), 10)
	if got := zoneLockDeadline(s); got != want {
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
	s := lockedServer("bob", now.Unix()+60)
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
	s := lockedServer("bob", now.Unix()+60)
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
	s := lockedServer("alice", now.Unix()+3600)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strconv.FormatInt(now.Unix(), 10)
	if got := zoneLockDeadline(s); got != want {
		t.Errorf("deadline: got %q, want %q (now)", got, want)
	}
	if got := zoneLockOwner(s); got != "alice" {
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
	s := lockedServer("bob", now.Unix()+3600)
	before := zoneLabelOf(s, ZoneLockLabelKey)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Unlock(context.Background()); !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("ErrNotLockHolder を期待したが %v", err)
	}
	if n := zoneLabelPutCount(s); n != 0 {
		t.Errorf("他者の排他を変更してはならない（PUT %d 回）", n)
	}
	if got := zoneLabelOf(s, ZoneLockLabelKey); got != before {
		t.Errorf("他者の排他が変わっている: got %q, want %q", got, before)
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
	s := lockedServer("bob", now.Unix()+3600)
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
	s := lockedServer("bob", now.Unix()+3600)
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
	m := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID)

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
	m2 := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID,
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
	// 専用レコードの名前はゾーン名と結合される。ゾーン名は Lock の中で保持済みである。
	got, err := m.lockRecordName(context.Background())
	if err != nil {
		t.Fatalf("専用レコード名を得られない: %v", err)
	}
	if got != "_mylock."+testZoneName {
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
		ownerDuringFn = zoneLockOwner(s)
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
	if got := zoneLockDeadline(s); got != want {
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
	s := lockedServer("bob", now.Unix()+3600)
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
	if got := zoneLockDeadline(s); got != want {
		t.Errorf("失敗時に解放されていない: deadline=%q", got)
	}
}

// ---- 利用シナリオ 1: 自動延長と打ち切り (T012〜T018) ----

// doMutex は延長の検証向けに、間隔を短くした Mutex を返す。
func doMutex(c *dpf.APIClient, owner string, now time.Time, opts ...Option) *Mutex {
	opts = append([]Option{WithRenewInterval(10 * time.Millisecond)}, opts...)
	return testMutex(c, owner, now, opts...)
}

// zoneLabelsOf は模擬サーバのゾーンのラベルの複製を返す。
//
// **ロックを取らない。** 既存の s.find と同じ作法である。ハンドラと並行して読む
// テストは、呼び出し側で s.mu を保持すること（保持したまま呼べるようにするため、
// ここでは取らない）。
func zoneLabelsOf(s *lockServer) map[string]string {
	out := map[string]string{}
	for k, v := range s.zoneLabels {
		out[k] = v
	}
	return out
}

// zoneLabelOf は指定したキーのゾーンのラベルの値を返す。無い場合は空文字。
// zoneLabelsOf と同じくロックを取らない。
func zoneLabelOf(s *lockServer, key string) string {
	return s.zoneLabels[key]
}

// setZoneLabels は前提条件としてゾーンのラベルを直接置く。
func setZoneLabels(s *lockServer, labels map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zoneLabels = map[string]string{}
	for k, v := range labels {
		s.zoneLabels[k] = v
	}
}

// zoneLabelPutCount はゾーンのラベルの更新の回数を返す。
func zoneLabelPutCount(s *lockServer) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zoneLabelPuts
}

// recordGetCount はレコード一覧の取得の回数を返す。
func recordGetCount(s *lockServer) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordGets
}

// waitZoneLabelPutAbove はゾーンのラベルの更新の回数が n を超えるまで待つ。
func waitZoneLabelPutAbove(s *lockServer, n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if zoneLabelPutCount(s) > n {
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
		after := zoneLabelPutCount(s) // 取得の書き込みまでを含む
		if !waitZoneLabelPutAbove(s, after, 2*time.Second) {
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

	m := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID)
	if want := DefaultLockTTL / renewIntervalDivisor; m.renewInterval != want {
		t.Errorf("既定の延長の間隔が %v（想定 %v）", m.renewInterval, want)
	}

	// 保持期間を変えると比が保たれる。
	m2 := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID, WithTTL(30*time.Minute))
	if want := 30 * time.Minute / renewIntervalDivisor; m2.renewInterval != want {
		t.Errorf("保持期間 30 分での延長の間隔が %v（想定 %v）", m2.renewInterval, want)
	}

	// 明示すればその値になる。順序にも依存しない。
	m3 := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID, WithRenewInterval(time.Second), WithTTL(time.Hour))
	if m3.renewInterval != time.Second {
		t.Errorf("WithRenewInterval が効いていない: %v", m3.renewInterval)
	}

	// 0 以下は無視される。
	m4 := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID, WithRenewInterval(0))
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
		s.zoneLabels = lockLabel("bob", now.Unix()+3600)
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
		s.zoneLabels = lockLabel("bob", now.Unix()+3600)
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
		s.zoneLabelPutFailN = 2
		s.mu.Unlock()

		// 失敗の後に成功する延長を待つ。
		after := zoneLabelPutCount(s)
		if !waitZoneLabelPutAbove(s, after+2, 2*time.Second) {
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
	stable := zoneLabelPutCount(s)
	time.Sleep(50 * time.Millisecond) // 延長の間隔 10ms の 5 倍
	if got := zoneLabelPutCount(s); got != stable {
		t.Errorf("復帰後に延長が走っている: PATCH が %d → %d", stable, got)
	}
}

// ---- 利用シナリオ 3: 取得の待機 (T039〜T040) ----

func TestMutexDo_LockWaitRetries(t *testing.T) {
	now := fixedNow()
	s := lockedServer("bob", now.Unix()+3600)
	c := newLockClient(t, s)
	m := doMutex(c, "alice", now)

	// 少し経ってから他者の排他が期限切れになる状況を作る。
	go func() {
		time.Sleep(20 * time.Millisecond)
		s.mu.Lock()
		s.zoneLabels = lockLabel("bob", now.Unix()-1)
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
	s := lockedServer("bob", now.Unix()+3600)
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

// ---- 009 T006: 値の組み立てと読み出し ----

// 値は右端から固定幅で読む。保持者に区切りと同じ文字が含まれていても曖昧にならない。
func TestLockValue_RoundTrip(t *testing.T) {
	deadline := time.Unix(1_790_319_252, 0)
	for _, owner := range []string{
		"a",                                // 1 文字
		"alice",                            // 通常
		"my-tool-v2",                       // 区切りと同じ文字を含む
		"web01-12345-a1b2c3d4",             // 既定の保持者の形
		"job-1790319252",                   // 時刻に見える文字列で終わる
		"tool-",                            // 区切りで終わる
		"12345678901234567890123456789012", // 32 文字
	} {
		t.Run(owner, func(t *testing.T) {
			v := lockValue(owner, deadline)
			gotOwner, gotDeadline, ok := parseLockValue(v)
			if !ok {
				t.Fatalf("組み立てた値を読み出せない: %q", v)
			}
			if gotOwner != owner {
				t.Errorf("保持者: got %q, want %q（値 %q）", gotOwner, owner, v)
			}
			if gotDeadline != deadline.Unix() {
				t.Errorf("奪ってよい時刻: got %d, want %d", gotDeadline, deadline.Unix())
			}
		})
	}
}

// 奪ってよい時刻は常に 10 桁で書く。10 桁に満たない場合は 0 で詰める。
func TestLockValue_ZeroPadsDeadline(t *testing.T) {
	v := lockValue("alice", time.Unix(1, 0))
	if want := "alice-0000000001"; v != want {
		t.Errorf("got %q, want %q", v, want)
	}
	_, d, ok := parseLockValue(v)
	if !ok || d != 1 {
		t.Errorf("ゼロ詰めした値を読み出せない: ok=%v d=%d", ok, d)
	}
}

// 値の全体はラベルの値の上限に収まる。
func TestLockValue_FitsLabelValueLimit(t *testing.T) {
	owner := strings.Repeat("x", maxOwnerLen)
	if got := len(lockValue(owner, time.Unix(9_999_999_999, 0))); got > maxLabelValueLen {
		t.Errorf("値の長さが %d。上限 %d に収まること", got, maxLabelValueLen)
	}
}

func TestParseLockValue_Invalid(t *testing.T) {
	for _, v := range []string{"", "x", "alice", "alice-", "-1790319252", "alice+1790319252",
		"alice-179031925", "alice-17903192529", "alice-abcdefghij"} {
		if _, _, ok := parseLockValue(v); ok {
			t.Errorf("形式を満たさない値 %q を読み出せてしまった", v)
		}
	}
}

// ---- 009 T008: 既定の保持者 ----

// ホスト名の枠は固定で、先頭を残して右側を落とす。
func TestTruncateHost(t *testing.T) {
	cases := []struct{ in, want string }{
		{"web01", "web01"},
		{strings.Repeat("a", ownerHostLen), strings.Repeat("a", ownerHostLen)},
		{"abcdefghijklmnopqrstuvwxyz", "abcdefghijklmno"}, // 先頭 15 文字が残る
	}
	for _, tc := range cases {
		if got := truncateHost(tc.in); got != tc.want {
			t.Errorf("truncateHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 既定の保持者は 32 文字に収まり、PID はゼロ詰めされない。
func TestNewOwner_Layout(t *testing.T) {
	owner := newOwner()
	if n := utf8.RuneCountInString(owner); n > maxOwnerLen {
		t.Errorf("既定の保持者が %d 文字。上限 %d に収まること: %q", n, maxOwnerLen, owner)
	}
	// **右から切り出す。** ホスト名自身が '-' を含みうるためである（値の読み出しを
	// 右端から固定幅で行うのと同じ理由）。
	i := strings.LastIndexByte(owner, '-')
	if i < 0 {
		t.Fatalf("既定の保持者の形が想定と異なる: %q", owner)
	}
	rnd, rest := owner[i+1:], owner[:i]
	j := strings.LastIndexByte(rest, '-')
	if j < 0 {
		t.Fatalf("既定の保持者に PID が無い: %q", owner)
	}
	pid, host := rest[j+1:], rest[:j]

	if got := len(host); got > ownerHostLen {
		t.Errorf("ホスト名部分が %d 文字。枠は %d 文字であること: %q", got, ownerHostLen, owner)
	}
	// PID はゼロ詰めしない。
	if want := strconv.Itoa(os.Getpid()); pid != want {
		t.Errorf("PID 部分が %q、期待は %q（ゼロ詰めしないこと）", pid, want)
	}
	if len(rnd) != 8 {
		t.Errorf("乱数部分が %q（8 桁の 16 進であること）", rnd)
	}
	// 呼ぶたびに異なる（乱数を含む）。
	if newOwner() == owner {
		t.Error("既定の保持者が呼び出しごとに変わらない")
	}
}

// ---- 009 T016: 保持者の検証 ----

// 32 文字を超える保持者では、書き込みを試みずに ErrOwnerTooLong を返す。
func TestMutexLock_OwnerTooLong(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	long := strings.Repeat("o", maxOwnerLen+1)
	m := testMutex(c, long, now)

	if err := m.Lock(context.Background()); !errors.Is(err, ErrOwnerTooLong) {
		t.Fatalf("ErrOwnerTooLong を期待したが %v", err)
	}
	if n := zoneLabelPutCount(s); n != 0 {
		t.Errorf("書き込みを試みてはならない（PUT %d 回）", n)
	}
	if s.posts != 0 {
		t.Errorf("専用レコードを作ってはならない（POST %d 回）", s.posts)
	}
	// 黙って切り詰めない。
	if v := zoneLabelOf(s, ZoneLockLabelKey); v != "" {
		t.Errorf("排他のラベルが書かれている: %q", v)
	}
}

// ちょうど 32 文字は取得できる。
func TestMutexLock_OwnerAtLimit(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	owner := strings.Repeat("o", maxOwnerLen)
	m := testMutex(c, owner, now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("32 文字の保持者で取得できること: %v", err)
	}
	if got := zoneLockOwner(s); got != owner {
		t.Errorf("保持者が切り詰められている: got %q (%d 文字)", got, len(got))
	}
}

// 保持者が長すぎる場合は、取得を待つ指定があっても待たない。
func TestMutexLockWait_OwnerTooLongDoesNotWait(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, strings.Repeat("o", maxOwnerLen+1), now)

	done := make(chan error, 1)
	go func() { done <- m.LockWait(context.Background(), 10*time.Millisecond) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrOwnerTooLong) {
			t.Fatalf("ErrOwnerTooLong を期待したが %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("待ち続けている。保持者の長さは待っても解消しない")
	}
}

// 空文字を指定した場合は既定の保持者が使われ、エラーにならない。
func TestMutexLock_OwnerEmptyUsesDefault(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := NewMutex(c.RecordsAPI, c.ZonesAPI, testZoneID, WithOwner(""), WithTTL(time.Hour),
		WithVerifyTimeout(20*time.Millisecond))
	m.now = func() time.Time { return now }
	m.pollInterval = time.Millisecond

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("空文字は既定の保持者になること: %v", err)
	}
	if got := zoneLockOwner(s); got == "" {
		t.Error("保持者が書かれていない")
	}
}

// ---- 009 T019: ゾーン名の取得 ----

// ゾーン名は一度得たら保持し、2 回目以降は SOA を読まない。
func TestMutexLock_ZoneNameCached(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	first := recordGetCount(s)
	if first == 0 {
		t.Fatal("ゾーン名の取得でレコードを読んでいない")
	}

	if err := m.Unlock(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	before := recordGetCount(s)
	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("2 回目の取得が失敗した: %v", err)
	}
	// 2 回目は専用レコードの確認でレコードを読むが、SOA は読み直さない。
	// 読み直していれば 1 回目と同じだけ増える。
	if got := recordGetCount(s) - before; got >= first {
		t.Errorf("2 回目のレコード読み取りが %d 回（1 回目 %d 回）。ゾーン名を読み直している", got, first)
	}
}

// ---- 009 T032・T033: レコードのラベルの枠 ----

// SOA レコードのラベルが上限まで埋まっていても排他を取得できる（SC-005）。
func TestMutexLock_SOALabelsFull(t *testing.T) {
	now := fixedNow()
	labels := map[string]string{}
	for i := 0; i < maxZoneLabels; i++ {
		labels[fmt.Sprintf("k%d.example", i)] = "v"
	}
	s := newLockServer(labels, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)

	if err := m.Lock(context.Background()); err != nil {
		t.Fatalf("SOA のラベルが満杯でも取得できること: %v", err)
	}
	// SOA のラベルは触られていない。
	if got := len(s.find(testSOAID).labels); got != maxZoneLabels {
		t.Errorf("SOA のラベルが %d 個に変わっている", got)
	}
	if n := s.patches; n != 0 {
		t.Errorf("レコードの更新が %d 回。排他はレコードを触らないこと", n)
	}
}

// ---- 009 T034: 区切りと同じ文字を含む保持者 ----

// 区切りと同じ文字（'-'）を含む保持者でも、取得・延長・解放のすべてが成功する。
func TestMutexLifecycle_OwnerWithSeparator(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	const owner = "my-tool-v2-1790319252"
	m := testMutex(c, owner, now)
	ctx := context.Background()

	if err := m.Lock(ctx); err != nil {
		t.Fatalf("取得できない: %v", err)
	}
	if got := zoneLockOwner(s); got != owner {
		t.Fatalf("保持者を読み違えている: got %q, want %q", got, owner)
	}
	if err := m.Renew(ctx); err != nil {
		t.Fatalf("延長できない: %v", err)
	}
	if err := m.Unlock(ctx); err != nil {
		t.Fatalf("解放できない: %v", err)
	}
	if got := zoneLockDeadline(s); got != timeString(now) {
		t.Errorf("解放されていない: deadline=%q", got)
	}
}

// ---- 009 T014: 解放はラベルを削除しない ----

// 解放してもラベルは残る。削除すると「一度も取得されていない」と区別が付かなくなる。
func TestMutexUnlock_KeepsLabel(t *testing.T) {
	now := fixedNow()
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)
	m := testMutex(c, "alice", now)
	ctx := context.Background()

	if err := m.Lock(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Unlock(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := zoneLabelsOf(s)[ZoneLockLabelKey]; !ok {
		t.Error("解放でラベルが削除されている。削除してはならない")
	}
	if got := zoneLockOwner(s); got != "alice" {
		t.Errorf("保持者が失われた: %q", got)
	}
	// 解放後は他者が取得できる。
	other := testMutex(c, "bob", now)
	if err := other.Lock(ctx); err != nil {
		t.Errorf("解放の後に他者が取得できない: %v", err)
	}
}
