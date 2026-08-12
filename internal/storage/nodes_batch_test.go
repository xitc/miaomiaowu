package storage

import (
	"context"
	"strings"
	"testing"
)

func TestBatchNodeNoFetchPathsPersistAtomically(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	nodes := []Node{
		{Username: "admin", RawURL: "source", NodeName: "a", Protocol: "ss", ClashConfig: `{"name":"a","type":"ss","server":"a","port":1}`, ParsedConfig: `{}`, Enabled: true, Tag: "source", Tags: []string{"source"}},
		{Username: "admin", RawURL: "source", NodeName: "b", Protocol: "trojan", ClashConfig: `{"name":"b","type":"trojan","server":"b","port":2}`, ParsedConfig: `{}`, Enabled: true, Tag: "source", Tags: []string{"source"}},
	}
	if err := repo.BatchCreateNodesNoFetch(ctx, nodes); err != nil {
		t.Fatalf("BatchCreateNodesNoFetch: %v", err)
	}
	stored, err := repo.ListNodes(ctx, "admin")
	if err != nil || len(stored) != 2 {
		t.Fatalf("ListNodes after create = %d, %v", len(stored), err)
	}
	for i := range stored {
		stored[i].Enabled = false
		stored[i].Tags = append(stored[i].Tags, "Provider/test")
	}
	if err := repo.BatchUpdateNodesNoFetch(ctx, stored); err != nil {
		t.Fatalf("BatchUpdateNodesNoFetch: %v", err)
	}
	updated, err := repo.ListNodes(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range updated {
		if node.Enabled || len(node.Tags) != 2 {
			t.Fatalf("node not batch-updated: %+v", node)
		}
	}
}

func TestBatchCreateNodesAddsSuffixForDuplicateNames(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mustCreateNode(t, repo, Node{
		Username: "alice", NodeName: "香港", Protocol: "ss",
		ClashConfig: `{"name":"香港","type":"ss"}`, Enabled: true,
	})

	created, err := repo.BatchCreateNodes(ctx, []Node{
		{Username: "alice", NodeName: "香港", Protocol: "vmess", ClashConfig: `{"name":"香港","type":"vmess"}`, Enabled: true},
		{Username: "alice", NodeName: "香港", Protocol: "trojan", ClashConfig: `{"name":"香港","type":"trojan"}`, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created[0].NodeName != "香港-2" || created[1].NodeName != "香港-3" {
		t.Fatalf("unexpected names: %q, %q", created[0].NodeName, created[1].NodeName)
	}
	if !strings.Contains(created[0].ClashConfig, `"name":"香港-2"`) || !strings.Contains(created[1].ClashConfig, `"name":"香港-3"`) {
		t.Fatalf("config names were not updated: %s; %s", created[0].ClashConfig, created[1].ClashConfig)
	}
}
