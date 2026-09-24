// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/miekg/dns"
)

// ロック情報を格納するレコードのラベルキー。
// ラベルには JSON を保存できないため、owner と deadline を別々のキーに分けて保存する。
// ゾーンの排他（SOA レコード）と一時レコード追加ロック（専用レコード）の双方で同じ
// キーを用いる。いずれも「保持者」と「奪ってよい時刻」を表すが、対象はレコードごとに
// 独立している。
const (
	// LockOwnerLabelKey はロック作成者(owner)を格納するラベルキー。
	LockOwnerLabelKey = "owner.lock.dpf-go"
	// LockDeadlineLabelKey はロックを奪っても良い時刻(UTC Unixtime)を格納するラベルキー。
	LockDeadlineLabelKey = "deadline.lock.dpf-go"
)

var (
	// ErrStillLock は、まだ奪えない排他に対して取得を試みた場合に返される。
	// 他者が保持している場合、自分自身が保持している場合（取得は再入できない）、
	// 一時レコード追加ロックを取得できなかった場合、書き込んだ内容を確認できなかった
	// 場合のいずれもこのエラーになる。呼び出し側の対処は「待って再試行する」の
	// 一択であるため、原因による区別はしない。
	ErrStillLock = errors.New("dpf: zone is still locked")
	// ErrNotLockHolder は、自分が保持していない排他に対して Renew または Unlock を
	// 行った場合に返される。保持中に他者へ奪われた場合も含む。
	ErrNotLockHolder = errors.New("dpf: not the lock holder")
	// ErrLabelLimit は、SOA レコードのラベル数が上限に達しており、排他のための
	// ラベルを追加できない場合に返される。排他はラベルを 2 つ使うため、利用者が
	// SOA レコードへ自由に付けられるラベルは 8 個までである。
	ErrLabelLimit = errors.New("dpf: soa record label limit reached")
)

const (
	// DefaultLockOwner はホスト名を取得できなかった場合に使うフォールバックの owner。
	DefaultLockOwner = "dpf-go"
	// DefaultLockTTL はデフォルトのロック保持期間。
	DefaultLockTTL = 15 * time.Minute
	// DefaultLockRecordTTL は一時レコード追加ロックが失効するまでのデフォルトの時間。
	// 排他の取得に要する時間だけ保持すれば足りるため、保持期間より大幅に短い。
	DefaultLockRecordTTL = time.Minute
	// DefaultVerifyTimeout は書き込んだ内容が読み出せるまで待つデフォルトの上限。
	// レコードの更新は非同期に処理されるため、書き込みの直後は反映されていない。
	DefaultVerifyTimeout = 10 * time.Second
	// DefaultLockRecordLabel は一時レコード追加ロックに使う専用レコードの最左ラベル。
	// アンダースコアで始まる名前は通常のホスト名と衝突しない。
	DefaultLockRecordLabel = "_dpf-go-lock"

	// maxRecordLabels は 1 レコードに付けられるラベル数の上限（DPF-API の仕様）。
	maxRecordLabels = 10
	// maxLabelValueLen はラベル値の最大長（DPF-API の仕様）。
	maxLabelValueLen = 63
	// renewIntervalDivisor は延長の間隔を保持期間から導く除数。
	renewIntervalDivisor = 3
	// defaultPollInterval は非同期処理の反映を待つ際のポーリング間隔。
	defaultPollInterval = 500 * time.Millisecond
)

// defaultLockRecordRrtype は専用レコードの既定の RRTYPE。
// TXT は権威サーバへ公開されても名前解決に影響しない。
const defaultLockRecordRrtype = dpf.RECORDSRRTYPEWITHOUTSOA_TXT

// defaultLockRecordRdata は専用レコードの既定の rdata。
// TXT レコードの規則により引用符で囲む必要がある。
var defaultLockRecordRdata = []string{`"dpf-go lock"`}

// newOwner は owner を生成する。
//
// 同一ホスト上の別プロセスおよび同一プロセス内の別インスタンスと衝突しないよう、
// ホスト名の nodename 部分に PID とランダム値を付ける。ランダム値を含めるのは、
// PID が再利用されたときに終了したプロセスの排他を引き継がないためである。
// ホスト名と PID を含めるのは、ラベルを見た人がどこから取られた排他かを
// 識別できるようにするためである。
func newOwner() string {
	host := DefaultLockOwner
	if h, err := os.Hostname(); err == nil && h != "" {
		if i := strings.IndexByte(h, '.'); i >= 0 {
			h = h[:i]
		}
		if h != "" {
			host = h
		}
	}

	suffix := "-" + strconv.Itoa(os.Getpid())
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		suffix += "-" + hex.EncodeToString(b[:])
	}

	if n := maxLabelValueLen - len(suffix); len(host) > n {
		host = host[:n]
	}
	return host + suffix
}

