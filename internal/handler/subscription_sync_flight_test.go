package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"miaomiaowu/internal/storage"
)

func TestReferencedRefreshConcurrentCancellationAndNextFreshRequest(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-release
		}
		fmt.Fprintf(w, "proxies:\n  - name: current\n    type: ss\n    server: version%d.invalid\n    port: 443\n    cipher: aes-128-gcm\n    password: test\n", n)
	}))
	defer upstream.Close()
	repo, err := storage.NewTrafficRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	if err := repo.CreateUser(ctx, "owner", "", "", "hash", storage.RoleAdmin, ""); err != nil {
		t.Fatal(err)
	}
	id, err := repo.CreateExternalSubscription(ctx, storage.ExternalSubscription{Username: "owner", Name: "source", URL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetExternalSubscription(ctx, id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	settings := storage.UserSettings{MatchRule: "node_name", SyncScope: "all", KeepNodeName: true}
	overlapStart := time.Now()
	leader, cancel := context.WithCancel(ctx)
	leaderDone := make(chan error, 1)
	go func() {
		_, _, e := syncReferencedSource(leader, upstream.Client(), repo, "", "owner", sub, settings)
		leaderDone <- e
	}()
	<-entered
	cancel()
	if e := <-leaderDone; e != context.Canceled {
		t.Fatalf("leader error %v", e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, updated, e := syncReferencedSource(ctx, upstream.Client(), repo, "", "owner", sub, settings)
			if e != nil || n != 1 || updated.LastSyncAt == nil {
				t.Errorf("refresh %d %v", n, e)
			}
		}()
	}
	// Upstream remains blocked while followers enter the shared refresh.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent upstream requests=%d", calls.Load())
	}
	late := context.WithValue(ctx, subscriptionRequestStartKey{}, overlapStart)
	if _, _, err := syncReferencedSource(late, upstream.Client(), repo, "", "owner", sub, settings); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("overlapping request repeated completed refresh")
	}
	if _, _, e := syncReferencedSource(ctx, upstream.Client(), repo, "", "owner", sub, settings); e != nil {
		t.Fatal(e)
	}
	nodes, e := repo.ListNodes(ctx, "owner")
	if e != nil || len(nodes) != 1 || !strings.Contains(nodes[0].ClashConfig, "version2.invalid") {
		t.Fatal("next cache=0 refresh was skipped")
	}
}
