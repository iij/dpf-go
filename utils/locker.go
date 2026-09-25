// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Locker はゾーン単位の排他を表す。
//
// 本パッケージの操作（RunLocked、ZoneApplier）はこの抽象を通して排他を扱う。既定では
// DPF-API のレコードを用いる Mutex が使われるが、etcd などの分散ロックを実装して
// 差し替えられる。
//
// **1 つの値は 1 つのゾーンに対する 1 つの保持者を表す。** どのゾーンに対応するかを決める
// 責任は実装にある。本パッケージは対応づけを知らないため、取り違えても検出できない。
//
// # 契約
//
// 実装は次の約束を守ること。
//
//   - Lock は取得できるまで待ってはならない。他者が保持している場合も、**自分が既に
//     保持している場合（再入）** も ErrStillLock に errors.Is で一致するエラーを返す。
//     保持期間を延ばす場合は Renew を使う。
//   - Renew は「保持を確かめ、必要なら延ばす」操作である。保持していないと分かった場合は
//     ErrNotLockHolder に一致するエラーを返す。一時的に確かめられなかった場合はその
//     エラーを返す（本パッケージは次の周期で再試行する）。**保持の維持を背景で行う仕組み
//     でも、確かめる処理は必要である。** 何もせず nil を返すと、排他を失った状態のまま
//     処理が走り続ける。確かめる手段を持たない実装は常に nil を返してよいが、その限界を
//     実装の文書に明記すること。
//   - Unlock は自分が保持者である場合に限り解放する。保持者でない場合は ErrNotLockHolder
//     を返し、他者の排他を変更してはならない。排他がそもそも存在しない場合は何もせず
//     nil を返す。
//   - ErrStillLock と ErrNotLockHolder を取り違えないこと。前者は待てば取れるかもしれない
//     状態、後者は既に失った状態である。本パッケージは前者を再試行の対象とし、後者を
//     処理の打ち切りの合図として扱う。
//   - 1 つの値に対する Lock / Renew / Unlock は複数の goroutine から同時に呼ばれうる
//     （延長は別の goroutine で行われる）。実装は自身で直列化すること。
//
// 実装が契約を満たすかは utils/lockertest で機械的に確かめられる。
//
// # 既定の実装との違い
//
// 既定のレコードを用いる排他は、編集中のレコードへの他ユーザからの編集を DPF-API が
// 拒否することによって、**本ライブラリを使っていない相手（管理画面など）にも効く。**
// 外部の仕組みを使う排他にはこの効果が無く、その仕組みを使うプログラム同士しか排他
// できない。詳しくはパッケージ文書の比較を参照。
type Locker interface {
	Lock(ctx context.Context) error
	Renew(ctx context.Context) error
	Unlock(ctx context.Context) error
}

// RenewIntervaler は、保持期間を延ばす間隔を自分で決められる排他が実装する任意の
// インターフェースである。
//
// 実装しなくてもよい。実装した場合、RunLocked はこの値を延長の間隔として使う。保持期間から
// 導ける実装は実装すること。利用者が保持期間を変えるたびに間隔を指定し直さずに済む。
// 実装しない場合は DefaultRenewInterval が使われる。利用者が WithRenewEvery を指定した
// 場合は、申告よりそちらが優先される。
type RenewIntervaler interface {
	RenewInterval() time.Duration
}

// zoneApplyConsumer は、ゾーン全体の一括置き換えによって排他が解かれることを申告する
// 任意のインターフェースである。
//
// メソッドを非公開にしているため、本パッケージの外からは実装できない。これが必要なのは
// DPF-API のレコードを用いる排他だけであり、外部のミドルウェアを使う排他でゾーン反映が
// 排他を解くことはないためである。すべての実装者に無関係な判断を強いないよう、意図して
// 閉じている。
type zoneApplyConsumer interface {
	consumedByZoneApply() bool
}

// Locker を満たすことをコンパイル時に確認する。
var _ Locker = (*Mutex)(nil)

// DefaultRenewInterval は、排他が延長の間隔を申告しない場合に使う既定の間隔。
const DefaultRenewInterval = 5 * time.Minute

// holdConfig は RunLocked 1 回分の設定。
type holdConfig struct {
	lockWait      time.Duration
	renewInterval time.Duration
}

// HoldOption は RunLocked の任意設定を変更する。
type HoldOption func(*holdConfig)