// Mutex はゾーン単位のロックを表す。
//
// ゾーンの SOA レコードのラベル（LockOwnerLabelKey / LockDeadlineLabelKey）に保持者と
// 奪ってよい時刻を書き込み、SOA を編集予定の状態にすることでロックする。ラベルの
// 読み取りから書き込みまでの区間は、専用レコードの追加（一時レコード追加ロック）で
// 排他する。DPF-API はレコードの新規追加に対して同名かつ同 RRTYPE の重複を拒否し、
// この拒否は編集者が誰かに依らないためである。
//
// 排他の強さは競合相手によって異なる。
//
//   - 別ユーザ: DPF-API が編集中のレコードへの他ユーザからの編集を拒否するため、
//     サーバ側で保証される。クライアントの協調に依存しない。
//   - 同一ユーザの別プロセス: DPF-API は編集を拒否しない。一時レコード追加ロックの
//     取得と解放によって成立する。
//   - 同一プロセス内の別インスタンス: 重複の拒否は DPF-API 側で行われるため、
//     同一ユーザの別プロセスと同じ仕組みで排他される。
//
// ロックは再入できない。保持中に Lock を呼ぶと ErrStillLock になるため、保持期間を
// 延ばす場合は Renew を使う。1 つの Mutex は 1 つの保持者を表し、Renew と Unlock は
// 自分が保持者であることを確認してから行う。
//
// WithOwner に一意でない値を渡しても排他は壊れない（排他は奪ってよい時刻によって
// 成立する）。壊れるのは Renew と Unlock の宛先の判別だけである。
//
// Lock/Renew/Unlock はいずれもゾーン反映を行わない。Lock 後にレコードを編集し、
// Lock した SOA ごとゾーン反映することを想定している。反映後に Unlock すると
// レコードは編集可能な状態に戻り、再 Lock できる。
//
// 反映の方法によって注意点が異なる。
//
// PatchZoneChanges（編集中レコードのゾーン反映）は未反映の編集をすべて反映するため、
// SOA に書いたロックの情報も一緒に反映される。ラベルは反映後も残るため、続けて
// Unlock で解放できる。これが想定している使い方である。
//
// PatchZoneAtomicChanges（レコードの一括更新とゾーン反映）は、**ロックを保持したまま
// 使えない。** ロックのラベルは SOA の「未反映の編集」としてのみ存在し、一括更新は未反映の
// 編集を引き継がないためである。overwrite_soa の値は関係しない。この項目は「リクエストに
// 入れた SOA を取り込むか」を決めるだけで、未反映の編集を復活させるものではない。
//
// GetRecordCurrents は反映済みの姿を返すため、そこから組み立てたリクエストにロックの
// ラベルは含まれない。編集予定の SOA は「更新前の状態」(state=5) として返る。
// GetRecordList が編集予定 (state=3) の行を返すのとは見え方が異なる。
//
// 反映の完了後は他者が排他を取得できる状態になり、Unlock は ErrNotLockHolder を返す。
// **ゾーン全体を一括で置き換える場合は ZoneApplier を使うこと。** 取り込みの可否を決める
// フラグを固定し、反映の前に自動延長を止め、反映の後は排他が残っていた場合に限り解放
// するところまでを引き受ける。
//
// レコードを 1 つずつ変更してゾーンへ反映する流れでは、この制約は無い。反映によって
// 排他は解かれず、ラベルは反映後も残る。Mutex.Do の中で反映まで行えばよい。
//
// ただしこの流れには注意がある。**ゾーン反映によってロックのラベルは「反映済み」に
// なる。その後の Unlock は未反映の編集を作るため、これを破棄すると、反映済みの側に
// 残った排他（奪ってよい時刻が未来）が復活する。** ゾーンの未反映の編集を一括で破棄する
// 操作（DeleteZoneChanges など）を解放の後に行うと、保持期間が過ぎるまでゾーンが
// ロックされたままになる。解放は破棄ではなく反映によって確定させること。
//
// 排他は SOA レコードのラベルを 2 つ使う。1 レコードのラベル数の上限は 10 であるため、
// 利用者が SOA レコードへ自由に付けられるラベルは 8 個までである。上限を超える場合、
// Lock は書き込みを試みずに ErrLabelLimit を返す。
//
// 保持中に他者がゾーン反映を行うと、一時レコード追加ロックのための専用レコードが
// 権威サーバへ公開されることがある。その場合、次の Lock がそのレコードを削除予定に
// してから入れ直すことで自力で回復する。削除予定は取り消さないため、利用者が次に
// ゾーン反映した時点で公開されたレコードは消える。ライブラリからゾーン反映は行わない。
type Mutex struct {
	mu sync.Mutex

	cr               dpf.RecordsApi
	zoneID           string
	owner            string
	ttl              time.Duration
	lockRecordTTL    time.Duration
	verifyTimeout    time.Duration
	lockRecordLabel  string
	lockRecordRrtype dpf.RecordsRrtypeWithoutSoa
	lockRecordRdata  []string
	renewInterval    time.Duration
	lockWait         time.Duration

	// zoneName は SOA レコードから導いたゾーン名。2 回目以降は問い合わせを省く。
	zoneName string

	// pollInterval は反映待ちのポーリング間隔。テスト差し替え用。
	pollInterval time.Duration

	// now は現在時刻を返す。テスト差し替え用。
	now func() time.Time
}

// Option は Mutex の任意設定を変更する。
type Option func(*Mutex)

