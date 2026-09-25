// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeLocker は挙動を指定できる排他。抽象そのものの振る舞いを確かめるために使う。
// RenewIntervaler は実装しない（申告しない実装を表す）。
type fakeLocker struct {
	mu        sync.Mutex
	lockErr   error
	renewErr  error
	unlockErr error
	unlocks   int
}

func (f *fakeLocker) Lock(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lockErr
}

func (f *fakeLocker) Renew(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewErr
}

func (f *fakeLocker) Unlock(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlocks++
	return f.unlockErr
}

func (f *fakeLocker) unlockCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unlocks
}

// intervalLocker は延長の間隔を申告する排他。
type intervalLocker struct {
	fakeLocker
	interval time.Duration
}

func (i *intervalLocker) RenewInterval() time.Duration { return i.interval }

// ---- Mutex.Do は RunLocked の薄い包み (T026・FR-007・SC-002) ----

// doAndRunLocked は同じ状況で Mutex.Do と RunLocked を実行し、結果を比べる。
func TestMutexDo_SameAsRunLocked(t *testing.T) {
	now := fixedNow()
	sentinel := errors.New("処理のエラー")

	cases := []struct {
		name   string
		labels map[string]string
		fn     func(ctx context.Context) error
		want   error
	}{
		{"成功", map[string]string{}, func(context.Context) error { return nil }, nil},
		{"処理のエラー", map[string]string{}, func(context.Context) error { return sentinel }, sentinel},
		{"取得できない", lockLabel("bob", now.Add(time.Hour).Unix()),
			func(context.Context) error { t.Error("処理が呼ばれた"); return nil }, ErrStillLock},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Mutex.Do
			s1 := newLockServer(cloneLabels(tc.labels), 0)
			m1 := doMutex(newLockClient(t, s1), "alice", now)
			errDo := m1.Do(context.Background(), tc.fn)

			// RunLocked（同じ排他を渡す）
			s2 := newLockServer(cloneLabels(tc.labels), 0)
			m2 := doMutex(newLockClient(t, s2), "alice", now)
			errRun := RunLocked(context.Background(), m2, tc.fn)

			if !errors.Is(errDo, tc.want) {
				t.Errorf("Do のエラーが %v、期待は %v", errDo, tc.want)
			}
			if !errors.Is(errRun, tc.want) {
				t.Errorf("RunLocked のエラーが %v、期待は %v", errRun, tc.want)
			}
			// 排他の痕跡も同じであること。
			if got, want := s1.find(testSOAID).labels, s2.find(testSOAID).labels; !sameLabels(got, want) {
				t.Errorf("SOA のラベルが Do=%v、RunLocked=%v で異なる", got, want)
			}
		})
	}
}

// Mutex.Do は HoldOption をそのまま RunLocked へ渡す。
func TestMutexDo_PassesHoldOptions(t *testing.T) {
	now := fixedNow()
	s := newLockServer(lockLabel("bob", now.Add(time.Hour).Unix()), 0)
	m := doMutex(newLockClient(t, s), "alice", now)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// 待機を指定すると、取得できないまま打ち切りまで繰り返す（ErrStillLock では帰らない）。
	err := m.Do(ctx, func(context.Context) error {
		t.Error("取得できていないのに処理が呼ばれた")
		return nil
	}, WithLockWait(5*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("エラーが %v、期待は context.DeadlineExceeded（WithLockWait が渡っていない）", err)
	}
}

func sameLabels(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ---- 延長の間隔の決まり方 (T027・FR-009) ----

func TestRenewIntervalOf(t *testing.T) {
	cases := []struct {
		name     string
		locker   Locker
		override time.Duration
		want     time.Duration
	}{
		{"申告があればその値", &intervalLocker{interval: 42 * time.Second}, 0, 42 * time.Second},
		{"利用者の指定が申告より優先される", &intervalLocker{interval: 42 * time.Second},
			7 * time.Second, 7 * time.Second},
		{"申告が無ければ既定値", &fakeLocker{}, 0, DefaultRenewInterval},
		{"申告が無く指定があればその値", &fakeLocker{}, 7 * time.Second, 7 * time.Second},
		{"申告が 0 以下なら既定値", &intervalLocker{interval: 0}, 0, DefaultRenewInterval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renewIntervalOf(tc.locker, tc.override); got != tc.want {
				t.Errorf("延長の間隔が %v、期待は %v", got, tc.want)
			}
		})
	}
}

// WithRenewEvery は 0 以下を無視する。
func TestWithRenewEvery_IgnoresNonPositive(t *testing.T) {
	cfg := &holdConfig{}
	WithRenewEvery(0)(cfg)
	WithRenewEvery(-time.Second)(cfg)
	if cfg.renewInterval != 0 {
		t.Errorf("延長の間隔が %v。0 以下は無視すること", cfg.renewInterval)
	}
}

// 既定のレコードを用いる排他は保持期間の 1/3 を申告する。
func TestMutex_DeclaresRenewInterval(t *testing.T) {
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)

	m := NewMutex(c.RecordsAPI, testZoneID, WithTTL(30*time.Minute))
	if got := renewIntervalOf(m, 0); got != 10*time.Minute {
		t.Errorf("延長の間隔が %v、期待は保持期間の 1/3（10m）", got)
	}
	// 利用者の指定は申告を上書きする。
	if got := renewIntervalOf(m, time.Minute); got != time.Minute {
		t.Errorf("延長の間隔が %v、期待は 1m", got)
	}
}

// ---- 消費の印 (T029・FR-009) ----

func TestConsumesLockOnZoneApply(t *testing.T) {
	s := newLockServer(map[string]string{}, 0)
	c := newLockClient(t, s)

	if !consumesLockOnZoneApply(NewMutex(c.RecordsAPI, testZoneID)) {
		t.Error("レコードを用いる排他が「一括置き換えで解かれる」と申告していない")
	}
	// 外部の仕組みを使う排他は申告できない（非公開のメソッドであるため）。
	if consumesLockOnZoneApply(&fakeLocker{}) {
		t.Error("外部の実装が申告できてしまっている")
	}
	if consumesLockOnZoneApply(&intervalLocker{}) {
		t.Error("外部の実装が申告できてしまっている")
	}
}

// 消費の印が立っている場合に限り、解放の「保持者でない」を成功として扱う。
func TestRunLocked_ConsumedRelease(t *testing.T) {
	cases := []struct {
		name    string
		consume bool
		want    error
	}{
		{"消費の印があれば成功として扱う", true, nil},
		{"印が無ければそのまま返す", false, ErrNotLockHolder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeLocker{unlockErr: ErrNotLockHolder}
			err := runLockedHold(context.Background(), f,
				func(_ context.Context, h *hold) error {
					if tc.consume {
						h.consume()
					}
					return nil
				})
			if !errors.Is(err, tc.want) && err != tc.want {
				t.Errorf("エラーが %v、期待は %v", err, tc.want)
			}
			// どちらの場合も解放は試みる（残っていれば解く）。
			if n := f.unlockCount(); n != 1 {
				t.Errorf("解放の回数が %d、期待は 1", n)
			}
		})
	}
}

// 排他が nil の場合は処理を呼ばずにエラーを返す。
func TestRunLocked_NilLocker(t *testing.T) {
	err := RunLocked(context.Background(), nil, func(context.Context) error {
		t.Error("排他が nil なのに処理が呼ばれた")
		return nil
	})
	if err == nil {
		t.Fatal("エラーが返らない")
	}
}
