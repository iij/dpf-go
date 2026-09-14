// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestParseWhy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "間接依存は起点と経由した直接依存を示す",
			out: "# github.com/hashicorp/errwrap\n" +
				"github.com/iij/dpf-go/misc/vault\n" +
				"github.com/hashicorp/vault/api\n" +
				"github.com/hashicorp/errwrap\n",
			want: "経由: github.com/iij/dpf-go/misc/vault -> github.com/hashicorp/vault/api",
		},
		{
			name: "連鎖が 1 つだけなら直接依存",
			out:  "# github.com/miekg/dns\ngithub.com/iij/dpf-go\n",
			want: "直接依存",
		},
		{
			name: "モジュールが不要な場合の括弧行は読み飛ばす",
			out: "# gopkg.in/yaml.v3\n" +
				"(main module does not need package gopkg.in/yaml.v3)\n",
			want: "経由元を特定できませんでした",
		},
		{
			name: "コメント行だけなら特定できない",
			out:  "# github.com/example/foo\n",
			want: "経由元を特定できませんでした",
		},
		{
			name: "空の出力なら特定できない",
			out:  "",
			want: "経由元を特定できませんでした",
		},
		{
			name: "空白だけの出力なら特定できない",
			out:  "\n  \n\t\n",
			want: "経由元を特定できませんでした",
		},
		{
			name: "連鎖が 3 つ以上でも最初の 2 つだけを示す",
			out: "# example.com/d\n" +
				"example.com/a\n" +
				"example.com/b\n" +
				"example.com/c\n" +
				"example.com/d\n",
			want: "経由: example.com/a -> example.com/b",
		},
		{
			name: "行頭の空白は取り除く",
			out:  "# example.com/b\n\texample.com/a\n\texample.com/b\n",
			want: "経由: example.com/a -> example.com/b",
		},
		{
			name: "複数のモジュールが並ぶ出力でも最初の 2 つを示す",
			out: "# example.com/x\n" +
				"example.com/a\n" +
				"example.com/x\n" +
				"\n" +
				"# example.com/y\n" +
				"example.com/a\n" +
				"example.com/y\n",
			want: "経由: example.com/a -> example.com/x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parseWhy([]byte(tt.out)); got != tt.want {
				t.Errorf("parseWhy() = %q, want %q", got, tt.want)
			}
		})
	}
}
