// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dpf "github.com/iij/dpf-go"
)

// zoneJSON は必須フィールドを満たす Zone の JSON 断片を返す。
func zoneJSON(id, name, serviceCode string) string {
	return `{"id":"` + id + `","common_config_id":1,"service_code":"` + serviceCode +
		`","state":1,"favorite":1,"name":"` + name + `","network":null,"description":""}`
}

// recordJSON は必須フィールドを満たす Record の JSON 断片を返す。
func recordJSON(id, name, rrtype string) string {
	return `{"id":"` + id + `","name":"` + name + `","ttl":3600,"rrtype":"` + rrtype +
		`","rdata":[],"labels":{},"state":1,"description":"","operator":null}`
}

// zonesServer は /zones と /zones/{id}/records を返すテストサーバを生成する。
func zonesServer(t *testing.T, zones []string, records []string) *dpf.APIClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/records"):
			_, _ = w.Write([]byte(`{"request_id":"r","results":[` + strings.Join(records, ",") + `]}`))
		case strings.HasSuffix(r.URL.Path, "/zones"):
			_, _ = w.Write([]byte(`{"request_id":"r","results":[` + strings.Join(zones, ",") + `]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := dpf.NewConfiguration()
	cfg.Servers = dpf.ServerConfigurations{{URL: srv.URL}}
	return dpf.NewAPIClient(cfg)
}

func TestGetZoneFromName_LongestMatch(t *testing.T) {
	c := zonesServer(t, []string{
		zoneJSON("zoneidcom00000", "com.", "svc-com"),
		zoneJSON("zoneidexample0", "example.com.", "svc-example"),
		zoneJSON("zoneidsub00000", "sub.example.com.", "svc-sub"),
	}, nil)

	cases := []struct {
		name   string
		parent bool
		wantID string
	}{
		{"www.sub.example.com.", false, "zoneidsub00000"},
		{"www.example.com.", false, "zoneidexample0"},
		{"example.com.", false, "zoneidexample0"}, // 完全一致
		{"example.com", false, "zoneidexample0"},  // 末尾ドット無しでも一致
		{"EXAMPLE.COM.", false, "zoneidexample0"}, // 大文字小文字を無視
		{"example.com.", true, "zoneidcom00000"},  // parent: 完全一致を除外し上位ゾーン
	}
	for _, tc := range cases {
		got, err := GetZoneFromName(context.Background(), c.ZonesAPI, tc.name, tc.parent)
		if err != nil {
			t.Fatalf("%s (parent=%v): unexpected error: %v", tc.name, tc.parent, err)
		}
		if got.Id != tc.wantID {
			t.Errorf("%s (parent=%v): got zone %q, want %q", tc.name, tc.parent, got.Id, tc.wantID)
		}
	}
}

func TestGetZoneFromName_NotFound(t *testing.T) {
	c := zonesServer(t, []string{zoneJSON("zoneidexample0", "example.com.", "svc")}, nil)
	_, err := GetZoneFromName(context.Background(), c.ZonesAPI, "example.org.", false)
	if !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("expected ErrZoneNotFound, got %v", err)
	}
}

func TestGetZoneIDFromZonename(t *testing.T) {
	c := zonesServer(t, []string{
		zoneJSON("zoneidcom00000", "com.", "svc-com"),
		zoneJSON("zoneidexample0", "example.com.", "svc-example"),
	}, nil)
	id, err := GetZoneIDFromZonename(context.Background(), c.ZonesAPI, "www.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "zoneidexample0" {
		t.Errorf("got %q, want zoneidexample0", id)
	}
}

func TestGetZoneFromServiceCode(t *testing.T) {
	c := zonesServer(t, []string{
		zoneJSON("zoneidexample0", "example.com.", "svc-example"),
		zoneJSON("zoneidother000", "other.com.", "svc-other"),
	}, nil)
	z, err := GetZoneFromServiceCode(context.Background(), c.ZonesAPI, "svc-other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if z.Id != "zoneidother000" {
		t.Errorf("got %q, want zoneidother000", z.Id)
	}

	if _, err := GetZoneFromServiceCode(context.Background(), c.ZonesAPI, "svc-missing"); !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("expected ErrZoneNotFound, got %v", err)
	}

	id, err := GetZoneIdFromServiceCode(context.Background(), c.ZonesAPI, "svc-other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "zoneidother000" {
		t.Errorf("got %q, want zoneidother000", id)
	}

	if _, err := GetZoneIdFromServiceCode(context.Background(), c.ZonesAPI, "svc-missing"); !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("expected ErrZoneNotFound, got %v", err)
	}
}

func TestGetRecordFromZoneID(t *testing.T) {
	c := zonesServer(t, nil, []string{
		recordJSON("rec-a", "www.example.com.", "A"),
		recordJSON("rec-aaaa", "www.example.com.", "AAAA"),
		recordJSON("rec-apex", "example.com.", "A"),
	})

	got, err := GetRecordFromZoneID(context.Background(), c.RecordsAPI, "zoneidexample0", "www.example.com.", dpf.RecordsRrtype("AAAA"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Id != "rec-aaaa" {
		t.Errorf("got %q, want rec-aaaa", got.Id)
	}

	// 名前は一致するが RRTYPE が無いケース。
	if _, err := GetRecordFromZoneID(context.Background(), c.RecordsAPI, "zoneidexample0", "www.example.com.", dpf.RecordsRrtype("MX")); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestGetRecordFromRecordName(t *testing.T) {
	c := zonesServer(t,
		[]string{
			zoneJSON("zoneidcom00000", "com.", "svc-com"),
			zoneJSON("zoneidexample0", "example.com.", "svc-example"),
		},
		[]string{
			recordJSON("rec-a", "www.example.com.", "A"),
		},
	)

	zone, rec, err := GetRecordFromRecordName(context.Background(), c.ZonesAPI, c.RecordsAPI, "www.example.com.", dpf.RecordsRrtype("A"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zone.Id != "zoneidexample0" {
		t.Errorf("zone: got %q, want zoneidexample0", zone.Id)
	}
	if rec.Id != "rec-a" {
		t.Errorf("record: got %q, want rec-a", rec.Id)
	}
}

func TestGetRecordFromZonename(t *testing.T) {
	c := zonesServer(t,
		[]string{zoneJSON("zoneidexample0", "example.com.", "svc-example")},
		[]string{recordJSON("rec-a", "www.example.com.", "A")},
	)

	zone, rec, err := GetRecordFromZonename(context.Background(), c.ZonesAPI, c.RecordsAPI, "example.com", "www.example.com.", dpf.RecordsRrtype("A"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zone.Id != "zoneidexample0" || rec.Id != "rec-a" {
		t.Errorf("got zone=%q rec=%q, want zoneidexample0/rec-a", zone.Id, rec.Id)
	}
}
