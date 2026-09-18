package handler

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"

	"github.com/MMWOrg/mmwX-plugins/proxyparser/substore"
	"miaomiaowu/internal/logger"
)

type renderResult struct {
	text string
	err  error
	done chan struct{}
}
type renderEntry struct {
	key  [32]byte
	text string
}

// Stores only pure rendering results, never HTTP responses or authorization.
// Keys include all current inputs, so source refreshes are never skipped.
type contentRenderCache struct {
	mu            sync.Mutex
	pending       map[[32]byte]*renderResult
	entries       map[[32]byte]*list.Element
	lru           *list.List
	bytes, budget int
	slots         chan struct{}
}

func newContentRenderCache(budget, parallel int) *contentRenderCache {
	return &contentRenderCache{pending: make(map[[32]byte]*renderResult), entries: make(map[[32]byte]*list.Element), lru: list.New(), budget: budget, slots: make(chan struct{}, parallel)}
}

func (c *contentRenderCache) render(ctx context.Context, key [32]byte, fn func() (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	if e := c.entries[key]; e != nil {
		c.lru.MoveToFront(e)
		text := e.Value.(renderEntry).text
		c.mu.Unlock()
		return text, nil
	}
	f := c.pending[key]
	if f == nil {
		f = &renderResult{done: make(chan struct{})}
		c.pending[key] = f
		go c.run(key, f, fn)
	}
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-f.done:
		return f.text, f.err
	}
}

func (c *contentRenderCache) run(key [32]byte, f *renderResult, fn func() (string, error)) {
	// A caller disconnecting must not cancel rendering for other subscribers.
	c.slots <- struct{}{}
	func() {
		defer func() {
			if recover() != nil {
				f.err = fmt.Errorf("subscription rendering failed")
			}
		}()
		f.text, f.err = fn()
	}()
	<-c.slots
	c.mu.Lock()
	defer c.mu.Unlock()
	if f.err == nil && len(f.text) <= c.budget/2 {
		for c.bytes+len(f.text) > c.budget || c.lru.Len() >= 32 {
			e := c.lru.Back()
			v := e.Value.(renderEntry)
			c.bytes -= len(v.text)
			delete(c.entries, v.key)
			c.lru.Remove(e)
		}
		c.entries[key] = c.lru.PushFront(renderEntry{key: key, text: f.text})
		c.bytes += len(f.text)
	}
	delete(c.pending, key)
	close(f.done)
}

var subscriptionResponseSlots = make(chan struct{}, min(2, runtime.GOMAXPROCS(0)))

var normalTemplateRenders = newContentRenderCache(16<<20, min(2, runtime.GOMAXPROCS(0)))

type normalTemplateInput struct {
	Template                          string
	Proxies, RootProxies, RelayGroups []map[string]any
	Providers                         map[string][]string
}

func renderNormalSubscriptionTemplate(ctx context.Context, input normalTemplateInput) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode template inputs: %w", err)
	}
	return normalTemplateRenders.render(ctx, sha256.Sum256(encoded), func() (string, error) {
		processor := substore.NewTemplateV3Processor(nil, input.Providers)
		result, err := processor.ProcessTemplate(input.Template, input.Proxies)
		if err != nil {
			return "", fmt.Errorf("处理模板失败: %w", err)
		}
		result, err = injectProxiesIntoTemplate(result, input.RootProxies)
		if err != nil {
			return "", fmt.Errorf("注入代理节点失败: %w", err)
		}
		if len(input.RelayGroups) > 0 {
			result, err = injectRelayGroupsIntoTemplate(result, input.RelayGroups)
			if err != nil {
				logger.Info("[模板生成] 注入中转代理组失败", "error", err)
			}
		}
		return result, nil
	})
}
