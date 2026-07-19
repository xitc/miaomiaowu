package handler

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBatchSyncNodesToYAMLFilesUpdatesAllNodesAndReferences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub.yaml")
	input := []byte("proxies:\n  - name: old-a\n    type: ss\n    server: old-a.example\n    port: 1\n  - name: old-b\n    type: trojan\n    server: old-b.example\n    port: 2\nproxy-groups:\n  - name: select\n    type: select\n    proxies: [old-a, old-b]\nrules:\n  - DOMAIN,a.example,old-a\n")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}

	err := batchSyncNodesToYAMLFiles(dir, []NodeUpdate{
		{OldName: "old-a", NewName: "new-a", ClashConfigJSON: `{"name":"new-a","type":"ss","server":"new-a.example","port":443}`},
		{OldName: "old-b", NewName: "old-b", ClashConfigJSON: `{"name":"old-b","type":"trojan","server":"new-b.example","port":8443}`},
	})
	if err != nil {
		t.Fatalf("batch sync: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	proxies := got["proxies"].([]any)
	first := proxies[0].(map[string]any)
	second := proxies[1].(map[string]any)
	if first["name"] != "new-a" || first["server"] != "new-a.example" {
		t.Fatalf("first proxy not updated: %#v", first)
	}
	if second["name"] != "old-b" || second["server"] != "new-b.example" {
		t.Fatalf("second proxy not updated: %#v", second)
	}
	groups := got["proxy-groups"].([]any)
	groupProxies := groups[0].(map[string]any)["proxies"].([]any)
	if groupProxies[0] != "new-a" {
		t.Fatalf("proxy-group reference = %#v, want new-a", groupProxies)
	}
	rules := got["rules"].([]any)
	if rules[0] != "DOMAIN,a.example,new-a" {
		t.Fatalf("rule reference = %#v", rules[0])
	}
}
