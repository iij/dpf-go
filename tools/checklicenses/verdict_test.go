// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestVerdictOf(t *testing.T) {
	t.Parallel()

	allow := map[string]obligation{
		"Apache-2.0": obligationNotice,
		"MIT":        obligationNotice,
		"MPL-2.0":    obligationReciprocal,
	}

	tests := []struct {
		name      string
		licenseID string
		isRoot    bool
		want      verdict
	}{
		{
			name:      "許容リストにある表示のみのライセンス",
			licenseID: "Apache-2.0",
			want:      verdictAllowed,
		},
		{
			name:      "本体でないモジュールのソース提供義務つきは許容",
			licenseID: "MPL-2.0",
			isRoot:    false,
			want:      verdictAllowedReciprocal,
		},
		{
			name:      "本体モジュールのソース提供義務つきは違反",
			licenseID: "MPL-2.0",
			isRoot:    true,
			want:      verdictViolationRootReciprocal,
		},
		{
			name:      "許容リストに無いライセンスは違反",
			licenseID: "GPL-3.0",
			want:      verdictViolationDisallowed,
		},
		{
			name:      "本体モジュールでも許容外は disallowed（root-reciprocal ではない）",
			licenseID: "GPL-3.0",
			isRoot:    true,
			want:      verdictViolationDisallowed,
		},
		{
			name:      "判別不能は違反。許容側へ倒さない",
			licenseID: "Unknown",
			want:      verdictViolationUndetermined,
		},
		{
			name:      "空の識別子も判別不能として扱う",
			licenseID: "",
			want:      verdictViolationUndetermined,
		},
		{
			name:      "判別不能は許容リストの有無より先に見る",
			licenseID: "Unknown",
			isRoot:    true,
			want:      verdictViolationUndetermined,
		},
		{
			name:      "大文字小文字が違うものは許容リストに無いものとして扱う",
			licenseID: "apache-2.0",
			want:      verdictViolationDisallowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, _ := verdictOf(tt.licenseID, tt.isRoot, allow)
			if got != tt.want {
				t.Errorf("verdictOf(%q, isRoot=%v) = %q, want %q", tt.licenseID, tt.isRoot, got, tt.want)
			}
		})
	}
}

func TestVerdictIsViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		v    verdict
		want bool
	}{
		{verdictAllowed, false},
		{verdictAllowedReciprocal, false},
		{verdictViolationDisallowed, true},
		{verdictViolationRootReciprocal, true},
		{verdictViolationUndetermined, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.v), func(t *testing.T) {
			t.Parallel()

			if got := tt.v.isViolation(); got != tt.want {
				t.Errorf("%q.isViolation() = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestVerdictOfExpression(t *testing.T) {
	t.Parallel()

	allow := map[string]obligation{
		"Apache-2.0":   obligationNotice,
		"MIT":          obligationNotice,
		"BSD-3-Clause": obligationNotice,
		"MPL-2.0":      obligationReciprocal,
	}

	tests := []struct {
		name      string
		licenseID string
		isRoot    bool
		want      verdict
		// wantLicense は報告に残すライセンス。OR では選択したものになる。
		wantLicense string
	}{
		{
			name:        "OR: 両方が許容なら、表示のみの側を選ぶ",
			licenseID:   "MPL-2.0 OR Apache-2.0",
			want:        verdictAllowed,
			wantLicense: "Apache-2.0",
		},
		{
			name:        "OR: 片方だけ許容ならそれを選ぶ",
			licenseID:   "GPL-3.0 OR MIT",
			want:        verdictAllowed,
			wantLicense: "MIT",
		},
		{
			name:        "OR: 許容がソース提供義務つきだけならそれを選ぶ",
			licenseID:   "GPL-3.0 OR MPL-2.0",
			want:        verdictAllowedReciprocal,
			wantLicense: "MPL-2.0",
		},
		{
			name:        "OR: 本体モジュールでソース提供義務つきしか選べないなら違反",
			licenseID:   "GPL-3.0 OR MPL-2.0",
			isRoot:      true,
			want:        verdictViolationRootReciprocal,
			wantLicense: "MPL-2.0",
		},
		{
			name:        "OR: 本体モジュールでも表示のみが選べるなら許容",
			licenseID:   "MPL-2.0 OR MIT",
			isRoot:      true,
			want:        verdictAllowed,
			wantLicense: "MIT",
		},
		{
			name:        "OR: どれも許容リストに無ければ違反",
			licenseID:   "GPL-3.0 OR AGPL-3.0",
			want:        verdictViolationDisallowed,
			wantLicense: "GPL-3.0 OR AGPL-3.0",
		},
		{
			name:        "AND: すべて許容なら許容",
			licenseID:   "Apache-2.0 AND BSD-3-Clause",
			want:        verdictAllowed,
			wantLicense: "Apache-2.0 AND BSD-3-Clause",
		},
		{
			name:        "AND: 一つでも許容外なら違反",
			licenseID:   "Apache-2.0 AND GPL-3.0",
			want:        verdictViolationDisallowed,
			wantLicense: "Apache-2.0 AND GPL-3.0",
		},
		{
			name:        "AND: ソース提供義務つきを含むなら義務つきとして扱う",
			licenseID:   "Apache-2.0 AND MPL-2.0",
			want:        verdictAllowedReciprocal,
			wantLicense: "Apache-2.0 AND MPL-2.0",
		},
		{
			name:        "AND: 本体モジュールで義務つきを含むなら違反",
			licenseID:   "Apache-2.0 AND MPL-2.0",
			isRoot:      true,
			want:        verdictViolationRootReciprocal,
			wantLicense: "Apache-2.0 AND MPL-2.0",
		},
		{
			name:        "演算子は大文字のみを演算子として扱う（SPDX の規定）",
			licenseID:   "MIT or GPL-3.0",
			want:        verdictViolationUndetermined,
			wantLicense: "MIT or GPL-3.0",
		},
		{
			name:        "AND と OR が混在する式は判別不能とする（優先順位が決まらない）",
			licenseID:   "MIT OR Apache-2.0 AND MPL-2.0",
			want:        verdictViolationUndetermined,
			wantLicense: "MIT OR Apache-2.0 AND MPL-2.0",
		},
		{
			name:        "括弧を含む式は判別不能とする",
			licenseID:   "(MIT OR Apache-2.0) AND BSD-3-Clause",
			want:        verdictViolationUndetermined,
			wantLicense: "(MIT OR Apache-2.0) AND BSD-3-Clause",
		},
		{
			name:        "WITH 例外つきの式は判別不能とする",
			licenseID:   "Apache-2.0 WITH LLVM-exception",
			want:        verdictViolationUndetermined,
			wantLicense: "Apache-2.0 WITH LLVM-exception",
		},
		{
			name:        "LicenseRef は判別不能とする",
			licenseID:   "LicenseRef-a6e830174d62dafad3a718384772ea1f",
			want:        verdictViolationUndetermined,
			wantLicense: "LicenseRef-a6e830174d62dafad3a718384772ea1f",
		},
		{
			name:        "AND に LicenseRef を含む式は判別不能とする",
			licenseID:   "Apache-2.0 AND LicenseRef-deadbeef",
			want:        verdictViolationUndetermined,
			wantLicense: "Apache-2.0 AND LicenseRef-deadbeef",
		},
		{
			name:        "演算子だけで被演算子が無い式は判別不能とする",
			licenseID:   "OR",
			want:        verdictViolationUndetermined,
			wantLicense: "OR",
		},
		{
			name:        "AND が末尾に残る不完全な式は判別不能とする",
			licenseID:   "MIT AND",
			want:        verdictViolationUndetermined,
			wantLicense: "MIT AND",
		},
		{
			name:        "OR が末尾に残る不完全な式は判別不能とする",
			licenseID:   "MIT OR",
			want:        verdictViolationUndetermined,
			wantLicense: "MIT OR",
		},
		{
			name:        "末尾に演算子が残る式は、許容側のライセンスでも判別不能とする",
			licenseID:   "MPL-2.0 AND",
			want:        verdictViolationUndetermined,
			wantLicense: "MPL-2.0 AND",
		},
		{
			name:        "被演算子が 3 つ以上の AND も扱える",
			licenseID:   "Apache-2.0 AND MIT AND BSD-3-Clause",
			want:        verdictAllowed,
			wantLicense: "Apache-2.0 AND MIT AND BSD-3-Clause",
		},
		{
			name:        "被演算子が 3 つ以上の OR も扱える",
			licenseID:   "GPL-3.0 OR AGPL-3.0 OR MIT",
			want:        verdictAllowed,
			wantLicense: "MIT",
		},
		{
			name:        "被演算子が 3 つの AND に許容外を含むなら違反",
			licenseID:   "Apache-2.0 AND GPL-3.0 AND MIT",
			want:        verdictViolationDisallowed,
			wantLicense: "Apache-2.0 AND GPL-3.0 AND MIT",
		},
		{
			name:        "空白のみの識別子は判別不能とする",
			licenseID:   "   ",
			want:        verdictViolationUndetermined,
			wantLicense: "   ",
		},
		{
			name:        "or-later を示す + つきの識別子は判別不能とする",
			licenseID:   "GPL-2.0+",
			want:        verdictViolationUndetermined,
			wantLicense: "GPL-2.0+",
		},
		{
			name:        "単一のライセンスは式として扱っても結果が変わらない",
			licenseID:   "MIT",
			want:        verdictAllowed,
			wantLicense: "MIT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotLicense := verdictOf(tt.licenseID, tt.isRoot, allow)
			if got != tt.want {
				t.Errorf("verdictOf(%q, isRoot=%v) の判定 = %q, want %q", tt.licenseID, tt.isRoot, got, tt.want)
			}
			if gotLicense != tt.wantLicense {
				t.Errorf("verdictOf(%q, isRoot=%v) の報告 = %q, want %q", tt.licenseID, tt.isRoot, gotLicense, tt.wantLicense)
			}
		})
	}
}