// WithOwner はロック作成者を指定する（デフォルト: ホスト名・PID・ランダム値から
// 生成した、インスタンスごとに一意な値）。空文字を渡した場合はデフォルトのままとする。
//
// 一意でない値を指定しても排他は壊れないが、Renew と Unlock が別のインスタンスの
// 排他を対象にしうる。複数のプログラムやインスタンスで同じ値を使わないこと。
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

// WithLockRecordTTL は一時レコード追加ロックが失効するまでの時間を指定する
// （デフォルト: DefaultLockRecordTTL 1分）。0 以下を渡した場合はデフォルトのままとする。
//
// この時間は、取得の途中でプログラムが異常終了した場合に、他者が専用レコードを
// 取り消して先へ進めるようになるまでの待ち時間である。
func WithLockRecordTTL(ttl time.Duration) Option {
	return func(m *Mutex) {
		if ttl > 0 {
			m.lockRecordTTL = ttl
		}
	}
}

// WithVerifyTimeout は書き込んだ内容が読み出せるまで待つ上限を指定する
// （デフォルト: DefaultVerifyTimeout 10秒）。0 以下を渡した場合はデフォルトのままとする。
//
// レコードの更新は非同期に処理されるため、Lock は書き込んだラベルが読み出せるまで待つ。
// この待機は、同時に書き込んだ別のプログラムに負けていないかの確認も兼ねる。
func WithVerifyTimeout(d time.Duration) Option {
	return func(m *Mutex) {
		if d > 0 {
			m.verifyTimeout = d
		}
	}
}

// WithLockRecordLabel は一時レコード追加ロックに使う専用レコードの最左ラベルを
// 指定する（デフォルト: DefaultLockRecordLabel `_dpf-go-lock`）。
// 空文字を渡した場合はデフォルトのままとする。
//
// 受け取るのは最左ラベルのみであり、レコード名はゾーン名と結合して組み立てる。
// ゾーンの範囲外の名前を指定できないようにするためである。
func WithLockRecordLabel(label string) Option {
	return func(m *Mutex) {
		if label != "" {
			m.lockRecordLabel = label
		}
	}
}

// WithLockRecordContent は専用レコードの RRTYPE と rdata を指定する
// （デフォルト: TXT と `"dpf-go lock"`）。rdata が空の場合はデフォルトのままとする。
//
// RRTYPE と rdata を対で受け取るのは、RRTYPE ごとに妥当な rdata が異なり、
// 独立に変更できないためである。rdata は排他の判定には用いない。
func WithLockRecordContent(rrtype dpf.RecordsRrtypeWithoutSoa, rdata []string) Option {
	return func(m *Mutex) {
		if len(rdata) > 0 {
			m.lockRecordRrtype = rrtype
			m.lockRecordRdata = rdata
		}
	}
}

// WithRenewInterval は Do の実行中に保持期間を延長する間隔を指定する
// （デフォルト: 保持期間の 1/3。既定の保持期間 15 分では 5 分）。
// 0 以下を渡した場合はデフォルトのままとする。
//
// 保持期間より十分短くすること。延長が一時的に失敗しても次の周期で間に合う
// ようにするためである。
func WithRenewInterval(d time.Duration) Option {
	return func(m *Mutex) {
		if d > 0 {
			m.renewInterval = d
		}
	}
}

// WithLockWait は Do が排他を取得できるまで待つ間隔を指定する
// （デフォルト: 待たない）。0 以下を渡した場合は待たない。
//
// 指定しない場合、Do は排他を取得できなければただちに ErrStillLock を返す。
// 指定した場合は、取得できるまでこの間隔で繰り返す。待機は呼び出し側の
// 打ち切りに従う。
func WithLockWait(d time.Duration) Option {
	return func(m *Mutex) {
		if d > 0 {
			m.lockWait = d
		}
	}
}

