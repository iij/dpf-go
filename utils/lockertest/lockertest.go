// SPDX-License-Identifier: Apache-2.0

package lockertest

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/iij/dpf-go/utils"
)

// maxLockDelay は Lock が待っていないと判断する上限。
//
// 契約は「取得できるまで待ってはならない」である。保持されている相手に対する Lock が
// これより長くかかる場合、待っているとみなす。
const maxLockDelay = 500 * time.Millisecond

// check は契約の 1 項目。violation は違反の内容を返す（違反が無ければ空文字）。
type check struct {
	name      string
	section   string
	violation func(ctx context.Context, a, b utils.Locker) string
}

// checks は契約の各項目。節の番号は
// specs/008-pluggable-lock/contracts/locker.md に対応する。
var checks = []check{
	{
		name:    "取得と解放",
		section: "第 2・4 節",
		violation: func(ctx context.Context, a, _ utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("空いている排他を取得できない: %v", err)
			}
			if err := a.Unlock(ctx); err != nil {
				return fmt.Sprintf("保持している排他を解放できない: %v", err)
			}
			return ""
		},
	},
	{
		name:    "他者が保持中は取得できない",
		section: "第 2 節",
		violation: func(ctx context.Context, a, b utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			err := b.Lock(ctx)
			if err == nil {
				_ = b.Unlock(ctx)
				return "他者が保持している排他を取得できてしまった。排他として機能していない"
			}
			if !errors.Is(err, utils.ErrStillLock) {
				return fmt.Sprintf("utils.ErrStillLock に一致しないエラーが返った: %v", err)
			}
			return ""
		},
	},
	{
		name:    "再入できない",
		section: "第 2 節",
		violation: func(ctx context.Context, a, _ utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			err := a.Lock(ctx)
			if err == nil {
				return "自分が保持している排他を再度取得できてしまった。" +
					"同じ保持者を持つプログラム同士が互いを排他できなくなる。" +
					"保持期間を延ばす場合は Renew を使うこと"
			}
			if !errors.Is(err, utils.ErrStillLock) {
				return fmt.Sprintf("utils.ErrStillLock に一致しないエラーが返った: %v", err)
			}
			return ""
		},
	},
	{
		name:    "取得は待たない",
		section: "第 2 節",
		violation: func(ctx context.Context, a, b utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			begin := time.Now()
			if err := b.Lock(ctx); err == nil {
				_ = b.Unlock(ctx)
			}
			if elapsed := time.Since(begin); elapsed > maxLockDelay {
				return fmt.Sprintf("保持されている排他への取得に %s かかった（上限 %s）。"+
					"取得できるまで待ってはならない。待つかどうかは呼び出し側が決める",
					elapsed.Round(time.Millisecond), maxLockDelay)
			}
			return ""
		},
	},
	{
		name:    "保持者でない延長は保持者でないことを返す",
		section: "第 3 節",
		violation: func(ctx context.Context, a, b utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			err := b.Renew(ctx)
			if err == nil {
				return "保持していない排他の延長が成功した。" +
					"排他を失ったことを呼び出し側へ伝えられないため、保護が切れたまま処理が走り続ける"
			}
			if !errors.Is(err, utils.ErrNotLockHolder) {
				return fmt.Sprintf("utils.ErrNotLockHolder に一致しないエラーが返った: %v", err)
			}
			return ""
		},
	},
	{
		name:    "保持者の延長は成功する",
		section: "第 3 節",
		violation: func(ctx context.Context, a, _ utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			if err := a.Renew(ctx); err != nil {
				return fmt.Sprintf("保持している排他の延長が失敗した: %v", err)
			}
			return ""
		},
	},
	{
		name:    "他者の排他を解放しない",
		section: "第 4 節",
		violation: func(ctx context.Context, a, b utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			err := b.Unlock(ctx)
			if err == nil {
				return "他者が保持している排他を解放できてしまった"
			}
			if !errors.Is(err, utils.ErrNotLockHolder) {
				return fmt.Sprintf("utils.ErrNotLockHolder に一致しないエラーが返った: %v", err)
			}
			// 解放されていないこと（まだ取得できないこと）を確かめる。
			if err := b.Lock(ctx); err == nil {
				_ = b.Unlock(ctx)
				return "解放を拒んだはずなのに、他者が取得できる状態になっている"
			}
			return ""
		},
	},
	{
		name:    "排他が無い状態の解放は成功する",
		section: "第 4 節",
		violation: func(ctx context.Context, a, _ utils.Locker) string {
			if err := a.Unlock(ctx); err != nil {
				return fmt.Sprintf("誰も保持していない状態の解放が失敗した: %v。"+
					"defer で解放する呼び出し側が、常にエラーを受け取ることになる", err)
			}
			return ""
		},
	},
	{
		name:    "解放後は他者が取得できる",
		section: "第 4 節",
		violation: func(ctx context.Context, a, b utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			if err := a.Unlock(ctx); err != nil {
				return fmt.Sprintf("解放できない: %v", err)
			}
			if err := b.Lock(ctx); err != nil {
				return fmt.Sprintf("解放された後に他者が取得できない: %v", err)
			}
			_ = b.Unlock(ctx)
			return ""
		},
	},
	{
		name:    "並行して呼べる",
		section: "第 7 節",
		violation: func(ctx context.Context, a, b utils.Locker) string {
			if err := a.Lock(ctx); err != nil {
				return fmt.Sprintf("取得できない: %v", err)
			}
			defer func() { _ = a.Unlock(ctx) }()

			// 延長は別の goroutine で行われる。実装が自身で直列化していない場合、
			// -race で競合が検出される。
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = a.Renew(ctx)
					if err := b.Lock(ctx); err == nil {
						_ = b.Unlock(ctx)
					}
				}()
			}
			wg.Wait()
			return ""
		},
	},
}

