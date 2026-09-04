package handler

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"miaomiaowu/internal/storage"

	"gopkg.in/yaml.v3"
)

const amadeusNodeNameFilter = "剩余|流量|到期|订阅|时间|重置|2026|政企"

var providerAllConfig = storage.ProxyProviderConfig{
	ID:                     39,
	ExternalSubscriptionID: 10,
	Name:                   "all",
	ProcessMode:            "client",
}

const providerAllTemplate = `proxy-groups:
  - name: test
    type: select
    include-all-providers: true
`

func TestUsableNodeNameFilterPatternMatchesSyncPath(t *testing.T) {
	t.Parallel()

	if got := usableNodeNameFilterPattern(""); got != "" {
		t.Fatalf("empty pattern should be skipped, got %q", got)
	}
	if got := usableNodeNameFilterPattern("   "); got != "" {
		t.Fatalf("whitespace pattern should be skipped, got %q", got)
	}
	if got := usableNodeNameFilterPattern("[invalid"); got != "" {
		t.Fatalf("invalid regex should be skipped like sync, got %q", got)
	}
	if got := usableNodeNameFilterPattern("  " + amadeusNodeNameFilter + "  "); got != amadeusNodeNameFilter {
		t.Fatalf("valid pattern should be trimmed, got %q", got)
	}
}

func TestMergeProxyProviderExcludeFilterZhengqi(t *testing.T) {
	t.Parallel()

	merged := mergeProxyProviderExcludeFilter("", amadeusNodeNameFilter)
	if merged != amadeusNodeNameFilter {
		t.Fatalf("empty provider exclude should use node_name_filter as-is, got %q", merged)
	}
	assertExcludeFilterMatches(t, merged, "香港 政企专线", true)
	assertExcludeFilterMatches(t, merged, "剩余流量: 123GB", true)
	assertExcludeFilterMatches(t, merged, "香港01", false)
}

func TestMergeProxyProviderExcludeFilterKeepsBothRules(t *testing.T) {
	t.Parallel()

	merged := mergeProxyProviderExcludeFilter("custom-drop", amadeusNodeNameFilter)
	if !strings.Contains(merged, "custom-drop") || !strings.Contains(merged, "政企") {
		t.Fatalf("merged exclude-filter lost a rule: %q", merged)
	}
	assertExcludeFilterMatches(t, merged, "custom-drop-node", true)
	assertExcludeFilterMatches(t, merged, "上海政企", true)
	assertExcludeFilterMatches(t, merged, "东京01", false)
}

func TestMergeProxyProviderExcludeFilterEmptyAndInvalid(t *testing.T) {
	t.Parallel()

	if got := mergeProxyProviderExcludeFilter("", ""); got != "" {
		t.Fatalf("both empty should stay empty, got %q", got)
	}
	if got := mergeProxyProviderExcludeFilter("keep-me", ""); got != "keep-me" {
		t.Fatalf("empty node_name_filter must keep provider exclude, got %q", got)
	}
	if got := mergeProxyProviderExcludeFilter("keep-me", "   "); got != "keep-me" {
		t.Fatalf("whitespace node_name_filter must keep provider exclude, got %q", got)
	}
	if got := mergeProxyProviderExcludeFilter("keep-me", "[invalid"); got != "keep-me" {
		t.Fatalf("invalid node_name_filter must keep provider exclude, got %q", got)
	}
	if got := mergeProxyProviderExcludeFilter("", "[invalid"); got != "" {
		t.Fatalf("invalid node_name_filter with empty provider exclude should stay empty, got %q", got)
	}
	if got := mergeProxyProviderExcludeFilter("政企", "政企"); got != "政企" {
		t.Fatalf("identical rules should not be duplicated, got %q", got)
	}
}

