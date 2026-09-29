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
	"unicode/utf8"

	dpf "github.com/iij/dpf-go"
	"github.com/miekg/dns"
)

// ZoneLockLabelKey はゾーンの排他を格納する、ゾーンのラベルのキー。
//
// 値は「保持者」と「奪ってよい時刻」を 1 つに詰めた形である（形式は lockValue を参照）。
// **ゾーンのラベルは未反映の編集の概念を持たない。** レコードのラベルと異なり、書き込みに
// ゾーン反映を必要としない。排他が消費するゾーンのラベルはこの 1 つだけである。
const ZoneLockLabelKey = "lock.dpf-go"

// 一時レコード追加ロックの専用レコードに付けるラベルのキー。
// レコードのラベルには JSON を保存できないため、owner と deadline を別々のキーへ分ける。
// **ゾーンの排他はこれらを使わない**（ZoneLockLabelKey に 1 つへ詰める）。
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
	// ErrLabelLimit は、ゾーンのラベル数が上限に達しており、排他のためのラベルを
	// 追加できない場合に返される。排他はゾーンのラベルを 1 つ使うため、利用者が
	// ゾーンへ自由に付けられるラベルは 9 個までである。
	//
	// 待っても解消しない状態であるため、取得を待つ指定があっても待たない。
	ErrLabelLimit = errors.New("dpf: zone label limit reached")
	// ErrOwnerTooLong は、保持者の文字数が上限を超えている場合に返される。
	// 保持者は 1 文字以上 32 文字以下でなければならない。
	//
	// **黙って切り詰めることはしない。** 切り詰めると別のインスタンスと同じ保持者に
	// なり、延長と解放の宛先の判別が壊れるためである。待っても解消しない状態であるため、
	// 取得を待つ指定があっても待たない。
	ErrOwnerTooLong = errors.New("dpf: lock owner too long")
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

	// maxZoneLabels は 1 ゾーンに付けられるラベル数の上限（DPF-API の仕様）。
	maxZoneLabels = 10
	// maxLabelValueLen はラベル値の最大長（DPF-API の仕様）。
	maxLabelValueLen = 63
	// maxOwnerLen は保持者の最大文字数。奪ってよい時刻（10 桁）と区切り（1 文字）を
	// 足しても maxLabelValueLen に収まる範囲で定めている（32 + 1 + 10 = 43）。
	maxOwnerLen = 32
	// ownerHostLen は既定の保持者がホスト名へ割り当てる文字数。**固定である。**
	// 接尾辞の実長で決めると PID の桁数によってホスト名の残り方が実行ごとに変わり、
	// 同じホストなのに別の文字列に見える。診断で突き合わせにくいため固定する。
	ownerHostLen = 15
	// lockDeadlineDigits は値に詰める奪ってよい時刻の桁数。Unix 秒は 2286 年まで
	// 10 桁である。10 桁に満たない場合は 0 で詰め、常にこの幅で書く。
	lockDeadlineDigits = 10
	// lockValueSep は保持者と奪ってよい時刻の区切り。
	//
	// 値は**右端から固定幅で読む**ため、保持者の側にこの文字が含まれていても曖昧に
	// ならない。既定の保持者自身がこの文字を含むため、含めない制約は置けない。
	lockValueSep = '-'
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

	return truncateHost(host) + suffix
}

// truncateHost はホスト名を既定の保持者の枠（ownerHostLen）へ収める。
//
// **先頭を残して右側を落とす。** 枠を固定するのは、接尾辞の実長で決めると PID の桁数に
// よってホスト名の残り方が実行ごとに変わり、同じホストなのに別の文字列に見えるためで
// ある。
func truncateHost(host string) string {
	if len(host) > ownerHostLen {
		return host[:ownerHostLen]
	}
	return host
}

