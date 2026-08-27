package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLookupGeoIPInfoReturnsRichDataAndCaches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ip":"8.8.8.8","country_code":"us","country":"United States","asn":"AS15169","as_name":"Google LLC"}`)
	}))
	defer server.Close()

	oldBase, oldClient := geoIPAPIBase, geoIPClient
	geoIPAPIBase = server.URL
	geoIPClient = server.Client()
	geoIPCache = sync.Map{}
	t.Cleanup(func() {
		geoIPAPIBase, geoIPClient = oldBase, oldClient
		geoIPCache = sync.Map{}
	})

	for i := 0; i < 2; i++ {
		got := lookupGeoIPInfo(context.Background(), "8.8.8.8")
		if got.CountryCode != "US" || got.Country != "United States" || got.ASN != "AS15169" || got.ASName != "Google LLC" {
			t.Fatalf("lookupGeoIPInfo() = %#v", got)
		}
	}
	if got := describeIPLocation(context.Background(), "8.8.8.8"); got != "United States (US) · AS15169 Google LLC" {
		t.Fatalf("describeIPLocation() = %q", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 due to cache", got)
	}
}

func TestLookupGeoIPInfoCachesFailures(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	oldBase, oldClient := geoIPAPIBase, geoIPClient
	geoIPAPIBase = server.URL
	geoIPClient = server.Client()
	geoIPCache = sync.Map{}
	t.Cleanup(func() {
		geoIPAPIBase, geoIPClient = oldBase, oldClient
		geoIPCache = sync.Map{}
	})

	_ = lookupGeoIPInfo(context.Background(), "1.1.1.1")
	_ = lookupGeoIPInfo(context.Background(), "1.1.1.1")
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1 due to negative cache", got)
	}
}
