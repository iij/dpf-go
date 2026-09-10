// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 終了コード。詳細は doc.go を参照する。
const (
	exitOK       = 0
	exitViolated = 1
	exitError    = 2
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	violations, err := run(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: 検査できませんでした: %v\n", err)
		os.Exit(exitError)
	}

	if len(violations) == 0 {
		os.Exit(exitOK)
	}

	// 違反は全件を出す。最初の 1 件で打ち切らない。
	// 1 件ずつ直して実行し直す手戻りを避けるため。
	for _, v := range violations {
		fmt.Println(v)
	}
	fmt.Fprintf(os.Stderr, "\n%d 件の違反があります。\n", len(violations))
	fmt.Fprintf(os.Stderr, "missing-spdx    : 先頭行に %s を追加してください。\n", spdxHeader)
	fmt.Fprintln(os.Stderr, "                  生成物の場合は templates/partial_header.mustache を直して再生成すること。")
	fmt.Fprintln(os.Stderr, "preamble-pragma : nolint を対象行へ移してください。package 宣言より前の指示はファイル全体に効きます。")
	os.Exit(exitViolated)
}

// run は root 以下の Go ファイルを走査し、違反をパス順に返す。
func run(root string) ([]violation, error) {
	var violations []violation

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			// .git のような隠しディレクトリは走査しない。
			// vendor / testdata は skipPath と同じ理由で除外する。
			name := d.Name()
			if rel != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata") {
				return fs.SkipDir
			}
			return nil
		}

		if filepath.Ext(path) != ".go" || skipPath(rel) {
			return nil
		}

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		violations = append(violations, checkSource(rel, src)...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// WalkDir の順序は環境に依存しうるため、出力を安定させる。
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Path != violations[j].Path {
			return violations[i].Path < violations[j].Path
		}
		return violations[i].Kind < violations[j].Kind
	})

	return violations, nil
}
