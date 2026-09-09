// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// obligation はライセンスに伴う義務の区分である。
type obligation string

const (
	// obligationNotice は表示のみで足りることを示す。
	obligationNotice obligation = "notice"
	// obligationReciprocal はソース提供義務を伴うことを示す。
	obligationReciprocal obligation = "reciprocal"
)

// parseAllowlist は許容リストを読み込む。
//
// 形式は「<SPDX 識別子><空白><区分>」の 1 行 1 件で、# から行末までを
// コメントとして無視する。識別子は大文字小文字を区別する。SPDX の識別子が
// そうであるためで、揺れを許すと許容の範囲が曖昧になる。
//
// 重複した識別子はエラーとする。どちらが有効か曖昧になるため。
// 未知の区分もエラーとする。新しい区分を黙って notice として扱うと、
// ソース提供義務を見落とす。
// 空のリストもエラーとする。設定漏れを「すべて許容外」でも
// 「すべて許容」でもなく、検査不能として扱うため。
func parseAllowlist(r io.Reader) (map[string]obligation, error) {
	out := make(map[string]obligation)

	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		fields := strings.Fields(text)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%d 行目: 2 列（識別子と区分）である必要があります: %q", line, sc.Text())
		}

		id, ob := fields[0], obligation(fields[1])
		switch ob {
		case obligationNotice, obligationReciprocal:
		default:
			return nil, fmt.Errorf("%d 行目: 未知の区分 %q（notice または reciprocal）", line, fields[1])
		}

		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("%d 行目: 識別子 %q が重複しています", line, id)
		}
		out[id] = ob
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("許容リストの読み込みに失敗しました: %w", err)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("許容リストが空です")
	}
	return out, nil
}
