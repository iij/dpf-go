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

			got := verdictOf(tt.licenseID, tt.isRoot, allow)
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
