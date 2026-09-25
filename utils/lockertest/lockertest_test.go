// SPDX-License-Identifier: Apache-2.0

package lockertest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/iij/dpf-go/utils"
)

// ---- 参照実装は契約を満たす (T032) ----

func TestRun_ReferenceImplementationPasses(t *testing.T) {
	m := NewMemory(time.Minute)
	Run(t, func(*testing.T) (utils.Locker, utils.Locker) { return m.Pair() })
}

// 参照実装に対しては、どの項目も違反を返さない。
func TestChecks_ReferenceImplementationHasNoViolation(t *testing.T) {
	t.Parallel()
	got := violations(func() (utils.Locker, utils.Locker) {
		return NewMemory(time.Minute).Pair()
	})
	for name, msg := range got {
		t.Errorf("参照実装が %q で違反と判定された: %s", name, msg)
	}
}

// violations は契約の各項目を新しい組に対して実行し、違反した項目の名前と内容を返す。
//
// Run は *testing.T へ報告するため、そのままでは「落ちること」を確かめられない。
// 契約の表を直接回すことで、どの項目が違反を検出したかを調べる。
func violations(newPair func() (utils.Locker, utils.Locker)) map[string]string {
	out := map[string]string{}
	for _, c := range checks {
		a, b := newPair()
		if msg := c.violation(context.Background(), a, b); msg != "" {
			out[c.name] = msg
		}
	}
	return out
}

