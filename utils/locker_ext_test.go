// SPDX-License-Identifier: Apache-2.0

// 本ファイルは外部テストパッケージ（package utils_test）である。
//
// utils/lockertest は utils を import するため、**package utils のテストから
// lockertest を import すると循環参照になる。** 参照実装を使うテストはここへ置く。
// Go は同じディレクトリに package utils と package utils_test の両方を許す。

package utils_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
	"github.com/iij/dpf-go/utils/lockertest"
)

// testTimeout は、打ち切られるはずの処理がいつまでも終わらない場合の保険。
const testTimeout = 5 * time.Second

// stubLocker は挙動を細かく決められる差し替えの実装。
// 参照実装では作れない状況（延長だけが失敗する、など）の再現に使う。
type stubLocker struct {
	mu       sync.Mutex
	lockErr  error
	renewErr error
	locks    int
	renews   int
	unlocks  int
	interval time.Duration
}

func (s *stubLocker) Lock(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locks++
	return s.lockErr
}

func (s *stubLocker) Renew(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renews++
	return s.renewErr
}

func (s *stubLocker) Unlock(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unlocks++
	return nil
}

// RenewInterval は延長の間隔を短くする（utils.RenewIntervaler）。
func (s *stubLocker) RenewInterval() time.Duration { return s.interval }

func (s *stubLocker) counts() (locks, renews, unlocks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locks, s.renews, s.unlocks
}

// ---- 参照実装が契約を満たす (T014) ----

func TestMemoryLockerSatisfiesContract(t *testing.T) {
	m := lockertest.NewMemory(time.Minute)
	lockertest.Run(t, func(*testing.T) (utils.Locker, utils.Locker) {
		return m.Pair()
	})
}

// ---- 差し替えた実装で RunLocked が動く (T017・FR-001) ----

