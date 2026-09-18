package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"miaomiaowu/internal/auth"
	"miaomiaowu/internal/storage"
)

// Synthetic data only; exercises template generation and the normal HTTP path.
func BenchmarkNormalSubscriptionRender(b *testing.B) { benchmarkSubscriptionRender(b, 1, false) }

func BenchmarkConcurrentSubscriptionRender(b *testing.B) { benchmarkSubscriptionRender(b, 8, false) }

func BenchmarkConcurrentUncachedSubscription(b *testing.B) { benchmarkSubscriptionRender(b, 8, true) }

func benchmarkSubscriptionRender(b *testing.B, clients int, refresh bool) {
	dir := b.TempDir()
	b.Chdir(dir)
	if err := os.Mkdir("rule_templates", 0700); err != nil {
		b.Fatal(err)
	}
	repo, err := storage.NewTrafficRepository(filepath.Join(dir, "test.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	if err := repo.CreateUser(ctx, "bench", "", "", "hash", storage.RoleAdmin, ""); err != nil {
		b.Fatal(err)
	}
	var sourceURL string
	var upstreamCalls atomic.Int64
	var sourceBody strings.Builder
	sourceBody.WriteString("proxies:\n")
	for i := 0; i < 233; i++ {
		fmt.Fprintf(&sourceBody, "  - name: 🇭🇰 测试-%03d\n    type: ss\n    server: node-%d.invalid\n    port: 443\n    cipher: aes-128-gcm\n    password: test\n", i, i)
	}
	if refresh {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCalls.Add(1)
			time.Sleep(20 * time.Millisecond)
			io.WriteString(w, sourceBody.String())
		}))
		defer upstream.Close()
		sourceURL = upstream.URL
		old := newReferencedSyncHTTPClient
		newReferencedSyncHTTPClient = func(time.Duration) *http.Client { return upstream.Client() }
		defer func() { newReferencedSyncHTTPClient = old }()
		if _, err := repo.CreateExternalSubscription(ctx, storage.ExternalSubscription{Username: "bench", Name: "bench-source", URL: sourceURL}); err != nil {
			b.Fatal(err)
		}
		if err := repo.UpsertUserSettings(ctx, storage.UserSettings{Username: "bench", ForceSyncExternal: true, CacheExpireMinutes: 0, MatchRule: "node_name", SyncScope: "all", KeepNodeName: true}); err != nil {
			b.Fatal(err)
		}
	}
	var names []string
	for i := 0; i < 233; i++ {
		name := fmt.Sprintf("🇭🇰 测试-%03d", i)
		names = append(names, name)
		cfg := fmt.Sprintf(`{"name":%q,"type":"ss","server":"node-%d.invalid","port":443,"cipher":"aes-128-gcm","password":"test"}`, name, i)
		_, err := repo.CreateNode(ctx, storage.Node{Username: "bench", RawURL: sourceURL, NodeName: name, Protocol: "ss", ClashConfig: cfg, ParsedConfig: cfg, Enabled: true})
		if err != nil {
			b.Fatal(err)
		}
	}
	var tpl strings.Builder
	tpl.WriteString("proxies: []\nproxy-groups:\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&tpl, "  - name: group-%d\n    type: select\n    proxies:\n", i)
		for _, name := range names {
			fmt.Fprintf(&tpl, "      - %s\n", name)
		}
	}
	tpl.WriteString("rules:\n  - MATCH,group-0\n")
	if err := os.WriteFile("rule_templates/bench.yaml", []byte(tpl.String()), 0600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bench.yaml"), []byte("proxies: []\n"), 0600); err != nil {
		b.Fatal(err)
	}
	f, err := repo.CreateSubscribeFile(ctx, storage.SubscribeFile{Name: "bench", Type: storage.SubscribeTypeCreate, Filename: "bench.yaml", TemplateFilename: "bench.yaml", NormalLinkEnabled: true, DefaultOutputMode: storage.OutputModeNormal})
	if err != nil {
		b.Fatal(err)
	}
	if err := repo.AssignSubscriptionToUser(ctx, "bench", f.ID); err != nil {
		b.Fatal(err)
	}
	h := newSubscriptionHandler(nil, repo, dir, subscriptionDefaultType)
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/api/clash/subscribe?filename=bench.yaml&mode=normal", nil)
	req = req.WithContext(auth.ContextWithUsername(req.Context(), "bench"))
	var healthMu sync.Mutex
	var healthTimes []time.Duration
	healthDone, stopHealth := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			io.WriteString(w, "ok")
			return
		}
		h.ServeHTTP(w, r.WithContext(auth.ContextWithUsername(r.Context(), "bench")))
	}))
	defer server.Close()
	go func() {
		defer close(healthDone)
		for {
			select {
			case <-stopHealth:
				return
			case <-time.After(5 * time.Millisecond):
				started := time.Now()
				resp, err := server.Client().Get(server.URL + "/health")
				if err == nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
				healthMu.Lock()
				healthTimes = append(healthTimes, time.Since(started))
				healthMu.Unlock()
			}
		}
	}()
	cpuTime := func() time.Duration {
		var r syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &r)
		return time.Duration(r.Utime.Sec+r.Stime.Sec)*time.Second + time.Duration(r.Utime.Usec+r.Stime.Usec)*time.Microsecond
	}
	cpuStart := cpuTime()
	startCalls := upstreamCalls.Load()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < clients; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := httptest.NewRecorder()
				if refresh {
					response, err := server.Client().Get(server.URL + "/api/clash/subscribe?filename=bench.yaml&mode=normal")
					if err != nil {
						b.Error(err)
						return
					}
					w.Code = response.StatusCode
					io.Copy(w.Body, response.Body)
					response.Body.Close()
				} else {
					h.ServeHTTP(w, req.Clone(req.Context()))
				}
				if w.Code != 200 || !strings.Contains(w.Body.String(), "node-232.invalid") {
					b.Errorf("invalid response: %d", w.Code)
				}
			}()
		}
		wg.Wait()
	}
	b.StopTimer()
	cpuUsed := cpuTime() - cpuStart
	close(stopHealth)
	<-healthDone
	b.ReportMetric(float64(cpuUsed.Nanoseconds())/float64(b.N), "cpu-ns/batch")
	b.ReportMetric(float64(upstreamCalls.Load()-startCalls)/float64(b.N), "fetches/batch")
	if len(healthTimes) > 0 {
		sort.Slice(healthTimes, func(i, j int) bool { return healthTimes[i] < healthTimes[j] })
		b.ReportMetric(float64(healthTimes[(len(healthTimes)-1)*95/100].Microseconds())/1000, "health-p95-ms")
	}
}
