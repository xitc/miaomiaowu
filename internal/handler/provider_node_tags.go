package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"miaomiaowu/internal/logger"
	"miaomiaowu/internal/storage"
)

const providerNodeTagPrefix = "Provider/"

var providerNodeTagSyncMu sync.Mutex

func providerNodeTag(config storage.ProxyProviderConfig) string {
	return providerNodeTagPrefix + strings.TrimSpace(config.Name)
}

type providerNodeIdentity struct {
	name     string
	endpoint string
}

func proxyIdentity(proxy map[string]any) providerNodeIdentity {
	name, _ := proxy["name"].(string)
	typeName, _ := proxy["type"].(string)
	server, _ := proxy["server"].(string)
	port := strings.TrimSpace(fmt.Sprint(proxy["port"]))
	endpoint := ""
	if strings.TrimSpace(typeName) != "" && strings.TrimSpace(server) != "" && port != "" && port != "<nil>" {
		endpoint = strings.ToLower(strings.TrimSpace(typeName)) + "\x00" + strings.ToLower(strings.TrimSpace(server)) + "\x00" + port
	}
	return providerNodeIdentity{name: strings.TrimSpace(name), endpoint: endpoint}
}

func providerNodeIdentitySets(providerNodes []any) (map[string]bool, map[string]bool) {
	names := make(map[string]bool)
	endpoints := make(map[string]bool)
	for _, raw := range providerNodes {
		proxy, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		identity := proxyIdentity(proxy)
		if identity.name != "" {
			names[identity.name] = true
		}
		if identity.endpoint != "" {
			endpoints[identity.endpoint] = true
		}
	}
	return names, endpoints
}

func hasTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}

func updateProviderTags(tags []string, removeTags map[string]bool, addTag string, add bool) ([]string, bool) {
	next := make([]string, 0, len(tags)+1)
	seen := make(map[string]bool, len(tags)+1)
	changed := false
	for _, tag := range tags {
		if removeTags[tag] {
			changed = true
			continue
		}
		if seen[tag] {
			changed = true
			continue
		}
		seen[tag] = true
		next = append(next, tag)
	}
	if add && addTag != "" && !seen[addTag] {
		next = append(next, addTag)
		changed = true
	}
	return next, changed
}

func reconcileProviderNodeTags(nodes []storage.Node, sourceURL, addTag string, staleTags []string, providerNodes []any) []storage.Node {
	names, _ := providerNodeIdentitySets(providerNodes)
	sourceNames := make(map[string]bool)
	for _, node := range nodes {
		if node.RawURL != sourceURL {
			continue
		}
		sourceNames[node.NodeName] = true
		var proxy map[string]any
		_ = json.Unmarshal([]byte(node.ClashConfig), &proxy)
		if identity := proxyIdentity(proxy); identity.name != "" {
			sourceNames[identity.name] = true
		}
	}
	// Endpoint matching is only a fallback for nodes renamed by Provider
	// overrides/scripts. Exact names remain authoritative when available.
	endpoints := make(map[string]bool)
	for _, raw := range providerNodes {
		proxy, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		identity := proxyIdentity(proxy)
		if identity.endpoint != "" && !sourceNames[identity.name] {
			endpoints[identity.endpoint] = true
		}
	}
	removeTags := make(map[string]bool, len(staleTags)+1)
	removeTags[addTag] = true
	for _, tag := range staleTags {
		removeTags[tag] = true
	}

	changedNodes := make([]storage.Node, 0)
	for _, node := range nodes {
		if node.RawURL != sourceURL {
			continue
		}

		var proxy map[string]any
		_ = json.Unmarshal([]byte(node.ClashConfig), &proxy)
		identity := proxyIdentity(proxy)
		matched := names[node.NodeName] || names[identity.name] || (identity.endpoint != "" && endpoints[identity.endpoint])
		nextTags, changed := updateProviderTags(node.Tags, removeTags, addTag, matched)
		if !changed {
			continue
		}
		node.Tags = nextTags
		if len(nextTags) > 0 {
			node.Tag = nextTags[0]
		} else {
			node.Tag = ""
		}
		changedNodes = append(changedNodes, node)
	}
	return changedNodes
}