// NewMutex はゾーン zoneID に対するロックを生成する。
//   - cr     : レコード API（*dpf.RecordsAPIService が利用できる）
//   - zoneID : 対象ゾーンの ID
//
// 返された Mutex は 1 つの保持者を表す。同じインスタンスから並行して排他を取ることは
// できない（2 つ目は ErrStillLock になる）。並行して別々に排他を取る場合は
// インスタンスを分ける。
//
// owner と ttl は基本的にデフォルト値（owner: インスタンスごとに一意な値 / ttl: 15分）
// を使う。変更したい場合のみ Option を opts に渡す。
func NewMutex(cr dpf.RecordsApi, zoneID string, opts ...Option) *Mutex {
	m := &Mutex{
		cr:               cr,
		zoneID:           zoneID,
		owner:            newOwner(),
		ttl:              DefaultLockTTL,
		lockRecordTTL:    DefaultLockRecordTTL,
		verifyTimeout:    DefaultVerifyTimeout,
		lockRecordLabel:  DefaultLockRecordLabel,
		lockRecordRrtype: defaultLockRecordRrtype,
		lockRecordRdata:  defaultLockRecordRdata,
		pollInterval:     defaultPollInterval,
		now:              time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.renewInterval <= 0 {
		// 保持期間が切れるまでに 3 回の機会があるようにする。保持期間を変えた
		// 利用者に対しても比が崩れないよう、固定値ではなく比で導く。
		m.renewInterval = m.ttl / renewIntervalDivisor
	}
	return m
}

// Owner は排他の保持者を表す値を返す。診断のために公開している。
func (t *Mutex) Owner() string {
	return t.owner
}

// Lock はゾーンのロックを取得する。
//
// 手順は次の 4 段である。
//
//  1. SOA レコードのラベルによる事前判定。取得できないと分かった場合は、専用レコードを
//     作らずに ErrStillLock（またはラベル数の上限による ErrLabelLimit）を返す。
//  2. 一時レコード追加ロックの取得。
//  3. SOA レコードの再読み取りと判定、ラベルの書き込み、書き込んだ内容の確認。
//     事前判定の結果は用いない。判定と書き込みの間に他者が取得しうるためである。
//  4. 一時レコード追加ロックの解放。第 3 段の成否によらず行う。
//
// 取得できるのは、奪ってよい時刻を過ぎている場合、ラベルが無い場合、ラベルの値が
// 数値として解釈できない場合のみである。owner は判定に用いないため、自分自身が
// 保持している場合も ErrStillLock となる。保持期間を延ばす場合は Renew を使う。
//
// 復帰した時点で、書き込んだ内容が読み出せることを確認済みである。確認できない場合は
// 取得に失敗したものとして扱う。
func (t *Mutex) Lock(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 第 1 段: 事前判定。ゾーン名の取得も兼ねる。
	soa, err := t.getSOA(ctx)
	if err != nil {
		return err
	}
	name := t.lockRecordName(soa)
	if err := t.acquirable(soa.Labels); err != nil {
		return err
	}

	// 第 2 段: 一時レコード追加ロックの取得。
	held, err := t.acquireLockRecord(ctx, name)
	if err != nil {
		return err
	}

	// 第 3 段: 保護下での再判定と書き込み。
	lockErr := t.lockGuarded(ctx)

	// 第 4 段: 一時レコード追加ロックの解放。成否によらず行う。
	relErr := t.releaseLockRecord(ctx, name, held)

	if lockErr != nil {
		return lockErr
	}
	if relErr != nil {
		// 解放を確認できないまま成功を返すと、呼び出し側が排他を持っていると考えて
		// 編集を始めた後も、専用レコードが他者の取得を妨げ続ける。失敗として返し、
		// 書き込んだ排他も取り消して「エラーなら保持していない」を保つ。
		t.abandon(ctx)
		return relErr
	}
	return nil
}

// lockGuarded は一時レコード追加ロックの保護下で、SOA レコードのラベルへ排他を
// 書き込む。判定は事前判定の結果を使わず、ここで改めて行う。
func (t *Mutex) lockGuarded(ctx context.Context) error {
	soa, err := t.getSOA(ctx)
	if err != nil {
		return err
	}
	if err := t.acquirable(soa.Labels); err != nil {
		return err
	}

	deadline := strconv.FormatInt(t.now().Add(t.ttl).Unix(), 10)
	labels := cloneLabels(soa.Labels)
	labels[LockOwnerLabelKey] = t.owner
	labels[LockDeadlineLabelKey] = deadline

	if err := t.patchLabels(ctx, soa.Id, labels); err != nil {
		return err
	}
	return t.verify(ctx, deadline)
}

// abandon は書き込んだ排他を最善努力で取り消す。エラーは無視する。
// 「Lock がエラーを返したなら排他を保持していない」を保つための後始末である。
func (t *Mutex) abandon(ctx context.Context) {
	soa, err := t.getSOA(ctx)
	if err != nil {
		return
	}
	if soa.Labels[LockOwnerLabelKey] != t.owner {
		return
	}
	labels := cloneLabels(soa.Labels)
	labels[LockDeadlineLabelKey] = strconv.FormatInt(t.now().Unix(), 10)
	_ = t.patchLabels(ctx, soa.Id, labels)
}

// acquirable はラベルの状態から排他を取得できるかを判定する。
//
// ラベル数の上限を先に見る。上限は利用者が対処しなければ解消しない恒久的な状態で
// あり、LockWait で待ち続けても取得できないためである。
func (t *Mutex) acquirable(labels map[string]string) error {
	if labelCountAfterLock(labels) > maxRecordLabels {
		return ErrLabelLimit
	}
	if !t.lockable(labels) {
		return ErrStillLock
	}
	return nil
}

// labelCountAfterLock は排他のラベルを書き込んだ後のラベル数を返す。
// 既にロックのラベルが付いている場合は置き換えになるため増えない。
func labelCountAfterLock(labels map[string]string) int {
	n := len(labels)
	if _, ok := labels[LockOwnerLabelKey]; !ok {
		n++
	}
	if _, ok := labels[LockDeadlineLabelKey]; !ok {
		n++
	}
	return n
}

// lockable は現在のラベルからロック取得可能かを判定する。
// 次のいずれかを満たせばロック可能:
//   - deadline ラベルが存在しない
//   - deadline ラベルの値が数値として解釈できない（保持者不明として奪取を許す）
//   - deadline ラベルが存在し、奪っても良い時刻を過ぎている
//
// owner が自分自身かどうかは見ない。owner 一致で取得を許すと、同じ owner を持つ
// 呼び出しがすべてロックを通り抜けてしまい、排他が成立しないためである。
func (t *Mutex) lockable(labels map[string]string) bool {
	return t.expired(labels)
}

// expired は deadline ラベルが示す時刻を過ぎているかを返す。
// ラベルが無い場合、および値を解釈できない場合も、保持者不明として過ぎたものとして扱う。
func (t *Mutex) expired(labels map[string]string) bool {
	v, ok := labels[LockDeadlineLabelKey]
	if !ok {
		return true
	}
	deadline, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return true
	}
	return t.now().Unix() >= deadline
}

