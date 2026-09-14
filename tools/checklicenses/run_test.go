// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAllowlist は許容リストを一時ファイルに書き、そのパスを返す。
func writeAllowlist(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "licenses-allowlist.txt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	return path
}

const testAllowlist = "Apache-2.0\tnotice\nMIT\tnotice\nMPL-2.0\treciprocal\n"

// TestFormat は出力 1 行の形式を固定する。
// contracts/gates.md が機械的に読める形を契約として定めている。
func TestFormat(t *testing.T) {
	t.Parallel()

	rec := record{
		Package:    "github.com/hashicorp/vault/api",
		LicenseURL: "https://example.com/LICENSE",
		LicenseID:  "MPL-2.0",
	}

	got := format("misc/vault", rec, verdictAllowedReciprocal, "ソース提供義務あり")

	fields := strings.Split(got, "\t")
	if len(fields) != 5 {
		t.Fatalf("タブ区切りの 5 列であること: got %d 列 (%q)", len(fields), got)
	}
	want := []string{"misc/vault", rec.Package, string(verdictAllowedReciprocal), rec.LicenseID, "ソース提供義務あり"}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("%d 列目: got %q, want %q", i+1, fields[i], want[i])
		}
	}
	if strings.Contains(got, "\n") {
		t.Error("1 件は 1 行であること: 改行が含まれている")
	}
}

func TestRunExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// module が "." のとき本体モジュールとして扱われる。
		module   string
		input    string
		wantCode int
		// wantOut は標準出力に含まれるべき文字列。
		wantOut []string
		// notOut は標準出力に含まれてはならない文字列。
		notOut []string
	}{
		{
			name:     "違反なしなら 0",
			module:   "misc/vault",
			input:    "a/b,https://x/L,MIT\n",
			wantCode: exitOK,
			notOut:   []string{"a/b"},
		},
		{
			name:     "ソース提供義務つきは一覧に出るが終了コードに影響しない",
			module:   "misc/vault",
			input:    "a/b,https://x/L,MPL-2.0\n",
			wantCode: exitOK,
			wantOut:  []string{"a/b", string(verdictAllowedReciprocal), "MPL-2.0"},
		},
		{
			name:     "許容リストに無ければ 1",
			module:   "misc/vault",
			input:    "a/b,https://x/L,GPL-3.0\n",
			wantCode: exitViolated,
			wantOut:  []string{"a/b", string(verdictViolationDisallowed)},
		},
		{
			name:     "判別不能なら 1",
			module:   "misc/vault",
			input:    "a/b,Unknown,Unknown\n",
			wantCode: exitViolated,
			wantOut:  []string{string(verdictViolationUndetermined)},
		},
		{
			name:     "本体モジュールのソース提供義務つきは 1",
			module:   ".",
			input:    "a/b,https://x/L,MPL-2.0\n",
			wantCode: exitViolated,
			wantOut:  []string{string(verdictViolationRootReciprocal)},
		},
		{
			name:     "義務つきと違反が混在する場合も 1 で、両方が出る",
			module:   "misc/vault",
			input:    "a/b,https://x/L,MPL-2.0\nc/d,https://y/L,GPL-3.0\n",
			wantCode: exitViolated,
			wantOut:  []string{"a/b", "c/d", string(verdictAllowedReciprocal), string(verdictViolationDisallowed)},
		},
		{
			name:     "OR で選択した側が報告に残る",
			module:   "misc/vault",
			input:    "a/b,https://x/L,GPL-3.0 OR MPL-2.0\n",
			wantCode: exitOK,
			wantOut:  []string{"MPL-2.0"},
			notOut:   []string{"GPL-3.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			code, err := run(tt.module, writeAllowlist(t, testAllowlist), strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if code != tt.wantCode {
				t.Errorf("終了コード = %d, want %d (出力: %q)", code, tt.wantCode, out.String())
			}
			for _, w := range tt.wantOut {
				if !strings.Contains(out.String(), w) {
					t.Errorf("出力に %q を含むこと: got %q", w, out.String())
				}
			}
			for _, n := range tt.notOut {
				if strings.Contains(out.String(), n) {
					t.Errorf("出力に %q を含まないこと: got %q", n, out.String())
				}
			}
		})
	}
}

// TestRunUnavailable は検査できなかった場合にエラーを返すことを確認する。
// 「検査できなかった」を「違反なし」として扱ってはならない。
func TestRunUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		allowlist string
		input     string
		// allowPath を空にすると存在しないパスを渡す。
		missingAllowlist bool
	}{
		{
			name:             "許容リストが無い",
			input:            "a/b,https://x/L,MIT\n",
			missingAllowlist: true,
		},
		{
			name:      "許容リストが空",
			allowlist: "# 何も定義していない\n",
			input:     "a/b,https://x/L,MIT\n",
		},
		{
			name:      "許容リストの区分が未知",
			allowlist: "MIT\tweak-copyleft\n",
			input:     "a/b,https://x/L,MIT\n",
		},
		{
			name:      "入力の列数が違う",
			allowlist: testAllowlist,
			input:     "a/b,MIT\n",
		},
		{
			name:      "入力が空",
			allowlist: testAllowlist,
			input:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "存在しない.txt")
			if !tt.missingAllowlist {
				path = writeAllowlist(t, tt.allowlist)
			}

			var out bytes.Buffer
			if _, err := run("misc/vault", path, strings.NewReader(tt.input), &out); err == nil {
				t.Error("エラーを返すこと（検査不能を違反なしとして扱わない）")
			}
		})
	}
}
