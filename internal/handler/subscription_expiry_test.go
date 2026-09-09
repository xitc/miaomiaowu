package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/storage"
)

func TestSubscriptionExpiryControlsNormalAndProviderGateway(t *testing.T) {
	var upstreamRequests atomic.Int32
	useLocalProviderContentClient(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte("proxies:\n  - name: live-node\n    type: ss\n    server: live.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: test\n"))
	}))
	t.Cleanup(upstream.Close)

	dir := t.TempDir()
	repo, err := storage.NewTrafficRepository(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	ctx := context.Background()
	const username = "expiry-admin"
	if err := repo.CreateUser(ctx, username, "", "", "test-hash", storage.RoleAdmin, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertUserSettings(ctx, storage.UserSettings{Username: username, NodeNameFilter: defaultNodeNameFilterPattern}); err != nil {
		t.Fatal(err)
	}
	externalID, err := repo.CreateExternalSubscription(ctx, storage.ExternalSubscription{
		Username: username,
		Name:     "source",
		URL:      upstream.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	providerID, err := repo.CreateProxyProviderConfig(ctx, &storage.ProxyProviderConfig{
		Username:               username,
		ExternalSubscriptionID: externalID,
		Name:                   "source",
		Type:                   "http",
		Interval:               300,
		ProcessMode:            "client",
	})
	if err != nil {
		t.Fatal(err)
	}

	filename := "expiry.yaml"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("proxies: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := repo.CreateSubscribeFile(ctx, storage.SubscribeFile{
		Name:                  "expiry-test",
		Type:                  storage.SubscribeTypeCreate,
		Filename:              filename,
		NormalLinkEnabled:     true,
		ProviderLinkEnabled:   true,
		DefaultOutputMode:     storage.OutputModeNormal,
		SelectedProviderNames: []string{"source"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignSubscriptionToUser(ctx, username, file.ID); err != nil {
		t.Fatal(err)
	}

	handler := newSubscriptionHandler(nil, repo, dir, subscriptionDefaultType)
	request := func(mode string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "https://sub.example.com/api/clash/subscribe?filename="+filename+"&mode="+mode+"&provider_id="+strconv.FormatInt(providerID, 10), nil)
		return req.WithContext(auth.ContextWithUsername(req.Context(), username))
	}

	activeProvider := httptest.NewRecorder()
	handler.ServeHTTP(activeProvider, request(providerSourceOutputMode))
	if activeProvider.Code != http.StatusOK || !strings.Contains(activeProvider.Body.String(), "live-node") {
		t.Fatalf("active provider request failed: status=%d body=%s", activeProvider.Code, activeProvider.Body.String())
	}
	if upstreamRequests.Load() != 1 {
		t.Fatalf("expected one upstream request, got %d", upstreamRequests.Load())
	}

	expiredAt := time.Now().Add(-time.Minute)
	file.ExpireAt = &expiredAt
	if _, err := repo.UpdateSubscribeFile(ctx, file); err != nil {
		t.Fatal(err)
	}

	expiredProvider := httptest.NewRecorder()
	handler.ServeHTTP(expiredProvider, request(providerSourceOutputMode))
	if expiredProvider.Code != http.StatusOK || !strings.HasPrefix(expiredProvider.Body.String(), "proxies:\n") || !strings.Contains(expiredProvider.Body.String(), "订阅已过期") {
		t.Fatalf("expired provider response failed: status=%d body=%s", expiredProvider.Code, expiredProvider.Body.String())
	}
	if upstreamRequests.Load() != 1 {
		t.Fatalf("expired provider request reached upstream; count=%d", upstreamRequests.Load())
	}

	expiredNormal := httptest.NewRecorder()
	handler.ServeHTTP(expiredNormal, request(storage.OutputModeNormal))
	if expiredNormal.Code != http.StatusOK || !strings.Contains(expiredNormal.Body.String(), "订阅已过期") {
		t.Fatalf("expired normal response failed: status=%d body=%s", expiredNormal.Code, expiredNormal.Body.String())
	}
}
