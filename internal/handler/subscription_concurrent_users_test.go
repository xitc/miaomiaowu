package handler

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/storage"
)

func TestConcurrentSubscriptionUsersKeepSelectionsIsolated(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("rule_templates", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("rule_templates/shared.yaml", []byte("proxies: []\nproxy-groups: []\nrules: [MATCH,DIRECT]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewTrafficRepository(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	if err := repo.CreateUser(ctx, "owner", "", "", "hash", storage.RoleAdmin, ""); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"alice", "bob"} {
		if err := repo.CreateUser(ctx, user, "", "", "hash", storage.RoleUser, ""); err != nil {
			t.Fatal(err)
		}
		cfg := fmt.Sprintf(`{"name":%q,"type":"ss","server":%q,"port":443,"cipher":"aes-128-gcm","password":"test"}`, user, user+".invalid")
		node, err := repo.CreateNode(ctx, storage.Node{Username: "owner", NodeName: user, Protocol: "ss", ClashConfig: cfg, ParsedConfig: cfg, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(user+".yaml", []byte("proxies: []\n"), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := repo.CreateSubscribeFile(ctx, storage.SubscribeFile{Name: user, Type: storage.SubscribeTypeCreate, Filename: user + ".yaml", TemplateFilename: "shared.yaml", NormalLinkEnabled: true, DefaultOutputMode: storage.OutputModeNormal, SelectedNodeIDs: []int64{node.ID}})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.AssignSubscriptionToUser(ctx, user, file.ID); err != nil {
			t.Fatal(err)
		}
	}
	h := newSubscriptionHandler(nil, repo, dir, subscriptionDefaultType)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		user, other := "alice", "bob"
		if i%2 == 1 {
			user, other = other, user
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "http://test.invalid/api/clash/subscribe?filename="+user+".yaml&mode=normal", nil)
			req = req.WithContext(auth.ContextWithUsername(req.Context(), user))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != 200 || !strings.Contains(w.Body.String(), user+".invalid") || strings.Contains(w.Body.String(), other+".invalid") {
				t.Errorf("subscription selection leaked for %s, status %d", user, w.Code)
			}
		}()
	}
	wg.Wait()
}
