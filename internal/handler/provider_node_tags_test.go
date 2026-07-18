package handler

import (
	"testing"

	"miaomiaowu/internal/storage"
)

func TestReconcileProviderNodeTags(t *testing.T) {
	tag := "Provider/HK"
	nodes := []storage.Node{
		{ID: 1, RawURL: "https://example.com/sub", NodeName: "HK-1", ClashConfig: `{"name":"HK-1","type":"ss","server":"hk.example.com","port":443}`, Tag: "source", Tags: []string{"source"}},
		{ID: 2, RawURL: "https://example.com/sub", NodeName: "JP-1", ClashConfig: `{"name":"JP-1","type":"ss","server":"jp.example.com","port":443}`, Tag: "source", Tags: []string{"source", tag}},
		{ID: 3, RawURL: "https://other.example/sub", NodeName: "HK-1", ClashConfig: `{"name":"HK-1","type":"ss","server":"hk.example.com","port":443}`, Tag: "other", Tags: []string{"other"}},
	}
	providerNodes := []any{
		map[string]any{"name": "renamed-by-override", "type": "ss", "server": "hk.example.com", "port": 443},
	}

	changed := reconcileProviderNodeTags(nodes, "https://example.com/sub", tag, nil, providerNodes)
	if len(changed) != 2 {
		t.Fatalf("changed nodes = %d, want 2", len(changed))
	}
	byID := map[int64]storage.Node{}
	for _, node := range changed {
		byID[node.ID] = node
	}
	if !hasTag(byID[1].Tags, tag) {
		t.Fatalf("matching node tags = %v, want %q", byID[1].Tags, tag)
	}
	if hasTag(byID[2].Tags, tag) {
		t.Fatalf("filtered node retained stale tag: %v", byID[2].Tags)
	}
	if _, ok := byID[3]; ok {
		t.Fatal("node from another subscription must not change")
	}
}

func TestReconcileProviderNodeTagsRemovesRenamedTag(t *testing.T) {
	oldTag := "Provider/Old"
	newTag := "Provider/New"
	nodes := []storage.Node{{
		ID: 1, RawURL: "u", NodeName: "n", ClashConfig: `{"name":"n","type":"ss","server":"s","port":1}`,
		Tag: "source", Tags: []string{"source", oldTag},
	}}
	providerNodes := []any{map[string]any{"name": "n", "type": "ss", "server": "s", "port": 1}}

	changed := reconcileProviderNodeTags(nodes, "u", newTag, []string{oldTag}, providerNodes)
	if len(changed) != 1 || hasTag(changed[0].Tags, oldTag) || !hasTag(changed[0].Tags, newTag) {
		t.Fatalf("renamed tags not reconciled: %+v", changed)
	}
}

func TestProviderNodeTagUsesNamespace(t *testing.T) {
	got := providerNodeTag(storage.ProxyProviderConfig{Name: " 香港 "})
	if got != "Provider/香港" {
		t.Fatalf("provider tag = %q", got)
	}
}

func TestMissingProviderNodesLoadsIntoPool(t *testing.T) {
	sub := storage.ExternalSubscription{Username: "admin", Name: "source", URL: "u"}
	existing := []storage.Node{{
		ID: 1, Username: "admin", RawURL: "u", NodeName: "existing",
		ClashConfig: `{"name":"existing","type":"ss","server":"a","port":1}`,
	}}
	providerNodes := []any{
		map[string]any{"name": "existing-renamed", "type": "ss", "server": "a", "port": 1},
		map[string]any{"name": "new", "type": "trojan", "server": "b", "port": 443},
	}

	missing := missingProviderNodes(existing, sub, "Provider/HK", providerNodes)
	if len(missing) != 1 {
		t.Fatalf("missing nodes = %d, want 1", len(missing))
	}
	if missing[0].NodeName != "new" || missing[0].Protocol != "trojan" || !hasTag(missing[0].Tags, "Provider/HK") || !hasTag(missing[0].Tags, "source") {
		t.Fatalf("unexpected imported node: %+v", missing[0])
	}
}
