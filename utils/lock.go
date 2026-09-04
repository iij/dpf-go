// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	dpf "github.com/iij/dpf-go"
)

// ロック情報を格納する SOA レコードのラベルキー。
// ラベルには JSON を保存できないため、owner と deadline を別々のキーに分けて保存する。
const (
	// LockOwnerLabelKey はロック作成者(owner)を格納するラベルキー。
	LockOwnerLabelKey = "owner.lock.dpf-go"
	// LockDeadlineLabelKey はロックを奪っても良い時刻(UTC Unixtime)を格納するラベルキー。
	LockDeadlineLabelKey = "deadline.lock.dpf-go"
)

// ErrStillLock は、他者が保持中でまだ奪取できないロックに対して
// ロックを試みた場合に返される。
var ErrStillLock = errors.New("dpf: zone is still locked")

const (
	// DefaultLockOwner はホスト名を取得できなかった場合に使うフォールバックの owner。
	DefaultLockOwner = "dpf-go"
	// DefaultLockTTL はデフォルトのロック保持期間。
	DefaultLockTTL = 15 * time.Minute
)

// defaultOwner はデフォルトの owner を返す。
// 重複を避けるためホスト名の nodename 部分（最初の "." より前）を使い、
// 取得できない場合は DefaultLockOwner にフォールバックする。
func defaultOwner() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return DefaultLockOwner
	}
	if i := strings.IndexByte(h, '.'); i >= 0 {
		h = h[:i]
	}
	if h == "" {
		return DefaultLockOwner
	}
	return h
}

// Mutex はゾーン単位のロックを表す。
//
// DPF では「追加予定/編集予定」状態のレコードは、同名・同 RRTYPE のレコードを
// 重ねて編集できない。この性質を利用し、ゾーンの SOA レコードのラベル
// （LockOwnerLabelKey / LockDeadlineLabelKey）にロック情報を書き込んで
// 編集予定状態にすることでロックする。
//
// Lock/Unlock 自体はゾーン反映を行わない。Lock 後にレコードを編集し、
// Lock した SOA ごとゾーン反映することを想定している。反映後に Unlock すると
// レコードは編集可能な状態に戻り、再 Lock できる。
//
// 埋め込んだ sync.Mutex により、同一インスタンスに対する Lock/Unlock 呼び出しは
// プロセス内で直列化される。
type Mutex struct {
	sync.Mutex

	cr     dpf.RecordsApi
	zoneID string
	owner  string
	ttl    time.Duration

	// now は現在時刻を返す。テスト差し替え用。
	now func() time.Time
}

// Option は Mutex の任意設定を変更する。
type Option func(*Mutex)

// WithOwner はロック作成者を指定する（デフォルト: ホスト名の nodename 部分）。
// 空文字を渡した場合はデフォルトのままとする。
func WithOwner(owner string) Option {
	return func(m *Mutex) {
		if owner != "" {
			m.owner = owner
		}
	}
}

// WithTTL はロック保持期間を指定する（デフォルト: DefaultLockTTL 15分）。
// 0 以下を渡した場合はデフォルトのままとする。
func WithTTL(ttl time.Duration) Option {
	return func(m *Mutex) {
		if ttl > 0 {
			m.ttl = ttl
		}
	}
}