// assertViolates は、指定した項目が違反として検出され、その内容が want を含むことを確かめる。
func assertViolates(t *testing.T, got map[string]string, name, want string) {
	t.Helper()
	msg, ok := got[name]
	if !ok {
		t.Fatalf("%q が違反として検出されなかった（検出されたのは %v）", name, keys(got))
	}
	if !strings.Contains(msg, want) {
		t.Errorf("%q のメッセージが %q。どの約束に反したかが分かること（%q を含むこと）", name, msg, want)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- 再入を許す実装 (T033・契約 第 2 節) ----

// reentrantLocker は自分が保持している排他の再取得を許してしまう実装。
type reentrantLocker struct {
	utils.Locker
	held bool
}

func (l *reentrantLocker) Lock(ctx context.Context) error {
	err := l.Locker.Lock(ctx)
	if err == nil {
		l.held = true
		return nil
	}
	if l.held && errors.Is(err, utils.ErrStillLock) {
		return nil // 再入を許してしまう
	}
	return err
}

func TestRun_DetectsReentrant(t *testing.T) {
	t.Parallel()
	got := violations(func() (utils.Locker, utils.Locker) {
		m := NewMemory(time.Minute)
		a, b := m.Pair()
		return &reentrantLocker{Locker: a}, &reentrantLocker{Locker: b}
	})
	assertViolates(t, got, "再入できない", "再度取得できてしまった")
}

// ---- 取得できるまで待つ実装 (T034・契約 第 2 節) ----

// waitingLocker は取得できない場合に待ってしまう実装。
type waitingLocker struct {
	utils.Locker
}

func (l *waitingLocker) Lock(ctx context.Context) error {
	err := l.Locker.Lock(ctx)
	if err != nil && errors.Is(err, utils.ErrStillLock) {
		// 呼び出し側が決めるべき待機を、実装が勝手に行ってしまう。
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(maxLockDelay + 100*time.Millisecond):
		}
	}
	return err
}

func TestRun_DetectsWaitingLock(t *testing.T) {
	t.Parallel()
	got := violations(func() (utils.Locker, utils.Locker) {
		m := NewMemory(time.Minute)
		a, b := m.Pair()
		return &waitingLocker{Locker: a}, &waitingLocker{Locker: b}
	})
	assertViolates(t, got, "取得は待たない", "取得できるまで待ってはならない")
}

// ---- 保持の喪失を報告しない実装 (T035・契約 第 3 節) ----

// silentRenewLocker は Renew が常に成功する実装。排他を失っても検知できない。
type silentRenewLocker struct {
	utils.Locker
}

func (l *silentRenewLocker) Renew(context.Context) error { return nil }

func TestRun_DetectsSilentRenew(t *testing.T) {
	t.Parallel()
	got := violations(func() (utils.Locker, utils.Locker) {
		m := NewMemory(time.Minute)
		a, b := m.Pair()
		return &silentRenewLocker{Locker: a}, &silentRenewLocker{Locker: b}
	})
	assertViolates(t, got, "保持者でない延長は保持者でないことを返す", "保護が切れたまま処理が走り続ける")
}

// ---- 他者の排他を解放する実装 (T036・契約 第 4 節) ----

// thiefUnlockLocker は保持者でなくても解放してしまう実装。
type thiefUnlockLocker struct {
	utils.Locker
	m *Memory
}

func (l *thiefUnlockLocker) Unlock(ctx context.Context) error {
	if err := l.Locker.Unlock(ctx); err == nil {
		return nil
	}
	l.m.mu.Lock()
	l.m.owner = ""
	l.m.deadline = time.Time{}
	l.m.mu.Unlock()
	return nil
}

func TestRun_DetectsThiefUnlock(t *testing.T) {
	t.Parallel()
	got := violations(func() (utils.Locker, utils.Locker) {
		m := NewMemory(time.Minute)
		a, b := m.Pair()
		return &thiefUnlockLocker{Locker: a, m: m}, &thiefUnlockLocker{Locker: b, m: m}
	})
	assertViolates(t, got, "他者の排他を解放しない", "他者が保持している排他を解放できてしまった")
}

// ---- 番兵エラーに一致しないエラーを返す実装 (T036・契約 第 6 節) ----

// wrongErrorLocker は独自のエラーを返す実装。errors.Is で判別できない。
type wrongErrorLocker struct {
	utils.Locker
}

func (l *wrongErrorLocker) Lock(ctx context.Context) error {
	if err := l.Locker.Lock(ctx); err != nil {
		return errors.New("locked by someone")
	}
	return nil
}

func (l *wrongErrorLocker) Renew(ctx context.Context) error {
	if err := l.Locker.Renew(ctx); err != nil {
		return errors.New("not the holder")
	}
	return nil
}

func (l *wrongErrorLocker) Unlock(ctx context.Context) error {
	if err := l.Locker.Unlock(ctx); err != nil {
		return errors.New("not the holder")
	}
	return nil
}

func TestRun_DetectsWrongErrors(t *testing.T) {
	t.Parallel()
	got := violations(func() (utils.Locker, utils.Locker) {
		m := NewMemory(time.Minute)
		a, b := m.Pair()
		return &wrongErrorLocker{Locker: a}, &wrongErrorLocker{Locker: b}
	})
	assertViolates(t, got, "他者が保持中は取得できない", "utils.ErrStillLock に一致しない")
	assertViolates(t, got, "保持者でない延長は保持者でないことを返す", "utils.ErrNotLockHolder に一致しない")
	assertViolates(t, got, "他者の排他を解放しない", "utils.ErrNotLockHolder に一致しない")
}

// ---- 契約に反する 5 種類がすべて検出される（関門） ----

func TestRun_DetectsAllFiveViolations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		newPair func() (utils.Locker, utils.Locker)
	}{
		{"再入を許す", func() (utils.Locker, utils.Locker) {
			m := NewMemory(time.Minute)
			a, b := m.Pair()
			return &reentrantLocker{Locker: a}, &reentrantLocker{Locker: b}
		}},
		{"取得できるまで待つ", func() (utils.Locker, utils.Locker) {
			m := NewMemory(time.Minute)
			a, b := m.Pair()
			return &waitingLocker{Locker: a}, &waitingLocker{Locker: b}
		}},
		{"保持の喪失を報告しない", func() (utils.Locker, utils.Locker) {
			m := NewMemory(time.Minute)
			a, b := m.Pair()
			return &silentRenewLocker{Locker: a}, &silentRenewLocker{Locker: b}
		}},
		{"他者の排他を解放する", func() (utils.Locker, utils.Locker) {
			m := NewMemory(time.Minute)
			a, b := m.Pair()
			return &thiefUnlockLocker{Locker: a, m: m}, &thiefUnlockLocker{Locker: b, m: m}
		}},
		{"番兵エラーに一致しない", func() (utils.Locker, utils.Locker) {
			m := NewMemory(time.Minute)
			a, b := m.Pair()
			return &wrongErrorLocker{Locker: a}, &wrongErrorLocker{Locker: b}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := violations(tc.newPair); len(got) == 0 {
				t.Error("契約に反する実装が検出されなかった")
			}
		})
	}
}
