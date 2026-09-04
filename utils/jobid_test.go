// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetJobID(t *testing.T) {
	body := `{"request_id":"782d746ac3cb46499b31708fa80e8660","jobs_url":"https://api/jobs/782d746ac3cb46499b31708fa80e8660"}`
	res := &http.Response{Body: io.NopCloser(strings.NewReader(body))}

	got := GetJobID(res)
	if got != "782d746ac3cb46499b31708fa80e8660" {
		t.Fatalf("got %q, want request_id", got)
	}

	// ボディが復元され、再度読めること。
	rest, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("body not restored: %v", err)
	}
	if string(rest) != body {
		t.Errorf("restored body mismatch:\n got %q\nwant %q", rest, body)
	}
}

func TestGetJobID_NilAndEmpty(t *testing.T) {
	if got := GetJobID(nil); got != "" {
		t.Errorf("nil response: got %q, want empty", got)
	}
	res := &http.Response{Body: io.NopCloser(strings.NewReader(`{"foo":"bar"}`))}
	if got := GetJobID(res); got != "" {
		t.Errorf("no request_id: got %q, want empty", got)
	}
}
