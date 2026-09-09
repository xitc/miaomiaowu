package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"miaomiaowu/internal/storage"
)

type nodeGroupSource struct {
	NodeID         int64      `json:"node_id"`
	SubscriptionID int64      `json:"subscription_id"`
	Name           string     `json:"name"`
	NodeName       string     `json:"node_name"`
	OriginalName   string     `json:"original_name"`
	LastSyncAt     *time.Time `json:"last_sync_at,omitempty"`
}
type nodeGroup struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Protocol  string            `json:"protocol"`
	Server    string            `json:"server"`
	Port      any               `json:"port"`
	Enabled   bool              `json:"enabled"`
	Tags      []string          `json:"tags"`
	Sources   []nodeGroupSource `json:"sources"`
	MemberIDs []int64           `json:"member_ids"`
	members   []storage.Node
}

// Compare the full connection configuration, including credentials, TLS and
// transport options. Only the display name and the port's scalar type differ.
func nodeConnectionKey(config map[string]any) string {
	cp := make(map[string]any, len(config))
	for k, v := range config {
		if k != "name" {
			cp[k] = v
		}
	}
	if v, ok := cp["port"]; ok {
		cp["port"] = fmt.Sprint(v)
	}
	b, err := json.Marshal(cp)
	if err != nil {
		return ""
	}
	return string(b)
}

// Source rows remain independent snapshots. Groups are a reversible projection;
// no source, node ID, tags, or sync timestamps are reassigned or deleted.
func buildNodeGroups(nodes []storage.Node, subs []storage.ExternalSubscription, keepNames bool) []nodeGroup {
	sources := map[string]storage.ExternalSubscription{}
	for _, s := range subs {
		sources[s.URL] = s
	}
	referenced := map[int64]bool{}
	for _, n := range nodes {
		if n.ChainProxyNodeID != nil {
			referenced[*n.ChainProxyNodeID] = true
		}
		for _, id := range n.RelayGroupNodeIDs {
			referenced[id] = true
		}
	}
	sorted := append([]storage.Node(nil), nodes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sources[sorted[i].RawURL].ID, sources[sorted[j].RawURL].ID
		if a != b {
			return a < b
		}
		return sorted[i].ID < sorted[j].ID
	})
	groups := []nodeGroup{}
	buckets := map[string][]int{}
	for _, n := range sorted {
		var cfg map[string]any
		err := json.Unmarshal([]byte(n.ClashConfig), &cfg)
		source, external := sources[n.RawURL]
		key := nodeConnectionKey(cfg)
		// Policies and chain references are not interchangeable connection identities.
		policy, _ := json.Marshal([]any{n.Username, n.Enabled, n.ProbeEnabled, n.OriginalServer, n.ProbeServer, n.ChainProxyNodeID, n.RelayGroupName, n.RelayGroupNodeIDs})
		key += string(policy)
		mergeable := external && err == nil && cfg != nil && cfg["type"] != nil && cfg["server"] != nil && !referenced[n.ID] && n.ChainProxyNodeID == nil && len(n.RelayGroupNodeIDs) == 0
		groupIdx := -1
		if mergeable {
			for _, idx := range buckets[key] {
				sameSource := false
				for _, m := range groups[idx].members {
					if m.RawURL == n.RawURL {
						sameSource = true
						break
					}
				}
				if !sameSource {
					groupIdx = idx
					break
				}
			}
		}
		if groupIdx < 0 {
			groupIdx = len(groups)
			server, _ := cfg["server"].(string)
			groups = append(groups, nodeGroup{ID: n.ID, Name: n.NodeName, Protocol: n.Protocol, Server: server, Port: cfg["port"], Enabled: n.Enabled, Tags: []string{}, Sources: []nodeGroupSource{}, MemberIDs: []int64{}})
			if mergeable {
				buckets[key] = append(buckets[key], groupIdx)
			}
		}
		g := &groups[groupIdx]
		g.members = append(g.members, n)
		g.MemberIDs = append(g.MemberIDs, n.ID)
		g.Tags = mergeExternalSyncTags(g.Tags, n.Tags)
		if external {
			g.Sources = append(g.Sources, nodeGroupSource{NodeID: n.ID, SubscriptionID: source.ID, Name: source.Name, NodeName: n.NodeName, OriginalName: n.SourceNodeName, LastSyncAt: source.LastSyncAt})
		}
	}
	used := map[string]bool{}
	// Reserve manual names so external aliases cannot hide manually managed nodes.
	for _, g := range groups {
		if len(g.Sources) == 0 {
			used[g.Name] = true
		}
	}
	for i := range groups {
		g := &groups[i]
		if len(g.Sources) == 0 {
			continue
		}
		if !keepNames {
			for _, n := range g.members {
				if n.SourceNodeName != "" {
					g.Name = n.SourceNodeName
					break
				}
			}
		}
		base := g.Name
		if used[g.Name] {
			g.Name = base + " [" + g.Sources[0].Name + "]"
		}
		for suffix := 2; used[g.Name]; suffix++ {
			g.Name = fmt.Sprintf("%s [%s %d]", base, g.Sources[0].Name, suffix)
		}
		used[g.Name] = true
	}
	return groups
}

