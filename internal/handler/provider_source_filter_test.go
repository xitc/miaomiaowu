package handler

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/storage"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const providerSourceSampleYAML = `proxies:
  - name: 香港01
    type: ss
    server: hk.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
  - name: 香港 政企专线
    type: ss
    server: corp.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
  - name: 东京01
    type: ss
    server: tyo.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
`

func TestFilterProviderSourceYAMLRemovesZhengqiAndKeepsNormalNodes(t *testing.T) {
	t.Parallel()

	src := []byte(providerSourceSampleYAML)
	original := append([]byte(nil), src...)

	got, removed, err := filterProviderSourceYAML(src, amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if string(src) != string(original) {
		t.Fatal("filter mutated the caller-owned source slice")
	}

	names := providerSourceProxyNames(t, got)
	if containsName(names, "香港 政企专线") {
		t.Fatalf("政企 node leaked: %v", names)
	}
	if !containsName(names, "香港01") || !containsName(names, "东京01") {
		t.Fatalf("normal nodes missing: %v", names)
	}
}

func TestFilterProviderSourceYAMLEmptyRuleKeepsAllNodes(t *testing.T) {
	t.Parallel()

	src := []byte(providerSourceSampleYAML)
	for _, pattern := range []string{"", "   "} {
		got, removed, err := filterProviderSourceYAML(src, pattern)
		if err != nil {
			t.Fatalf("pattern %q: %v", pattern, err)
		}
		if removed != 0 {
			t.Fatalf("pattern %q removed %d, want 0", pattern, removed)
		}
		names := providerSourceProxyNames(t, got)
		if !containsName(names, "香港 政企专线") || !containsName(names, "香港01") || !containsName(names, "东京01") {
			t.Fatalf("empty rule deleted nodes for %q: %v", pattern, names)
		}
	}
}

func TestFilterProviderSourceYAMLInvalidRuleDoesNotLeak(t *testing.T) {
	t.Parallel()

	src := []byte(providerSourceSampleYAML)
	original := append([]byte(nil), src...)
	got, removed, err := filterProviderSourceYAML(src, "[invalid")
	if !errors.Is(err, errInvalidNodeNameFilter) {
		t.Fatalf("err = %v, want %v", err, errInvalidNodeNameFilter)
	}
	if got != nil || removed != 0 {
		t.Fatalf("invalid rule returned payload bytes=%q removed=%d", got, removed)
	}
	if string(src) != string(original) {
		t.Fatal("invalid rule path mutated the source slice")
	}
}

func TestFilterProviderSourceYAMLRejectsUnparseableUpstream(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{name: "broken-yaml", src: "proxies: [\n  - name: 香港 政企专线\n"},
		{name: "missing-proxies", src: "foo: bar\n"},
		{name: "proxies-not-list", src: "proxies: nope\n"},
		{name: "proxies-mapping", src: "proxies:\n  name: 香港 政企专线\n  type: ss\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := []byte(tc.src)
			original := append([]byte(nil), src...)
			got, _, err := filterProviderSourceYAML(src, amadeusNodeNameFilter)
			if !errors.Is(err, errProviderSourceInvalidYAML) {
				t.Fatalf("err = %v, want %v", err, errProviderSourceInvalidYAML)
			}
			if got != nil {
				t.Fatalf("unparseable source leaked payload:\n%s", got)
			}
			if string(src) != string(original) {
				t.Fatal("parse failure mutated the source slice")
			}
			if bytes.Contains(got, []byte("政企")) {
				t.Fatal("parse failure leaked 政企 bytes")
			}
		})
	}
}

func TestFilterProviderSourceYAMLRejectsUnprovableProxyEntries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
	}{
		{name: "non-mapping", src: `proxies:
  - name: 香港01
    type: ss
    server: hk.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
  - 香港 政企专线
`},
		{name: "missing-name", src: `proxies:
  - type: ss
    server: corp.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
    remark: 香港 政企专线
`},
		{name: "non-string-name", src: `proxies:
  - name: 2026
    type: ss
    server: corp.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
`},
		{name: "nested-name", src: `proxies:
  - name:
      text: 香港 政企专线
    type: ss
    server: corp.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := []byte(tc.src)
			original := append([]byte(nil), src...)
			got, removed, err := filterProviderSourceYAML(src, amadeusNodeNameFilter)
			if !errors.Is(err, errProviderSourceInvalidYAML) {
				t.Fatalf("err = %v, want %v", err, errProviderSourceInvalidYAML)
			}
			if got != nil || removed != 0 {
				t.Fatalf("unprovable entry leaked payload bytes=%q removed=%d", got, removed)
			}
			if string(src) != string(original) {
				t.Fatal("unprovable entry path mutated the source slice")
			}
		})
	}
}

