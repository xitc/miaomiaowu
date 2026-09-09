package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"
	"miaomiaowu/internal/storage"
)

func groupFixture() ([]storage.Node, []storage.ExternalSubscription) {
	subs := []storage.ExternalSubscription{{ID: 1, Name: "bo", URL: "bo-url"}, {ID: 2, Name: "am", URL: "am-url"}, {ID: 3, Name: "p", URL: "p-url"}}
	nodes := []storage.Node{}
	for i, s := range subs {
		name := fmt.Sprintf("old-%d", i)
		cfg := fmt.Sprintf(`{"name":%q,"type":"ss","server":"same.example","port":443,"password":"secret","cipher":"aes-128-gcm","tls":true}`, name)
		nodes = append(nodes, storage.Node{ID: int64(i + 1), Username: "admin", RawURL: s.URL, NodeName: name, SourceNodeName: "new-name", Protocol: "ss", ClashConfig: cfg, ParsedConfig: cfg, Enabled: true, Tags: []string{"Provider/" + s.Name}})
	}
	return nodes, subs
}

func TestNodeGroupsRetainIndependentSourcesAndSplitOnChange(t *testing.T) {
	nodes, subs := groupFixture()
	groups := buildNodeGroups(nodes, subs, false)
	if len(groups) != 1 || len(groups[0].Sources) != 3 || len(groups[0].Tags) != 3 || groups[0].Name != "new-name" {
		t.Fatalf("groups=%+v", groups)
	}
	if nodes[0].NodeName != "old-0" || len(nodes[0].Tags) != 1 {
		t.Fatal("projection mutated source")
	}
	// Source membership is derived afresh: removing bo leaves am and p available.
	groups = buildNodeGroups(nodes[1:], subs, false)
	if len(groups) != 1 || len(groups[0].Sources) != 2 || len(groups[0].Tags) != 2 {
		t.Fatal("source removal lost shared node or retained tag")
	}
	for _, change := range []struct{ old, new string }{{`"secret"`, `"other"`}, {`"tls":true`, `"tls":false`}, {`"port":443`, `"port":8443`}} {
		copyNodes := append([]storage.Node(nil), nodes...)
		copyNodes[0].ClashConfig = strings.Replace(copyNodes[0].ClashConfig, change.old, change.new, 1)
		if got := buildNodeGroups(copyNodes, subs, false); len(got) != 2 {
			t.Fatalf("different configuration merged: %s", change.new)
		}
	}
	// Original names and user policy remain respected.
	if got := buildNodeGroups(nodes, subs, true); got[0].Name != "old-0" {
		t.Fatal("keep name ignored")
	}
	nodes[0].Enabled = false
	if got := buildNodeGroups(nodes, subs, false); len(got) != 2 {
		t.Fatal("different enabled policy merged")
	}
}

func TestNodeGroupsDoNotMergeSameSourceOrReferencedNodes(t *testing.T) {
	nodes, subs := groupFixture()
	same := nodes[0]
	same.ID = 4
	same.NodeName = "same-source-copy"
	if got := buildNodeGroups(append(nodes, same), subs, false); len(got) != 2 {
		t.Fatal("same-source multiplicity lost")
	}
	target := nodes[0].ID
	nodes = append(nodes, storage.Node{ID: 4, Username: "admin", NodeName: "chain", ChainProxyNodeID: &target, ClashConfig: `{"type":"ss","server":"other.example","port":443}`})
	groups := buildNodeGroups(nodes, subs, false)
	if len(groups) != 3 {
		t.Fatalf("referenced source identity merged, groups=%d", len(groups))
	}
}

func TestAggregateSubscriptionNodesPreservesReferencesAndSelection(t *testing.T) {
	nodes, subs := groupFixture()
	groups := buildNodeGroups(nodes, subs, false)
	proxies := []any{}
	for _, n := range nodes {
		var p map[string]any
		_ = json.Unmarshal([]byte(n.ClashConfig), &p)
		proxies = append(proxies, p)
	}
	config := map[string]any{"proxies": proxies, "proxy-groups": []any{map[string]any{"name": "PROXY", "type": "select", "proxies": []any{"old-0", "old-1", "old-2", "DIRECT"}}}, "rules": []any{"DOMAIN,example.com,old-1", "MATCH,PROXY"}}
	input, _ := yaml.Marshal(config)
	body, err := aggregateSubscriptionNodes(input, groups)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = yaml.Unmarshal(body, &got)
	if len(got["proxies"].([]any)) != 1 || strings.Contains(string(body), "old-") {
		t.Fatalf("duplicate or dangling reference: %s", body)
	}
	if len(got["proxy-groups"].([]any)[0].(map[string]any)["proxies"].([]any)) != 2 {
		t.Fatal("group references not deduplicated")
	}
	// A bo-only subscription does not import p/am's nodes or add source metadata.
	config["proxies"] = proxies[:1]
	input, _ = yaml.Marshal(config)
	body, err = aggregateSubscriptionNodes(input, groups)
	if err != nil {
		t.Fatal(err)
	}
	_ = yaml.Unmarshal(body, &got)
	if len(got["proxies"].([]any)) != 1 {
		t.Fatal("selection expanded")
	}
	// Template-added transport differences must survive even for the same group.
	changed := proxies[1].(map[string]any)
	changed["dialer-proxy"] = "chain"
	config["proxies"] = proxies
	input, _ = yaml.Marshal(config)
	body, err = aggregateSubscriptionNodes(input, groups)
	if err != nil {
		t.Fatal(err)
	}
	_ = yaml.Unmarshal(body, &got)
	if len(got["proxies"].([]any)) != 2 {
		t.Fatal("different template output merged")
	}
}