// verify は書き込んだ排他が実際に読み出せることを確認する。
//
// レコードの更新は非同期に処理されるため、書き込みの直後は反映されていない。加えて、
// 同一ユーザからの同時更新は DPF-API に拒否されないため、自分が書いた値がそのまま
// 残っているかどうかで競合に負けていないかを判定する。上限を過ぎても自分の値が
// 読み出せない場合は ErrStillLock を返す。
func (t *Mutex) verify(ctx context.Context, deadline string) error {
	for waited := time.Duration(0); ; waited += t.pollInterval {
		soa, err := t.getSOA(ctx)
		if err != nil {
			return err
		}
		if soa.Labels[LockOwnerLabelKey] == t.owner &&
			soa.Labels[LockDeadlineLabelKey] == deadline {
			return nil
		}
		if waited >= t.verifyTimeout {
			return ErrStillLock
		}
		if err := t.sleep(ctx); err != nil {
			return err
		}
	}
}

// sleep はポーリング間隔だけ待つ。ctx が打ち切られた場合はそのエラーを返す。
func (t *Mutex) sleep(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(t.pollInterval):
		return nil
	}
}

// Renew は保持している排他の奪ってよい時刻を、現在時刻 + TTL へ進める。
//
// 自分が保持者でない場合（延長の最中に他者へ奪われた場合を含む）は
// ErrNotLockHolder を返す。一時レコード追加ロックは取得しない。判定の対象が
// 「自分が保持者か」であり、競合しうる相手は奪ってよい時刻を過ぎたと判断して
// 奪った者だけで、それは延長が失敗すべき場合そのものであるためである。
func (t *Mutex) Renew(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	soa, err := t.getSOA(ctx)
	if err != nil {
		return err
	}
	if soa.Labels[LockOwnerLabelKey] != t.owner {
		return ErrNotLockHolder
	}

	deadline := strconv.FormatInt(t.now().Add(t.ttl).Unix(), 10)
	labels := cloneLabels(soa.Labels)
	labels[LockDeadlineLabelKey] = deadline

	if err := t.patchLabels(ctx, soa.Id, labels); err != nil {
		return err
	}
	if err := t.verify(ctx, deadline); err != nil {
		if errors.Is(err, ErrStillLock) {
			return ErrNotLockHolder
		}
		return err
	}
	return nil
}

// Unlock はロックを解放する。
// ロックを奪っても良い時刻(deadline)を現在時刻に更新することで、他者が即座に
// 奪取できるようにする。ロック(deadline ラベル)が存在しない場合は何もしない。
//
// 自分が保持者でない場合は、他者の排他を変更せずに ErrNotLockHolder を返す。
// defer から呼ぶ利用者が、保持中に奪われた事実を検知できるようにするためである。
func (t *Mutex) Unlock(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	soa, err := t.getSOA(ctx)
	if err != nil {
		return err
	}

	if _, ok := soa.Labels[LockDeadlineLabelKey]; !ok {
		return nil
	}
	if soa.Labels[LockOwnerLabelKey] != t.owner {
		return ErrNotLockHolder
	}

	labels := cloneLabels(soa.Labels)
	labels[LockDeadlineLabelKey] = strconv.FormatInt(t.now().Unix(), 10)

	return t.patchLabels(ctx, soa.Id, labels)
}

// LockWait はロックを取得できるまで Lock をリトライし続ける。
// interval で指定した間隔でリトライする。
// ErrStillLock 以外のエラーが発生した場合は、そのエラーを返して終了する。
// ctx がキャンセルされた場合は ctx.Err() を返す。
//
// 取得できない場合の Lock は事前判定だけで終わるため、1 回のリトライあたりの
// 問い合わせは 1 回である。
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

// hold は Do が保持している排他を表す。Do の実行中にのみ存在する。
//
// 一括置き換えのように「反映そのものが排他を解く」操作のために consume を持つ。
// これを公開すると、Do を使うすべての利用者が一括置き換えの都合を背負うことに
// なるため、型ごと非公開にしている。
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

// consume は、この先の操作が排他を解くことを Do へ伝える。
// 延長を止め、終了時の解放で ErrNotLockHolder を成功として扱うようにする。
//
// 一括置き換えを呼ぶ直前に呼ぶ。これより早いと編集中の延長が止まり、遅いと
// 反映中に延長が走って、作り直される SOA を掴もうとする。
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

