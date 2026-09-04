package handler

import (
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

func TestFilterExternalSubsNeedingSync(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-5 * time.Minute)
	stale := now.Add(-40 * time.Minute)
	subs := []storage.ExternalSubscription{
		{Name: "fresh", LastSyncAt: &fresh},
		{Name: "stale", LastSyncAt: &stale},
		{Name: "never", LastSyncAt: nil},
	}
	need := filterExternalSubsNeedingSync(subs, 30)
	if len(need) != 2 {
		t.Fatalf("want 2, got %d", len(need))
	}
	names := map[string]bool{}
	for _, s := range need {
		names[s.Name] = true
	}
	if !names["stale"] || !names["never"] || names["fresh"] {
		t.Fatalf("unexpected set: %v", names)
	}

	all := filterExternalSubsNeedingSync(subs, 0)
	if len(all) != 3 {
		t.Fatalf("cache=0 should sync all, got %d", len(all))
	}
}

func TestSplitExternalSyncUrgency(t *testing.T) {
	now := time.Now()
	stale := now.Add(-time.Hour)
	subs := []storage.ExternalSubscription{
		{Name: "first", LastSyncAt: nil},
		{Name: "expired", LastSyncAt: &stale},
	}

	// Provider: everything background
	b, bg := splitExternalSyncUrgency(subs, storage.OutputModeProvider)
	if len(b) != 0 || len(bg) != 2 {
		t.Fatalf("provider: blocking=%d bg=%d", len(b), len(bg))
	}

	// Normal: first-time blocks, expired backgrounds
	b, bg = splitExternalSyncUrgency(subs, storage.OutputModeNormal)
	if len(b) != 1 || b[0].Name != "first" {
		t.Fatalf("normal blocking=%v", b)
	}
	if len(bg) != 1 || bg[0].Name != "expired" {
		t.Fatalf("normal bg=%v", bg)
	}
}

func TestSelectedProviderMetadataKeepsExternalReferenceWhenOutputIsEmpty(t *testing.T) {
	file := storage.SubscribeFile{SelectedTags: []string{"Provider/am"}}
	configs := []storage.ProxyProviderConfig{
		{ID: 38, Name: "am", ExternalSubscriptionID: 41, ProcessMode: "client"},
		{ID: 39, Name: "all", ExternalSubscriptionID: 44, ProcessMode: "client"},
	}
	subs := []storage.ExternalSubscription{
		{ID: 41, URL: "https://provider.example/am.yaml"},
		{ID: 44, URL: "https://provider.example/all.yaml"},
	}
	got := selectedProviderExternalSubscriptionURLs(file, storage.OutputModeNormal, configs, subs)
	if !got["https://provider.example/am.yaml"] || got["https://provider.example/all.yaml"] || len(got) != 1 {
		t.Fatalf("unexpected selected Provider URLs: %v", got)
	}
}

func TestProviderModeEmptySelectionReferencesAllClientProviders(t *testing.T) {
	file := storage.SubscribeFile{}
	configs := []storage.ProxyProviderConfig{
		{Name: "client", ExternalSubscriptionID: 1, ProcessMode: "client"},
		{Name: "mmw", ExternalSubscriptionID: 2, ProcessMode: "mmw"},
	}
	subs := []storage.ExternalSubscription{{ID: 1, URL: "client-url"}, {ID: 2, URL: "mmw-url"}}
	got := selectedProviderExternalSubscriptionURLs(file, storage.OutputModeProvider, configs, subs)
	if !got["client-url"] || got["mmw-url"] || len(got) != 1 {
		t.Fatalf("unexpected default Provider URLs: %v", got)
	}
}
