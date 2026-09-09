// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"strings"
)

// violationKind は違反の種別である。出力の 2 列目に現れる。
type violationKind string

const (
	// kindMissingSPDX は先頭行に SPDX 識別子が無いことを示す。
	kindMissingSPDX violationKind = "missing-spdx"
	// kindPreamblePragma は package 宣言より前に nolint 指示があることを示す。
	kindPreamblePragma violationKind = "preamble-pragma"
)

// spdxHeader は全 Go ファイルの先頭行に要求される文字列である。
const spdxHeader = "// SPDX-License-Identifier: Apache-2.0"

// violation は 1 件の違反である。
type violation struct {
	// Path はリポジトリルートからの相対パスである。
	Path string
	// Kind は違反の種別である。
	Kind violationKind
	// Detail は人が読むための説明である。
	Detail string
}

// String は「パス<TAB>種別<TAB>詳細」の 1 行を返す。
// 機械的に読めるようにタブ区切りとする。
func (v violation) String() string {
	return fmt.Sprintf("%s\t%s\t%s", v.Path, v.Kind, v.Detail)
}

// skipPath は検査対象から除外するパスかどうかを返す。
//
// vendor と testdata の配下を除外する。パス要素として一致した場合のみ
// 除外するため、vendored や testdata_helper のようなディレクトリは
// 除外されない。
func skipPath(path string) bool {
	for _, elem := range strings.Split(path, "/") {
		if elem == "vendor" || elem == "testdata" {
			return true
		}
	}
	// Windows 由来の区切りが混ざった場合にも同じ判定をする。
	for _, elem := range strings.Split(path, "\\") {
		if elem == "vendor" || elem == "testdata" {
			return true
		}
	}
	return false
}

// checkSource は 1 ファイルの内容を検査し、違反を返す。
// 違反が無い場合は nil を返す。
//
// 返す順序は kindMissingSPDX、kindPreamblePragma で固定する。
// 出力の並びを実行ごとに変えないため。
func checkSource(path string, src []byte) []violation {
	var out []violation

	if !hasSPDXFirstLine(src) {
		out = append(out, violation{
			Path:   path,
			Kind:   kindMissingSPDX,
			Detail: "先頭行が " + spdxHeader + " でない",
		})
	}

	if line, ok := preamblePragmaLine(src); ok {
		out = append(out, violation{
			Path:   path,
			Kind:   kindPreamblePragma,
			Detail: fmt.Sprintf("%d 行目の nolint が package 宣言より前にあり、ファイル全体に効く", line),
		})
	}

	return out
}

// hasSPDXFirstLine は先頭行が要求どおりの SPDX 識別子かを返す。
//
// 先頭行だけを見る。2 行目以降にあっても不足として扱う。憲章が
// 「ファイルの先頭に」と定めているためである。
func hasSPDXFirstLine(src []byte) bool {
	line, _, _ := bytes.Cut(src, []byte("\n"))
	return strings.TrimRight(string(line), "\r") == spdxHeader
}

// preamblePragmaLine は package 宣言より前にある nolint 指示の行番号を返す。
//
// 見つからない場合は ok が false になる。package 宣言が現れた時点で
// 走査を終える。宣言より後の nolint は範囲が限定されており、
// 本コマンドの対象ではない。
//
// 判定は行頭（前後の空白を除いた先頭）が "//nolint" で始まるかで行う。
// コメント本文に nolint という語が出てくるだけの行は対象にしない。
func preamblePragmaLine(src []byte) (int, bool) {
	for i, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(raw)

		if isPackageClause(line) {
			return 0, false
		}
		if isNolintDirective(line) {
			return i + 1, true
		}
	}
	return 0, false
}

// isPackageClause は package 宣言の行かどうかを返す。
func isPackageClause(line string) bool {
	const clause = "package"
	if !strings.HasPrefix(line, clause) {
		return false
	}
	rest := line[len(clause):]
	return rest != "" && (rest[0] == ' ' || rest[0] == '\t')
}

// isNolintDirective は nolint 指示の行かどうかを返す。
//
// golangci-lint が指示として認識するのは "//nolint" で始まる形、すなわち
// スラッシュ 2 つと nolint の間に空白を入れない形である。同じ条件で判定する。
// 空白を入れた形は指示として扱われないため、本関数も検出しない。
func isNolintDirective(line string) bool {
	const directive = "//nolint"
	if !strings.HasPrefix(line, directive) {
		return false
	}
	rest := line[len(directive):]
	// "//nolint" 単体、"//nolint:linter"、"//nolint // 理由" のいずれも指示である。
	return rest == "" || rest[0] == ':' || rest[0] == ' ' || rest[0] == '\t'
}