func TestRunLocked_WithReplacedLocker(t *testing.T) {
	m := lockertest.NewMemory(time.Minute)
	mine, other := m.Pair()

	called := false
	err := utils.RunLocked(t.Context(), mine, func(ctx context.Context) error {
		called = true
		// 処理の最中は排他の下にある。
		if err := other.Lock(ctx); !errors.Is(err, utils.ErrStillLock) {
			t.Errorf("処理の最中に他者が取得できた: %v", err)
		}
		if err := ctx.Err(); err != nil {
			t.Errorf("処理へ渡された context が既に打ち切られている: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("処理が呼ばれていない")
	}
	// 終了後は解放されている。
	if err := other.Lock(t.Context()); err != nil {
		t.Fatalf("終了後に他者が取得できない（解放されていない）: %v", err)
	}
}

// ---- 取得できない場合 (T018・FR-003) ----

func TestRunLocked_NotCalledWhenLocked(t *testing.T) {
	m := lockertest.NewMemory(time.Minute)
	mine, other := m.Pair()

	if err := other.Lock(t.Context()); err != nil {
		t.Fatalf("前提の取得に失敗した: %v", err)
	}

	err := utils.RunLocked(t.Context(), mine, func(context.Context) error {
		t.Error("排他を取得できていないのに処理が呼ばれた")
		return nil
	})
	if !errors.Is(err, utils.ErrStillLock) {
		t.Fatalf("エラーが %v、期待は utils.ErrStillLock", err)
	}
}

// 番兵エラーは実装ごとの型を包んでいても判別できる（errors.Is で一致すればよい）。
func TestRunLocked_WrappedStillLock(t *testing.T) {
	st := &stubLocker{
		lockErr:  fmt.Errorf("etcd: %w", utils.ErrStillLock),
		interval: time.Millisecond,
	}
	err := utils.RunLocked(t.Context(), st, func(context.Context) error {
		t.Error("排他を取得できていないのに処理が呼ばれた")
		return nil
	})
	if !errors.Is(err, utils.ErrStillLock) {
		t.Fatalf("エラーが %v、期待は utils.ErrStillLock に一致すること", err)
	}
	if _, _, unlocks := st.counts(); unlocks != 0 {
		t.Errorf("取得できなかったのに解放を %d 回呼んでいる", unlocks)
	}
}

// ---- 延長が保持の喪失を報告した場合 (T019・FR-003) ----

func TestRunLocked_RenewReportsLost(t *testing.T) {
	st := &stubLocker{
		renewErr: utils.ErrNotLockHolder,
		interval: time.Millisecond,
	}

	var ctxErr error
	err := utils.RunLocked(t.Context(), st, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			ctxErr = ctx.Err()
		case <-time.After(testTimeout):
			t.Error("排他を失ったのに処理の context が打ち切られない")
		}
		return nil
	})
	if !errors.Is(err, utils.ErrNotLockHolder) {
		t.Fatalf("エラーが %v、期待は utils.ErrNotLockHolder", err)
	}
	if !errors.Is(ctxErr, context.Canceled) {
		t.Errorf("処理の context の打ち切り理由が %v", ctxErr)
	}
	if _, renews, unlocks := st.counts(); renews == 0 || unlocks != 1 {
		t.Errorf("延長 %d 回・解放 %d 回。延長は 1 回以上、解放は 1 回であること", renews, unlocks)
	}
}

// 延長が「保持者でない」以外の理由で失敗した場合は、次の周期で再試行する。
func TestRunLocked_RenewRetriesOnTransientError(t *testing.T) {
	st := &stubLocker{
		renewErr: errors.New("一時的な通信の失敗"),
		interval: time.Millisecond,
	}

	err := utils.RunLocked(t.Context(), st, func(ctx context.Context) error {
		deadline := time.Now().Add(testTimeout)
		for {
			if _, renews, _ := st.counts(); renews >= 3 {
				return nil
			}
			if time.Now().After(deadline) {
				return errors.New("延長が再試行されない")
			}
			select {
			case <-ctx.Done():
				return errors.New("失敗が続いただけで処理が打ち切られた")
			case <-time.After(time.Millisecond):
			}
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---- 差し替えた実装での一括置き換え (T020・FR-008・SC-005) ----

// keepAll は受け取った一覧をそのまま返す編集関数。
func keepAll(_ context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
	return records, nil
}

func TestZoneApplier_WithReplacedLocker(t *testing.T) {
	env := utils.NewApplyTestEnv(t)
	m := lockertest.NewMemory(time.Minute)
	mine, other := m.Pair()

	a := utils.NewZoneApplier(env.Client.RecordsAPI, env.Client.ZonesAPI, env.Client.JobsAPI,
		env.ZoneID, utils.WithLocker(mine))

	if err := a.Apply(t.Context(), keepAll); err != nil {
		t.Fatalf("差し替えた排他で一括置き換えが失敗した: %v", err)
	}

	// 反映は既定の排他と同じく 1 回、フラグも同じ。
	if n := env.Atomics(); n != 1 {
		t.Errorf("一括置き換えの回数が %d、期待は 1", n)
	}
	soa, apexNS := env.OverwriteFlags()
	if soa == nil || *soa || apexNS == nil || *apexNS {
		t.Errorf("取り込みのフラグが soa=%v apexNS=%v。いずれも false であること", soa, apexNS)
	}

	// レコードを用いる排他ではないため、SOA は触られていない。
	if n := env.Patches(); n != 0 {
		t.Errorf("SOA への更新が %d 回。差し替えた排他ではレコードを触らないこと", n)
	}
	if labels := env.SOALabels(); len(labels) != 0 {
		t.Errorf("SOA に排他のラベルが付いている: %v", labels)
	}

	// **反映が排他を解かない実装でも、解放は正しく行われる。**
	if err := other.Lock(t.Context()); err != nil {
		t.Fatalf("反映の後に排他が解放されていない: %v", err)
	}
}

// 失敗した場合も解放される。
func TestZoneApplier_WithReplacedLockerReleasesOnFailure(t *testing.T) {
	env := utils.NewApplyTestEnv(t)
	m := lockertest.NewMemory(time.Minute)
	mine, other := m.Pair()

	a := utils.NewZoneApplier(env.Client.RecordsAPI, env.Client.ZonesAPI, env.Client.JobsAPI,
		env.ZoneID, utils.WithLocker(mine))

	sentinel := errors.New("編集のエラー")
	edit := func(context.Context, []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		return nil, sentinel
	}
	if err := a.Apply(t.Context(), edit); !errors.Is(err, sentinel) {
		t.Fatalf("エラーが %v、期待は編集のエラー", err)
	}
	if n := env.Atomics(); n != 0 {
		t.Errorf("編集が失敗したのに一括置き換えを %d 回呼んでいる", n)
	}
	if err := other.Lock(t.Context()); err != nil {
		t.Fatalf("失敗の後に排他が解放されていない: %v", err)
	}
}
