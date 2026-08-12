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
