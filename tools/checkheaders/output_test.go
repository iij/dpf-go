// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestViolationString は出力 1 行の形式を固定する。
// contracts/gates.md が「違反 1 件につき 1 行、機械的に読める形」を契約として
// 定めているため、区切りと列の並びを変えられないようにする。
func TestViolationString(t *testing.T) {
	t.Parallel()

	v := violation{
		Path:   "internal/integration/main_test.go",
		Kind:   kindMissingSPDX,
		Detail: "先頭行が // SPDX-License-Identifier: Apache-2.0 でない",
	}

	got := v.String()

	fields := strings.Split(got, "\t")
	if len(fields) != 3 {
		t.Fatalf("タブ区切りの 3 列であること: got %d 列 (%q)", len(fields), got)
	}
	if fields[0] != v.Path {
		t.Errorf("1 列目はパス: got %q, want %q", fields[0], v.Path)
	}
	if fields[1] != string(v.Kind) {
		t.Errorf("2 列目は種別: got %q, want %q", fields[1], v.Kind)
	}
	if fields[2] != v.Detail {
		t.Errorf("3 列目は詳細: got %q, want %q", fields[2], v.Detail)
	}
	if strings.Contains(got, "\n") {
		t.Error("1 件は 1 行であること: 改行が含まれている")
	}
}

func TestViolationStringKinds(t *testing.T) {
	t.Parallel()

	// 種別の文字列は契約であり、変えると出力を読む側が壊れる。
	if kindMissingSPDX != "missing-spdx" {
		t.Errorf("kindMissingSPDX = %q, want %q", kindMissingSPDX, "missing-spdx")
	}
	if kindPreamblePragma != "preamble-pragma" {
		t.Errorf("kindPreamblePragma = %q, want %q", kindPreamblePragma, "preamble-pragma")
	}
}

// TestRun は走査の対象と除外、および出力の並びを検証する。
func TestRun(t *testing.T) {
	t.Parallel()

	const spdx = "// SPDX-License-Identifier: Apache-2.0\n"

	// 検査されるべきファイルと、除外されるべきファイルを混ぜて置く。
	files := map[string]string{
		// 違反あり（検査対象）
		"b_violation.go":                  "package a\n",
		"a_violation_test.go":             "package a\n",
		"api_generated.go":                "package a\n",
		filepath.Join("sub", "nested.go"): "package sub\n",
		filepath.Join("sub", "pragma.go"): spdx + "\n//nolint:errcheck // 理由\n\npackage sub\n",
		// 違反なし（検査対象）
		"ok.go": spdx + "\npackage a\n",
		// 除外されるべきもの
		filepath.Join("vendor", "dep.go"):            "package dep\n",
		filepath.Join("testdata", "input.go"):        "package testdata\n",
		filepath.Join("sub", "testdata", "input.go"): "package testdata\n",
		filepath.Join(".hidden", "hidden.go"):        "package hidden\n",
		"notgo.txt":                                  "SPDX なし\n",
		filepath.Join("vendored", "included.go"):     spdx + "\npackage vendored\n",
	}

	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	violations, err := run(root)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	got := make([]string, 0, len(violations))
	for _, v := range violations {
		got = append(got, v.Path+"\t"+string(v.Kind))
	}

	// パス順に並ぶこと。走査順は環境に依存しうるため、出力を安定させている。
	want := []string{
		"a_violation_test.go\t" + string(kindMissingSPDX),
		"api_generated.go\t" + string(kindMissingSPDX),
		"b_violation.go\t" + string(kindMissingSPDX),
		"sub/nested.go\t" + string(kindMissingSPDX),
		"sub/pragma.go\t" + string(kindPreamblePragma),
	}

	if len(got) != len(want) {
		t.Fatalf("違反の件数が違う:\n got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("違反[%d] が違う: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRunMissingRoot は走査できない場合にエラーを返すことを確認する。
// 「検査できなかった」を「違反なし」として扱ってはならない。
func TestRunMissingRoot(t *testing.T) {
	t.Parallel()

	if _, err := run(filepath.Join(t.TempDir(), "存在しない")); err == nil {
		t.Error("存在しないディレクトリではエラーを返すこと")
	}
}