// NewMutex はゾーン zoneID に対するロックを生成する。
//   - cr     : レコード API（*dpf.RecordsAPIService が利用できる）
//   - zoneID : 対象ゾーンの ID
//
// owner と ttl は基本的にデフォルト値（owner: ホスト名の nodename 部分 / ttl: 15分）
// を使う。変更したい場合のみ WithOwner / WithTTL を opts に渡す。
func NewMutex(cr dpf.RecordsApi, zoneID string, opts ...Option) *Mutex {
	m := &Mutex{
		cr:     cr,
		zoneID: zoneID,
		owner:  defaultOwner(),
		ttl:    DefaultLockTTL,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Lock はゾーンのロックを取得する。
// 既存ロックが存在し、まだ奪取可能時刻に達しておらず、かつ owner が自分自身でない
// 場合は ErrStillLock を返す。それ以外（ロック無し／奪取可能時刻経過／owner が自分）は
// owner と deadline を SOA ラベルに書き込み、編集予定状態にする。
func (t *Mutex) Lock(ctx context.Context) error {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()

	soa, err := t.getSOA(ctx)
	if err != nil {
		return err
	}

	if !t.lockable(soa.Labels) {
		return ErrStillLock
	}

	labels := cloneLabels(soa.Labels)
	labels[LockOwnerLabelKey] = t.owner
	labels[LockDeadlineLabelKey] = strconv.FormatInt(t.now().Add(t.ttl).Unix(), 10)

	return t.patchLabels(ctx, soa.Id, labels)
}

// lockable は現在のラベルからロック取得可能かを判定する。
// 次のいずれかを満たせばロック可能:
//   - deadline ラベルが存在しない
//   - deadline ラベルが存在し、奪っても良い時刻を過ぎている
//   - owner ラベルが存在し、owner が自分自身である
func (t *Mutex) lockable(labels map[string]string) bool {
	deadlineStr, hasDeadline := labels[LockDeadlineLabelKey]
	if !hasDeadline {
		return true
	}
	if deadline, err := strconv.ParseInt(deadlineStr, 10, 64); err == nil {
		if t.now().Unix() >= deadline {
			return true
		}
	}
	if owner, ok := labels[LockOwnerLabelKey]; ok && owner == t.owner {
		return true
	}
	return false
}

// Unlock はロックを解放する。
// ロックを奪っても良い時刻(deadline)を現在時刻に更新することで、他者が即座に
// 奪取できるようにする。ロック(deadline ラベル)が存在しない場合は何もしない。
func (t *Mutex) Unlock(ctx context.Context) error {
	t.Mutex.Lock()
	defer t.Mutex.Unlock()

	soa, err := t.getSOA(ctx)
	if err != nil {
		return err
	}

	if _, ok := soa.Labels[LockDeadlineLabelKey]; !ok {
		return nil
	}

	labels := cloneLabels(soa.Labels)
	labels[LockDeadlineLabelKey] = strconv.FormatInt(t.now().Unix(), 10)

	return t.patchLabels(ctx, soa.Id, labels)
}

// LockWait はロックを取得できるまで Lock をリトライし続ける。
// interval で指定した間隔でリトライする。
// ErrStillLock 以外のエラーが発生した場合は、そのエラーを返して終了する。
// ctx がキャンセルされた場合は ctx.Err() を返す。
func (t *Mutex) LockWait(ctx context.Context, interval time.Duration) error {
	for {
		err := t.Lock(ctx)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrStillLock) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// getSOA は対象ゾーンの SOA レコードを取得する。
// 反映済み(state=0)のものを優先し、なければ最初に見つかった SOA を返す。
func (t *Mutex) getSOA(ctx context.Context) (*dpf.Record, error) {
	records, _, err := t.cr.GetRecordList(ctx, t.zoneID).
		KeywordsRrtype(dpf.RecordsRrtype("SOA")).
		ExecuteAll()
	if err != nil {
		return nil, err
	}
	if records == nil {
		return nil, ErrRecordNotFound
	}

	var soa *dpf.Record
	for i := range records.Results {
		r := &records.Results[i]
		if r.Rrtype != dpf.RecordsRrtype("SOA") {
			continue
		}
		if soa == nil || r.State == dpf.RECORDSSTATE__0 {
			soa = r
		}
	}
	if soa == nil {
		return nil, ErrRecordNotFound
	}
	return soa, nil
}

// patchLabels は SOA レコードのラベルを更新し、編集予定状態にする。
func (t *Mutex) patchLabels(ctx context.Context, recordID string, labels map[string]string) error {
	body := dpf.PatchRecord{Labels: &labels}
	_, _, err := t.cr.PatchRecord(ctx, t.zoneID, recordID).PatchRecord(body).Execute()
	return err
}

// cloneLabels はラベルマップを複製する（nil の場合も空マップを返す）。
func cloneLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