// WithLockWait は排他を取得できるまで待つ間隔を指定する（デフォルト: 待たない）。
// 0 以下を渡した場合は待たない。
//
// 指定しない場合、RunLocked は排他を取得できなければただちに ErrStillLock を返す。
// 指定した場合は、取得できるまでこの間隔で繰り返す。待機は呼び出し側の打ち切りに従う。
func WithLockWait(d time.Duration) HoldOption {
	return func(c *holdConfig) {
		if d > 0 {
			c.lockWait = d
		}
	}
}

// WithRenewEvery は保持期間を延ばす間隔を指定し、**排他の申告を上書きする**
// （デフォルト: 排他の申告 → 無ければ DefaultRenewInterval）。0 以下を渡した場合は
// 上書きしない。
//
// 通常は指定しなくてよい。排他が RenewIntervaler を実装していれば、保持期間に見合った
// 間隔がその実装から得られる。既定のレコードを用いる排他は保持期間の 1/3 を申告する
// （その値は NewMutex の Option WithRenewInterval で変えられる）。
//
// 保持期間より十分短くすること。延長が一時的に失敗しても次の周期で間に合うように
// するためである。
func WithRenewEvery(d time.Duration) HoldOption {
	return func(c *holdConfig) {
		if d > 0 {
			c.renewInterval = d
		}
	}
}

// hold は RunLocked が保持している排他の状態を表す。実行中にのみ存在する。
//
// 一括置き換えのように「反映そのものが排他を解く」操作のために consume を持つ。これを
// 公開すると、RunLocked を使うすべての利用者が一括置き換えの都合を背負うことになるため、
// 型ごと非公開にしている。
type hold struct {
	// cancel は利用者の処理へ渡した context を打ち切る。
	cancel context.CancelFunc
	// stop は延長の goroutine への停止の合図。
	stop chan struct{}
	// done は延長の goroutine が終了したことの通知。
	done chan struct{}

	stopOnce sync.Once

	mu sync.Mutex
	// consumed は、この先で排他が解かれることの印。
	consumed bool
	// lostErr は延長によって保持者でないと判明したときのエラー。
	lostErr error
}

// consume は、この先の操作が排他を解くことを RunLocked へ伝える。
// 延長を止め、終了時の解放で ErrNotLockHolder を成功として扱うようにする。
func (h *hold) consume() {
	h.mu.Lock()
	h.consumed = true
	h.mu.Unlock()
	h.stopRenew()
}

// isConsumed は consume が呼ばれたかを返す。
func (h *hold) isConsumed() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.consumed
}

// lost は延長によって保持者でないと判明したことを記録する。
func (h *hold) lost(err error) {
	h.mu.Lock()
	if h.lostErr == nil {
		h.lostErr = err
	}
	h.mu.Unlock()
}

// lostError は記録されたエラーを返す。失われていない場合は nil。
func (h *hold) lostError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lostErr
}

// stopRenew は延長の goroutine を止め、終了を待つ。複数回呼んでもよい。
func (h *hold) stopRenew() {
	h.stopOnce.Do(func() { close(h.stop) })
	<-h.done
}

// RunLocked は排他 l を取得し、fn を実行し、終了時に解放する。
//
// fn へ渡される context は ctx から派生したものであり、**排他を他者に奪われた時点で
// 打ち切られる。** 保護が切れたことを知らないまま処理が走り続ける状態を作らないためで
// ある。ただし打ち切りは通知であって強制的な中断ではない。fn が context を無視すれば
// 走り続け、RunLocked はその終了を待つ。
//
// 実行中、保持期間は自動で延長される。間隔は、WithRenewEvery の指定があればその値、
// 無ければ排他が RenewIntervaler で申告する値、それも無ければ DefaultRenewInterval で
// ある。利用者が延長を書く必要はない。
// 延長が「保持者でない」以外の理由で失敗した場合は、次の周期で再試行する。**失敗が続く
// 間は、排他が実際には失効しているのに fn が走りうる。** この窓の長さは延長の間隔に依存する。
//
// 排他を取得できない場合、fn は呼ばれず ErrStillLock を返す。取得できるまで待つ場合は
// WithLockWait を使う。
//
// 終了時は、自分が保持者である場合に限り解放する。fn が返したエラーは種類を判別できる
// 形のまま返す。排他を失った場合は ErrNotLockHolder を返し、fn もエラーを返していた
// 場合は両方を判別できる形で返す。
//
// RunLocked が復帰した時点で、延長のための goroutine は終了している。
//
// fn の異常終了（panic）は捕捉しない。この場合、解放は行われず、排他は実装の保持期間の
// 経過によって解ける（保持期間を持たない実装では解けない）。
func RunLocked(ctx context.Context, l Locker, fn func(ctx context.Context) error, opts ...HoldOption) error {
	return runLockedHold(ctx, l, func(ctx context.Context, _ *hold) error {
		return fn(ctx)
	}, opts...)
}

