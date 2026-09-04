package handler

import (
	"net/http/httptest"
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
	result, err := processProviderOnlyV3Template(template, configs, urls, nil, "")
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

func TestProviderTemplateUsesSubscriptionGatewayURL(t *testing.T) {
	configs := []storage.ProxyProviderConfig{{ID: 7, ExternalSubscriptionID: 10, Name: "valid", ProcessMode: "client"}}
	upstreamURLs := map[int64]string{10: "https://upstream.example/secret.yaml"}
	gatewayURLs := map[int64]string{7: "https://mmw.example/api/clash/subscribe?filename=user.yaml&mode=provider-source&provider_id=7&token=user-token"}
	template := `proxy-groups:
  - name: test
    type: select
    include-all-providers: true
`

	result, err := processProviderOnlyV3Template(template, configs, upstreamURLs, gatewayURLs, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, gatewayURLs[7]) {
		t.Fatalf("subscription gateway URL missing from output:\n%s", result)
	}
	if strings.Contains(result, upstreamURLs[10]) {
		t.Fatalf("upstream provider URL leaked into output:\n%s", result)
	}
}

func TestBuildSubscriptionProviderGatewayURLs(t *testing.T) {
	req := httptest.NewRequest("GET", "http://internal:8080/api/clash/subscribe?filename=user.yaml&mode=provider&t=clash&token=user-token", nil)
	req.Host = "sub.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	urls := buildSubscriptionProviderGatewayURLs(req, []storage.ProxyProviderConfig{{ID: 7}})
	got := urls[7]
	for _, expected := range []string{"https://sub.example.com/api/clash/subscribe?", "filename=user.yaml", "mode=provider-source", "provider_id=7", "token=user-token"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("gateway URL %q missing %q", got, expected)
		}
	}
	if strings.Contains(got, "t=clash") || strings.Contains(got, "mode=provider&") {
		t.Fatalf("gateway URL retained client/main-output parameters: %s", got)
	}
}

func TestBuildShortSubscriptionProviderGatewayURLDoesNotLeakInternalParameters(t *testing.T) {
	req := httptest.NewRequest("GET", "https://sub.example.com/abc123?filename=internal.yaml&mode=provider&token=unused", nil)

	got := buildSubscriptionProviderGatewayURLs(req, []storage.ProxyProviderConfig{{ID: 9}})[9]
	if !strings.HasPrefix(got, "https://sub.example.com/abc123?") {
		t.Fatalf("unexpected short gateway URL: %s", got)
	}
	if strings.Contains(got, "filename=") || strings.Contains(got, "token=") {
		t.Fatalf("short gateway URL leaked internal parameters: %s", got)
	}
}

func TestExpiredProviderResponseIsValidProviderPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &SubscriptionHandler{}
	handler.serveExpiredProviderResponse(recorder)

	if recorder.Code != 200 {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if !strings.HasPrefix(recorder.Body.String(), "proxies:\n") || !strings.Contains(recorder.Body.String(), "订阅已过期") {
		t.Fatalf("unexpected expired provider payload:\n%s", recorder.Body.String())
	}
}

func TestExpiredNormalResponseIsInvalidSubscriptionPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "https://sub.example.com/api/clash/subscribe?mode=normal", nil)
	handler := &SubscriptionHandler{}
	handler.serveTokenInvalidResponse(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expired normal response must not be cached: %q", recorder.Header().Get("Cache-Control"))
	}
	if !strings.Contains(recorder.Body.String(), "订阅已过期") {
		t.Fatalf("unexpected expired normal payload:\n%s", recorder.Body.String())
	}
}
