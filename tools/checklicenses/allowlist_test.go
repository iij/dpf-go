// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestParseAllowlist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    map[string]obligation
		wantErr string
	}{
		{
			name: "正常なリスト",
			src:  "Apache-2.0\tnotice\nMPL-2.0\treciprocal\n",
			want: map[string]obligation{
				"Apache-2.0": obligationNotice,
				"MPL-2.0":    obligationReciprocal,
			},
		},
		{
			name: "コメントと空行を無視する",
			src:  "# 憲章の許容リストと一対一で対応する\n\nMIT\tnotice\n\n# 末尾のコメント\n",
			want: map[string]obligation{"MIT": obligationNotice},
		},
		{
			name: "行末のコメントを無視する",
			src:  "ISC\tnotice # 表示のみ\n",
			want: map[string]obligation{"ISC": obligationNotice},
		},
		{
			name: "空白の連続を区切りとして扱う",
			src:  "BSD-3-Clause   notice\n",
			want: map[string]obligation{"BSD-3-Clause": obligationNotice},
		},
		{
			name:    "同じ識別子の重複はエラー",
			src:     "MIT\tnotice\nMIT\treciprocal\n",
			wantErr: "重複",
		},
		{
			name:    "未知の区分はエラー",
			src:     "MIT\tweak-copyleft\n",
			wantErr: "未知の区分",
		},
		{
			name:    "区分が無い行はエラー",
			src:     "MIT\n",
			wantErr: "2 列",
		},
		{
			name:    "列が多すぎる行はエラー",
			src:     "MIT\tnotice\textra\n",
			wantErr: "2 列",
		},
		{
			name: "大文字小文字は区別する（別物として扱う）",
			src:  "MIT\tnotice\nmit\tnotice\n",
			want: map[string]obligation{"MIT": obligationNotice, "mit": obligationNotice},
		},
		{
			name:    "空のリストはエラー（設定漏れを通さない）",
			src:     "# 何も定義していない\n",
			wantErr: "空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseAllowlist(strings.NewReader(tt.src))
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
			for id, ob := range tt.want {
				if got[id] != ob {
					t.Errorf("%s の区分が違う: got %q, want %q", id, got[id], ob)
				}
			}
		})
	}
}