func TestProviderTemplateNodeNameFilterUsesOwnerSettings(t *testing.T) {
	t.Parallel()

	ownerSettings := storage.UserSettings{NodeNameFilter: amadeusNodeNameFilter}
	got := providerTemplateNodeNameFilter(ownerSettings, nil)
	if got != amadeusNodeNameFilter {
		t.Fatalf("owner filter = %q, want %q", got, amadeusNodeNameFilter)
	}

	subscriberMissing := errors.New("sql: no rows in result set")
	wrongOwner := providerTemplateNodeNameFilter(storage.UserSettings{}, subscriberMissing)
	if strings.Contains(wrongOwner, "政企") || strings.Contains(wrongOwner, "2026") {
		t.Fatalf("subscriber settings miss must not invent owner custom rules, got %q", wrongOwner)
	}
	if wrongOwner != defaultNodeNameFilterPattern {
		t.Fatalf("load failure fallback = %q, want default %q", wrongOwner, defaultNodeNameFilterPattern)
	}

	result, err := processProviderOnlyV3Template(providerAllTemplate, []storage.ProxyProviderConfig{providerAllConfig}, map[int64]string{10: "https://example.com/all.yaml"}, nil, got)
	if err != nil {
		t.Fatal(err)
	}
	exclude := mustProviderExcludeFilter(t, result, "all")
	if exclude != amadeusNodeNameFilter {
		t.Fatalf("Provider template did not receive owner filter: %q", exclude)
	}
	assertExcludeFilterMatches(t, exclude, "香港 政企专线", true)
}

func TestProviderTemplateNodeNameFilterLoadErrorUsesDefault(t *testing.T) {
	t.Parallel()

	got := providerTemplateNodeNameFilter(storage.UserSettings{NodeNameFilter: amadeusNodeNameFilter}, errors.New("db unavailable"))
	if got != defaultNodeNameFilterPattern {
		t.Fatalf("load error should ignore owner payload and use default, got %q", got)
	}
	if strings.Contains(got, "政企") || strings.Contains(got, "2026") {
		t.Fatalf("default fallback leaked custom owner rules: %q", got)
	}

	result, err := processProviderOnlyV3Template(providerAllTemplate, []storage.ProxyProviderConfig{providerAllConfig}, map[int64]string{10: "https://example.com/all.yaml"}, nil, got)
	if err != nil {
		t.Fatal(err)
	}
	exclude := mustProviderExcludeFilter(t, result, "all")
	if exclude != defaultNodeNameFilterPattern {
		t.Fatalf("Provider template fallback exclude-filter = %q, want %q", exclude, defaultNodeNameFilterPattern)
	}
	assertExcludeFilterMatches(t, exclude, "剩余流量: 123GB", true)
	assertExcludeFilterMatches(t, exclude, "香港 政企专线", false)
}

func TestProviderTemplateNodeNameFilterEmptyOwnerSettingIsKept(t *testing.T) {
	t.Parallel()

	got := providerTemplateNodeNameFilter(storage.UserSettings{NodeNameFilter: ""}, nil)
	if got != "" {
		t.Fatalf("successful empty owner setting must not fall back to default, got %q", got)
	}
}

func TestProviderTemplateAppliesNodeNameFilterAsExcludeFilter(t *testing.T) {
	t.Parallel()

	configs := []storage.ProxyProviderConfig{{
		ID:                     39,
		ExternalSubscriptionID: 10,
		Name:                   "all",
		ProcessMode:            "client",
		ExcludeFilter:          "",
	}}
	upstreamURLs := map[int64]string{10: "https://upstream.example/all.yaml"}
	gatewayURLs := map[int64]string{39: "https://mmw.example/api/clash/subscribe?filename=labmem.yaml&mode=provider-source&provider_id=39&token=user-token"}
	template := `proxy-groups:
  - name: test
    type: select
    include-all-providers: true
`

	result, err := processProviderOnlyV3Template(template, configs, upstreamURLs, gatewayURLs, amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, gatewayURLs[39]) {
		t.Fatalf("gateway URL missing from Provider output:\n%s", result)
	}
	if strings.Contains(result, upstreamURLs[10]) {
		t.Fatalf("upstream URL leaked into Provider output:\n%s", result)
	}

	exclude := mustProviderExcludeFilter(t, result, "all")
	if exclude != amadeusNodeNameFilter {
		t.Fatalf("all exclude-filter = %q, want %q", exclude, amadeusNodeNameFilter)
	}
	assertExcludeFilterMatches(t, exclude, "香港 政企专线", true)
	assertExcludeFilterMatches(t, exclude, "日本01", false)
}