func TestFilterProviderSourceYAMLEmptyRulePassesUnprovableEntries(t *testing.T) {
	t.Parallel()

	src := []byte(`proxies:
  - 香港 政企专线
  - name: 香港01
    type: ss
    server: hk.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
`)
	got, removed, err := filterProviderSourceYAML(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("empty rule removed %d, want 0", removed)
	}
	if !bytes.Contains(got, []byte("香港 政企专线")) || !bytes.Contains(got, []byte("香港01")) {
		t.Fatalf("empty rule must pass original structure through:\n%s", got)
	}
}

func TestFilterClashProxiesNodeByNameDecodeFailure(t *testing.T) {
	t.Parallel()

	proxies := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.SequenceNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "name"}}},
			{Kind: yaml.ScalarNode, Value: "香港 政企专线"},
		},
	}}}
	re, err := regexp.Compile(amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	got, removed, err := filterClashProxiesNodeByName(proxies, re)
	if !errors.Is(err, errProviderSourceInvalidYAML) {
		t.Fatalf("err = %v, want %v", err, errProviderSourceInvalidYAML)
	}
	if got != nil || removed != 0 {
		t.Fatalf("decode failure leaked node got=%v removed=%d", got, removed)
	}
}

func TestFilterProviderSourceYAMLDoesNotAliasReturnedBuffer(t *testing.T) {
	t.Parallel()

	src := []byte(providerSourceSampleYAML)
	original := append([]byte(nil), src...)
	got, _, err := filterProviderSourceYAML(src, amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected filtered yaml")
	}
	got[0] ^= 0xff
	if string(src) != string(original) {
		t.Fatal("mutating the filtered copy changed the source slice")
	}
}

func TestProviderOutputModeKeepsExcludeFilterAsDualProtection(t *testing.T) {
	t.Parallel()

	result, err := processProviderOnlyV3Template(providerAllTemplate, []storage.ProxyProviderConfig{providerAllConfig}, map[int64]string{10: "https://example.com/all.yaml"}, nil, amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	exclude := mustProviderExcludeFilter(t, result, "all")
	if exclude != amadeusNodeNameFilter {
		t.Fatalf("Provider exclude-filter = %q, want owner node_name_filter", exclude)
	}
	assertExcludeFilterMatches(t, exclude, "香港 政企专线", true)
	assertExcludeFilterMatches(t, exclude, "香港01", false)

	filtered, _, err := filterProviderSourceYAML([]byte(providerSourceSampleYAML), amadeusNodeNameFilter)
	if err != nil {
		t.Fatal(err)
	}
	names := providerSourceProxyNames(t, filtered)
	if containsName(names, "香港 政企专线") {
		t.Fatalf("source endpoint leaked 政企 under dual protection: %v", names)
	}
}

func TestServeSubscriptionProviderFiltersByOwnerNodeNameFilter(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), amadeusNodeNameFilter)

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "yaml") {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	names := providerSourceProxyNames(t, rec.Body.Bytes())
	if containsName(names, "香港 政企专线") {
		t.Fatalf("政企 leaked to provider-source: %v", names)
	}
	if !containsName(names, "香港01") || !containsName(names, "东京01") {
		t.Fatalf("normal nodes missing: %v", names)
	}
}

func TestServeSubscriptionProviderEmptyOwnerFilterKeepsNodes(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), amadeusNodeNameFilter)
	setUserNodeNameFilterRaw(t, h.dbPath, h.admin, "")

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	names := providerSourceProxyNames(t, rec.Body.Bytes())
	if !containsName(names, "香港 政企专线") || !containsName(names, "香港01") {
		t.Fatalf("empty owner rule deleted nodes: %v", names)
	}
}

func TestServeSubscriptionProviderInvalidOwnerFilterDoesNotLeak(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), "[invalid")

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid node name filter") {
		t.Fatalf("expected explainable invalid-filter error, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "政企") || strings.Contains(rec.Body.String(), "香港01") {
		t.Fatalf("invalid filter leaked node payload: %s", rec.Body.String())
	}
}

func TestServeSubscriptionProviderNonAdminUsesOwnerFilter(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), amadeusNodeNameFilter)
	if err := h.repo.UpsertUserSettings(context.Background(), storage.UserSettings{
		Username:       h.subscriber,
		NodeNameFilter: "香港01",
	}); err != nil {
		t.Fatal(err)
	}

	if got := h.handler.providerNodeOwner(context.Background(), h.subscriber); got != h.admin {
		t.Fatalf("providerNodeOwner(%q)=%q, want admin %q", h.subscriber, got, h.admin)
	}

	rec := h.doProviderSource(h.subscriber, h.providerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	names := providerSourceProxyNames(t, rec.Body.Bytes())
	if containsName(names, "香港 政企专线") {
		t.Fatalf("subscriber request did not apply owner 政企 filter: %v", names)
	}
	if !containsName(names, "香港01") {
		t.Fatalf("subscriber filter was applied instead of owner filter: %v", names)
	}
	if !containsName(names, "东京01") {
		t.Fatalf("normal node missing: %v", names)
	}
}

