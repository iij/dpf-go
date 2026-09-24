// SPDX-License-Identifier: Apache-2.0

package utils_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
)

// ドメイン名から longest match でゾーンを取得する。
func ExampleGetZoneFromName() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)

	zone, err := utils.GetZoneFromName(context.Background(), client.ZonesAPI, "www.example.jp.", false)
	if errors.Is(err, utils.ErrZoneNotFound) {
		fmt.Println("zone not found")
		return
	}
	if err != nil {
		// ネットワークエラーなど。
		return
	}
	fmt.Println(zone.Name)
}

// レコード名と RRTYPE から、それを含むゾーンとレコードを取得する。
func ExampleGetRecordFromRecordName() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)

	zone, record, err := utils.GetRecordFromRecordName(
		context.Background(),
		client.ZonesAPI,
		client.RecordsAPI,
		"www.example.jp.",
		dpf.RecordsRrtype("A"),
	)
	if err != nil {
		return
	}
	fmt.Println(zone.Name, record.Name)
}

// ゾーン単位ロックをデフォルト設定（owner: インスタンスごとに一意 / ttl: 15分）で
// 取得し、レコード編集後にロックを解放する。
func ExampleNewMutex() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)
	ctx := context.Background()

	mu := utils.NewMutex(client.RecordsAPI, "zone-id-123456")
	if err := mu.Lock(ctx); err != nil {
		// ErrStillLock の場合は他者がロック中。
		return
	}
	defer func() {
		if err := mu.Unlock(ctx); err != nil {
			fmt.Println(err)
		}
	}()

	// ここでロック対象ゾーンのレコードを編集し、ゾーン反映する。
}

// 編集が長引く場合に保持期間を延長する。
//
// ロックは再入できないため、Lock を呼び直して延長することはできない。
// 延長は Renew で行う。
func ExampleMutex_Renew() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)
	ctx := context.Background()

	mu := utils.NewMutex(client.RecordsAPI, "zone-id-123456")
	if err := mu.Lock(ctx); err != nil {
		return
	}
	defer func() {
		if err := mu.Unlock(ctx); err != nil {
			// ErrNotLockHolder の場合は保持中に奪われている。
			fmt.Println(err)
		}
	}()

	// 長い編集の途中で保持期間を延ばす。
	if err := mu.Renew(ctx); err != nil {
		// ErrNotLockHolder の場合は既に他者へ渡っているため、編集を中止する。
		return
	}
}

// 排他を保持したまま処理を実行する。
//
// 実行中は保持期間が自動で延長されるため、処理が長引いても排他は保たれる。
// 排他を他者に奪われた場合は、処理へ渡された context が打ち切られる。
// 終了時には自動で解放される。
func ExampleMutex_Do() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)
	ctx := context.Background()

	mu := utils.NewMutex(client.RecordsAPI, "zone-id-123456")

	err := mu.Do(ctx, func(ctx context.Context) error {
		// ここでレコードを 1 つずつ変更し、ゾーンへ反映する。
		// この流れでは反映によって排他が解かれないため、復帰後に解放される。
		return nil
	})
	if err != nil {
		// ErrStillLock: 他者が保持中。ErrNotLockHolder: 保持中に奪われた。
		fmt.Println(err)
	}
}

// ゾーン全体を読んで編集し、一括で置き換える。
//
// 取り込みの可否を決めるフラグ（既定はいずれも「取り込む」）の固定、反映によって
// 排他が解かれることの扱い、非同期処理の完了待ちは ZoneApplier が引き受ける。
// 利用者が書くのは編集の内容だけである。
func ExampleZoneApplier_Apply() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)
	ctx := context.Background()

	ap := utils.NewZoneApplier(
		client.RecordsAPI, client.ZonesAPI, client.JobsAPI,
		"zone-id-123456",
		utils.WithTTL(30*time.Minute),
	)

	err := ap.Apply(ctx, func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error) {
		// 編集しない要素はそのまま返せばよい。ラベル・TTL・コメントは保たれる。
		ttl := int32(300)
		return append(records, dpf.OverwriteRecordsInner{
			Name:   "www.example.jp.",
			Ttl:    *dpf.NewNullableInt32(&ttl),
			Rrtype: dpf.RECORDSRRTYPE_A,
			Rdata:  []dpf.RecordsRdataInner{{Value: dpf.PtrString("192.0.2.1")}},
			Labels: map[string]string{},
		}), nil
	}, utils.WithApplyDescription("台帳と同期"))
	if err != nil {
		// ErrNoRecords: 編集の結果が空だった。
		fmt.Println(err)
	}
}

// owner と TTL を変更してロックを生成する。
func ExampleNewMutex_options() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)

	mu := utils.NewMutex(
		client.RecordsAPI,
		"zone-id-123456",
		utils.WithOwner("deployer"),
		utils.WithTTL(30*time.Minute),
	)
	_ = mu
}

// ロックを取得できるまで待機する。
func ExampleMutex_LockWait() {
	cfg := dpf.NewConfiguration()
	client := dpf.NewAPIClient(cfg)

	mu := utils.NewMutex(client.RecordsAPI, "zone-id-123456")
	ctx := context.Background()
	if err := mu.LockWait(ctx, 5*time.Second); err != nil {
		return
	}
	defer func() {
		if err := mu.Unlock(ctx); err != nil {
			fmt.Println(err)
		}
	}()
}

// 既定設定（エンドポイントは環境変数 DPF_API_ENDPOINT または本番、
// トークンは環境変数 DPF_API_TOKEN）で Client を作る。
func ExampleNewClient() {
	c, err := utils.NewClient()
	if err != nil {
		fmt.Println(err)
		return
	}

	ctx := context.Background()
	err = c.Operation(ctx, func() error {
		zones, _, err := c.GetAPIClient().ZonesAPI.GetZoneList(ctx).ExecuteAll()
		if err != nil {
			return err
		}
		fmt.Println(len(zones.GetResults()))
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
}

// トークンをファイルから取得する。ファイルは API リクエストのたびに読み込まれるため、
// 外部プロセスがトークンをローテーションしても Client を作り直す必要はない。
func ExampleNewClient_tokenFile() {
	c, err := utils.NewClient(
		utils.WithEndpoint("https://api.dns-platform.jp/dpf/v1"),
		utils.WithTokenFile("/etc/dpf/token"),
		// 毎リクエストのファイル読み込みを避けたい場合は TTL を指定する。
		utils.WithTokenTTL(5*time.Minute),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = c
}

// 任意の取得処理で TokenProvider を組み立てる。
func ExampleWithTokenProvider() {
	// 例えば外部のシークレットストアから取得する。
	provider := func(ctx context.Context) (string, error) {
		return fetchTokenFromVault(ctx)
	}

	c, err := utils.NewClient(utils.WithTokenProvider(provider))
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = c
}

// fetchTokenFromVault は ExampleWithTokenProvider 用のダミー。
func fetchTokenFromVault(context.Context) (string, error) {
	return "token-from-vault", nil
}
