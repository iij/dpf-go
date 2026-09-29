// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"fmt"

	dpf "github.com/iij/dpf-go"
	"github.com/miekg/dns"
)

// ErrNoRecords は、編集の結果が空だった場合に返される。
// ゾーンの一括置き換えはレコードを 1 件以上必要とする。
//
// 空は「何も変更しない」ことを意味しない。**ゾーンの全削除と区別できないため、失敗として
// 扱う。** 反映が不要な場合は ErrSkipApply を返すこと。
var ErrNoRecords = errors.New("dpf: no records to apply")

// ErrSkipApply は、編集の結果を反映しないことを表す合図である。**失敗ではない。**
//
// ZoneApplier.Apply は編集関数がこれを返すと、一括置き換えを行わずに排他を解放し、nil を
// 返す。公開されているレコードを読んだ結果、変える必要が無かった場合に使う。
//
// 意味を持つのは ZoneApplier.Apply へ渡す編集関数の戻り値だけである。RunLocked や
// Mutex.Do へ渡した処理が返した場合は、他のエラーと同じくそのまま呼び出し側へ返る。
var ErrSkipApply = errors.New("dpf: skip zone apply")

// ZoneRecordsEditor はゾーンへ反映したいレコードの一覧を決める。
//
// 受け取る一覧は、公開されているレコードを変換したものである。**入力と出力が同じ型
// であるため、編集しない要素はそのまま返せばよい。** ラベル・TTL・コメントを写し替える
// 必要はなく、写し忘れて失う心配もない。
//
// ctx は ZoneApplier.Apply へ渡されたものから派生しており、排他を他者に奪われた時点で
// 打ち切られる。外部から情報を読んで編集内容を決める場合は、この打ち切りに従うこと。
// 打ち切られた場合は ErrSkipApply ではなく ctx.Err() を返すこと。
//
// 反映が不要だと分かった場合は ErrSkipApply を返す。一括置き換えは行われず、Apply は nil を
// 返す。エラーを返す場合、返した一覧は使われない。
//
// 省略の判定は errors.Is で行う。このため**包んだ場合、そのメッセージは捨てられる。**
// Apply は反映したか省略したかを呼び出し側へ返さないため、理由を残したい場合はこの関数の
// 中で記録するか、クロージャの変数へ控えること。errors.Join(ErrSkipApply, 本物のエラー) を
// 返してはならない。省略が優先され、もう一方が失われる。
type ZoneRecordsEditor func(ctx context.Context, records []dpf.OverwriteRecordsInner) ([]dpf.OverwriteRecordsInner, error)

// ZoneApplier はゾーン全体の一括置き換えを、ゾーン単位の排他の下で行う。
//
// 生成された API を直接使う場合、次の 2 つを自分で扱う必要がある。いずれも間違えても
// その場では成功したように見えるため、この型がまとめて引き受ける。
//
//  1. SOA レコードと Zone Apex の NS レコードを取り込むか否かのフラグ。**既定は
//     「取り込む」である。** 公開されているレコードから組み立てたリクエストをそのまま
//     送ると、これらが意図せず置き換わる。本型は常に「取り込まない」を指定する。
//  2. 公開されているレコードの一覧は、編集中の SOA を「更新前の状態」として返す。
//     レコードの一覧を取得する API が「更新予定」として返すのとは見え方が異なる。
//
// **一括置き換えは排他に影響しない。** 置き換えの対象はレコードであり、排他の状態は
// ゾーンのラベルにあるためである。反映の後も排他は保持され、終了時に通常どおり解放される。
// 排他の仕組みを WithLocker で差し替えても同じである。
//
// 排他は内部に持ち、公開しない。レコードを 1 つずつ変更してゾーンへ反映する流れでは、
// この型ではなく RunLocked（または Mutex.Do）を使うこと。
type ZoneApplier struct {
	locker      Locker
	holdOptions []HoldOption
	cr          dpf.RecordsApi
	cz          dpf.ZonesApi
	cj          dpf.JobsApi
	zoneID      string
}

// applierConfig は ZoneApplier の設定。
type applierConfig struct {
	locker      Locker
	lockOptions []Option
	holdOptions []HoldOption
}

// ApplierOption は ZoneApplier の任意設定を変更する。
type ApplierOption func(*applierConfig)

// WithLocker は排他の仕組みを差し替える（デフォルト: レコードを用いる Mutex）。
//
// etcd などの分散ロックを使う場合に指定する。指定した場合、WithLockOptions は意味を
// 持たない（既定の排他を作らないため）。
//
// **外部の仕組みを使う排他に替えると、2 者の同時取得を防ぐ範囲が狭まる。** 既定の
// ゾーンのラベルを用いる排他は、DPF-API の重複の拒否によってユーザをまたいで防ぐ。外部の
// 仕組みでは、その仕組みを使うプログラム同士でしか防げない。詳しくはパッケージ文書の
// 比較を参照。
func WithLocker(l Locker) ApplierOption {
	return func(c *applierConfig) {
		if l != nil {
			c.locker = l
		}
	}
}

