// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
)

// applyServer は一括置き換えを検証するための模擬サーバを返す。
// SOA と Zone Apex の NS、それに通常のレコード 1 件を持つ。
func applyServer(soaLabels map[string]string) *lockServer {
	s := newLockServer(soaLabels, 0)
	s.allowApply = true
	s.addRecord(&testRecord{
		name:   testZoneName,
		rrtype: "NS",
		labels: map[string]string{"ns.example": "keep"},
		state:  0,
	})
	s.addRecord(&testRecord{
		name:   "www." + testZoneName,
		rrtype: "A",
		labels: map[string]string{"www.example": "keep"},
		state:  0,
	})
	return s
}

// testApplier は now とポーリング間隔を固定した ZoneApplier を返す。
func testApplier(c *dpf.APIClient, owner string, now time.Time, opts ...Option) *ZoneApplier {
	opts = append([]Option{
		WithOwner(owner), WithTTL(time.Hour),
		WithVerifyTimeout(20 * time.Millisecond),
		WithRenewInterval(10 * time.Millisecond),
	}, opts...)
	// 排他の設定は WithLockOptions で包んで渡す（008 の移行）。
	a := NewZoneApplier(c.RecordsAPI, c.ZonesAPI, c.JobsAPI, testZoneID, WithLockOptions(opts...))
	mu := a.locker.(*Mutex)
	mu.now = func() time.Time { return now }
	mu.pollInterval = time.Millisecond
	return a
}

// keepAll は受け取った一覧をそのまま返す編集関数。
func keepAll(_ context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
	return records, nil
}

func init() { dpf.SyncWaitPollInterval = time.Millisecond }

// ---- 基本の流れ (T023) ----

func TestZoneApplier_Apply(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	var got []dpf.OverwriteRecordsInner
	err := a.Apply(context.Background(), func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		got = records
		// レコードを 1 件足す。
		ttl := int32(300)
		return append(records, dpf.OverwriteRecordsInner{
			Name:   "added." + testZoneName,
			Ttl:    *dpf.NewNullableInt32(&ttl),
			Rrtype: dpf.RECORDSRRTYPE_TXT,
			Rdata:  []dpf.RecordsRdataInner{{Value: dpf.PtrString(`"added"`)}},
			Labels: map[string]string{},
		}), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 公開されているレコードが渡る。編集中の SOA は「更新前の状態」から変換される。
	if len(got) != 3 {
		t.Fatalf("編集関数へ渡った件数が %d（SOA・NS・A の 3 件であること）", len(got))
	}
	if s.atomics != 1 {
		t.Errorf("一括置き換えの呼び出しが %d 回", s.atomics)
	}
	if s.applies != 0 {
		t.Errorf("編集中レコードのゾーン反映が呼ばれた（%d 回）", s.applies)
	}
	// 足したレコードが反映済みになっている。
	found := false
	for _, r := range s.records {
		if r.name == "added."+testZoneName && r.state == 0 {
			found = true
		}
	}
	if !found {
		t.Error("足したレコードが反映されていない")
	}
}

// ---- フラグの固定 (T024) ----

func TestZoneApplier_FlagsAlwaysFalse(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	if err := a.Apply(context.Background(), keepAll); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.atomicBody == nil {
		t.Fatal("一括置き換えのリクエストが記録されていない")
	}
	if s.atomicBody.OverwriteSoa == nil || *s.atomicBody.OverwriteSoa {
		t.Errorf("overwrite_soa が false で送られていない: %v", s.atomicBody.OverwriteSoa)
	}
	if s.atomicBody.OverwriteZoneApexNs == nil || *s.atomicBody.OverwriteZoneApexNs {
		t.Errorf("overwrite_zone_apex_ns が false で送られていない: %v", s.atomicBody.OverwriteZoneApexNs)
	}
}

// ---- 項目の保持 (T025) ----

func TestZoneApplier_KeepsLabelsAndTTL(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{"soa.example": "mine"})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	if err := a.Apply(context.Background(), keepAll); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 送られたリクエストに、元のラベルがそのまま入っている。
	for _, r := range s.atomicBody.Records {
		switch r.Rrtype {
		case "A":
			if r.Labels["www.example"] != "keep" {
				t.Errorf("A レコードのラベルが失われた: %v", r.Labels)
			}
		case "NS":
			if r.Labels["ns.example"] != "keep" {
				t.Errorf("NS レコードのラベルが失われた: %v", r.Labels)
			}
		}
		if r.Ttl == nil {
			t.Errorf("%s の TTL が失われた", r.Rrtype)
		}
	}

	// 反映済みの SOA のラベルは残る（排他のラベルだけが消える）。
	soa := s.find(testSOAID)
	if soa.labels["soa.example"] != "mine" {
		t.Errorf("反映済みの SOA のラベルが失われた: %v", soa.labels)
	}
	if _, ok := soa.labels[LockOwnerLabelKey]; ok {
		t.Errorf("排他のラベルが残っている: %v", soa.labels)
	}
}

// ---- 補完 (T026) ----

