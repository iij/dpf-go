// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	dpf "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"
)

// specs/006-zone-lock-redesign の「一時レコード追加ロック」が依存する
// DPF-API の性質を確認する。
//
// 同仕様 FR-028 は 4 つの性質の確認を求めている。本ファイルはそのうち
// レコードの状態に関する 2 つを扱う。
//
//   - (1) 追加予定の状態にあるレコードに対しても、同名かつ同 RRTYPE の
//     レコードの追加が重複として拒否されること（排他の根拠）
//   - (2) 削除予定の状態にあるレコードは、その重複判定に影響しないこと
//     （公開されてしまった専用のレコードからの回復の根拠。FR-008）
//
// 残る 2 つのうち「同一ユーザからの編集は拒否されない」は TestZoneMutex が
// 同一 owner でのロック再取得を通すことで既に示している（編集予定の SOA へ
// 同じトークンから再度 PatchRecord できている）。「他ユーザからの編集は
// 拒否される」の確認には別ユーザのトークンが必要で、本テスト群は
// 参照系・更新系の 2 本しか持たないため対象外とする。

// lockRecordLabel は一時レコード追加ロックに使う最左ラベル。
// 名前解決に影響しないよう、アンダースコアで始まる TXT レコードを使う。
const lockRecordLabel = "_dpf-go-lock"

// lockRecordName は書き込み対象ゾーンにおける専用レコードの FQDN を返す。
// zoneSubSub は末尾ドット付きの定数なので、そのまま連結すれば FQDN になる。
func lockRecordName() string {
	return lockRecordLabel + "." + zoneSubSub
}

// postLockRecord は専用レコードを追加予定の状態で作成する。
//
// エラーを返り値にするのは、重複が拒否されること自体が検証対象であり、
// syncWaiter のようにテストを終了させてはならないため。重複の拒否が同期の
// 400 で返るか非同期 JOB の失敗で返るかは実測しないと分からないので、
// どちらの経路でもエラーとして受け取れる形にしている。
func postLockRecord(ctx context.Context, c *utils.Client, zoneID, owner string, expire time.Time) error {
	ttl := int32(300)
	body := dpf.PostRecord{
		Name:   lockRecordName(),
		Ttl:    *dpf.NewNullableInt32(&ttl),
		Rrtype: dpf.RECORDSRRTYPEWITHOUTSOA_TXT,
		Rdata:  []dpf.RecordsRdataInner{{Value: dpf.PtrString(`"` + owner + `"`)}},
		Labels: &map[string]string{
			utils.LockOwnerLabelKey:    owner,
			utils.LockDeadlineLabelKey: strconv.FormatInt(expire.Unix(), 10),
		},
	}
	api := c.GetAPIClient()
	async, resp, err := api.RecordsAPI.PostRecord(ctx, zoneID).PostRecord(body).Execute()
	if err != nil {
		return err
	}
	_, _, err = api.JobsAPI.SyncWait(async, resp, err)
	return err
}

// lockRecords は専用レコードを state 込みで返す。
func lockRecords(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) []dpf.Record {
	t.Helper()
	return findRecords(t, ctx, c, zoneID, lockRecordName(), dpf.RECORDSRRTYPE_TXT)
}

