package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/storage"
)

// Exercise the whole request: upstream changes after the first sync, and the
// very next normal response must contain the new endpoint without a retry.
func TestNormalSubscriptionReturnsFreshNodesOnFirstRefresh(t *testing.T) {
	for _, minutes := range []int{0, 5} {
		t.Run(fmt.Sprintf("cache_%d", minutes), func(t *testing.T) {
			var version atomic.Int32
			version.Store(1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, "proxies:\n  - name: fresh-node\n    type: ss\n    server: version%d.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: test\n", version.Load())
			}))
			defer upstream.Close()
			originalClientFactory := newReferencedSyncHTTPClient
			newReferencedSyncHTTPClient = func(timeout time.Duration) *http.Client { return upstream.Client() }
			defer func() { newReferencedSyncHTTPClient = originalClientFactory }()
			dir := t.TempDir()
			repo, err := storage.NewTrafficRepository(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			ctx := context.Background()
			const username = "fresh-admin"
			if err := repo.CreateUser(ctx, username, "", "", "hash", storage.RoleAdmin, ""); err != nil {
				t.Fatal(err)
			}
			settings := storage.UserSettings{Username: username, ForceSyncExternal: true, CacheExpireMinutes: minutes, MatchRule: "node_name", SyncScope: "all", KeepNodeName: true}
			if err := repo.UpsertUserSettings(ctx, settings); err != nil {
				t.Fatal(err)
			}
			id, err := repo.CreateExternalSubscription(ctx, storage.ExternalSubscription{Username: username, Name: "fresh-source", URL: upstream.URL})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreateProxyProviderConfig(ctx, &storage.ProxyProviderConfig{Username: username, ExternalSubscriptionID: id, Name: "fresh-source", Type: "http", ProcessMode: "client"}); err != nil {
				t.Fatal(err)
			}
			const filename = "fresh.yaml"
			if err := os.WriteFile(filepath.Join(dir, filename), []byte("proxies: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := repo.CreateSubscribeFile(ctx, storage.SubscribeFile{Name: "fresh", Type: storage.SubscribeTypeCreate, Filename: filename, NormalLinkEnabled: true, DefaultOutputMode: storage.OutputModeNormal, SelectedTags: []string{"Provider/fresh-source"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.AssignSubscriptionToUser(ctx, username, file.ID); err != nil {
				t.Fatal(err)
			}
			h := newSubscriptionHandler(nil, repo, dir, subscriptionDefaultType)
			request := func() string {
				req := httptest.NewRequest("GET", "https://sub.example.com/api/clash/subscribe?filename="+filename+"&mode=normal", nil)
				req = req.WithContext(auth.ContextWithUsername(req.Context(), username))
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != 200 {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
				return w.Body.String()
			}
			if body := request(); !strings.Contains(body, "version1.example.com") {
				t.Fatalf("initial response missing node: %s", body)
			}
			version.Store(2)
			if minutes > 0 {
				if body := request(); !strings.Contains(body, "version1.example.com") {
					t.Fatalf("valid cache not retained: %s", body)
				}
				subs, err := repo.ListExternalSubscriptions(ctx, username)
				if err != nil {
					t.Fatal(err)
				}
				expired := time.Now().Add(-time.Hour)
				subs[0].LastSyncAt = &expired
				if err := repo.UpdateExternalSubscription(ctx, subs[0]); err != nil {
					t.Fatal(err)
				}
			}
			body := request()
			if !strings.Contains(body, "version2.example.com") || strings.Contains(body, "version1.example.com") {
				t.Fatalf("first refresh returned stale nodes: %s", body)
			}
		})
	}
}
