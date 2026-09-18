package handler

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestContentRenderingSharesWorkAndCancellation(t *testing.T) {
	cache := newContentRenderCache(1024, 1)
	key := sha256.Sum256([]byte("same"))
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	fn := func() (string, error) { calls.Add(1); close(entered); <-release; return "value", nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := cache.render(ctx, key, fn); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := cache.render(context.Background(), key, fn)
			if err != nil || v != "value" {
				t.Errorf("result %q %v", v, err)
			}
		}()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("runs=%d", calls.Load())
	}
}

func TestContentRenderingEvictsAndRetriesFailures(t *testing.T) {
	cache := newContentRenderCache(8, 1)
	ctx := context.Background()
	var runs int
	run := func() (string, error) { runs++; return "1234", nil }
	a, b, c := sha256.Sum256([]byte("a")), sha256.Sum256([]byte("b")), sha256.Sum256([]byte("c"))
	for _, k := range [][32]byte{a, b, c, a} {
		if _, err := cache.render(ctx, k, run); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 4 || cache.bytes > 8 {
		t.Fatalf("runs=%d bytes=%d", runs, cache.bytes)
	}
	bad := sha256.Sum256([]byte("error"))
	if _, err := cache.render(ctx, bad, func() (string, error) { return "", errors.New("failed") }); err == nil {
		t.Fatal("missing error")
	}
	if v, err := cache.render(ctx, bad, run); err != nil || v != "1234" {
		t.Fatal("failed result cached")
	}
}

func TestTemplateRenderingUsesCurrentSelectedInputs(t *testing.T) {
	makeInput := func(server string) normalTemplateInput {
		p := []map[string]any{{"name": "selected", "type": "ss", "server": server, "port": 443, "cipher": "aes-128-gcm", "password": "test"}}
		return normalTemplateInput{Template: "proxies: []\nproxy-groups: []\nrules: [MATCH,DIRECT]\n", Proxies: p, RootProxies: p}
	}
	for _, server := range []string{"first.invalid", "second.invalid", "first.invalid"} {
		out, err := renderNormalSubscriptionTemplate(context.Background(), makeInput(server))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, server) {
			t.Fatal("served old render")
		}
	}
	empty := makeInput("private.invalid")
	empty.Proxies = nil
	empty.RootProxies = nil
	out, err := renderNormalSubscriptionTemplate(context.Background(), empty)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "private.invalid") || strings.Contains(out, "first.invalid") {
		t.Fatal("leaked unselected nodes")
	}
	changed := makeInput("first.invalid")
	changed.Template += "mixed-port: 12345\n"
	out, err = renderNormalSubscriptionTemplate(context.Background(), changed)
	if err != nil || !strings.Contains(out, "12345") {
		t.Fatal("template change ignored")
	}
}
