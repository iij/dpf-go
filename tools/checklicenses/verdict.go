// SPDX-License-Identifier: Apache-2.0

package main

import "strings"

// verdict は 1 つの依存に対する判定である。
type verdict string

const (
	// verdictAllowed は表示のみで足りるライセンスとして許容することを示す。
	verdictAllowed verdict = "allowed"
	// verdictAllowedReciprocal はソース提供義務を伴うが許容することを示す。
	// 違反ではないが、区別して報告する。
	verdictAllowedReciprocal verdict = "reciprocal"
	// verdictViolationDisallowed は許容リストに無いライセンスであることを示す。
	verdictViolationDisallowed verdict = "disallowed"
	// verdictViolationRootReciprocal は本体モジュールにソース提供義務を伴う
	// 依存が現れたことを示す。
	verdictViolationRootReciprocal verdict = "root-reciprocal"
	// verdictViolationUndetermined はライセンスを判別できないことを示す。
	verdictViolationUndetermined verdict = "undetermined"
)

// undeterminedID は go-licenses が判別できなかった場合に返す識別子である。
const undeterminedID = "Unknown"

// SPDX 式の演算子と、参照ライセンスの接頭辞。
const (
	operatorAND = "AND"
	operatorOR  = "OR"
	// licenseRefPrefix で始まる識別子は SPDX の一覧に無いライセンスへの参照で、
	// 内容を判断できない。
	licenseRefPrefix = "LicenseRef-"
)

// isViolation は判定が違反かどうかを返す。
func (v verdict) isViolation() bool {
	switch v {
	case verdictViolationDisallowed, verdictViolationRootReciprocal, verdictViolationUndetermined:
		return true
	default:
		return false
	}
}

// verdictOf は 1 つの依存の判定と、報告に残すライセンスを返す。
//
// isRoot は、この依存を持つモジュールが本体（github.com/iij/dpf-go）か
// どうかである。
//
// licenseID は単一の識別子だけでなく SPDX の複合式も受け取る。返す 2 つ目の値は
// 報告に残すライセンスで、OR で選択が生じた場合は選んだ側になる。それ以外は
// 入力をそのまま返す。
//
// 判定の順序は次のとおりで、先に一致したものを採る。
//
//  1. 判別不能
//  2. 許容リストに無い
//  3. 本体モジュールのソース提供義務つき
//  4. ソース提供義務つき（本体以外）
//  5. 許容
//
// 判別不能を最優先で見るのは、判別できないものを許容側へ倒さないためである。
// 識別子は大文字小文字を区別して照合する。SPDX の識別子がそうであるため、
// 揺れを許すと許容の範囲が曖昧になる。
func verdictOf(licenseID string, isRoot bool, allow map[string]obligation) (verdict, string) {
	if licenseID == "" || licenseID == undeterminedID {
		return verdictViolationUndetermined, licenseID
	}

	ob, selected, ok := evaluate(licenseID, allow)
	if !ok {
		return verdictViolationUndetermined, licenseID
	}
	if selected == "" {
		return verdictViolationDisallowed, licenseID
	}

	if ob == obligationReciprocal {
		if isRoot {
			return verdictViolationRootReciprocal, selected
		}
		return verdictAllowedReciprocal, selected
	}

	return verdictAllowed, selected
}