// runLockedHold は RunLocked の実装。fn へ hold を渡すため非公開にしている。
func runLockedHold(ctx context.Context, l Locker, fn func(context.Context, *hold) error, opts ...HoldOption) error {
	if l == nil {
		return errors.New("dpf: RunLocked: 排他が nil である")
	}
	cfg := &holdConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := acquireLock(ctx, l, cfg.lockWait); err != nil {
		return err
	}

	hctx, cancel := context.WithCancel(ctx)
	defer cancel()

	h := &hold{
		cancel: cancel,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go renewLoop(hctx, l, h, renewIntervalOf(l, cfg.renewInterval))

	fnErr := fn(hctx, h)
	h.stopRenew()

	lostErr := h.lostError()
	relErr := releaseLock(ctx, l, h)

	switch {
	case lostErr != nil:
		// 排他を失ったことが根本原因である。解放のエラー（保持者でない）は同じことを
		// 言っているため重ねない。
		if fnErr != nil {
			return errors.Join(lostErr, fnErr)
		}
		return lostErr
	case fnErr != nil:
		return fnErr
	default:
		return relErr
	}
}

// acquireLock は排他を取得する。待機の指定があれば取得できるまで繰り返す。
func acquireLock(ctx context.Context, l Locker, wait time.Duration) error {
	for {
		err := l.Lock(ctx)
		if err == nil {
			return nil
		}
		if wait <= 0 || !errors.Is(err, ErrStillLock) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// releaseLock は終了時の解放を行う。
//
// Unlock は保持者でない場合に ErrNotLockHolder を返すため、「自分が保持者である場合に
// 限り解放する」はこれで満たされる。consume されている場合（一括置き換えによって排他が
// 解かれた場合）は、残っていた場合に限り解放するという意味になるため、ErrNotLockHolder を
// 成功として扱う。
func releaseLock(ctx context.Context, l Locker, h *hold) error {
	err := l.Unlock(ctx)
	if err == nil {
		return nil
	}
	if h.isConsumed() && errors.Is(err, ErrNotLockHolder) {
		return nil
	}
	return err
}

// renewIntervalOf は延長の間隔を返す。
//
// 利用者の指定（WithRenewEvery）→ 排他の申告（RenewIntervaler）→ 既定値、の順で決める。
// 利用者の指定を先に見るのは、排他の申告が保持期間から機械的に導かれた値であり、利用者が
// それより短くしたい場合があるためである。
func renewIntervalOf(l Locker, override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if r, ok := l.(RenewIntervaler); ok {
		if d := r.RenewInterval(); d > 0 {
			return d
		}
	}
	return DefaultRenewInterval
}

// consumesLockOnZoneApply は、ゾーン全体の一括置き換えがこの排他を解くかを返す。
// 申告が無ければ解かれないものとして扱う。
func consumesLockOnZoneApply(l Locker) bool {
	c, ok := l.(zoneApplyConsumer)
	return ok && c.consumedByZoneApply()
}

// renewLoop は保持期間を周期的に延長する。RunLocked の実行中のみ動く。
//
// 停止の合図または context の打ち切りを受けたら終了する。終了時に done を閉じるため、
// stopRenew はこれを待てる。
func renewLoop(ctx context.Context, l Locker, h *hold, interval time.Duration) {
	defer close(h.done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := l.Renew(ctx)
			if err == nil {
				continue
			}
			if errors.Is(err, ErrNotLockHolder) {
				// 他者に奪われた。保護が切れたことを処理へ伝えて終了する。
				h.lost(err)
				h.cancel()
				return
			}
			// それ以外（通信の失敗など）は次の周期で再試行する。一時的な失敗で編集作業を
			// 落とさないためである。保持期間が切れれば他者に奪われ、上の経路で中止される。
		}
	}
}