func TestServeSubscriptionProviderFilterDoesNotPolluteCache(t *testing.T) {
	payload := []byte(providerSourceSampleYAML)
	original := append([]byte(nil), payload...)
	h := newProviderSourceHTTPHarness(t, payload, amadeusNodeNameFilter)

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if containsName(providerSourceProxyNames(t, rec.Body.Bytes()), "香港 政企专线") {
		t.Fatal("expected filtered provider-source response")
	}
	if string(payload) != string(original) {
		t.Fatal("handler mutated the upstream payload slice")
	}

	sub := &storage.ExternalSubscription{URL: h.upstream.URL}
	cached, err := fetchSubscriptionContent(sub)
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != string(original) {
		t.Fatalf("shared cache was rewritten; cache=%q original=%q", cached, original)
	}
	if !bytes.Contains(cached, []byte("香港 政企专线")) {
		t.Fatal("shared cache lost unfiltered 政企 node")
	}
	if h.upstreamHits.Load() != 1 {
		t.Fatalf("cache isolation caused extra upstream fetches: %d", h.upstreamHits.Load())
	}
}

func TestServeSubscriptionProviderUnparseableUpstreamDoesNotLeak(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte("proxies: [\n  - name: 香港 政企专线\n"), amadeusNodeNameFilter)

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "provider source filter failed") {
		t.Fatalf("expected explainable filter failure, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "政企") || strings.Contains(rec.Body.String(), "proxies:") {
		t.Fatalf("unparseable upstream leaked payload: %s", rec.Body.String())
	}
}

func TestServeSubscriptionProviderSettingsLoadFailureDoesNotLeak(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), amadeusNodeNameFilter)
	dropUserSettingsTable(t, h.dbPath)

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "node name filter unavailable") {
		t.Fatalf("expected explainable settings-load error, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "政企") || strings.Contains(rec.Body.String(), "香港01") || strings.Contains(rec.Body.String(), "proxies:") {
		t.Fatalf("settings load failure leaked node payload: %s", rec.Body.String())
	}
	if h.upstreamHits.Load() != 0 {
		t.Fatalf("settings load failure fetched upstream: %d", h.upstreamHits.Load())
	}
}

