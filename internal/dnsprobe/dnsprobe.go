// SPDX-License-Identifier: Apache-2.0

// Package dnsprobe は権威 DNS サーバへ直接問い合わせて、ゾーン反映の結果が
// 実際に配信されているかを確認するための処理を提供する。
//
// 権威サーバに直接問い合わせるため、リゾルバのキャッシュは介在しない。
// 待っているのは「ゾーン反映が全権威サーバに行き渡ること」であって、
// いわゆる DNS の「伝播(propagation)」ではない。DNS にそのような伝播現象は無く、
// 権威サーバが新しい内容を返すか、リゾルバが TTL の間だけ古い内容を
// キャッシュしているかのどちらかである。
//
// 統合テスト（internal/integration）から利用するが、統合テストは
// ビルドタグで隔離されており go test ./... や golangci-lint の対象外になる。
// ロジックを単体テストできるよう、タグを付けない独立したパッケージとして分離している。
package dnsprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// DefaultPort は権威サーバへの問い合わせに使うポート。
const DefaultPort = "53"

// デフォルトのタイミング設定。
const (
	// DefaultQueryTimeout は 1 回の問い合わせのタイムアウト。
	DefaultQueryTimeout = 5 * time.Second
	// DefaultInterval は反映確認のポーリング間隔。
	DefaultInterval = 3 * time.Second
	// DefaultTimeout は反映確認の上限時間。
	DefaultTimeout = 10 * time.Minute
)

// Answer は 1 台の権威サーバへの 1 回の問い合わせ結果。
type Answer struct {
	// Server は問い合わせ先（"ip:port" 形式）。
	Server string
	// Rcode は応答コード（dns.RcodeSuccess など）。
	Rcode int
	// Authoritative は応答の AA フラグ。
	Authoritative bool
	// Truncated は UDP 応答が切り詰められていたか。
	Truncated bool
	// TXT は ANSWER セクションの TXT レコードの値。
	// 1 レコードが複数の character-string に分かれている場合は連結する。
	TXT []string
	// Err は問い合わせ自体に失敗した場合のエラー。
	Err error
}

// Usable は、その応答をレコードの有無の判定に使えるかを返す。
//
// 応答が得られない場合はもちろん、権威応答(AA=0)でない場合も false を返す。
// AA=0 を「レコードが無い」と解釈すると、到達したサーバが lame だった場合に
// 「レコードが消えた」という誤った判定になるため。
func (a Answer) Usable() bool {
	return a.Err == nil && a.Authoritative
}

// String は診断用の 1 行表現を返す。
func (a Answer) String() string {
	if a.Err != nil {
		return fmt.Sprintf("%s: error: %v", a.Server, a.Err)
	}
	return fmt.Sprintf("%s: rcode=%s aa=%t txt=%q",
		a.Server, dns.RcodeToString[a.Rcode], a.Authoritative, a.TXT)
}

// QueryFunc は 1 台のサーバへ 1 回問い合わせる関数。
// 実際の問い合わせは Exchange が行う。単体テストではこれを差し替える。
type QueryFunc func(ctx context.Context, server, qname string, qtype uint16) Answer

// Predicate は Answer が期待した状態かどうかを判定する。
type Predicate func(Answer) bool

// Exchange は権威サーバへ問い合わせる。
//
// 再帰要求は行わず(RD=0)、EDNS0 を有効にしてフラグメントを避ける。
// UDP で失敗した場合、または応答が切り詰められていた場合は TCP で再試行する。
func Exchange(ctx context.Context, server, qname string, qtype uint16) Answer {
	a := Answer{Server: server}

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(qname), qtype)
	m.RecursionDesired = false
	m.SetEdns0(1232, false)

	c := &dns.Client{Net: "udp", Timeout: DefaultQueryTimeout}
	in, _, err := c.ExchangeContext(ctx, m, server)
	if err == nil && in != nil && !in.Truncated {
		return fillAnswer(a, in)
	}

	// UDP が失敗した、または切り詰められた場合は TCP で再試行する。
	c.Net = "tcp"
	in, _, tcpErr := c.ExchangeContext(ctx, m, server)
	if tcpErr != nil {
		if err != nil {
			a.Err = fmt.Errorf("udp: %w; tcp: %w", err, tcpErr)
		} else {
			a.Err = fmt.Errorf("tcp: %w", tcpErr)
		}
		return a
	}
	if in == nil {
		a.Err = errors.New("dnsprobe: empty response")
		return a
	}
	return fillAnswer(a, in)
}

// fillAnswer は応答メッセージから Answer を組み立てる。
func fillAnswer(a Answer, in *dns.Msg) Answer {
	a.Rcode = in.Rcode
	a.Authoritative = in.Authoritative
	a.Truncated = in.Truncated
	for _, rr := range in.Answer {
		if t, ok := rr.(*dns.TXT); ok {
			// TXT は複数の character-string に分割されうるため連結する。
			a.TXT = append(a.TXT, strings.Join(t.Txt, ""))
		}
	}
	return a
}

// NormalizeRdata は DPF-API の rdata 表記を DNS 応答と比較できる形に正規化する。
//
// API 側の TXT の値はリテラルのダブルクォートを含む（例: "\"sample\""）が、
// miekg/dns の TXT.Txt はクォートを除いた文字列を返すため、これを揃える。
func NormalizeRdata(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		if unquoted, err := strconv.Unquote(v); err == nil {
			return unquoted
		}
		return v[1 : len(v)-1]
	}
	return v
}