// evaluate は SPDX 式を許容リストと照合する。
//
// 返す値は、成立した場合の義務の区分、報告に残すライセンス、そして式を解釈できたか
// である。解釈できなかった場合 ok が false になり、呼び出し側は判別不能として扱う。
// 許容リストに収まらなかった場合は ok が true で selected が空になる。
//
// 扱うのは単一の識別子と、演算子を 1 種類だけ含む平坦な式（`A OR B`、`A AND B`）に
// 限る。括弧、AND と OR の混在、WITH による例外、LicenseRef はいずれも解釈しない。
// 優先順位や参照先の内容を推測して通すと、許容の範囲が実際より広くなる。解釈できない
// ものは判別不能へ倒す。憲章は判別不能を許容として扱うことを禁じている。
func evaluate(expr string, allow map[string]obligation) (obligation, string, bool) {
	if strings.ContainsAny(expr, "()") || strings.Contains(expr, "+") {
		return "", "", false
	}

	// 式は「被演算子 演算子 被演算子 ...」と交互に並び、被演算子で終わる。
	// したがって語数は必ず奇数になる。偶数なら演算子で終わっているか
	// 被演算子が欠けており、式として不完全である。
	//
	// ここを見ないと `MIT AND` のような不完全な式で被演算子が 1 つだけ残り、
	// 演算子を無視して単一のライセンスとして許容してしまう。`A AND B` が
	// 途中で切れた入力では B の義務が落ちるため、判定が実際より緩くなる。
	fields := strings.Fields(expr)
	if len(fields) == 0 || len(fields)%2 == 0 {
		return "", "", false
	}

	var operands []string
	var operator string
	for i, f := range fields {
		// SPDX の演算子は大文字で書く。小文字の and / or は演算子ではないため、
		// 識別子として扱えば許容リストに無いものになる。ここでは式の形を
		// 判断できないものとして倒す。
		if isSPDXOperator(f) {
			if i%2 == 0 {
				// 被演算子が来るべき位置に演算子がある。
				return "", "", false
			}
			if f == "WITH" {
				return "", "", false
			}
			if operator != "" && operator != f {
				// AND と OR の混在。優先順位が決まらない。
				return "", "", false
			}
			operator = f
			continue
		}
		if i%2 == 1 {
			// 演算子が来るべき位置に被演算子がある。
			return "", "", false
		}
		if strings.HasPrefix(f, licenseRefPrefix) {
			// 参照先の内容を判断できない。
			return "", "", false
		}
		operands = append(operands, f)
	}

	// 語数が奇数で交互に並ぶことを検証済みのため、被演算子は (語数+1)/2 個ある。
	if len(operands) == 1 {
		ob, ok := allow[operands[0]]
		if !ok {
			return "", "", true
		}
		return ob, operands[0], true
	}

	switch operator {
	case operatorOR:
		return evaluateOR(operands, allow)
	case operatorAND:
		return evaluateAND(expr, operands, allow)
	default:
		return "", "", false
	}
}

// evaluateOR は選択できる側を選ぶ。
//
// 許容リストに収まる選択肢が一つでもあれば許容とし、選んだ側を報告に残す。
// 表示のみで足りる選択肢を優先する。ソース提供義務は満たす手間がかかるため、
// 避けられるならそのほうがよい。
func evaluateOR(operands []string, allow map[string]obligation) (obligation, string, bool) {
	var reciprocal string
	for _, o := range operands {
		ob, ok := allow[o]
		if !ok {
			continue
		}
		if ob == obligationNotice {
			return obligationNotice, o, true
		}
		if reciprocal == "" {
			reciprocal = o
		}
	}
	if reciprocal != "" {
		return obligationReciprocal, reciprocal, true
	}
	return "", "", true
}

// evaluateAND はすべての被演算子が許容リストに収まることを求める。
//
// 一つでも収まらなければ許容しない。義務は最も重いものを採る。AND は選択では
// なく重畳であるため、報告には式全体を残す。
func evaluateAND(expr string, operands []string, allow map[string]obligation) (obligation, string, bool) {
	result := obligationNotice
	for _, o := range operands {
		ob, ok := allow[o]
		if !ok {
			return "", "", true
		}
		if ob == obligationReciprocal {
			result = obligationReciprocal
		}
	}
	return result, expr, true
}

// isSPDXOperator は SPDX 式の演算子かどうかを返す。
func isSPDXOperator(s string) bool {
	return s == operatorAND || s == operatorOR || s == "WITH"
}