func TestZoneApplier_FillsRequiredRecords(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	// SOA と Zone Apex の NS を落として返す。
	err := a.Apply(context.Background(), func(_ context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		out := make([]dpf.OverwriteRecordsInner, 0, len(records))
		for _, r := range records {
			if r.Rrtype == dpf.RECORDSRRTYPE_SOA || r.Rrtype == dpf.RECORDSRRTYPE_NS {
				continue
			}
			out = append(out, r)
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasSOA, hasNS := false, false
	for _, r := range s.atomicBody.Records {
		if r.Rrtype == "SOA" {
			hasSOA = true
		}
		if r.Rrtype == "NS" && r.Name == testZoneName {
			hasNS = true
		}
	}
	if !hasSOA {
		t.Error("SOA レコードが補われていない")
	}
	if !hasNS {
		t.Error("Zone Apex の NS レコードが補われていない")
	}
}

// ---- 空の結果 (T027) ----

func TestZoneApplier_NoRecords(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	err := a.Apply(context.Background(), func(_ context.Context, _ []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		return nil, nil
	})
	if !errors.Is(err, ErrNoRecords) {
		t.Fatalf("ErrNoRecords を期待したが %v", err)
	}
	if s.atomics != 0 {
		t.Errorf("空の結果で一括置き換えを呼んだ（%d 回）", s.atomics)
	}
	// 排他は解放されている。
	want := timeString(now)
	if got := s.find(testSOAID).labels[LockDeadlineLabelKey]; got != want {
		t.Errorf("排他が解放されていない: deadline=%q, want %q", got, want)
	}
}

// ---- 成功時の後始末 (T028) ----

func TestZoneApplier_NoUnlockOnSuccess(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	if err := a.Apply(context.Background(), keepAll); err != nil {
		t.Fatalf("成功した操作が失敗として返った: %v", err)
	}
	// 一括置き換えの後、SOA は反映済みで排他のラベルを持たない。
	soa := s.find(testSOAID)
	if soa.state != 0 {
		t.Errorf("SOA が反映済みでない: state=%d", soa.state)
	}
	if _, ok := soa.labels[LockDeadlineLabelKey]; ok {
		t.Errorf("排他のラベルが残っている: %v", soa.labels)
	}
}

// ---- 失敗時の後始末 (T029) ----

func TestZoneApplier_ReleasesOnFailure(t *testing.T) {
	now := fixedNow()
	sentinel := errors.New("編集のエラー")

	cases := []struct {
		name  string
		setup func(s *lockServer)
		edit  ZoneRecordsEditor
		want  error
	}{
		{
			name:  "公開レコードの取得が失敗",
			setup: func(s *lockServer) { s.currentsErr = true },
			edit:  keepAll,
		},
		{
			name: "編集が失敗",
			edit: func(_ context.Context, _ []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
				return nil, sentinel
			},
			want: sentinel,
		},
		{
			name:  "一括置き換えが失敗",
			setup: func(s *lockServer) { s.atomicErr = [2]string{"invalid", "records"} },
			edit:  keepAll,
		},
		{
			name:  "JOB が失敗",
			setup: func(s *lockServer) { s.jobFailed = true },
			edit:  keepAll,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := applyServer(map[string]string{})
			if tc.setup != nil {
				tc.setup(s)
			}
			c := newLockClient(t, s)
			a := testApplier(c, "alice", now)

			err := a.Apply(context.Background(), tc.edit)
			if err == nil {
				t.Fatal("エラーを期待したが nil")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("エラーが判別できない: %v", err)
			}
			// 排他が解放されている（奪ってよい時刻が現在時刻）。
			soa := s.find(testSOAID)
			if got := soa.labels[LockDeadlineLabelKey]; got != timeString(now) {
				t.Errorf("排他が解放されていない: deadline=%q labels=%v", got, soa.labels)
			}
		})
	}
}

// ---- 消費の印の位置 (T030) ----

func TestZoneApplier_NoRenewDuringApply(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	// 延長の間隔を極端に短くし、編集の中で待つ。反映の最中に延長が走らないことを見る。
	a := testApplier(c, "alice", now, WithRenewInterval(time.Millisecond))

	var patchesBeforeApply int
	err := a.Apply(context.Background(), func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		// 編集中は延長が走る。
		before := patchCount(s)
		if !waitPatchAbove(s, before, time.Second) {
			t.Error("編集中に延長が走っていない")
		}
		return records, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	patchesBeforeApply = patchCount(s)

	// 反映の後は延長が走らない。
	time.Sleep(20 * time.Millisecond)
	if got := patchCount(s); got != patchesBeforeApply {
		t.Errorf("反映の後に延長が走っている: PATCH が %d → %d", patchesBeforeApply, got)
	}
}

// ---- Option (T031) ----

func TestZoneApplier_Options(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "deployer", now)

	if err := a.Apply(context.Background(), keepAll, WithApplyDescription("台帳と同期")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.atomicBody.Description == nil || *s.atomicBody.Description != "台帳と同期" {
		t.Errorf("コメントが渡っていない: %v", s.atomicBody.Description)
	}
	// 排他の Option が内部の排他へ渡っている。
	if owner := a.locker.(*Mutex).Owner(); owner != "deployer" {
		t.Errorf("保持者が %q（WithOwner が渡っていない）", owner)
	}
}

func TestZoneApplier_NilEditor(t *testing.T) {
	now := fixedNow()
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := testApplier(c, "alice", now)

	if err := a.Apply(context.Background(), nil); err == nil {
		t.Error("編集関数が nil の場合はエラーを返すこと")
	}
	if s.patches != 0 {
		t.Errorf("排他の取得を試みた（PATCH %d 回）", s.patches)
	}
}

// ---- 外部テストパッケージ向けの窓口 (T020) ----

// ApplyTestEnv は package utils_test から模擬サーバを使うための窓口である。
//
// 参照実装（utils/lockertest）を使うテストは、循環参照を避けるため package utils_test
// に置く必要がある。模擬サーバは本ファイル（package utils）にあり、外部テストパッケージ
// からは非公開の識別子を参照できないため、必要な操作だけをここで公開する。
type ApplyTestEnv struct {
	Client *dpf.APIClient
	ZoneID string
	s      *lockServer
}

// NewApplyTestEnv は一括置き換えを検証できる模擬環境を返す。
func NewApplyTestEnv(t *testing.T) *ApplyTestEnv {
	t.Helper()
	s := applyServer(map[string]string{})
	return &ApplyTestEnv{Client: newLockClient(t, s), ZoneID: testZoneID, s: s}
}

// Atomics は一括置き換えが呼ばれた回数を返す。
func (e *ApplyTestEnv) Atomics() int {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	return e.s.atomics
}

// Patches はレコードの更新が呼ばれた回数を返す。レコードを用いる排他を使っていない
// ことの確認に使う（差し替えた場合、SOA への更新は行われない）。
func (e *ApplyTestEnv) Patches() int {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	return e.s.patches
}

// OverwriteFlags は直近の一括置き換えのフラグを返す。
func (e *ApplyTestEnv) OverwriteFlags() (soa, apexNS *bool) {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	if e.s.atomicBody == nil {
		return nil, nil
	}
	return e.s.atomicBody.OverwriteSoa, e.s.atomicBody.OverwriteZoneApexNs
}

// SOALabels は SOA レコードのラベルの複製を返す。
func (e *ApplyTestEnv) SOALabels() map[string]string {
	e.s.mu.Lock()
	defer e.s.mu.Unlock()
	out := map[string]string{}
	for k, v := range e.s.find(testSOAID).labels {
		out[k] = v
	}
	return out
}

// ---- 排他の差し替え (T021) ----

// noopLocker は何もしない差し替えの実装。どちらの Option が効いたかだけを見るために使う。
type noopLocker struct{}

func (noopLocker) Lock(context.Context) error   { return nil }
func (noopLocker) Renew(context.Context) error  { return nil }
func (noopLocker) Unlock(context.Context) error { return nil }

func TestNewZoneApplier_WithLockerIgnoresLockOptions(t *testing.T) {
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	want := noopLocker{}

	// 指定の順序によらず、WithLocker が勝つ。
	cases := []struct {
		name string
		opts []ApplierOption
	}{
		{"WithLockOptions が先", []ApplierOption{
			WithLockOptions(WithOwner("alice")), WithLocker(want)}},
		{"WithLocker が先", []ApplierOption{
			WithLocker(want), WithLockOptions(WithOwner("alice"))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewZoneApplier(c.RecordsAPI, c.ZonesAPI, c.JobsAPI, testZoneID, tc.opts...)
			if a.locker != want {
				t.Fatalf("排他が %T。WithLocker で渡したものが使われていない", a.locker)
			}
			if _, ok := a.locker.(*Mutex); ok {
				t.Error("WithLockOptions によってレコードを用いる排他が作られている")
			}
		})
	}
}

// WithLocker に nil を渡しても既定の排他が使われる（FR-006）。
func TestNewZoneApplier_NilLockerFallsBack(t *testing.T) {
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := NewZoneApplier(c.RecordsAPI, c.ZonesAPI, c.JobsAPI, testZoneID, WithLocker(nil))
	if _, ok := a.locker.(*Mutex); !ok {
		t.Fatalf("排他が %T。既定はレコードを用いる排他であること", a.locker)
	}
}

// ---- 既定の排他 (T028) ----

// WithLocker を指定しない場合はレコードを用いる排他が使われ、対象のゾーンも引き継がれる。
func TestNewZoneApplier_DefaultsToRecordMutex(t *testing.T) {
	s := applyServer(map[string]string{})
	c := newLockClient(t, s)
	a := NewZoneApplier(c.RecordsAPI, c.ZonesAPI, c.JobsAPI, testZoneID)

	mu, ok := a.locker.(*Mutex)
	if !ok {
		t.Fatalf("排他が %T。既定はレコードを用いる排他であること", a.locker)
	}
	if mu.zoneID != testZoneID {
		t.Errorf("排他のゾーンが %q、期待は %q", mu.zoneID, testZoneID)
	}
	if !consumesLockOnZoneApply(mu) {
		t.Error("既定の排他が「一括置き換えで解かれる」と申告していない")
	}
}