// Do は排他を取得し、fn を実行し、終了時に解放する。
//
// fn へ渡される context は ctx から派生したものであり、**排他を他者に奪われた
// 時点で打ち切られる。** 保護が切れたことを知らないまま処理が走り続ける状態を
// 作らないためである。ただし打ち切りは通知であって強制的な中断ではない。
// fn が context を無視すれば走り続け、Do はその終了を待つ。
//
// 実行中、保持期間は自動で延長される（既定では保持期間の 1/3 ごと。
// WithRenewInterval で変更できる）。利用者が延長を書く必要はない。延長が
// 「保持者でない」以外の理由で失敗した場合は、次の周期で再試行する。
// **失敗が続く間は、排他が実際には失効しているのに fn が走りうる。** この窓の
// 長さは延長の間隔に依存する。
//
// 排他を取得できない場合、fn は呼ばれず ErrStillLock を返す。取得できるまで
// 待つ場合は WithLockWait を使う。
//
// 終了時は、自分が保持者である場合に限り解放する。fn が返したエラーは種類を
// 判別できる形のまま返す。排他を失った場合は ErrNotLockHolder を返し、fn も
// エラーを返していた場合は両方を判別できる形で返す。
//
// Do が復帰した時点で、延長のための goroutine は終了している。
//
// fn の異常終了（panic）は捕捉しない。この場合、解放は行われず、排他は保持期間の
// 経過によって解ける。
//
// レコードを 1 つずつ変更してゾーンへ反映する流れは、この操作で足りる。その流れ
// では反映によって排他が解かれないため、fn の中で反映まで行い、復帰後に解放される。
// ゾーン全体を一括で置き換える場合は ZoneApplier を使うこと。一括置き換えは排他を
// 解くため、この操作から直接呼ぶと終了時の解放が ErrNotLockHolder になる。
func (t *Mutex) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.doHold(ctx, func(ctx context.Context, _ *hold) error {
		return fn(ctx)
	})
}

