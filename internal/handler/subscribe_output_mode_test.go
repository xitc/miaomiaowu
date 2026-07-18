package handler

import (
	"strings"
	"testing"

	"miaomiaowu/internal/storage"
)

func TestMatchesSubscribeNodeSelectionPrefersNodeIDs(t *testing.T) {
	node := storage.Node{ID: 2, Tags: []string{"selected-tag"}}
	selectedIDs := map[int64]bool{1: true}
	selectedTags := map[string]bool{"selected-tag": true}
	if matchesSubscribeNodeSelection(node, selectedIDs, selectedTags) {
		t.Fatal("node ID selection must take priority over matching tags")
	}
	node.ID = 1
	if !matchesSubscribeNodeSelection(node, selectedIDs, selectedTags) {
		t.Fatal("selected node ID should match")
	}
}

func TestMatchesSubscribeNodeSelectionFallsBackToTags(t *testing.T) {
	node := storage.Node{ID: 2, Tags: []string{"selected-tag"}}
	if !matchesSubscribeNodeSelection(node, nil, map[string]bool{"selected-tag": true}) {
		t.Fatal("tag should match when no node IDs are selected")
	}
	if !matchesSubscribeNodeSelection(node, nil, nil) {
		t.Fatal("empty selection should include all enabled nodes")
	}
}

func TestProviderTemplateExcludesProvidersWithoutSourceURL(t *testing.T) {
	configs := []storage.ProxyProviderConfig{
		{ID: 1, ExternalSubscriptionID: 10, Name: "valid", ProcessMode: "client"},
		{ID: 2, ExternalSubscriptionID: 20, Name: "missing", ProcessMode: "client"},
		{ID: 3, ExternalSubscriptionID: 30, Name: "server-mode", ProcessMode: "mmw"},
	}
	urls := map[int64]string{10: "https://example.com/valid.yaml", 30: "https://example.com/server.yaml"}
	usable := filterUsableClientProxyProviders(configs, urls)
	if len(usable) != 1 || usable[0].Name != "valid" {
		t.Fatalf("unexpected usable providers: %+v", usable)
	}

	template := `proxy-groups:
  - name: test
    type: select
    include-all-providers: true
`
	result, err := processProviderOnlyV3Template(template, configs, urls)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "valid:") || !strings.Contains(result, "- valid") {
		t.Fatalf("valid provider missing from output:\n%s", result)
	}
	if strings.Contains(result, "missing:") || strings.Contains(result, "- missing") {
		t.Fatalf("provider without URL leaked into output:\n%s", result)
	}
	if strings.Contains(result, "server-mode:") || strings.Contains(result, "- server-mode") {
		t.Fatalf("non-client provider leaked into output:\n%s", result)
	}
}
