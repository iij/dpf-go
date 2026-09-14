// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestParseRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    []record
		wantErr string
	}{
		{
			name: "3 列を読む",
			src:  "github.com/miekg/dns,https://example.com/LICENSE,BSD-3-Clause\n",
			want: []record{{
				Package:    "github.com/miekg/dns",
				LicenseURL: "https://example.com/LICENSE",
				LicenseID:  "BSD-3-Clause",
			}},
		},
		{
			name: "複数行を読む",
			src: "a/b,https://x/L,MIT\n" +
				"c/d,https://y/L,Apache-2.0\n",
			want: []record{
				{Package: "a/b", LicenseURL: "https://x/L", LicenseID: "MIT"},
				{Package: "c/d", LicenseURL: "https://y/L", LicenseID: "Apache-2.0"},
			},
		},
		{
			name: "Unknown は判別不能としてそのまま保持する",
			src:  "a/b,Unknown,Unknown\n",
			want: []record{{Package: "a/b", LicenseURL: "Unknown", LicenseID: "Unknown"}},
		},
		{
			name: "空行は無視する",
			src:  "a/b,https://x/L,MIT\n\n",
			want: []record{{Package: "a/b", LicenseURL: "https://x/L", LicenseID: "MIT"}},
		},
		{
			name:    "列が少ない行はエラー（無視してはならない）",
			src:     "a/b,MIT\n",
			wantErr: "3 列",
		},
		{
			name:    "列が多い行はエラー（上流の形式変更を通さない）",
			src:     "a/b,https://x/L,MIT,extra\n",
			wantErr: "3 列",
		},
		{
			name:    "識別子が空の行はエラー",
			src:     "a/b,https://x/L,\n",
			wantErr: "空",
		},
		{
			name:    "パッケージが空の行はエラー",
			src:     ",https://x/L,MIT\n",
			wantErr: "空",
		},
		{
			name:    "入力が空はエラー（検査対象を取り違えたことを通さない）",
			src:     "",
			wantErr: "空",
		},
		{
			// csv は空行を返さないが、引用符だけの行は 1 列として返す。
			// 列数の異なる行は無視せず中断する（契約）。
			name:    "引用符だけの行は 1 列としてエラー",
			src:     "\"\"\n",
			wantErr: "3 列",
		},
		{
			name:    "正常な行に混ざった 1 列の行もエラー",
			src:     "a/b,https://x/L,MIT\n\"\"\nc/d,https://y/L,MIT\n",
			wantErr: "3 列",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseRecords(strings.NewReader(tt.src))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("エラーを期待したが nil: got %v", got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("エラーに %q を含むことを期待: got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("件数が違う: got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("record[%d] が違う: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