// WithLockOptions は既定の排他（レコードを用いる Mutex）への設定を渡す。
// WithLocker を指定した場合は意味を持たない。
//
//	utils.NewZoneApplier(cr, cz, cj, zoneID,
//		utils.WithLockOptions(utils.WithTTL(30*time.Minute)))
func WithLockOptions(opts ...Option) ApplierOption {
	return func(c *applierConfig) {
		c.lockOptions = append(c.lockOptions, opts...)
	}
}

// WithHoldOptions は排他を保持したままの実行への設定を渡す（WithLockWait など）。
func WithHoldOptions(opts ...HoldOption) ApplierOption {
	return func(c *applierConfig) {
		c.holdOptions = append(c.holdOptions, opts...)
	}
}

// applyConfig は Apply 1 回分の設定。
type applyConfig struct {
	description string
}

// ApplyOption は Apply の任意設定を変更する。
type ApplyOption func(*applyConfig)

// WithApplyDescription は反映に付けるコメントを指定する（デフォルト: 空）。
func WithApplyDescription(description string) ApplyOption {
	return func(c *applyConfig) {
		c.description = description
	}
}

// NewZoneApplier はゾーン zoneID に対する一括置き換えを生成する。
//   - cr     : レコード API（*dpf.RecordsAPIService が利用できる）
//   - cz     : ゾーン API（*dpf.ZonesAPIService が利用できる）
//   - cj     : JOB API（*dpf.JobsAPIService が利用できる）
//   - zoneID : 対象ゾーンの ID
//
// opts で排他の仕組みを差し替えたり（WithLocker）、既定の排他へ設定を渡したり
// （WithLockOptions）できる。指定しない場合は、ゾーンのラベルを用いる既定の排他が使われる。
func NewZoneApplier(cr dpf.RecordsApi, cz dpf.ZonesApi, cj dpf.JobsApi, zoneID string, opts ...ApplierOption) *ZoneApplier {
	cfg := &applierConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.locker == nil {
		cfg.locker = NewMutex(cr, cz, zoneID, cfg.lockOptions...)
	}

	return &ZoneApplier{
		locker:      cfg.locker,
		holdOptions: cfg.holdOptions,
		cr:          cr,
		cz:          cz,
		cj:          cj,
		zoneID:      zoneID,
	}
}

// Apply は排他を取得し、公開されているレコードを edit へ渡し、その結果でゾーン全体を
// 置き換えて反映する。反映の完了を待ってから復帰する。
//
// 手順は次のとおりである。
//
//  1. 排他を取得する（取得できない場合は ErrStillLock。WithLockWait を指定すれば待つ）
//  2. 公開されているレコードを取得し、リクエストの形へ変換する
//  3. edit を呼ぶ。ErrSkipApply が返った場合は 4・5 を行わず nil を返す。返された一覧が
//     空の場合は ErrNoRecords
//  4. 一括置き換えと反映を行う（SOA と Zone Apex の NS は取り込まない）
//  5. 反映の完了を待つ
//
// 反映しても省略しても、終了時に排他を解放する。**一括置き換えは排他に影響しないため、
// 反映の後も保持されている。** 解放に失敗した場合はそのエラーが返る。
//
// 省略しても、排他の取得・公開されているレコードの取得・解放は行われる（レート制限を
// 消費する）。減るのは一括置き換えと反映の待ちであり、**公開されているゾーンのシリアルと
// 履歴が動かない**ことが省略の効果である。
//
// edit が返した一覧に SOA レコードまたは Zone Apex の NS レコードが含まれていない
// 場合は、変換前の一覧のものを補う。いずれも取り込まれないため内容に影響しないが、
// リクエストの必須項目を満たすためである。利用者はこれらを意識する必要がない。
//
// edit の実行中も保持期間は自動で延長される。排他を他者に奪われた場合は edit へ渡した
// context が打ち切られ、ErrNotLockHolder が返る。詳細は Mutex.Do を参照。
func (a *ZoneApplier) Apply(ctx context.Context, edit ZoneRecordsEditor, opts ...ApplyOption) error {
	if edit == nil {
		return fmt.Errorf("dpf: ZoneApplier.Apply: 編集の内容を表す関数が nil である")
	}
	cfg := &applyConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 省略の合図を nil へ変換するのは、この関数の**内側**でなければならない。
	// runLockedHold は処理がエラーを返すと解放のエラーを捨て、排他を失っていた場合は
	// errors.Join で包んで返す。外側で変換すると、包まれた排他の喪失にも errors.Is が
	// 一致してしまい、解放の失敗と排他の喪失がどちらも消える。
	return runLockedHold(ctx, a.locker, func(ctx context.Context, h *hold) error {
		err := a.apply(ctx, h, edit, cfg)
		if errors.Is(err, ErrSkipApply) {
			// 打ち切られていた場合は省略を成功にしない。省略の経路は API を 1 つも
			// 呼ばないため、打ち切りが表面化する経路が他に無い。
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			return nil
		}
		return err
	}, a.holdOptions...)
}