// Run は排他の実装が契約を満たすかを確かめる。
//
// newPair は、**同じ対象に対する排他を 2 つ**返すこと。契約の中心が「一方が保持している
// 間はもう一方が取得できない」ことであり、1 つでは競合を確かめられない。項目ごとに
// 呼ばれるため、毎回まっさらな状態を返すこと。
//
// 契約の詳細は utils.Locker の godoc を参照。違反があった場合は、どの約束に反しているかを
// 示して失敗させる。
func Run(t *testing.T, newPair func(t *testing.T) (utils.Locker, utils.Locker)) {
	t.Helper()

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			a, b := newPair(t)
			if a == nil || b == nil {
				t.Fatal("newPair が nil を返した")
			}
			if msg := c.violation(t.Context(), a, b); msg != "" {
				t.Errorf("契約違反（utils.Locker の契約 %s）: %s", c.section, msg)
			}
		})
	}
}

// Memory は同じ対象に対する排他を複数作るための、プロセス内で完結する参照実装である。
//
// 契約を満たしている。自分で実装を書くときの手本として読めるが、**分散ロックの難しさは
// 再現しない。** ネットワークの分断も、保持期間の失効による奪い合いも起こらない。
type Memory struct {
	mu       sync.Mutex
	ttl      time.Duration
	owner    string
	deadline time.Time
	seq      int
}

// NewMemory は参照実装の土台を作る。ttl が 0 以下の場合は 1 分とする。
func NewMemory(ttl time.Duration) *Memory {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Memory{ttl: ttl}
}

// Locker は新しい保持者を作る。同じ Memory から作った排他は、同じ対象を奪い合う。
func (m *Memory) Locker() utils.Locker {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return &memoryLocker{m: m, id: "memory-" + strconv.Itoa(m.seq)}
}

// Pair は同じ対象に対する排他を 2 つ作る。Run へそのまま渡せる。
func (m *Memory) Pair() (utils.Locker, utils.Locker) {
	return m.Locker(), m.Locker()
}

type memoryLocker struct {
	m  *Memory
	id string
}

// Lock は排他を取得する。保持されている場合は、保持者が自分であっても
// utils.ErrStillLock を返す（再入できない）。
func (l *memoryLocker) Lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.m.mu.Lock()
	defer l.m.mu.Unlock()

	if l.m.owner != "" && time.Now().Before(l.m.deadline) {
		return utils.ErrStillLock
	}
	l.m.owner = l.id
	l.m.deadline = time.Now().Add(l.m.ttl)
	return nil
}

// Renew は保持を確かめ、保持していれば期限を延ばす。
func (l *memoryLocker) Renew(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.m.mu.Lock()
	defer l.m.mu.Unlock()

	if l.m.owner != l.id {
		return utils.ErrNotLockHolder
	}
	l.m.deadline = time.Now().Add(l.m.ttl)
	return nil
}

// Unlock は自分が保持者である場合に限り解放する。
func (l *memoryLocker) Unlock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.m.mu.Lock()
	defer l.m.mu.Unlock()

	if l.m.owner == "" {
		return nil
	}
	if l.m.owner != l.id {
		return utils.ErrNotLockHolder
	}
	l.m.owner = ""
	l.m.deadline = time.Time{}
	return nil
}

// RenewInterval は保持期間の 1/3 を返す（utils.RenewIntervaler）。
func (l *memoryLocker) RenewInterval() time.Duration { return l.m.ttl / 3 }