// QuoteRdata は TXT レコードの値を DPF-API が期待するクォート付き表記にする。
func QuoteRdata(v string) string {
	return strconv.Quote(v)
}

// HasTXT は、指定した値の TXT が ANSWER に含まれることを要求する Predicate を返す。
// value は API 表記（クォート付き）でも素の文字列でもよい。
func HasTXT(value string) Predicate {
	want := NormalizeRdata(value)
	return func(a Answer) bool {
		return slices.Contains(a.TXT, want)
	}
}

// NotHasTXT は、指定した値の TXT が ANSWER に含まれないことを要求する Predicate を返す。
//
// NXDOMAIN は要求しない。ワイルドカードや DNSSEC 署名により、レコードが
// 存在しなくても NOERROR が返ることがあるため、「その値の TXT が無いこと」
// だけを判定する。
func NotHasTXT(value string) Predicate {
	has := HasTXT(value)
	return func(a Answer) bool {
		return !has(a)
	}
}

// Responds は権威応答が返ることのみを要求する Predicate を返す。
func Responds() Predicate {
	return func(Answer) bool { return true }
}

// Dropped は Preflight で判定対象から外されたサーバとその理由。
type Dropped struct {
	Server string
	Reason string
}

// Preflight は各サーバにゾーン頂点の SOA を問い合わせ、権威応答を返すものだけを残す。
//
// 実行環境から到達できないサーバを判定対象から外すための前処理。これを行わないと
// 「全サーバの一致」を要求する WaitAll が、DPF の挙動ではなく実行環境の
// ネットワーク到達性を検証してしまう。
func Preflight(ctx context.Context, q QueryFunc, servers []string, zone string) (alive []string, dropped []Dropped) {
	for _, s := range servers {
		a := q(ctx, s, zone, dns.TypeSOA)
		switch {
		case a.Err != nil:
			dropped = append(dropped, Dropped{Server: s, Reason: a.Err.Error()})
		case !a.Authoritative:
			dropped = append(dropped, Dropped{Server: s, Reason: "not authoritative (AA=0)"})
		default:
			alive = append(alive, s)
		}
	}
	return alive, dropped
}

// WaitOptions は WaitAll のポーリング設定。ゼロ値はデフォルトに置き換わる。
type WaitOptions struct {
	// Interval はポーリング間隔（既定 DefaultInterval）。
	Interval time.Duration
	// Timeout は待機の上限（既定 DefaultTimeout）。
	Timeout time.Duration
}

// WaitAll は全サーバが want を満たすまでポーリングし、待機に要した時間を返す。
//
// 1 回のパスで全サーバが揃って満たすことを要求する。パスをまたいでサーバごとの
// 成功を積み上げると、応答が安定しないサーバがあっても成功と判定されてしまうため。
//
// 判定に使えない応答（到達不可・AA=0）は「条件を満たさない」として扱い、待機を続ける。
// 反映の途中でサーバが一時的に応答しないことがあるため、即座に失敗とはしない。
func WaitAll(ctx context.Context, q QueryFunc, servers []string, qname string, qtype uint16, want Predicate, opts WaitOptions) (time.Duration, error) {
	if len(servers) == 0 {
		return 0, errors.New("dnsprobe: no servers to query")
	}

	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	start := time.Now()
	deadline := start.Add(timeout)

	for {
		reason := checkAll(ctx, q, servers, qname, qtype, want)
		if reason == "" {
			return time.Since(start), nil
		}

		if time.Now().Add(interval).After(deadline) {
			return time.Since(start), fmt.Errorf(
				"dnsprobe: %s が %s 以内に全権威サーバへ反映されなかった: %s", qname, timeout, reason)
		}

		select {
		case <-ctx.Done():
			return time.Since(start), ctx.Err()
		case <-time.After(interval):
		}
	}
}

// checkAll は全サーバを 1 パス確認し、満たさない場合はその理由を返す。
// すべて満たす場合は空文字を返す。
func checkAll(ctx context.Context, q QueryFunc, servers []string, qname string, qtype uint16, want Predicate) string {
	for _, s := range servers {
		a := q(ctx, s, qname, qtype)
		if !a.Usable() {
			return "使用できない応答: " + a.String()
		}
		if !want(a) {
			return "条件を満たさない: " + a.String()
		}
	}
	return ""
}

// ResolveServers は権威サーバのホスト名を IPv4 アドレスに解決し、
// "ip:port" 形式にして返す。
//
// IPv4 のみを対象とするのは、CI ランナーが IPv6 の外向き通信を持たないことが
// あるため。同一アドレスに解決される名前は重複を除く（複数の名前が同じ
// anycast クラスタを指すことが多く、問い合わせ回数を減らせる）。
func ResolveServers(ctx context.Context, names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	var errs []error

	for _, n := range names {
		host := strings.TrimSuffix(n, ".")
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n, err))
			continue
		}
		for _, addr := range addrs {
			s := net.JoinHostPort(addr.Unmap().String(), DefaultPort)
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("dnsprobe: IPv4 アドレスを解決できなかった: %w", errors.Join(errs...))
	}
	return out, nil
}
