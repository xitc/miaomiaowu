package storage

import (
	"context"
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
