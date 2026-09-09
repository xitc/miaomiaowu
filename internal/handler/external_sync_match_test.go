package handler

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

func TestMergeExternalSyncTagsPreservesLabelsAndRestoresSource(t *testing.T) {
	got := mergeExternalSyncTags(
		[]string{"自定义", "Provider/HK", "自定义"},
		[]string{"provider-service", "Provider/HK"},
	)
	want := []string{"自定义", "Provider/HK", "provider-service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged tags = %v, want %v", got, want)
	}
}

func TestExternalSyncSingleflightSharesOneRun(t *testing.T) {
	const callers = 16
	key := externalSyncFlightKey("singleflight-test", 42)
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32

	fn := func() (int, storage.ExternalSubscription, error) {
		if runs.Add(1) == 1 {
			close(started)
		}
		<-release
		return 7, storage.ExternalSubscription{ID: 42, Username: "singleflight-test"}, nil
	}

	var wg sync.WaitGroup
	wg.Add(callers)
	results := make(chan int, callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			count, sub, err := doExternalSyncSingleflight(key, fn)
			if err != nil || sub.ID != 42 {
				results <- -1
				return
			}
			results <- count
		}()
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("singleflight function did not start")
	}
	// Let all callers reach the in-flight entry before completing the run.
	time.Sleep(10 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)

	if got := runs.Load(); got != 1 {
		t.Fatalf("underlying runs = %d, want 1", got)
	}
	for result := range results {
		if result != 7 {
			t.Fatalf("shared result = %d, want 7", result)
		}
	}
}

func TestSourceNodeMatchIndexDoesNotCrossSources(t *testing.T) {
	nodes := []storage.Node{
		{RawURL: "source-a", NodeName: "same", ClashConfig: `{"name":"same","type":"ss","server":"a.example","port":443}`},
		{RawURL: "source-b", NodeName: "same", ClashConfig: `{"name":"same","type":"ss","server":"b.example","port":443}`},
	}
	idx := buildSourceNodeMatchIndex(nodes, "source-b")

	if got := idx.find("node_name", "same", map[string]any{"name": "same"}); got != 1 {
		t.Fatalf("name match index = %d, want source-b index 1", got)
	}
	if got := idx.find("server_port", "", map[string]any{"server": "a.example", "port": 443}); got != -1 {
		t.Fatalf("cross-source endpoint matched index %d", got)
	}
}

func TestNodeNameCollisionFallsBackToUniqueSourceEndpoint(t *testing.T) {
	nodes := []storage.Node{
		{RawURL: "other-source", NodeName: "same", ClashConfig: `{"name":"same","type":"ss","server":"other.example","port":443}`},
		{RawURL: "target-source", NodeName: "same-2", ClashConfig: `{"name":"same-2","type":"ss","server":"target.example","port":443}`},
	}
	idx := buildSourceNodeMatchIndex(nodes, "target-source")
	incoming := map[string]any{"name": "same", "type": "ss", "server": "target.example", "port": 443}
	if !nodeNameTakenOutsideSource(nodes, "target-source", "same") {
		t.Fatal("expected an outside-source name collision")
	}
	if got := idx.findUniqueEndpoint(incoming); got != 1 {
		t.Fatalf("endpoint fallback index = %d, want 1", got)
	}
}

func TestNodeNameCollisionDoesNotGuessAmbiguousEndpoint(t *testing.T) {
	nodes := []storage.Node{
		{RawURL: "target", NodeName: "one", ClashConfig: `{"name":"one","type":"ss","server":"same.example","port":443}`},
		{RawURL: "target", NodeName: "two", ClashConfig: `{"name":"two","type":"ss","server":"same.example","port":443}`},
	}
	idx := buildSourceNodeMatchIndex(nodes, "target")
	if got := idx.findUniqueEndpoint(map[string]any{"type": "ss", "server": "same.example", "port": 443}); got != -1 {
		t.Fatalf("ambiguous endpoint matched index %d", got)
	}
}

func TestManualSelectionDefersOrphanCleanup(t *testing.T) {
	if shouldCleanupExternalSyncOrphans("all", true) {
		t.Fatal("manual selection preview must not delete unmatched source nodes")
	}
	if !shouldCleanupExternalSyncOrphans("all", false) {
		t.Fatal("completed automatic sync should clean source orphans")
	}
}

func TestNodeSyncPayloadEqualDetectsMaterialChanges(t *testing.T) {
	base := storage.Node{
		RawURL:       "source",
		NodeName:     "node",
		Protocol:     "ss",
		ParsedConfig: `{"name":"node","type":"ss","server":"a.example","port":443}`,
		ClashConfig:  `{"name":"node","type":"ss","server":"a.example","port":443}`,
		Enabled:      true,
		Tag:          "source",
		Tags:         []string{"source", "自定义"},
	}
	if !nodeSyncPayloadEqual(base, base, false) {
		t.Fatal("identical payload should be equal")
	}
	changed := base
	changed.ClashConfig = `{"name":"node","type":"ss","server":"b.example","port":443}`
	if nodeSyncPayloadEqual(base, changed, false) {
		t.Fatal("server change must not be treated as equal")
	}
	changed = base
	changed.Enabled = false
	if nodeSyncPayloadEqual(base, changed, false) {
		t.Fatal("enabled change must not be treated as equal")
	}
	changed = base
	changed.ParsedConfig = `{"name":"node","type":"ss","server":"parsed-change.example","port":443}`
	if nodeSyncPayloadEqual(base, changed, false) {
		t.Fatal("parsed config change must not be treated as equal")
	}
	changed = base
	changed.Tags = []string{"source"}
	if nodeSyncPayloadEqual(base, changed, false) {
		t.Fatal("tag change must not be skipped as an unchanged payload")
	}
}

func TestCredentialMatchingStaysWithinSourceAndDistinguishesUsers(t *testing.T) {
	nodes := []storage.Node{
		{RawURL: "a", NodeName: "first", ClashConfig: `{"type":"vless","server":"same.example","port":443,"uuid":"first"}`},
		{RawURL: "a", NodeName: "second", ClashConfig: `{"type":"vless","server":"same.example","port":443,"uuid":"second"}`},
		{RawURL: "b", NodeName: "foreign", ClashConfig: `{"type":"vless","server":"same.example","port":443,"uuid":"foreign"}`},
	}
	idx := buildSourceNodeMatchIndex(nodes, "a")
	cfg := map[string]any{"type": "vless", "server": "same.example", "port": 443, "uuid": "second"}
	if got := idx.find("type_server_port_cred", "renamed", cfg); got != 1 {
		t.Fatalf("index=%d", got)
	}
	cfg["uuid"] = "foreign"
	if got := idx.find("type_server_port_cred", "renamed", cfg); got != -1 {
		t.Fatalf("foreign index=%d", got)
	}
}