func missingProviderNodes(nodes []storage.Node, sub storage.ExternalSubscription, tag string, providerNodes []any) []storage.Node {
	existingNames := make(map[string]bool, len(nodes))
	existingSourceNames := make(map[string]bool)
	existingSourceEndpoints := make(map[string]bool)
	for _, node := range nodes {
		existingNames[node.NodeName] = true
		if node.RawURL != sub.URL {
			continue
		}
		existingSourceNames[node.NodeName] = true
		var proxy map[string]any
		_ = json.Unmarshal([]byte(node.ClashConfig), &proxy)
		identity := proxyIdentity(proxy)
		if identity.name != "" {
			existingSourceNames[identity.name] = true
		}
		if identity.endpoint != "" {
			existingSourceEndpoints[identity.endpoint] = true
		}
	}

	missing := make([]storage.Node, 0)
	for _, raw := range providerNodes {
		proxy, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		identity := proxyIdentity(proxy)
		if identity.name == "" || existingSourceNames[identity.name] || (identity.endpoint != "" && existingSourceEndpoints[identity.endpoint]) {
			continue
		}
		// Avoid introducing duplicate proxy names across different sources. Such a
		// node remains available through its existing pool entry.
		if existingNames[identity.name] {
			continue
		}
		clashConfig, err := json.Marshal(proxy)
		if err != nil {
			continue
		}
		protocol, _ := proxy["type"].(string)
		tags := []string{sub.Name}
		if tag != "" && tag != sub.Name {
			tags = append(tags, tag)
		}
		missing = append(missing, storage.Node{
			Username:     sub.Username,
			RawURL:       sub.URL,
			NodeName:     identity.name,
			Protocol:     strings.ToLower(strings.TrimSpace(protocol)),
			ParsedConfig: string(clashConfig),
			ClashConfig:  string(clashConfig),
			Enabled:      true,
			Tag:          tags[0],
			Tags:         tags,
		})
		existingNames[identity.name] = true
		existingSourceNames[identity.name] = true
		if identity.endpoint != "" {
			existingSourceEndpoints[identity.endpoint] = true
		}
	}
	return missing
}

func syncProviderNodeTags(ctx context.Context, repo *storage.TrafficRepository, sub storage.ExternalSubscription, config storage.ProxyProviderConfig, entry *CacheEntry, staleTags ...string) error {
	if repo == nil || entry == nil {
		return nil
	}
	providerNodeTagSyncMu.Lock()
	defer providerNodeTagSyncMu.Unlock()
	nodes, err := repo.ListNodes(ctx, config.Username)
	if err != nil {
		return fmt.Errorf("list nodes for provider tags: %w", err)
	}
	changed := reconcileProviderNodeTags(nodes, sub.URL, providerNodeTag(config), staleTags, entry.Nodes)
	for _, node := range changed {
		if _, err := repo.UpdateNode(ctx, node); err != nil {
			return fmt.Errorf("update provider tag for node %d: %w", node.ID, err)
		}
	}
	created := 0
	for _, node := range missingProviderNodes(nodes, sub, providerNodeTag(config), entry.Nodes) {
		if strings.TrimSpace(node.Protocol) == "" {
			logger.Warn("[代理集合标签] Provider 节点缺少协议，跳过载入节点池", "provider", config.Name, "node", node.NodeName)
			continue
		}
		if _, err := repo.CreateNode(ctx, node); err != nil {
			return fmt.Errorf("create provider node %q: %w", node.NodeName, err)
		}
		created++
	}
	logger.Info("[代理集合标签] 已同步 Provider 节点标签", "provider", config.Name, "tag", providerNodeTag(config), "matched_nodes", entry.NodeCount, "updated_nodes", len(changed), "created_nodes", created)
	return nil
}

func refreshAndSyncProviderNodeTags(ctx context.Context, repo *storage.TrafficRepository, sub storage.ExternalSubscription, config storage.ProxyProviderConfig, staleTags ...string) error {
	entry, err := RefreshProxyProviderCache(&sub, &config)
	if err != nil {
		return err
	}
	return syncProviderNodeTags(ctx, repo, sub, config, entry, staleTags...)
}

func removeProviderNodeTag(ctx context.Context, repo *storage.TrafficRepository, username string, tag string) error {
	if repo == nil || strings.TrimSpace(tag) == "" {
		return nil
	}
	providerNodeTagSyncMu.Lock()
	defer providerNodeTagSyncMu.Unlock()
	nodes, err := repo.ListNodes(ctx, username)
	if err != nil {
		return err
	}
	removeTags := map[string]bool{tag: true}
	for _, node := range nodes {
		if !hasTag(node.Tags, tag) {
			continue
		}
		nextTags, _ := updateProviderTags(node.Tags, removeTags, "", false)
		node.Tags = nextTags
		if len(nextTags) > 0 {
			node.Tag = nextTags[0]
		} else {
			node.Tag = ""
		}
		if _, err := repo.UpdateNode(ctx, node); err != nil {
			return err
		}
	}
	return nil
}