func TestProviderTemplateMergesExistingExcludeFilter(t *testing.T) {
	t.Parallel()

	configs := []storage.ProxyProviderConfig{{
		ID:                     39,
		ExternalSubscriptionID: 10,
		Name:                   "all",
		ProcessMode:            "client",
		ExcludeFilter:          "custom-drop",
	}}
	urls := map[int64]string{10: "https://example.com/all.yaml"}
	template := `proxy-groups:
  - name: test
    type: select
    include-all-providers: true
`

	result, err := processProviderOnlyV3Template(template, configs, urls, nil, amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	exclude := mustProviderExcludeFilter(t, result, "all")
	if !strings.Contains(exclude, "custom-drop") || !strings.Contains(exclude, "政企") {
		t.Fatalf("Provider exclude-filter lost a merged rule: %q", exclude)
	}
	assertExcludeFilterMatches(t, exclude, "custom-drop", true)
	assertExcludeFilterMatches(t, exclude, "北京政企", true)
}

func TestProviderTemplateSkipsEmptyAndInvalidNodeNameFilter(t *testing.T) {
	t.Parallel()

	configs := []storage.ProxyProviderConfig{{
		ID:                     39,
		ExternalSubscriptionID: 10,
		Name:                   "all",
		ProcessMode:            "client",
	}}
	urls := map[int64]string{10: "https://example.com/all.yaml"}
	template := `proxy-groups:
  - name: test
    type: select
    include-all-providers: true
`

	emptyResult, err := processProviderOnlyV3Template(template, configs, urls, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupProviderExcludeFilter(t, emptyResult, "all"); ok {
		t.Fatalf("empty node_name_filter should not emit exclude-filter:\n%s", emptyResult)
	}

	invalidResult, err := processProviderOnlyV3Template(template, configs, urls, nil, "[invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupProviderExcludeFilter(t, invalidResult, "all"); ok {
		t.Fatalf("invalid node_name_filter should not emit exclude-filter:\n%s", invalidResult)
	}

	configs[0].ExcludeFilter = "keep-me"
	keptResult, err := processProviderOnlyV3Template(template, configs, urls, nil, "[invalid")
	if err != nil {
		t.Fatal(err)
	}
	got := mustProviderExcludeFilter(t, keptResult, "all")
	if got != "keep-me" {
		t.Fatalf("invalid node_name_filter overwrote provider exclude-filter: %q", got)
	}
}

func assertExcludeFilterMatches(t *testing.T, pattern, name string, want bool) {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("exclude-filter %q is not a valid regex: %v", pattern, err)
	}
	if got := re.MatchString(name); got != want {
		t.Fatalf("exclude-filter %q MatchString(%q)=%v, want %v", pattern, name, got, want)
	}
}

func mustProviderExcludeFilter(t *testing.T, yamlText, providerName string) string {
	t.Helper()
	got, ok := lookupProviderExcludeFilter(t, yamlText, providerName)
	if !ok {
		t.Fatalf("provider %q missing exclude-filter in:\n%s", providerName, yamlText)
	}
	return got
}

func lookupProviderExcludeFilter(t *testing.T, yamlText, providerName string) (string, bool) {
	t.Helper()
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &parsed); err != nil {
		t.Fatalf("parse generated yaml: %v\n%s", err, yamlText)
	}
	providers, _ := parsed["proxy-providers"].(map[string]any)
	provider, _ := providers[providerName].(map[string]any)
	if provider == nil {
		t.Fatalf("provider %q missing in:\n%s", providerName, yamlText)
	}
	exclude, ok := provider["exclude-filter"].(string)
	return exclude, ok && exclude != ""
}
