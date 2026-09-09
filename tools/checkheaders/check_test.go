// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"
)

func TestCheckFile(t *testing.T) {
	t.Parallel()

	const spdx = "// SPDX-License-Identifier: Apache-2.0\n"

	tests := []struct {
		name string
		src  string
		want []violationKind
	}{
		{
			name: "先頭行に SPDX がある",
			src:  spdx + "\npackage foo\n",
			want: nil,
		},
		{
			name: "SPDX が無い",
			src:  "package foo\n",
			want: []violationKind{kindMissingSPDX},
		},
		{
			name: "SPDX が 2 行目にある（先頭でなければ不足）",
			src:  "// 説明\n" + spdx + "\npackage foo\n",
			want: []violationKind{kindMissingSPDX},
		},
		{
			name: "SPDX の前に空行がある（先頭でなければ不足）",
			src:  "\n" + spdx + "package foo\n",
			want: []violationKind{kindMissingSPDX},
		},
		{
			name: "別のライセンスの SPDX は不足として扱う",
			src:  "// SPDX-License-Identifier: MIT\n\npackage foo\n",
			want: []violationKind{kindMissingSPDX},
		},
		{
			name: "ビルドタグがあっても先頭行の SPDX を要求する",
			src:  "//go:build integration\n\npackage foo\n",
			want: []violationKind{kindMissingSPDX},
		},
		{
			name: "SPDX の後にビルドタグがあるのは通す",
			src:  spdx + "\n//go:build integration\n\npackage foo\n",
			want: nil,
		},
		{
			name: "package 宣言より前の nolint を検出する",
			src:  spdx + "\n//nolint:errcheck\n\npackage foo\n",
			want: []violationKind{kindPreamblePragma},
		},
		{
			name: "package 宣言より前の nolint は理由と linter 名を伴っても検出する",
			src:  spdx + "\n//nolint:errcheck // 理由をここに書いた\n\npackage foo\n",
			want: []violationKind{kindPreamblePragma},
		},
		{
			name: "package 宣言より前の nolint:all も検出する",
			src:  spdx + "\n//nolint:all // 全部止める\n\npackage foo\n",
			want: []violationKind{kindPreamblePragma},
		},
		{
			name: "対象行に限定された nolint は検出しない（FR-017 の領域）",
			src:  spdx + "\npackage foo\n\nfunc f() {\n\t_ = 1 //nolint:errcheck // 理由\n}\n",
			want: nil,
		},
		{
			name: "package 宣言後の行頭 nolint も検出しない",
			src:  spdx + "\npackage foo\n\n//nolint:errcheck // 理由\nvar x = 1\n",
			want: nil,
		},
		{
			name: "SPDX 欠落と広域 nolint は両方報告する",
			src:  "//nolint:errcheck // 理由\n\npackage foo\n",
			want: []violationKind{kindMissingSPDX, kindPreamblePragma},
		},
		{
			name: "コメントの中の nolint という語だけでは検出しない",
			src:  spdx + "\n// nolint の扱いについての説明\n\npackage foo\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := checkSource("x.go", []byte(tt.src))
			if len(got) != len(tt.want) {
				t.Fatalf("違反の件数が違う: got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i].Kind != tt.want[i] {
					t.Errorf("違反[%d] の種別が違う: got %q, want %q", i, got[i].Kind, tt.want[i])
				}
			}
		})
	}
}

func TestSkipPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{filepath.Join("vendor", "x.go"), true},
		{filepath.Join("a", "vendor", "x.go"), true},
		{filepath.Join("testdata", "x.go"), true},
		{filepath.Join("a", "testdata", "b", "x.go"), true},
		{filepath.Join("tools", "checkheaders", "main.go"), false},
		{"api_zones.go", false},
		{filepath.Join("internal", "integration", "main_test.go"), false},
		// vendor / testdata で始まるだけの名前は除外しない
		{filepath.Join("vendored", "x.go"), false},
		{filepath.Join("testdata_helper", "x.go"), false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := skipPath(tt.path); got != tt.want {
				t.Errorf("skipPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