// lockValue は排他の状態を 1 つのラベルの値へ詰める。
//
//	<保持者><区切り><奪ってよい時刻の 10 桁>
//	例: web01.tokyo-12345-a1b2c3d4-1790319252
func lockValue(owner string, deadline time.Time) string {
	return fmt.Sprintf("%s%c%0*d", owner, lockValueSep, lockDeadlineDigits, deadline.Unix())
}

// parseLockValue は lockValue が詰めた値を読み出す。
//
// **右端から固定幅で読む。** 末尾 lockDeadlineDigits 文字を奪ってよい時刻、その直前の
// 1 文字を区切りとして確かめ、残りを保持者とする。左から区切りを探さないため、保持者に
// 区切りと同じ文字が含まれていても曖昧にならない。
//
// 形式を満たさない場合は ok=false を返す。呼び出し側はこれを「排他が成立していない」と
// して扱う。解釈できない値に引きずられて、ゾーンが永久に取得できない状態にしないためで
// ある。
func parseLockValue(v string) (owner string, deadline int64, ok bool) {
	// 保持者 1 文字以上 + 区切り 1 文字 + 時刻 10 文字。
	if len(v) < 1+1+lockDeadlineDigits {
		return "", 0, false
	}
	sepAt := len(v) - lockDeadlineDigits - 1
	if v[sepAt] != lockValueSep {
		return "", 0, false
	}
	tail := v[sepAt+1:]
	for i := 0; i < len(tail); i++ {
		if tail[i] < '0' || tail[i] > '9' {
			return "", 0, false
		}
	}
	d, err := strconv.ParseInt(tail, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return v[:sepAt], d, true
}

// Mutex はゾーン単位のロックを表す。
//
// 対象ゾーンのラベル（ZoneLockLabelKey）に保持者と奪ってよい時刻を書き込むことで
// ロックする。ラベルの読み取りから書き込みまでの区間は、専用レコードの追加（一時レコード
// 追加ロック）で排他する。DPF-API はレコードの新規追加に対して同名かつ同 RRTYPE の重複を
// 拒否し、この拒否は編集者が誰かに依らないためである。
//
// # 排他の効く相手
//
// **2 者が同時に取得することはない。** 一時レコード追加ロックの重複の拒否は DPF-API が
// 行うため、**ユーザをまたいで機械的に効く。** 同一ユーザの別プロセス、同一プロセス内の
// 別インスタンスも同じ仕組みで排他される。
//
// **一方、保持中に他ユーザや管理画面からレコードを編集することは止められない。** 保持中の
// 尊重は、奪ってよい時刻を読んで譲るという協調によって成り立つ。本ライブラリ（または同じ
// ラベルを読む実装）を使わない相手には効かない。2 つの方式の比較はパッケージ文書を参照。
//
// # ゾーンに残るもの
//
// **排他はゾーンのラベルを 1 つ消費する。** ゾーンのラベル数の上限は 10 であるため、
// 利用者がゾーンへ自由に付けられるラベルは 9 個までである。上限を超える場合、Lock は
// 書き込みを試みずに ErrLabelLimit を返す。
//
// **解放してもラベルは残る。** Unlock は奪ってよい時刻を現在時刻へ更新するだけであり、
// ラベルを削除しない（削除すると「一度も取得されていない」状態と区別が付かなくなる）。
// したがって一度でも排他を取得したゾーンは、以降そのラベルを保持し続ける。
//
// **レコードのラベルは消費しない。** 利用者は SOA レコードのラベルを 10 個すべて使える。
//
// # ゾーン反映との関係
//
// Lock/Renew/Unlock はいずれもゾーン反映を行わず、**ゾーンに未反映の編集を作らない。**
// ゾーンのラベルは未反映の編集の概念を持たないためである。取得・延長・解放は権威サーバへ
// 公開されるデータにも影響しない。
//
// **レコードの一括更新とゾーン反映（PatchZoneAtomicChanges）は排他に影響しない。** 置き換え
// の対象はレコードであり、ゾーンのラベルは別のリソースであるためである。反映の後も排他は
// 保持され、通常どおり解放できる。ゾーン全体を一括で置き換える場合は、取り込みの可否を
// 決めるフラグの固定と反映の完了待ちを引き受ける ZoneApplier を使うとよい。
//
// # 保持者
//
// **保持者は 1 文字以上 32 文字以下でなければならない。** 超える場合、Lock は書き込みを
// 試みずに ErrOwnerTooLong を返す。**黙って切り詰めることはしない**（切り詰めると別の
// インスタンスと同じ保持者になり、Renew と Unlock の宛先の判別が壊れる）。文字の種類に
// 制約は無く、区切りと同じ文字を含んでいてもよい。
//
// WithOwner に一意でない値を渡しても排他は壊れない（排他は奪ってよい時刻によって
// 成立する）。壊れるのは Renew と Unlock の宛先の判別だけである。
//
// # 旧版との混在
//
// **v0.5.0 以前は排他の状態を SOA レコードのラベルへ書いていた。** 新旧が同じゾーンを
// 触ると、互いの排他を認識できず、両方が取得に成功しうる。ライブラリはこの混在を検出
// できないため、**同じゾーンを触るプログラムはまとめて更新すること。**
//
// 旧版が SOA レコードへ残したラベルと未反映の編集は、本実装では掃除しない。必要であれば
// 利用者の側で取り除く。
//
// # その他
//
// ロックは再入できない。保持中に Lock を呼ぶと ErrStillLock になるため、保持期間を
// 延ばす場合は Renew を使う。1 つの Mutex は 1 つの保持者を表し、Renew と Unlock は
// 自分が保持者であることを確認してから行う。
//
// Mutex は Locker を満たす。排他の仕組みを差し替えたい場合は Locker を実装し、
// ZoneApplier の WithLocker や RunLocked へ渡す。
//
// 保持中に他者がゾーン反映を行うと、一時レコード追加ロックのための専用レコードが
// 権威サーバへ公開されることがある。その場合、次の Lock がそのレコードを削除予定に
// してから入れ直すことで自力で回復する。削除予定は取り消さないため、利用者が次に
// ゾーン反映した時点で公開されたレコードは消える。ライブラリからゾーン反映は行わない。
type Mutex struct {
	mu sync.Mutex

	cr               dpf.RecordsApi
	cz               dpf.ZonesApi
	zoneID           string
	owner            string
	ttl              time.Duration
	lockRecordTTL    time.Duration
	verifyTimeout    time.Duration
	lockRecordLabel  string
	lockRecordRrtype dpf.RecordsRrtypeWithoutSoa
	lockRecordRdata  []string
	renewInterval    time.Duration

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

// WithRenewInterval は、この排他が申告する延長の間隔を指定する
// （デフォルト: 保持期間の 1/3。既定の保持期間 15 分では 5 分）。
// 0 以下を渡した場合はデフォルトのままとする。
//
// RunLocked（および Do）は、この値を延長の間隔として使う（RenewIntervaler）。
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

// NewMutex はゾーン zoneID に対するロックを生成する。
//   - cr     : レコード API（*dpf.RecordsAPIService が利用できる）
//   - cz     : ゾーン API（*dpf.ZonesAPIService が利用できる）
//   - zoneID : 対象ゾーンの ID
//
// 返された Mutex は 1 つの保持者を表す。同じインスタンスから並行して排他を取ることは
// できない（2 つ目は ErrStillLock になる）。並行して別々に排他を取る場合は
// インスタンスを分ける。
//
// owner と ttl は基本的にデフォルト値（owner: インスタンスごとに一意な値 / ttl: 15分）
// を使う。変更したい場合のみ Option を opts に渡す。
func NewMutex(cr dpf.RecordsApi, cz dpf.ZonesApi, zoneID string, opts ...Option) *Mutex {
	m := &Mutex{
		cr:               cr,
		cz:               cz,
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
//  1. ゾーンのラベルによる事前判定。取得できないと分かった場合は、専用レコードを
//     作らずに ErrStillLock（またはラベル数の上限による ErrLabelLimit）を返す。
//  2. 一時レコード追加ロックの取得。
//  3. ゾーンのラベルの再読み取りと判定、書き込み、書き込んだ内容の確認。
//     事前判定の結果は用いない。判定と書き込みの間に他者が取得しうるためである。
//  4. 一時レコード追加ロックの解放。第 3 段の成否によらず行う。
//
// 保持者が 32 文字を超える場合は、いずれの段にも入らず ErrOwnerTooLong を返す。
//
// 取得できるのは、奪ってよい時刻を過ぎている場合、ラベルが無い場合、ラベルの値が
// 形式を満たさない場合のみである。owner は判定に用いないため、自分自身が
// 保持している場合も ErrStillLock となる。保持期間を延ばす場合は Renew を使う。
//
// 復帰した時点で、書き込んだ内容が読み出せることを確認済みである。確認できない場合は
// 取得に失敗したものとして扱う。
func (t *Mutex) Lock(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// 保持者の検証。書き込みを試みる前に返す。
	if err := t.validateOwner(); err != nil {
		return err
	}

	// 第 1 段: 事前判定。
	labels, err := t.zoneLabels(ctx)
	if err != nil {
		return err
	}
	if err := t.acquirable(labels); err != nil {
		return err
	}

	// 第 2 段: 一時レコード追加ロックの取得。ゾーン名はここで初めて必要になるため、
	// 取得できないと分かった場合は SOA を読まずに戻る。
	name, err := t.lockRecordName(ctx)
	if err != nil {
		return err
	}
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

// lockGuarded は一時レコード追加ロックの保護下で、ゾーンのラベルへ排他を書き込む。
// 判定は事前判定の結果を使わず、ここで改めて行う。
//
// 書き込みは読み取ったラベルをすべて含めて行う。ゾーンのラベルの更新はマップ全体の
// 置き換えであり、排他のラベルだけを渡すと利用者のラベルが消えるためである。
func (t *Mutex) lockGuarded(ctx context.Context) error {
	labels, err := t.zoneLabels(ctx)
	if err != nil {
		return err
	}
	if err := t.acquirable(labels); err != nil {
		return err
	}

	value := lockValue(t.owner, t.now().Add(t.ttl))
	labels[ZoneLockLabelKey] = value

	if err := t.putZoneLabels(ctx, labels); err != nil {
		return err
	}
	return t.verify(ctx, value)
}

// abandon は書き込んだ排他を最善努力で取り消す。エラーは無視する。
// 「Lock がエラーを返したなら排他を保持していない」を保つための後始末である。
func (t *Mutex) abandon(ctx context.Context) {
	labels, err := t.zoneLabels(ctx)
	if err != nil {
		return
	}
	owner, _, ok := parseLockValue(labels[ZoneLockLabelKey])
	if !ok || owner != t.owner {
		return
	}
	labels[ZoneLockLabelKey] = lockValue(t.owner, t.now())
	_ = t.putZoneLabels(ctx, labels)
}

// validateOwner は保持者が値へ詰められる長さかを確かめる。
// 文字数で数える（利用者が指定できる値であり、文字単位で示す方が扱いやすい）。
func (t *Mutex) validateOwner() error {
	if utf8.RuneCountInString(t.owner) > maxOwnerLen {
		return ErrOwnerTooLong
	}
	return nil
}

// acquirable はラベルの状態から排他を取得できるかを判定する。
//
// ラベル数の上限を先に見る。上限は利用者が対処しなければ解消しない恒久的な状態で
// あり、LockWait で待ち続けても取得できないためである。
func (t *Mutex) acquirable(labels map[string]string) error {
	if labelCountAfterLock(labels) > maxZoneLabels {
		return ErrLabelLimit
	}
	if !t.lockable(labels) {
		return ErrStillLock
	}
	return nil
}

// labelCountAfterLock は排他のラベルを書き込んだ後のゾーンのラベル数を返す。
// 既に排他のラベルが付いている場合は置き換えになるため増えない。
func labelCountAfterLock(labels map[string]string) int {
	n := len(labels)
	if _, ok := labels[ZoneLockLabelKey]; !ok {
		n++
	}
	return n
}

// lockable は現在のラベルからロック取得可能かを判定する。
// 次のいずれかを満たせばロック可能:
//   - 排他のラベルが存在しない
//   - 排他のラベルの値が形式を満たさない（保持者不明として奪取を許す）
//   - 排他のラベルが存在し、奪っても良い時刻を過ぎている
//
// owner が自分自身かどうかは見ない。owner 一致で取得を許すと、同じ owner を持つ
// 呼び出しがすべてロックを通り抜けてしまい、排他が成立しないためである。
func (t *Mutex) lockable(labels map[string]string) bool {
	return t.expired(labels)
}

// expired は排他のラベルが示す時刻を過ぎているかを返す。
// ラベルが無い場合、および値の形式を満たさない場合も、保持者不明として過ぎたものと
// して扱う。
func (t *Mutex) expired(labels map[string]string) bool {
	_, deadline, ok := parseLockValue(labels[ZoneLockLabelKey])
	if !ok {
		return true
	}
	return t.now().Unix() >= deadline
}

// verify は書き込んだ排他が実際に読み出せることを確認する。
//
// ゾーンのラベルの更新は非同期に処理されるため、書き込みの直後は反映されていない。
// 加えて、同一ユーザからの同時更新は DPF-API に拒否されないため、自分が書いた値が
// そのまま残っているかどうかで競合に負けていないかを判定する。上限を過ぎても自分の値が
// 読み出せない場合は ErrStillLock を返す。
//
// JOB の待ち合わせではなく読み戻しで確認するのは、「非同期の処理が終わったか」と
// 「同時に書き込んだ他者に負けていないか」を同時に確かめられるためである。
func (t *Mutex) verify(ctx context.Context, want string) error {
	for waited := time.Duration(0); ; waited += t.pollInterval {
		labels, err := t.zoneLabels(ctx)
		if err != nil {
			return err
		}
		if labels[ZoneLockLabelKey] == want {
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

	labels, err := t.zoneLabels(ctx)
	if err != nil {
		return err
	}
	owner, _, ok := parseLockValue(labels[ZoneLockLabelKey])
	if !ok || owner != t.owner {
		return ErrNotLockHolder
	}

	value := lockValue(t.owner, t.now().Add(t.ttl))
	labels[ZoneLockLabelKey] = value

	if err := t.putZoneLabels(ctx, labels); err != nil {
		return err
	}
	if err := t.verify(ctx, value); err != nil {
		if errors.Is(err, ErrStillLock) {
			return ErrNotLockHolder
		}
		return err
	}
	return nil
}

// Unlock はロックを解放する。
// ロックを奪っても良い時刻(deadline)を現在時刻に更新することで、他者が即座に
// 奪取できるようにする。排他のラベルが存在しない場合は何もしない。
//
// **ラベルは削除しない。** 削除すると「一度も取得されていない」状態と区別が付かなく
// なるためである。したがって一度でも排他を取得したゾーンは、解放の後もゾーンのラベルを
// 1 つ保持し続ける。
//
// **解放の確定は非同期である。** ゾーンのラベルの更新は非同期に処理され、Unlock は
// 書き込みの確定を確認しない（Lock と違い、待つ必要が無いためである）。したがって
// **復帰した直後は、他者からまだ保持されているように見えることがある。** 解放を見届けて
// から次の操作へ進みたい場合は、ラベルを読んで奪ってよい時刻が過去になったことを確かめる
// こと。2026-09-29 に実 API で観測した。
//
// 自分が保持者でない場合は、他者の排他を変更せずに ErrNotLockHolder を返す。
// defer から呼ぶ利用者が、保持中に奪われた事実を検知できるようにするためである。
func (t *Mutex) Unlock(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	labels, err := t.zoneLabels(ctx)
	if err != nil {
		return err
	}

	v, exists := labels[ZoneLockLabelKey]
	if !exists {
		return nil
	}
	owner, _, ok := parseLockValue(v)
	if !ok || owner != t.owner {
		return ErrNotLockHolder
	}

	labels[ZoneLockLabelKey] = lockValue(t.owner, t.now())

	return t.putZoneLabels(ctx, labels)
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

// Do は排他を取得し、fn を実行し、終了時に解放する。RunLocked の薄い包みである。
//
// 振る舞いの詳細は RunLocked を参照。この Mutex は延長の間隔として保持期間の 1/3 を
// 申告するため、利用者が間隔を指定する必要はない。
func (t *Mutex) Do(ctx context.Context, fn func(ctx context.Context) error, opts ...HoldOption) error {
	return RunLocked(ctx, t, fn, opts...)
}

// RenewInterval は延長の間隔として保持期間の 1/3 を返す（RenewIntervaler）。
// WithRenewInterval で変更できる。
func (t *Mutex) RenewInterval() time.Duration {
	return t.renewInterval
}

// zoneLabels はゾーンのラベルを読み、複製を返す。
//
// 複製を返すのは、呼び出し側が排他のラベルを足して書き戻すためである。ゾーンのラベルの
// 更新はマップ全体の置き換えであり、**読み取ったラベルをすべて含めて書き戻さなければ
// 利用者のラベルが消える。**
func (t *Mutex) zoneLabels(ctx context.Context) (map[string]string, error) {
	res, _, err := t.cz.GetZoneLabels(ctx, t.zoneID).Execute()
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, ErrZoneNotFound
	}
	return cloneLabels(res.Result.Labels), nil
}

// putZoneLabels はゾーンのラベルを更新する。
// 更新は非同期に処理されるため、確定の確認は verify で行う。
func (t *Mutex) putZoneLabels(ctx context.Context, labels map[string]string) error {
	_, _, err := t.cz.PutZoneLabels(ctx, t.zoneID).
		ZoneLabels(dpf.ZoneLabels{Labels: labels}).Execute()
	return err
}

// getSOA はゾーン名を得るための SOA レコードを返す。
//
// **排他の判定には用いない。** 排他の状態はゾーンのラベルにあり、SOA を読む理由は
// 専用レコードの名前に必要なゾーン名だけである。SOA はゾーンの apex に 1 つだけ存在し、
// 編集予定と反映済みの行が並存していても名前は同じであるため、最初に見つかった行を
// 返す。見つからない場合は ErrRecordNotFound を返す。
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

	for i := range records.Results {
		r := &records.Results[i]
		if r.Rrtype == dpf.RECORDSRRTYPE_SOA {
			return r, nil
		}
	}
	return nil, ErrRecordNotFound
}

// lockRecordName は専用レコードの FQDN を返す。
//
// ゾーン名は SOA レコードの name から導く。SOA レコードはゾーンの apex に 1 つだけ
// 存在するため、その名前はゾーン名そのものである。結合と正規化は miekg/dns で行い、
// 最左ラベルのみを受け取るためゾーンの範囲外にはならない。
//
// **一度得たゾーン名は保持し、2 回目以降は SOA を読まない。** ゾーン名は zoneID に
// 対して不変である。
func (t *Mutex) lockRecordName(ctx context.Context) (string, error) {
	if t.zoneName == "" {
		soa, err := t.getSOA(ctx)
		if err != nil {
			return "", err
		}
		t.zoneName = dns.Fqdn(soa.Name)
	}
	return t.lockRecordLabel + "." + t.zoneName, nil
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