// requireLockRecord は専用レコードが指定の state で 1 件だけ存在することを求める。
func requireLockRecord(t *testing.T, ctx context.Context, c *utils.Client, zoneID string, want dpf.RecordsState, what string) dpf.Record {
	t.Helper()
	recs := lockRecords(t, ctx, c, zoneID)
	if len(recs) != 1 {
		for _, r := range recs {
			t.Logf("専用レコード: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
		}
		t.Fatalf("%s: 専用レコードが %d 件ある（1 件であること）", what, len(recs))
	}
	if recs[0].State != want {
		t.Fatalf("%s: 専用レコードの state が %d（想定 %d）", what, recs[0].State, want)
	}
	t.Logf("%s: id=%s state=%d", what, recs[0].Id, recs[0].State)
	return recs[0]
}

// TestLockRecordPremises は一時レコード追加ロックの前提を通しで確認する。
//
// 専用レコードを実際に権威サーバへ公開し、削除予定にしてから入れ直して、
// 最後に消す。ゾーン反映を 2 回行うため、書き込み対象ゾーンが統合テスト専用で
// あることが前提になる（writeZone の二重確認を参照）。
func TestLockRecordPremises(t *testing.T) {
	logSkipHint(t)
	c := writeClient(t)
	ctx := testContext(t)
	zone := writeZone(t, ctx, c)

	resetPendingChanges(t, ctx, c, zone.Id)
	if recs := lockRecords(t, ctx, c, zone.Id); len(recs) != 0 {
		for _, r := range recs {
			t.Logf("残存: id=%s state=%d", r.Id, r.State)
		}
		t.Fatalf("開始前に専用レコード %s が %d 件ある。手動で片付けること",
			lockRecordName(), len(recs))
	}

	api := c.GetAPIClient()
	expire := time.Now().Add(time.Minute)

	// 後始末。テストが途中で失敗しても、追加予定は取り消し、公開済みは
	// 削除して反映する。専用レコードを公開したまま残さないため。
	t.Cleanup(func() {
		cctx, cancel := cleanupContext()
		defer cancel()
		cleanupLockRecord(t, cctx, c, zone.Id)
	})

	// (1) 追加予定に対する重複 POST は拒否される。
	if err := postLockRecord(ctx, c, zone.Id, "premise-first", expire); err != nil {
		t.Fatalf("1 回目の追加に失敗した: %v", describeAPIError(err))
	}
	added := requireLockRecord(t, ctx, c, zone.Id, dpf.RECORDSSTATE__1, "1 回目の追加後")

	err := postLockRecord(ctx, c, zone.Id, "premise-second", expire)
	if err == nil {
		t.Fatal("追加予定のレコードに対する重複 POST が成功した。" +
			"一時レコード追加ロックは排他として成立しない")
	}
	t.Logf("追加予定への重複 POST は拒否された: %s", describeAPIError(err))
	if !hasErrorDetail(err, "duplicated", "record") {
		t.Logf("注意: error_details に duplicated/record が無い。"+
			"拒否の理由が重複であることを確認すること: %v", errorDetails(err))
	}

	// 追加予定の取り消しはゾーン反映を伴わない（一時ロックの解放に相当）。
	syncWaiter(t, c, "追加予定の取り消し")(
		api.RecordsAPI.DeleteRecordChanges(ctx, zone.Id, added.Id).Execute())
	if recs := lockRecords(t, ctx, c, zone.Id); len(recs) != 0 {
		t.Fatalf("取り消し後も専用レコードが %d 件ある", len(recs))
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Fatalf("取り消し後の未反映件数が %d 件（0 件であること）", n)
	}
	t.Log("追加予定の取り消しでレコードは消え、未反映も残らない")

	// 「誰かがゾーン反映して専用レコードが公開された」状態を作る。
	if err := postLockRecord(ctx, c, zone.Id, "premise-published", expire); err != nil {
		t.Fatalf("公開用の追加に失敗した: %v", describeAPIError(err))
	}
	requireLockRecord(t, ctx, c, zone.Id, dpf.RECORDSSTATE__1, "公開前")
	applyZone(t, ctx, c, zone.Id, 1, "premise: publish lock record")
	published := requireLockRecord(t, ctx, c, zone.Id, dpf.RECORDSSTATE__0, "公開後")

	// 反映済みに対しても重複 POST は拒否される（取り消しでは除去できない）。
	if err := postLockRecord(ctx, c, zone.Id, "premise-after-publish", expire); err == nil {
		t.Fatal("反映済みのレコードに対する重複 POST が成功した")
	} else {
		t.Logf("反映済みへの重複 POST は拒否された: %s", describeAPIError(err))
	}

	// (2) 削除予定にすれば、同名同 RRTYPE を追加予定として入れられる。
	syncWaiter(t, c, "反映済みレコードの削除")(
		api.RecordsAPI.DeleteRecord(ctx, zone.Id, published.Id).Execute())
	requireLockRecord(t, ctx, c, zone.Id, dpf.RECORDSSTATE__2, "削除予定にした後")

	if err := postLockRecord(ctx, c, zone.Id, "premise-recovered", expire); err != nil {
		t.Fatalf("削除予定がある状態での追加に失敗した。"+
			"FR-008 の回復方式（削除予定にして追加予定を入れる）は成立しない: %s",
			describeAPIError(err))
	}
	t.Log("削除予定は重複判定に影響しない。FR-008 の回復方式は成立する")

	// 追加予定と削除予定が並存する。
	recs := lockRecords(t, ctx, c, zone.Id)
	states := map[dpf.RecordsState]dpf.Record{}
	for _, r := range recs {
		t.Logf("並存の確認: id=%s state=%d labels=%v", r.Id, r.State, r.Labels)
		states[r.State] = r
	}
	if len(recs) != 2 {
		t.Fatalf("専用レコードが %d 件ある（追加予定と削除予定の 2 件であること）", len(recs))
	}
	recovered, ok := states[dpf.RECORDSSTATE__1]
	if !ok {
		t.Fatal("追加予定のレコードが無い")
	}
	if _, ok := states[dpf.RECORDSSTATE__2]; !ok {
		t.Fatal("削除予定のレコードが無い")
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 2 {
		t.Fatalf("未反映件数が %d 件（追加予定と削除予定で 2 件であること）", n)
	}

	// 回復後の解放: 追加予定を取り消すと削除予定だけが残り、
	// 次のゾーン反映で公開済みのレコードが消える（FR-008）。
	syncWaiter(t, c, "回復後の追加予定の取り消し")(
		api.RecordsAPI.DeleteRecordChanges(ctx, zone.Id, recovered.Id).Execute())
	requireLockRecord(t, ctx, c, zone.Id, dpf.RECORDSSTATE__2, "解放後")

	applyZone(t, ctx, c, zone.Id, 1, "premise: remove published lock record")
	if recs := lockRecords(t, ctx, c, zone.Id); len(recs) != 0 {
		for _, r := range recs {
			t.Logf("残存: id=%s state=%d", r.Id, r.State)
		}
		t.Fatalf("反映後も専用レコードが %d 件ある", len(recs))
	}
	if n := pendingCount(t, ctx, c, zone.Id); n != 0 {
		t.Fatalf("反映後の未反映件数が %d 件（0 件であること）", n)
	}
	t.Log("削除予定の反映で専用レコードは消えた")
}

// cleanupLockRecord は専用レコードを痕跡なく片付ける。
//
// 追加予定は取り消し、反映済みは削除して反映する。テストが途中で失敗しても
// 専用レコードを公開したまま残さないために、状態ごとに処理を分ける。
func cleanupLockRecord(t *testing.T, ctx context.Context, c *utils.Client, zoneID string) {
	t.Helper()

	api := c.GetAPIClient()
	needApply := false
	for _, r := range lockRecords(t, ctx, c, zoneID) {
		switch r.State {
		case dpf.RECORDSSTATE__1:
			t.Logf("後始末: 追加予定 (id=%s) を取り消す", r.Id)
			syncWaiter(t, c, "後始末の取り消し")(
				api.RecordsAPI.DeleteRecordChanges(ctx, zoneID, r.Id).Execute())
		case dpf.RECORDSSTATE__2:
			t.Logf("後始末: 削除予定 (id=%s) を反映する", r.Id)
			needApply = true
		case dpf.RECORDSSTATE__0:
			t.Logf("後始末: 反映済み (id=%s) を削除して反映する", r.Id)
			syncWaiter(t, c, "後始末の削除")(
				api.RecordsAPI.DeleteRecord(ctx, zoneID, r.Id).Execute())
			needApply = true
		default:
			t.Errorf("後始末: 想定しない state のレコードがある: id=%s state=%d", r.Id, r.State)
		}
	}

	if needApply {
		applyZone(t, ctx, c, zoneID, pendingCount(t, ctx, c, zoneID), "cleanup: lock record")
	}
	if recs := lockRecords(t, ctx, c, zoneID); len(recs) != 0 {
		t.Errorf("後始末後も専用レコードが %d 件残っている", len(recs))
	}
	if n := pendingCount(t, ctx, c, zoneID); n != 0 {
		t.Errorf("後始末後も未反映の編集が %d 件残っている", n)
	}
}
