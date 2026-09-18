// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
	"github.com/miekg/dns"
)

// writeZoneName は書き込みを許す唯一のゾーン。
const writeZoneName = "sub.sub.dns-tool-test.jp."

func main() {
	at := flag.String("at", "", "取得を開始する時刻 (RFC3339Nano)。全プロセスへ同じ値を渡して同時に開始する")
	hold := flag.Duration("hold", 3*time.Second, "取得できた場合に保持する時間")
	ttl := flag.Duration("ttl", 2*time.Minute, "排他の保持期間")
	cleanup := flag.Bool("cleanup", true, "解放後に SOA の未反映編集を破棄する")
	flag.Parse()

	if err := run(*at, *hold, *ttl, *cleanup); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR %v\n", err)
		os.Exit(1)
	}
}

func run(at string, hold, ttl time.Duration, cleanup bool) error {
	token := os.Getenv("DPF_TOKEN_RW")
	if token == "" {
		return fmt.Errorf("DPF_TOKEN_RW が未設定である")
	}
	serviceCode := os.Getenv("DPF_TEST_SERVICE_CODE")
	if serviceCode == "" {
		return fmt.Errorf("DPF_TEST_SERVICE_CODE が未設定である")
	}

	// 更新系のリトライは無効にする。POST が受理された後に接続が切れると
	// レコードが二重に作成されうる。
	c, err := utils.NewClient(
		utils.WithToken(token),
		utils.WithMaxConcurrency(1),
		utils.WithMaxRetry(0),
		utils.WithRateLimit(2, 2),
	)
	if err != nil {
		return fmt.Errorf("クライアントを生成できない: %w", err)
	}
	api := c.GetAPIClient()

	ctx := context.Background()

	// 安全確認: サービスコードから解決したゾーンが書き込み対象と一致すること。
	zone, err := utils.GetZoneFromServiceCode(ctx, api.ZonesAPI, serviceCode)
	if err != nil {
		return fmt.Errorf("サービスコード %s のゾーンを取得できない: %w", serviceCode, err)
	}
	if dns.CanonicalName(zone.Name) != dns.CanonicalName(writeZoneName) {
		return fmt.Errorf("安全確認に失敗した。サービスコード %s のゾーンは %q だが、書き込み対象は %q でなければならない",
			serviceCode, zone.Name, writeZoneName)
	}

	mu := utils.NewMutex(api.RecordsAPI, zone.Id, utils.WithTTL(ttl))

	// 開始時刻を揃える。ここまでで接続は確立しているため、待機の後は
	// ただちに最初の問い合わせが飛ぶ。
	if at != "" {
		t0, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return fmt.Errorf("-at の値 %q を解釈できない: %w", at, err)
		}
		wait := time.Until(t0)
		if wait < 0 {
			return fmt.Errorf("-at の時刻が既に過ぎている（%s 前）。余裕を持った時刻を指定すること", -wait)
		}
		fmt.Printf("READY  owner=%s zone=%s 開始まで %s\n", mu.Owner(), zone.Name, wait.Round(time.Millisecond))
		time.Sleep(wait)
	}

	begin := time.Now()
	lockErr := mu.Lock(ctx)
	elapsed := time.Since(begin).Round(time.Millisecond)

	if lockErr != nil {
		fmt.Printf("RESULT acquired=false owner=%s elapsed=%s err=%v\n", mu.Owner(), elapsed, lockErr)
		return nil // 取得できないことは異常ではない
	}
	fmt.Printf("RESULT acquired=true  owner=%s elapsed=%s\n", mu.Owner(), elapsed)

	time.Sleep(hold)

	if err := mu.Unlock(ctx); err != nil {
		return fmt.Errorf("解放できない: %w", err)
	}
	fmt.Printf("UNLOCK owner=%s\n", mu.Owner())

	if cleanup {
		if err := discardSOAChanges(ctx, api, zone.Id); err != nil {
			return fmt.Errorf("SOA の未反映編集を破棄できない: %w", err)
		}
		fmt.Printf("CLEAN  owner=%s SOA の未反映編集を破棄した\n", mu.Owner())
	}
	return nil
}

// discardSOAChanges は SOA レコードの未反映編集を破棄する。
// internal/integration/mutex_test.go の同名の後始末と同じことを行う。
func discardSOAChanges(ctx context.Context, api *dpf.APIClient, zoneID string) error {
	list, _, err := api.RecordsAPI.GetRecordList(ctx, zoneID).
		KeywordsRrtype(dpf.RECORDSRRTYPE_SOA).
		ExecuteAll()
	if err != nil {
		return err
	}
	if list == nil {
		return nil
	}
	for _, r := range list.Results {
		if r.Rrtype != dpf.RECORDSRRTYPE_SOA || r.State == dpf.RECORDSSTATE__0 {
			continue
		}
		async, resp, err := api.RecordsAPI.DeleteRecordChanges(ctx, zoneID, r.Id).Execute()
		_, _, err = api.JobsAPI.SyncWaitContext(ctx, async, resp, err)
		return err
	}
	return nil
}
