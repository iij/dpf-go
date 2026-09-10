// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
	"golang.org/x/time/rate"
)

// 環境変数名。
const (
	envTokenRO     = "DPF_TOKEN_RO"
	envTokenRW     = "DPF_TOKEN_RW"
	envServiceCode = "DPF_TEST_SERVICE_CODE"
	envEndpoint    = "DPF_API_ENDPOINT"
	envDNSTimeout  = "DPF_TEST_DNS_TIMEOUT"
)

// テスト対象ゾーン。
//
// ゾーン名は DNS を引けば誰でも分かる情報なので定数として持つ。一方
// サービスコードは社内の識別子なので環境変数から受け取り、リポジトリには置かない。
// 書き込み対象の特定にはこの 2 つの一致を要求する（writeZone を参照）。
const (
	// zoneApex は最上位のテストゾーン。ネームサーバ申請の検証に使う。
	zoneApex = "dns-tool-test.jp."
	// zoneSub は中間のテストゾーン。longest match の検証にのみ使う。
	zoneSub = "sub.dns-tool-test.jp."
	// zoneSubSub は最下位のテストゾーン。レコードの書き込み対象。
	zoneSubSub = "sub.sub.dns-tool-test.jp."
)

const (
	// apiInterval は API リクエストの最小間隔。
	// 「1 req/s より短くしない」制約に対して 10% の余裕を持たせている。
	apiInterval = 1100 * time.Millisecond

	// defaultDNSTimeout は権威サーバへの反映確認の既定の上限。
	// 環境変数 DPF_TEST_DNS_TIMEOUT で変更できる。
	defaultDNSTimeout = 10 * time.Minute

	// apiOpTimeout は 1 つの非同期操作（実行 + JOB 完了待ち）の上限。
	apiOpTimeout = 15 * time.Minute

	// syncWaitPollInterval は非同期 JOB の進捗確認間隔。
	syncWaitPollInterval = 3 * time.Second
)

// sharedTransport はプロセス全体で 1 つだけ存在するレート制限付き
// http.RoundTripper。
//
// utils.Client はクライアントごとに独自のレートリミッタを持つため、RO / RW の
// 2 つのクライアントを作ると合計で 2 倍のレートになってしまう。全クライアントで
// この Transport を共有することで、プロセス全体として 1 req/s を超えないことを
// 1 箇所で保証する。
type sharedTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func (t *sharedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

var apiTransport *sharedTransport

func TestMain(m *testing.M) {
	dpf.SyncWaitPollInterval = syncWaitPollInterval

	apiTransport = &sharedTransport{
		base: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			// DPF-API はバージョン 2.0.0 で HTTP/2 対応を削除している。
			ForceAttemptHTTP2: false,
		},
		limiter: rate.NewLimiter(rate.Every(apiInterval), 1),
	}

	os.Exit(m.Run())
}

// newClient は共有トランスポートを使う utils.Client を生成する。
//
// http.Client の Timeout は 0 のままにする。net/http は Timeout が非ゼロかつ
// 独自の Transport が使われている場合、レスポンスボディの Close 時にリクエストの
// context をキャンセルするため、SyncWait へ context を引き継げなくなる。
// 個々のタイムアウトは http.Transport 側で設定している。
func newClient(token string, maxRetry int) (*utils.Client, error) {
	opts := []utils.ClientOption{
		utils.WithHTTPClient(&http.Client{Transport: apiTransport}),
		utils.WithToken(token),
		utils.WithMaxConcurrency(1),
		utils.WithMaxRetry(maxRetry),
		// レート制御は apiTransport の 1 箇所に集約する。
		// ラッパ側のリミッタは実質無効化しておく（0 は無視されるため大きな値を渡す）。
		utils.WithRateLimit(100, 100),
	}
	if ep := os.Getenv(envEndpoint); ep != "" {
		opts = append(opts, utils.WithEndpoint(ep))
	}
	return utils.NewClient(opts...)
}

var (
	roOnce sync.Once
	roCli  *utils.Client
	roErr  error

	rwOnce sync.Once
	rwCli  *utils.Client
	rwErr  error
)

// readOnlyClient は参照系トークンのクライアントを返す。
// トークンが未設定の場合はテストをスキップする。
//
// 参照系は GET のみなのでリトライしても副作用が無く、CI の一時的な
// 通信エラーに耐えられるようリトライを有効にしている。
func readOnlyClient(t *testing.T) *utils.Client {
	t.Helper()
	token := os.Getenv(envTokenRO)
	if token == "" {
		t.Skipf("%s が未設定のためスキップする", envTokenRO)
	}
	roOnce.Do(func() { roCli, roErr = newClient(token, 2) })
	if roErr != nil {
		t.Fatalf("参照系クライアントの生成に失敗した: %v", roErr)
	}
	return roCli
}

// writeClient は更新系トークンのクライアントを返す。
// トークンが未設定の場合はテストをスキップする。
//
// リトライは無効にする。utils.Client.Operation は「レスポンスが得られなかった
// エラー」を再試行するため、POST が受理された後に接続が切れるとレコードが
// 二重に作成されうる。
func writeClient(t *testing.T) *utils.Client {
	t.Helper()
	token := os.Getenv(envTokenRW)
	if token == "" {
		t.Skipf("%s が未設定のためスキップする", envTokenRW)
	}
	rwOnce.Do(func() { rwCli, rwErr = newClient(token, 0) })
	if rwErr != nil {
		t.Fatalf("更新系クライアントの生成に失敗した: %v", rwErr)
	}
	return rwCli
}

// dnsTimeout は権威サーバへの反映確認の上限を返す。
//
// 指定が不正な場合は既定値にフォールバックせず失敗させる。タイポで意図せず
// 短い上限になると「反映されなかった」という誤った失敗になるため。
func dnsTimeout(t *testing.T) time.Duration {
	t.Helper()
	v := os.Getenv(envDNSTimeout)
	if v == "" {
		return defaultDNSTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		t.Fatalf("%s の値 %q を解釈できない（例: 10m, 300s）: %v", envDNSTimeout, v, err)
	}
	if d <= 0 {
		t.Fatalf("%s には正の値を指定すること（指定値: %q）", envDNSTimeout, v)
	}
	return d
}

// testContext は API 操作用の context を返す。
//
// 非同期 API は「実行」と「JOB の完了待ち」を 1 本の context で通す必要がある。
// HTTP コール 1 回分の短い timeout を付けると SyncWait のポーリングが
// 途中で打ち切られるため、操作全体を覆う長さを渡すこと。
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), apiOpTimeout)
	t.Cleanup(cancel)
	return ctx
}

// cleanupContext は後始末用の context を返す。
//
// t.Context() は使わない。Go 1.24 以降、t.Context() は t.Cleanup が走る前に
// キャンセルされるため、後始末の API 呼び出しが必ず失敗する。
func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Minute)
}

// requireEnv は必須の環境変数を返す。未設定ならスキップする。
func requireEnv(t *testing.T, name, why string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("%s が未設定のためスキップする（%s）", name, why)
	}
	return v
}

// logSkipHint は「全部スキップ」が「全部成功」に見えないよう、
// 実行条件を明示的にログへ残す。
func logSkipHint(t *testing.T) {
	t.Helper()
	t.Logf("実行条件: %s=%t %s=%t %s=%t",
		envTokenRO, os.Getenv(envTokenRO) != "",
		envTokenRW, os.Getenv(envTokenRW) != "",
		envServiceCode, os.Getenv(envServiceCode) != "")
}
