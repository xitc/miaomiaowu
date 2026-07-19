package handler

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"miaomiaowu/internal/storage"
	"miaomiaowu/internal/util"

	"gopkg.in/yaml.v3"
)

func TestStoreSubscriptionContentCacheReusesFreshSyncPayload(t *testing.T) {
	payload := []byte("proxies:\n  - name: cached\n    type: ss\n")
	var upstreamRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	defer InvalidateSubscriptionContentCache(server.URL)

	storeSubscriptionContentCache(server.URL, payload)
	sub := &storage.ExternalSubscription{URL: server.URL}

	got, err := fetchSubscriptionContent(sub)
	if err != nil {
		t.Fatalf("fetch cached subscription content: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("cached payload = %q, want %q", got, payload)
	}
	if got := upstreamRequests.Load(); got != 0 {
		t.Fatalf("upstream requests with primed cache = %d, want 0", got)
	}

	InvalidateSubscriptionContentCache(server.URL)
	got, err = fetchSubscriptionContent(sub)
	if err != nil {
		t.Fatalf("fetch subscription content after invalidation: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("refetched payload = %q, want %q", got, payload)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream requests after invalidation = %d, want 1", got)
	}
}

func TestRefreshProxyProviderCacheReusesParsedTreeAndIsolatesConfigs(t *testing.T) {
	content := []byte(`proxies:
  - name: keep-node
    type: ss
    server: keep.example.com
    port: 443
  - name: other-node
    type: trojan
    server: other.example.com
    port: 8443
`)
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		t.Fatalf("parse source yaml: %v", err)
	}
	source := findProxiesNode(&root)
	if source == nil {
		t.Fatal("source proxies node not found")
	}

	sub := &storage.ExternalSubscription{Name: "source", URL: "http://unused.invalid/sub"}
	keepConfig := &storage.ProxyProviderConfig{ID: 91001, Name: "keep", Filter: "^keep", Override: `{"udp":true}`}
	otherConfig := &storage.ProxyProviderConfig{ID: 91002, Name: "other", Filter: "^other"}
	defer GetProxyProviderCache().Delete(keepConfig.ID)
	defer GetProxyProviderCache().Delete(otherConfig.ID)

	keepEntry, err := refreshProxyProviderCacheFromNode(sub, keepConfig, source)
	if err != nil {
		t.Fatalf("refresh keep provider from parsed tree: %v", err)
	}
	otherEntry, err := refreshProxyProviderCacheFromNode(sub, otherConfig, source)
	if err != nil {
		t.Fatalf("refresh other provider from parsed tree: %v", err)
	}

	if keepEntry.NodeCount != 1 || len(keepEntry.NodeNames) != 1 || keepEntry.NodeNames[0] != "keep-node" {
		t.Fatalf("keep entry nodes = %#v, count=%d", keepEntry.NodeNames, keepEntry.NodeCount)
	}
	if otherEntry.NodeCount != 1 || len(otherEntry.NodeNames) != 1 || otherEntry.NodeNames[0] != "other-node" {
		t.Fatalf("other entry nodes = %#v, count=%d", otherEntry.NodeNames, otherEntry.NodeCount)
	}
	if got := len(source.Content); got != 2 {
		t.Fatalf("shared source node count after provider builds = %d, want 2", got)
	}
	for _, proxyNode := range source.Content {
		if got := util.GetNodeFieldValue(proxyNode, "udp"); got != "" {
			t.Fatalf("provider override mutated shared source tree: udp=%q", got)
		}
	}
}