func TestSourceNodeNamePersistsAcrossSyncAndLocalEdit(t *testing.T) {
	repo := relayTestRepo(t)
	ctx := context.Background()
	nodes, _ := groupFixture()
	created, err := repo.CreateNode(ctx, nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	if created.SourceNodeName != "new-name" {
		t.Fatal("source name not saved")
	}
	created.NodeName = "custom-local"
	if err := repo.BatchUpdateNodesNoFetch(ctx, []storage.Node{created}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetNode(ctx, created.ID, created.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeName != "custom-local" || got.SourceNodeName != "new-name" {
		t.Fatalf("names not independent: %+v", got)
	}
	groups := buildNodeGroups([]storage.Node{got}, []storage.ExternalSubscription{{ID: 1, URL: got.RawURL, Name: "bo"}}, false)
	if groups[0].Name != "new-name" {
		t.Fatal("upstream name unavailable")
	}
}

func TestNodeGroupsFollowRealSourceSyncLifecycle(t *testing.T) {
	repo := relayTestRepo(t)
	ctx := context.Background()
	var version atomic.Int32
	version.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		password := "shared"
		if r.URL.Path == "/bo" && version.Load() == 2 {
			password = "changed"
		}
		fmt.Fprintln(w, "proxies:")
		if r.URL.Path != "/bo" || version.Load() != 3 {
			fmt.Fprintf(w, "  - {name: fresh-name, type: ss, server: same.example, port: 443, password: %s, cipher: aes-128-gcm}\n", password)
		}
		fmt.Fprintln(w, "  - {name: anchor, type: ss, server: anchor.example, port: 443, password: anchor, cipher: aes-128-gcm}")
	}))
	defer upstream.Close()
	settings := storage.UserSettings{Username: "admin", MatchRule: "type_server_port_cred", KeepNodeName: false, SyncScope: "all"}
	if err := repo.UpsertUserSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	subs := []storage.ExternalSubscription{}
	for _, name := range []string{"bo", "p"} {
		sub := storage.ExternalSubscription{Username: "admin", Name: name, URL: upstream.URL + "/" + name}
		id, err := repo.CreateExternalSubscription(ctx, sub)
		if err != nil {
			t.Fatal(err)
		}
		sub.ID = id
		subs = append(subs, sub)
		if _, err := repo.CreateProxyProviderConfig(ctx, &storage.ProxyProviderConfig{Username: "admin", Name: name, ExternalSubscriptionID: id, Type: "http", ProcessMode: "client"}); err != nil {
			t.Fatal(err)
		}
	}
	syncSource := func(sub storage.ExternalSubscription) {
		t.Helper()
		if _, _, err := syncSingleExternalSubscription(ctx, upstream.Client(), repo, "", "admin", sub, settings); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range subs {
		syncSource(s)
	}
	groups, err := loadNodeGroups(ctx, repo, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("initial groups=%d, want 2", len(groups))
	}
	for _, g := range groups {
		if len(g.Sources) != 2 {
			t.Fatal("missing source relationship")
		}
	}
	before, err := repo.ListNodes(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	version.Store(2)
	syncSource(subs[0])
	groups, _ = loadNodeGroups(ctx, repo, "admin")
	if len(groups) != 3 {
		t.Fatalf("credential split groups=%d", len(groups))
	}
	version.Store(3)
	syncSource(subs[0])
	groups, _ = loadNodeGroups(ctx, repo, "admin")
	if len(groups) != 2 {
		t.Fatalf("removal groups=%d", len(groups))
	}
	for _, g := range groups {
		if g.Name == "fresh-name" && (len(g.Sources) != 1 || g.Sources[0].Name != "p") {
			t.Fatal("removed wrong source")
		}
	}
	after, _ := repo.ListNodes(ctx, "admin")
	for _, n := range before {
		if n.RawURL == subs[1].URL {
			found := false
			for _, m := range after {
				if n.ID == m.ID && n.ClashConfig == m.ClashConfig && n.SourceNodeName == m.SourceNodeName {
					found = true
				}
			}
			if !found {
				t.Fatal("sync bo changed p's snapshot")
			}
		}
	}
}