func loadNodeGroups(ctx context.Context, repo *storage.TrafficRepository, username string) ([]nodeGroup, error) {
	nodes, err := repo.ListNodes(ctx, username)
	if err != nil {
		return nil, err
	}
	subs, err := repo.ListExternalSubscriptions(ctx, username)
	if err != nil {
		return nil, err
	}
	settings, err := repo.GetUserSettings(ctx, username)
	keepNames := true
	if err == nil {
		keepNames = settings.KeepNodeName
	}
	return buildNodeGroups(nodes, subs, keepNames), nil
}

// Aggregate only the source members actually present in this response. Traffic,
// source refreshes and filtering have already run using their original ownership.
func aggregateSubscriptionNodes(data []byte, groups []nodeGroup) ([]byte, error) {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	proxies, ok := root["proxies"].([]any)
	if !ok {
		return data, nil
	}
	byName := map[string]int{}
	ambiguous := map[string]bool{}
	for i, g := range groups {
		for _, n := range g.members {
			if _, exists := byName[n.NodeName]; exists {
				ambiguous[n.NodeName] = true
			}
			byName[n.NodeName] = i
		}
	}
	reserved := map[string]bool{}
	for _, raw := range proxies {
		if n, ok := raw.(map[string]any); ok {
			name, _ := n["name"].(string)
			if _, known := byName[name]; !known || ambiguous[name] {
				reserved[name] = true
			}
		}
	}
	if pg, ok := root["proxy-groups"].([]any); ok {
		for _, raw := range pg {
			if g, ok := raw.(map[string]any); ok {
				name, _ := g["name"].(string)
				reserved[name] = true
			}
		}
	}
	aliases := map[string]string{}
	seen := map[string]string{}
	out := make([]any, 0, len(proxies))
	changed := false
	for _, raw := range proxies {
		n, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		name, _ := n["name"].(string)
		idx, known := byName[name]
		if !known || ambiguous[name] {
			out = append(out, raw)
			continue
		}
		key := fmt.Sprint(idx) + "\x00" + nodeConnectionKey(n)
		if target, ok := seen[key]; ok {
			aliases[name] = target
			changed = true
			continue
		}
		target := groups[idx].Name
		for suffix := 2; reserved[target]; suffix++ {
			target = fmt.Sprintf("%s [%d]", groups[idx].Name, suffix)
		}
		reserved[target] = true
		seen[key] = target
		aliases[name] = target
		if name != target {
			n["name"] = target
			changed = true
		}
		out = append(out, n)
	}
	if !changed {
		return data, nil
	}
	root["proxies"] = out
	for _, raw := range out {
		if n, ok := raw.(map[string]any); ok {
			if old, ok := n["dialer-proxy"].(string); ok {
				if target, exists := aliases[old]; exists {
					n["dialer-proxy"] = target
				}
			}
		}
	}
	if pg, ok := root["proxy-groups"].([]any); ok {
		for _, raw := range pg {
			g, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			members, ok := g["proxies"].([]any)
			if !ok {
				continue
			}
			dedup := []any{}
			taken := map[string]bool{}
			for _, m := range members {
				name, ok := m.(string)
				if !ok {
					dedup = append(dedup, m)
					continue
				}
				if alias, ok := aliases[name]; ok {
					name = alias
				}
				if !taken[name] {
					taken[name] = true
					dedup = append(dedup, name)
				}
			}
			g["proxies"] = dedup
		}
	}
	if rules, ok := root["rules"].([]any); ok {
		for i, raw := range rules {
			rule, ok := raw.(string)
			if !ok {
				continue
			}
			parts := strings.Split(rule, ",")
			at := len(parts) - 1
			if at > 0 && parts[at] == "no-resolve" {
				at--
			}
			if target, ok := aliases[parts[at]]; ok {
				parts[at] = target
				rules[i] = strings.Join(parts, ",")
			}
		}
	}
	return yaml.Marshal(root)
}
