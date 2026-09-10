// SPDX-License-Identifier: Apache-2.0

package dnsprobe

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestNormalizeRdata(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"クォート付き（API 表記）", `"sample"`, "sample"},
		{"クォート無し", "sample", "sample"},
		{"空文字", "", ""},
		{"クォートのみ", `""`, ""},
		{"内部にクォートを含む", `"a\"b"`, `a"b`},
		{"片側だけクォート", `"sample`, `"sample`},
		{"エスケープが壊れている", `"a\q"`, `a\q`},
		{"日本語", `"テスト"`, "テスト"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeRdata(tt.in); got != tt.want {
				t.Errorf("NormalizeRdata(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuoteRdata_RoundTrip は QuoteRdata と NormalizeRdata が往復することを検証する。
func TestQuoteRdata_RoundTrip(t *testing.T) {
	for _, v := range []string{"sample", "", `a"b`, "テスト", "a b c"} {
		if got := NormalizeRdata(QuoteRdata(v)); got != v {
			t.Errorf("round trip failed for %q: got %q", v, got)
		}
	}
}

func TestPredicates(t *testing.T) {
	a := Answer{TXT: []string{"alpha", "beta"}}

	if !HasTXT("alpha")(a) {
		t.Error("HasTXT(alpha) should match")
	}
	// API 表記（クォート付き）でも一致すること。
	if !HasTXT(`"alpha"`)(a) {
		t.Error(`HasTXT("alpha") with API quoting should match`)
	}
	if HasTXT("gamma")(a) {
		t.Error("HasTXT(gamma) should not match")
	}
	if !NotHasTXT("gamma")(a) {
		t.Error("NotHasTXT(gamma) should match")
	}
	if NotHasTXT("alpha")(a) {
		t.Error("NotHasTXT(alpha) should not match")
	}
	if !Responds()(a) {
		t.Error("Responds should always match")
	}
}

func TestAnswer_Usable(t *testing.T) {
	tests := []struct {
		name string
		a    Answer
		want bool
	}{
		{"権威応答", Answer{Authoritative: true}, true},
		{"エラー", Answer{Err: errors.New("boom"), Authoritative: true}, false},
		{"AA=0", Answer{Authoritative: false}, false},
		{"NXDOMAIN でも権威なら使える", Answer{Authoritative: true, Rcode: dns.RcodeNameError}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Usable(); got != tt.want {
				t.Errorf("Usable() = %t, want %t", got, tt.want)
			}
		})
	}
}

// stubQuery は server ごとに固定の Answer を返す QueryFunc を作る。
func stubQuery(byServer map[string]Answer) QueryFunc {
	return func(_ context.Context, server, _ string, _ uint16) Answer {
		a := byServer[server]
		a.Server = server
		return a
	}
}

func TestPreflight(t *testing.T) {
	q := stubQuery(map[string]Answer{
		"1.1.1.1:53": {Authoritative: true},
		"2.2.2.2:53": {Err: errors.New("i/o timeout")},
		"3.3.3.3:53": {Authoritative: false},
		"4.4.4.4:53": {Authoritative: true},
	})

	alive, dropped := Preflight(context.Background(), q,
		[]string{"1.1.1.1:53", "2.2.2.2:53", "3.3.3.3:53", "4.4.4.4:53"}, "example.jp.")

	wantAlive := []string{"1.1.1.1:53", "4.4.4.4:53"}
	if len(alive) != len(wantAlive) {
		t.Fatalf("alive = %v, want %v", alive, wantAlive)
	}
	for i := range wantAlive {
		if alive[i] != wantAlive[i] {
			t.Fatalf("alive = %v, want %v", alive, wantAlive)
		}
	}
	if len(dropped) != 2 {
		t.Fatalf("dropped = %+v, want 2 entries", dropped)
	}
	if !strings.Contains(dropped[1].Reason, "AA=0") {
		t.Errorf("expected AA=0 reason, got %q", dropped[1].Reason)
	}
}

func TestWaitAll_NoServers(t *testing.T) {
	_, err := WaitAll(context.Background(), stubQuery(nil), nil, "a.example.jp.", dns.TypeTXT,
		Responds(), WaitOptions{})
	if err == nil {
		t.Fatal("expected error for empty server list")
	}
}

func TestWaitAll_ImmediateSuccess(t *testing.T) {
	q := stubQuery(map[string]Answer{
		"1.1.1.1:53": {Authoritative: true, TXT: []string{"v1"}},
		"2.2.2.2:53": {Authoritative: true, TXT: []string{"v1"}},
	})

	elapsed, err := WaitAll(context.Background(), q,
		[]string{"1.1.1.1:53", "2.2.2.2:53"}, "a.example.jp.", dns.TypeTXT,
		HasTXT("v1"), WaitOptions{Interval: time.Millisecond, Timeout: time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected immediate success, took %s", elapsed)
	}
}

// TestWaitAll_EventualSuccess は、反映が遅れたサーバがあってもポーリングで
// 最終的に成功することを検証する。
func TestWaitAll_EventualSuccess(t *testing.T) {
	var passes atomic.Int32
	q := func(_ context.Context, server, _ string, _ uint16) Answer {
		a := Answer{Server: server, Authoritative: true}
		// 2.2.2.2 だけ 3 パス目以降で反映される。
		if server != "2.2.2.2:53" || passes.Load() >= 2 {
			a.TXT = []string{"v1"}
		}
		if server == "2.2.2.2:53" {
			passes.Add(1)
		}
		return a
	}

	if _, err := WaitAll(context.Background(), q,
		[]string{"1.1.1.1:53", "2.2.2.2:53"}, "a.example.jp.", dns.TypeTXT,
		HasTXT("v1"), WaitOptions{Interval: time.Millisecond, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestWaitAll_RequiresSinglePassAgreement は、サーバごとの成功をパスをまたいで
// 積み上げないことを検証する。1.1.1.1 と 2.2.2.2 が交互にしか条件を満たさない
// 場合、「全サーバが揃うパス」は永遠に来ないため失敗しなければならない。
func TestWaitAll_RequiresSinglePassAgreement(t *testing.T) {
	var pass atomic.Int32
	q := func(_ context.Context, server, _ string, _ uint16) Answer {
		// checkAll は先頭のサーバから順に問い合わせるため、
		// 1.1.1.1 への問い合わせをパスの開始とみなす。
		if server == "1.1.1.1:53" {
			pass.Add(1)
		}
		odd := pass.Load()%2 == 1

		// 奇数パスでは 1.1.1.1 のみ、偶数パスでは 2.2.2.2 のみが値を返す。
		// どのパスでも両方が揃うことはない。
		a := Answer{Server: server, Authoritative: true}
		if (server == "1.1.1.1:53") == odd {
			a.TXT = []string{"v1"}
		}
		return a
	}

	_, err := WaitAll(context.Background(), q,
		[]string{"1.1.1.1:53", "2.2.2.2:53"}, "a.example.jp.", dns.TypeTXT,
		HasTXT("v1"), WaitOptions{Interval: time.Millisecond, Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("expected failure: servers never agree within a single pass")
	}
}

func TestWaitAll_Timeout(t *testing.T) {
	q := stubQuery(map[string]Answer{
		"1.1.1.1:53": {Authoritative: true, TXT: []string{"old"}},
	})

	_, err := WaitAll(context.Background(), q,
		[]string{"1.1.1.1:53"}, "a.example.jp.", dns.TypeTXT,
		HasTXT("new"), WaitOptions{Interval: time.Millisecond, Timeout: 50 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "条件を満たさない") {
		t.Errorf("error should explain the reason, got: %v", err)
	}
}

// TestWaitAll_UnusableIsRetried は、到達できないサーバがあっても即座に
// 失敗せず、タイムアウトまで再試行することを検証する。
func TestWaitAll_UnusableIsRetried(t *testing.T) {
	var calls atomic.Int32
	q := func(_ context.Context, server, _ string, _ uint16) Answer {
		if calls.Add(1) < 3 {
			return Answer{Server: server, Err: errors.New("i/o timeout")}
		}
		return Answer{Server: server, Authoritative: true, TXT: []string{"v1"}}
	}

	if _, err := WaitAll(context.Background(), q,
		[]string{"1.1.1.1:53"}, "a.example.jp.", dns.TypeTXT,
		HasTXT("v1"), WaitOptions{Interval: time.Millisecond, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls.Load() < 3 {
		t.Errorf("expected retries, got %d calls", calls.Load())
	}
}

func TestWaitAll_ContextCancel(t *testing.T) {
	q := stubQuery(map[string]Answer{"1.1.1.1:53": {Authoritative: true}})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := WaitAll(ctx, q, []string{"1.1.1.1:53"}, "a.example.jp.", dns.TypeTXT,
		func(Answer) bool { return false },
		WaitOptions{Interval: 5 * time.Millisecond, Timeout: time.Minute})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// startTestDNS は UDP と TCP の両方で待ち受けるテスト用 DNS サーバを起動し、
// その "ip:port" を返す。
//
// UDP と TCP を同じポートで開くのは DNS の仕様上の要請である。Exchange は
// UDP の応答が切り詰められた場合に同じ "host:port" へ TCP で再試行するため、
// ポートを別にすると再試行の宛先が存在せず、その分岐を検証できない。
func startTestDNS(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()

	pc, ln, addr := listenBoth(t)

	udpSrv := &dns.Server{PacketConn: pc, Handler: handler}
	tcpSrv := &dns.Server{Listener: ln, Handler: handler}
	go func() { _ = udpSrv.ActivateAndServe() }()
	go func() { _ = tcpSrv.ActivateAndServe() }()

	t.Cleanup(func() {
		_ = udpSrv.Shutdown()
		_ = tcpSrv.Shutdown()
	})

	// 待ち受け開始を待つ。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c := &dns.Client{Net: "udp", Timeout: 100 * time.Millisecond}
		m := new(dns.Msg)
		m.SetQuestion("probe.example.jp.", dns.TypeTXT)
		if _, _, err := c.Exchange(m, addr); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return addr
}

// listenBoth は同じポートで UDP と TCP の両方を待ち受け、その組と "ip:port" を返す。
//
// TCP を先に取る。UDP から取ると、ポート番号が決まってから TCP を開くまでの間に
// 別プロセスが同じ番号の TCP を取ることがあり、"bind: address already in use" で
// 落ちる。UDP と TCP はポート空間が独立しているため、UDP で空いていることは
// TCP で空いていることを保証しない。TCP の :0 は OS が未使用の番号を選び、
// listen している間はその番号を保持するため、先に取るほうが衝突しにくい。
//
// それでも「TCP で取れた番号の UDP が使われている」可能性は残るので、その場合は
// 番号を捨てて取り直す。
func listenBoth(t *testing.T) (net.PacketConn, net.Listener, string) {
	t.Helper()

	// 数回で十分。これを超えて衝突し続けるのは、番号の偶然ではなく
	// 環境側の問題（ephemeral ポートの枯渇など）である。
	const attempts = 5

	var lastErr error
	for range attempts {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen tcp: %v", err)
		}
		addr := ln.Addr().String()

		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			// この番号は UDP 側が使われている。TCP を閉じて別の番号を試す。
			_ = ln.Close()
			lastErr = err
			continue
		}
		return pc, ln, addr
	}

	t.Fatalf("listen udp+tcp: 同じポートを %d 回試しても確保できませんでした: %v", attempts, lastErr)
	return nil, nil, ""
}

// txtHandler は指定した値の TXT を権威応答で返すハンドラを作る。
func txtHandler(value string, truncateUDP bool) dns.HandlerFunc {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true

		_, isUDP := w.RemoteAddr().(*net.UDPAddr)
		if truncateUDP && isUDP {
			m.Truncated = true
			_ = w.WriteMsg(m)
			return
		}

		if len(r.Question) > 0 && r.Question[0].Qtype == dns.TypeTXT {
			rr := &dns.TXT{
				Hdr: dns.RR_Header{
					Name: r.Question[0].Name, Rrtype: dns.TypeTXT,
					Class: dns.ClassINET, Ttl: 60,
				},
				Txt: []string{value},
			}
			m.Answer = append(m.Answer, rr)
		}
		_ = w.WriteMsg(m)
	}
}

// TestExchange_UDP は実際の DNS トランスポート経路を検証する。
func TestExchange_UDP(t *testing.T) {
	addr := startTestDNS(t, txtHandler("hello", false))

	a := Exchange(context.Background(), addr, "a.example.jp.", dns.TypeTXT)
	if a.Err != nil {
		t.Fatalf("unexpected error: %v", a.Err)
	}
	if !a.Usable() {
		t.Fatalf("expected usable answer, got %s", a)
	}
	if !HasTXT("hello")(a) {
		t.Fatalf("expected TXT hello, got %s", a)
	}
}

// TestExchange_TCPFallback は UDP 応答が切り詰められた場合に TCP で
// 再試行することを検証する。
func TestExchange_TCPFallback(t *testing.T) {
	addr := startTestDNS(t, txtHandler("via-tcp", true))

	a := Exchange(context.Background(), addr, "a.example.jp.", dns.TypeTXT)
	if a.Err != nil {
		t.Fatalf("unexpected error: %v", a.Err)
	}
	if a.Truncated {
		t.Error("expected the TCP answer (not truncated)")
	}
	if !HasTXT("via-tcp")(a) {
		t.Fatalf("expected TXT via-tcp, got %s", a)
	}
}

// TestExchange_Unreachable は到達できない場合に Err が入ることを検証する。
func TestExchange_Unreachable(t *testing.T) {
	// ポート 1 は待ち受けていない前提。
	a := Exchange(context.Background(), "127.0.0.1:1", "a.example.jp.", dns.TypeTXT)
	if a.Err == nil {
		t.Fatal("expected an error for unreachable server")
	}
	if a.Usable() {
		t.Error("unreachable answer must not be usable")
	}
}

// TestResolveServers_Dedupe は、同じアドレスに解決される名前が
// 重複排除されることを検証する（localhost を利用）。
func TestResolveServers_Dedupe(t *testing.T) {
	got, err := ResolveServers(context.Background(), []string{"localhost.", "localhost"})
	if err != nil {
		t.Skipf("localhost を解決できない環境のためスキップ: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped address, got %v", got)
	}
	if !strings.HasSuffix(got[0], ":"+DefaultPort) {
		t.Errorf("expected :%s suffix, got %q", DefaultPort, got[0])
	}
}

func TestResolveServers_AllFail(t *testing.T) {
	_, err := ResolveServers(context.Background(), []string{"invalid.invalid."})
	if err == nil {
		t.Fatal("expected error when nothing resolves")
	}
}