// apply は排他の保護下で行う本体。
func (a *ZoneApplier) apply(ctx context.Context, h *hold, edit ZoneRecordsEditor, cfg *applyConfig) error {
	current, err := a.currents(ctx)
	if err != nil {
		return err
	}

	edited, err := edit(ctx, current)
	if err != nil {
		return err
	}
	if len(edited) == 0 {
		return ErrNoRecords
	}
	edited = fillRequiredRecords(edited, current)

	// 一括置き換えが排他を解くと申告する実装では、自動延長を止め、終了時の無条件の
	// 解放を行わないようにする。この位置より早いと編集中の延長が止まり、遅いと反映中に
	// 延長が走ってしまう。
	//
	// **009 以降、この申告を行う実装は無い。** 既定の排他はゾーンのラベルを使い、
	// レコードの一括更新とゾーン反映はゾーンのラベルに影響しない。申告は utils の外から
	// 実装できない非公開の形であるため、現在この分岐へ入る経路は無い。008 で定めた抽象の
	// 一部であり、削除は別の機能として扱う（specs/009-zone-label-lock/research.md D11）。
	if consumesLockOnZoneApply(a.locker) {
		h.consume()
	}

	body := dpf.PatchZoneAtomicChanges{
		Records:             edited,
		OverwriteSoa:        dpf.PtrBool(false),
		OverwriteZoneApexNs: dpf.PtrBool(false),
	}
	if cfg.description != "" {
		body.Description = dpf.PtrString(cfg.description)
	}

	async, resp, err := a.cz.PatchZoneAtomicChanges(ctx, a.zoneID).
		PatchZoneAtomicChanges(body).Execute()
	if _, _, err := a.cj.SyncWaitContext(ctx, async, resp, err); err != nil {
		return err
	}
	return nil
}

// currents は公開されているレコードを取得し、リクエストの形へ変換する。
//
// 名前・TTL・RRTYPE・レコードの値・コメント・ラベルをすべて引き継ぐ。編集関数が
// 受け取った要素をそのまま返せば、これらは失われない。
func (a *ZoneApplier) currents(ctx context.Context) ([]dpf.OverwriteRecordsInner, error) {
	list, _, err := a.cr.GetRecordCurrents(ctx, a.zoneID).ExecuteAll()
	if err != nil {
		return nil, err
	}
	if list == nil {
		return nil, ErrRecordNotFound
	}

	out := make([]dpf.OverwriteRecordsInner, 0, len(list.Results))
	for i := range list.Results {
		r := &list.Results[i]
		out = append(out, dpf.OverwriteRecordsInner{
			Name:        r.Name,
			Ttl:         r.Ttl,
			Rrtype:      r.Rrtype,
			Rdata:       r.Rdata,
			Description: r.Description,
			Labels:      r.Labels,
		})
	}
	return out, nil
}

// fillRequiredRecords は、編集の結果に SOA レコードと Zone Apex の NS レコードが
// 含まれていなければ、変換前の一覧のものを補う。
//
// いずれも取り込まないフラグを指定するため内容に影響しない。リクエストの必須項目を
// 満たすために補う。ゾーン名は SOA レコードの名前から導く（SOA はゾーンの apex に
// 1 つだけ存在する）。名前の比較は miekg/dns で行う。
func fillRequiredRecords(edited, current []dpf.OverwriteRecordsInner) []dpf.OverwriteRecordsInner {
	var zoneName string
	for _, r := range current {
		if r.Rrtype == dpf.RECORDSRRTYPE_SOA {
			zoneName = dns.CanonicalName(r.Name)
			break
		}
	}

	isApexNS := func(r *dpf.OverwriteRecordsInner) bool {
		return zoneName != "" && r.Rrtype == dpf.RECORDSRRTYPE_NS &&
			dns.CanonicalName(r.Name) == zoneName
	}

	hasSOA, hasApexNS := false, false
	for i := range edited {
		if edited[i].Rrtype == dpf.RECORDSRRTYPE_SOA {
			hasSOA = true
		}
		if isApexNS(&edited[i]) {
			hasApexNS = true
		}
	}

	for i := range current {
		r := &current[i]
		if !hasSOA && r.Rrtype == dpf.RECORDSRRTYPE_SOA {
			edited = append(edited, *r)
			hasSOA = true
			continue
		}
		if !hasApexNS && isApexNS(r) {
			edited = append(edited, *r)
			hasApexNS = true
		}
	}
	return edited
}