func TestServeSubscriptionProviderUnprovableProxyEntryDoesNotLeak(t *testing.T) {
	payload := []byte(`proxies:
  - name: 香港01
    type: ss
    server: hk.example.com
    port: 443
    cipher: aes-128-gcm
    password: test
  - 香港 政企专线
`)
	h := newProviderSourceHTTPHarness(t, payload, amadeusNodeNameFilter)

	rec := h.doProviderSource(h.admin, h.providerID)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "provider source filter failed") {
		t.Fatalf("expected explainable structure error, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "政企") || strings.Contains(rec.Body.String(), "香港01") || strings.Contains(rec.Body.String(), "proxies:") {
		t.Fatalf("unprovable proxy entry leaked payload: %s", rec.Body.String())
	}

	cached, err := fetchSubscriptionContent(&storage.ExternalSubscription{URL: h.upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cached, []byte("香港 政企专线")) {
		t.Fatal("structure failure must not rewrite shared cache")
	}
}

func TestServeSubscriptionProviderKeepsAuthProcessModeAndSelection(t *testing.T) {
	h := newProviderSourceHTTPHarness(t, []byte(providerSourceSampleYAML), amadeusNodeNameFilter)

	unselected := httptest.NewRecorder()
	h.handler.ServeHTTP(unselected, h.request(h.admin, h.unselectedID))
	if unselected.Code != http.StatusNotFound {
		t.Fatalf("unselected provider status=%d, want 404", unselected.Code)
	}

	mmw := httptest.NewRecorder()
	h.handler.ServeHTTP(mmw, h.request(h.admin, h.mmwID))
	if mmw.Code != http.StatusBadRequest || !strings.Contains(mmw.Body.String(), "provider is not in client mode") {
		t.Fatalf("mmw provider status=%d body=%s", mmw.Code, mmw.Body.String())
	}

	disabled := h.file
	disabled.ProviderLinkEnabled = false
	if _, err := h.repo.UpdateSubscribeFile(context.Background(), disabled); err != nil {
		t.Fatal(err)
	}
	disabledRec := h.doProviderSource(h.admin, h.providerID)
	if disabledRec.Code != http.StatusNotFound {
		t.Fatalf("disabled provider link status=%d, want 404", disabledRec.Code)
	}

	if h.upstreamHits.Load() != 0 {
		t.Fatalf("rejected requests fetched upstream: %d", h.upstreamHits.Load())
	}
}

type providerSourceHTTPHarness struct {
	t            *testing.T
	repo         *storage.TrafficRepository
	handler      *SubscriptionHandler
	upstream     *httptest.Server
	dbPath       string
	filename     string
	providerID   int64
	unselectedID int64
	mmwID        int64
	admin        string
	subscriber   string
	file         storage.SubscribeFile
	upstreamHits atomic.Int32
}

func newProviderSourceHTTPHarness(t *testing.T, payload []byte, ownerFilter string) *providerSourceHTTPHarness {
	t.Helper()

	h := &providerSourceHTTPHarness{
		t:          t,
		admin:      "source-admin",
		subscriber: "source-user",
		filename:   "provider-source.yaml",
	}

	useLocalProviderContentClient(t)
	h.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h.upstreamHits.Add(1)
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(h.upstream.Close)
	t.Cleanup(func() { InvalidateSubscriptionContentCache(h.upstream.URL) })

	dir := t.TempDir()
	h.dbPath = filepath.Join(dir, "test.db")
	repo, err := storage.NewTrafficRepository(h.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	h.repo = repo

	ctx := context.Background()
	if err := repo.CreateUser(ctx, h.admin, "", "", "test-hash", storage.RoleAdmin, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateUser(ctx, h.subscriber, "", "", "test-hash", storage.RoleUser, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertUserSettings(ctx, storage.UserSettings{
		Username:       h.admin,
		NodeNameFilter: ownerFilter,
	}); err != nil {
		t.Fatal(err)
	}

	externalID, err := repo.CreateExternalSubscription(ctx, storage.ExternalSubscription{
		Username: h.admin,
		Name:     "source",
		URL:      h.upstream.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.providerID, err = repo.CreateProxyProviderConfig(ctx, &storage.ProxyProviderConfig{
		Username:               h.admin,
		ExternalSubscriptionID: externalID,
		Name:                   "source",
		Type:                   "http",
		Interval:               300,
		ProcessMode:            "client",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.unselectedID, err = repo.CreateProxyProviderConfig(ctx, &storage.ProxyProviderConfig{
		Username:               h.admin,
		ExternalSubscriptionID: externalID,
		Name:                   "other",
		Type:                   "http",
		Interval:               300,
		ProcessMode:            "client",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.mmwID, err = repo.CreateProxyProviderConfig(ctx, &storage.ProxyProviderConfig{
		Username:               h.admin,
		ExternalSubscriptionID: externalID,
		Name:                   "mmw",
		Type:                   "http",
		Interval:               300,
		ProcessMode:            "mmw",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, h.filename), []byte("proxies: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := repo.CreateSubscribeFile(ctx, storage.SubscribeFile{
		Name:                     "provider-source-test",
		Type:                     storage.SubscribeTypeCreate,
		Filename:                 h.filename,
		NormalLinkEnabled:        true,
		ProviderLinkEnabled:      true,
		DefaultOutputMode:        storage.OutputModeProvider,
		ProviderTemplateFilename: "fake_ip__v3.yaml",
		SelectedProviderNames:    []string{"source", "mmw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.file = file
	if err := repo.AssignSubscriptionToUser(ctx, h.admin, file.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignSubscriptionToUser(ctx, h.subscriber, file.ID); err != nil {
		t.Fatal(err)
	}

	h.handler = newSubscriptionHandler(nil, repo, dir, subscriptionDefaultType)
	return h
}

func (h *providerSourceHTTPHarness) request(username string, providerID int64) *http.Request {
	h.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "https://sub.example.com/api/clash/subscribe?filename="+h.filename+"&mode="+providerSourceOutputMode+"&provider_id="+strconv.FormatInt(providerID, 10), nil)
	return req.WithContext(auth.ContextWithUsername(req.Context(), username))
}

func (h *providerSourceHTTPHarness) doProviderSource(username string, providerID int64) *httptest.ResponseRecorder {
	h.t.Helper()
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, h.request(username, providerID))
	return rec
}

func setUserNodeNameFilterRaw(t *testing.T, dbPath, username, filter string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE user_settings SET node_name_filter = ? WHERE username = ?`, filter, username); err != nil {
		t.Fatal(err)
	}
}

func dropUserSettingsTable(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP TABLE user_settings`); err != nil {
		t.Fatal(err)
	}
}

func providerSourceProxyNames(t *testing.T, raw []byte) []string {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse yaml: %v\n%s", err, raw)
	}
	proxies, ok := doc["proxies"].([]any)
	if !ok {
		t.Fatalf("missing proxies list:\n%s", raw)
	}
	names := make([]string, 0, len(proxies))
	for _, proxy := range proxies {
		proxyMap, _ := proxy.(map[string]any)
		name, _ := proxyMap["name"].(string)
		names = append(names, name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