// doHold は Do の実装。fn へ hold を渡すため非公開にしている。
func (t *Mutex) doHold(ctx context.Context, fn func(context.Context, *hold) error) error {
	if err := t.acquire(ctx); err != nil {
		return err
	}

	hctx, cancel := context.WithCancel(ctx)
	defer cancel()

	h := &hold{
		cancel: cancel,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go t.renewLoop(hctx, h)

	fnErr := fn(hctx, h)
	h.stopRenew()

	lostErr := h.lostError()
	relErr := t.release(ctx, h)

	switch {
	case lostErr != nil:
		// 排他を失ったことが根本原因である。解放のエラー（保持者でない）は
		// 同じことを言っているため重ねない。
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

// renewLoop は保持期間を周期的に延長する。Do の実行中のみ動く。
//
// 停止の合図または context の打ち切りを受けたら終了する。終了時に done を
// 閉じるため、stopRenew はこれを待てる。
func (t *Mutex) renewLoop(ctx context.Context, h *hold) {
	defer close(h.done)

	ticker := time.NewTicker(t.renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := t.Renew(ctx)
			if err == nil {
				continue
			}
			if errors.Is(err, ErrNotLockHolder) {
				// 他者に奪われた。保護が切れたことを処理へ伝えて終了する。
				h.lost(err)
				h.cancel()
				return
			}
			// それ以外（通信の失敗など）は次の周期で再試行する。一時的な失敗で
			// 編集作業を落とさないためである。保持期間が切れれば他者に奪われ、
			// 上の経路で中止される。
		}
	}
}

// acquire は排他を取得する。待機の指定があれば取得できるまで繰り返す。
func (t *Mutex) acquire(ctx context.Context) error {
	if t.lockWait > 0 {
		return t.LockWait(ctx, t.lockWait)
	}
	return t.Lock(ctx)
}

// release は終了時の解放を行う。
//
// Unlock は保持者でない場合に ErrNotLockHolder を返すため、「自分が保持者である
// 場合に限り解放する」はこれで満たされる。consume されている場合（一括置き換えに
// よって排他が解かれた場合）は、残っていた場合に限り解放するという意味になるため、
// ErrNotLockHolder を成功として扱う。
func (t *Mutex) release(ctx context.Context, h *hold) error {
	err := t.Unlock(ctx)
	if err == nil {
		return nil
	}
	if h.isConsumed() && errors.Is(err, ErrNotLockHolder) {
		return nil
	}
	return err
}

// getSOA は排他の判定に用いる SOA レコードを返す。
//
// 編集予定(state=3)の行があればそれを、無ければ反映済み(state=0)の行を返す。
// 排他を保持している間の SOA は編集予定であり、反映済みの行には排他のラベルが
// 付いていないためである。反映済みを優先すると、保持中の排他を他者へ渡してしまう。
// どちらも見つからない場合、および同じ state の行が複数ある場合は、判定材料が
// 一意に決まらないためエラーを返す。
func (t *Mutex) getSOA(ctx context.Context) (*dpf.Record, error) {
	records, _, err := t.cr.GetRecordList(ctx, t.zoneID).
		KeywordsRrtype(dpf.RECORDSRRTYPE_SOA).
		ExecuteAll()
	if err != nil {
		return nil, err
	}
	if records == nil {
		return nil, ErrRecordNotFound
	}

	var editing, applied []*dpf.Record
	for i := range records.Results {
		r := &records.Results[i]
		if r.Rrtype != dpf.RECORDSRRTYPE_SOA {
			continue
		}
		switch r.State {
		case dpf.RECORDSSTATE__3:
			editing = append(editing, r)
		case dpf.RECORDSSTATE__0:
			applied = append(applied, r)
		}
	}

	for _, candidates := range [][]*dpf.Record{editing, applied} {
		switch len(candidates) {
		case 0:
			continue
		case 1:
			return candidates[0], nil
		default:
			return nil, fmt.Errorf("dpf: SOA レコードが state=%d で %d 件見つかった。排他の判定材料が一意に決まらない",
				candidates[0].State, len(candidates))
		}
	}
	return nil, ErrRecordNotFound
}

// patchLabels はレコードのラベルを更新し、編集予定状態にする。
// 更新は非同期に処理されるため、反映の確認は verify で行う。
func (t *Mutex) patchLabels(ctx context.Context, recordID string, labels map[string]string) error {
	body := dpf.PatchRecord{Labels: &labels}
	_, _, err := t.cr.PatchRecord(ctx, t.zoneID, recordID).PatchRecord(body).Execute()
	return err
}

// lockRecordName は専用レコードの FQDN を返す。
//
// ゾーン名は SOA レコードの name から導く。SOA レコードはゾーンの apex に 1 つだけ
// 存在するため、その名前はゾーン名そのものである。結合と正規化は miekg/dns で行い、
// 最左ラベルのみを受け取るためゾーンの範囲外にはならない。
func (t *Mutex) lockRecordName(soa *dpf.Record) string {
	if t.zoneName == "" {
		t.zoneName = dns.Fqdn(soa.Name)
	}
	return t.lockRecordLabel + "." + t.zoneName
}

// acquireLockRecord は一時レコード追加ロックを取得し、自分が作った専用レコードの
// ID を返す。
//
// 追加が重複として拒否された場合は、既存の専用レコードの状態に応じて 1 度だけ
// 回復を試みる。回復してもなお拒否された場合は、他者が先に取得したものとして
// ErrStillLock を返す。
func (t *Mutex) acquireLockRecord(ctx context.Context, name string) (string, error) {
	if err := t.postLockRecord(ctx, name); err != nil {
		if !hasErrorDetail(err, "duplicated", "record") {
			return "", err
		}
		if err := t.clearLockRecord(ctx, name); err != nil {
			return "", err
		}
		if err := t.postLockRecord(ctx, name); err != nil {
			if hasErrorDetail(err, "duplicated", "record") {
				return "", ErrStillLock
			}
			return "", err
		}
	}
	return t.settleLockRecord(ctx, name)
}

// postLockRecord は専用レコードを追加予定の状態で作成する。
func (t *Mutex) postLockRecord(ctx context.Context, name string) error {
	ttl := int32(300)
	rdata := make([]dpf.RecordsRdataInner, 0, len(t.lockRecordRdata))
	for _, v := range t.lockRecordRdata {
		rdata = append(rdata, dpf.RecordsRdataInner{Value: dpf.PtrString(v)})
	}
	labels := map[string]string{
		LockOwnerLabelKey:    t.owner,
		LockDeadlineLabelKey: strconv.FormatInt(t.now().Add(t.lockRecordTTL).Unix(), 10),
	}
	body := dpf.PostRecord{
		Name:   name,
		Ttl:    *dpf.NewNullableInt32(&ttl),
		Rrtype: t.lockRecordRrtype,
		Rdata:  rdata,
		Labels: &labels,
	}
	_, _, err := t.cr.PostRecord(ctx, t.zoneID).PostRecord(body).Execute()
	return err
}

// lockRecords は専用レコードを state 込みで返す。
func (t *Mutex) lockRecords(ctx context.Context, name string) ([]dpf.Record, error) {
	rrtype := dpf.RecordsRrtype(t.lockRecordRrtype)
	records, _, err := t.cr.GetRecordList(ctx, t.zoneID).
		KeywordsName([]string{name}).
		KeywordsRrtype(rrtype).
		ExecuteAll()
	if err != nil {
		return nil, err
	}
	if records == nil {
		return nil, nil
	}

	want := dns.CanonicalName(name)
	var out []dpf.Record
	for _, r := range records.Results {
		if r.Rrtype == rrtype && dns.CanonicalName(r.Name) == want {
			out = append(out, r)
		}
	}
	return out, nil
}

// clearLockRecord は追加を妨げている専用レコードを取り除く。
//
//   - 追加予定で失効時刻を過ぎていない: 他者が保持中である。ErrStillLock を返す
//   - 追加予定で失効時刻を過ぎている: 追加予定を取り消す
//   - 反映済み: 削除予定にする。削除予定は取り消さないため、利用者が次にゾーン反映
//     した時点で公開されたレコードが消える
//
// 取り消しや削除が「対象が存在しない」「レコードの状態が違う」として拒否された場合は、
// 他者が先に同じ回復を行ったものとして扱い、失敗とせずに追加へ進む。
func (t *Mutex) clearLockRecord(ctx context.Context, name string) error {
	records, err := t.lockRecords(ctx, name)
	if err != nil {
		return err
	}

	cleared := false
	for i := range records {
		r := &records[i]
		switch r.State {
		case dpf.RECORDSSTATE__1:
			if !t.expired(r.Labels) {
				return ErrStillLock
			}
			if err := t.cancelRecord(ctx, r.Id); err != nil {
				return err
			}
			cleared = true
		case dpf.RECORDSSTATE__0:
			if err := t.deleteRecord(ctx, r.Id); err != nil {
				return err
			}
			cleared = true
		}
	}

	if !cleared {
		// 重複の原因が見当たらない。他者が同時に取得と解放を行っている可能性が
		// あるため、この試行は諦める。
		return ErrStillLock
	}
	return nil
}

// cancelRecord は追加予定・編集予定の取り消しを行う。
func (t *Mutex) cancelRecord(ctx context.Context, recordID string) error {
	_, _, err := t.cr.DeleteRecordChanges(ctx, t.zoneID, recordID).Execute()
	if err != nil && isRecordGone(err) {
		return nil
	}
	return err
}

// deleteRecord は反映済みレコードを削除予定にする。
func (t *Mutex) deleteRecord(ctx context.Context, recordID string) error {
	_, _, err := t.cr.DeleteRecord(ctx, t.zoneID, recordID).Execute()
	if err != nil && isRecordGone(err) {
		return nil
	}
	return err
}

// settleLockRecord は自分の追加予定が確立するまで待ち、専用レコードの ID を返す。
//
// 追加が受理されてから追加予定が実際に立つまでの間に別の追加が受理されると、同名
// 同 RRTYPE の追加予定が複数並ぶ。このとき双方が譲るとどちらも取得できず、双方が
// 続行すると排他が破れる。そこで ID の昇順で最小の 1 件を勝者とする。全員が同じ
// 一覧を読めば同じ勝者を選ぶため、結論が一致する。
func (t *Mutex) settleLockRecord(ctx context.Context, name string) (string, error) {
	for waited := time.Duration(0); ; waited += t.pollInterval {
		records, err := t.lockRecords(ctx, name)
		if err != nil {
			return "", err
		}

		var pending []dpf.Record
		for _, r := range records {
			if r.State == dpf.RECORDSSTATE__1 {
				pending = append(pending, r)
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].Id < pending[j].Id })

		mine := ""
		for _, r := range pending {
			if r.Labels[LockOwnerLabelKey] == t.owner {
				mine = r.Id
				break
			}
		}
		if mine != "" {
			if pending[0].Id == mine {
				return mine, nil
			}
			// 敗者。自分の追加予定を取り消して譲る。
			if err := t.cancelRecord(ctx, mine); err != nil {
				return "", err
			}
			return "", ErrStillLock
		}

		if waited >= t.verifyTimeout {
			// 自分の追加予定が確立しなかった。作成が反映された場合は失効時刻の
			// 経過によって他者が取り消せる。
			return "", ErrStillLock
		}
		if err := t.sleep(ctx); err != nil {
			return "", err
		}
	}
}

// releaseLockRecord は一時レコード追加ロックを解放する。
//
// 追加予定を取り消し、実際に消えたことを確認する。ゾーン反映は行わない。
// 確認できない場合はエラーを返す。専用レコードは失効時刻を持つため、他者は失効後に
// 取り消して取得できる。
func (t *Mutex) releaseLockRecord(ctx context.Context, name, recordID string) error {
	if recordID == "" {
		return nil
	}
	if err := t.cancelRecord(ctx, recordID); err != nil {
		return err
	}

	for waited := time.Duration(0); ; waited += t.pollInterval {
		records, err := t.lockRecords(ctx, name)
		if err != nil {
			return err
		}
		found := false
		for _, r := range records {
			if r.Id == recordID {
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		if waited >= t.verifyTimeout {
			return fmt.Errorf("dpf: 一時レコード追加ロック(%s)の解放を確認できない", recordID)
		}
		if err := t.sleep(ctx); err != nil {
			return err
		}
	}
}

// isRecordGone は、対象のレコードが既に無い（他者が先に同じ回復を行った）ことを
// 示すエラーかを返す。
func isRecordGone(err error) bool {
	return hasErrorDetail(err, "not_found", "record") ||
		hasErrorDetail(err, "forbidden", "record")
}

// hasErrorDetail は API のエラー応答に指定した code / attribute の error_details が
// 含まれるかを返す。
func hasErrorDetail(err error, code, attribute string) bool {
	var apiErr *dpf.GenericOpenAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	m, ok := apiErr.Model().(dpf.ParameterErrorResponse)
	if !ok || m.ParameterError == nil {
		return false
	}
	for _, d := range m.ParameterError.ErrorDetails {
		if d.Code == code && d.Attribute == attribute {
			return true
		}
	}
	return false
}

// cloneLabels はラベルマップを複製する（nil の場合も空マップを返す）。
func cloneLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
