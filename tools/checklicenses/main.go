// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// 終了コード。詳細は doc.go を参照する。
const (
	exitOK       = 0
	exitViolated = 1
	exitError    = 2
)

// rootModule は本体モジュールを指す -module の値である。
const rootModule = "."

func main() {
	var (
		module    = flag.String("module", "", "検査対象のモジュール（リポジトリルートからの相対パス。本体は \".\"）")
		allowPath = flag.String("allowlist", "licenses-allowlist.txt", "許容リストのパス")
	)
	flag.Parse()

	if *module == "" {
		fmt.Fprintln(os.Stderr, "ERROR: -module は必須です")
		os.Exit(exitError)
	}

	code, err := run(*module, *allowPath, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: 検査できませんでした: %v\n", err)
		os.Exit(exitError)
	}
	os.Exit(code)
}

// run は許容リストと入力を読み、判定を出力して終了コードを返す。
//
// 判定と解析は allowlist.go / input.go / verdict.go に委ね、ここでは
// 入出力の接続だけを行う。
func run(module, allowPath string, stdin *os.File, stdout *os.File) (int, error) {
	f, err := os.Open(allowPath)
	if err != nil {
		return 0, fmt.Errorf("許容リストを開けません: %w", err)
	}
	// 読み取り専用で開いているため、閉じる際の失敗に対して行える対処はない。
	defer func() { _ = f.Close() }()

	allow, err := parseAllowlist(f)
	if err != nil {
		return 0, fmt.Errorf("許容リスト %s: %w", allowPath, err)
	}

	records, err := parseRecords(stdin)
	if err != nil {
		return 0, fmt.Errorf("入力（go-licenses csv の出力）: %w", err)
	}

	isRoot := module == rootModule

	var violations, reciprocals []string
	for _, rec := range records {
		v := verdictOf(rec.LicenseID, isRoot, allow)
		switch {
		case v.isViolation():
			violations = append(violations, format(module, rec, v, why(module, rec.Package)))
		case v == verdictAllowedReciprocal:
			reciprocals = append(reciprocals, format(module, rec, v, "ソース提供義務あり。終了コードには影響しない"))
		}
	}

	// 出力の並びを実行ごとに変えない。
	sort.Strings(violations)
	sort.Strings(reciprocals)

	// ソース提供義務を伴う依存は、違反が無くても一覧として出す。
	//
	// 出力に失敗した場合は検査不能として扱う。報告できていないのに
	// 「違反なし」で終わると、判定の結果が伝わらないまま通ってしまう。
	for _, line := range append(reciprocals, violations...) {
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return 0, fmt.Errorf("結果を出力できませんでした: %w", err)
		}
	}

	if len(violations) == 0 {
		return exitOK, nil
	}

	fmt.Fprintf(os.Stderr, "\n%s: %d 件の違反があります。\n", module, len(violations))
	fmt.Fprintln(os.Stderr, "undetermined    : ライセンスを判別できません。許容として扱いません。")
	fmt.Fprintln(os.Stderr, "disallowed      : 許容リストにありません。追加するには先に憲章を改訂してください。")
	fmt.Fprintln(os.Stderr, "root-reciprocal : ソース提供義務を伴う依存が本体モジュールに入っています。")
	fmt.Fprintln(os.Stderr, "                  optional な機能は misc/ 配下の独立したモジュールへ隔離してください。")
	return exitViolated, nil
}

// format は 1 件の判定を「モジュール<TAB>パッケージ<TAB>区分<TAB>ライセンス<TAB>詳細」に整える。
func format(module string, rec record, v verdict, detail string) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s", module, rec.Package, v, rec.LicenseID, detail)
}

// why は指定パッケージがどの直接依存を経由して入ったかを返す。
//
// go-licenses の出力に経由元は含まれないため、go mod why で補う。違反した
// 依存に対してのみ呼ぶため、実行の負荷は違反の件数に比例する。
//
// 特定できなかった場合もエラーにはしない。違反そのものは既に検出しており、
// 経由元が分からないことを理由に検査を止める意味がないためである。
func why(module, pkg string) string {
	cmd := exec.Command("go", "mod", "why", pkg)
	cmd.Dir = module

	out, err := cmd.Output()
	if err != nil {
		return "経由元を特定できませんでした（go mod why " + pkg + " を手元で実行してください）"
	}

	// go mod why はコメント行（#）と、import の連鎖を出す。
	// 連鎖のうち最初の 2 つが「起点」と「経由した直接依存」である。
	var chain []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "(") {
			continue
		}
		chain = append(chain, line)
	}

	switch {
	case len(chain) == 0:
		return "経由元を特定できませんでした"
	case len(chain) == 1:
		return "直接依存"
	default:
		return "経由: " + strings.Join(chain[:2], " -> ")
	}
}
