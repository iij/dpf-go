// SPDX-License-Identifier: Apache-2.0

package main

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

// isViolation は判定が違反かどうかを返す。
func (v verdict) isViolation() bool {
	switch v {
	case verdictViolationDisallowed, verdictViolationRootReciprocal, verdictViolationUndetermined:
		return true
	default:
		return false
	}
}

// verdictOf は 1 つの依存の判定を返す。
//
// isRoot は、この依存を持つモジュールが本体（github.com/iij/dpf-go）か
// どうかである。
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
func verdictOf(licenseID string, isRoot bool, allow map[string]obligation) verdict {
	if licenseID == "" || licenseID == undeterminedID {
		return verdictViolationUndetermined
	}

	ob, ok := allow[licenseID]
	if !ok {
		return verdictViolationDisallowed
	}

	if ob == obligationReciprocal {
		if isRoot {
			return verdictViolationRootReciprocal
		}
		return verdictAllowedReciprocal
	}

	return verdictAllowed
}
