package handler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

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

func TestNodeSyncPayloadEqualDetectsMaterialChanges(t *testing.T) {
	base := storage.Node{
		RawURL:       "source",
		NodeName:     "node",
		Protocol:     "ss",
		ParsedConfig: `{"name":"node","type":"ss","server":"a.example","port":443}`,
		ClashConfig:  `{"name":"node","type":"ss","server":"a.example","port":443}`,
		Enabled:      true,
		Tag:          "source",
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
}
