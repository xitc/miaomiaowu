package handler

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"miaomiaowu/internal/storage"
)

// mergeExternalSyncTags keeps node metadata owned by the user or by derived
// Providers and also restores the source-subscription tag from the incoming
// node. External sync updates transport fields; it must not replace labels.
func mergeExternalSyncTags(existing, incoming []string) []string {
	merged := make([]string, 0, len(existing)+len(incoming))
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, tags := range [][]string{existing, incoming} {
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			merged = append(merged, tag)
		}
	}
	return merged
}

// Singleflight for external subscription sync: key = username + "\x00" + subID.
var (
	externalSyncFlightMu sync.Mutex
	externalSyncFlights  = map[string]*externalSyncFlight{}
)

type externalSyncFlight struct {
	done      chan struct{}
	nodeCount int
	sub       storage.ExternalSubscription
	err       error
}

func externalSyncFlightKey(username string, subID int64) string {
	return username + "\x00" + fmt.Sprintf("%d", subID)
}

// doExternalSyncSingleflight runs fn once per key; concurrent callers wait and share the result.
func doExternalSyncSingleflight(key string, fn func() (int, storage.ExternalSubscription, error)) (int, storage.ExternalSubscription, error) {
	externalSyncFlightMu.Lock()
	if f, ok := externalSyncFlights[key]; ok {
		externalSyncFlightMu.Unlock()
		<-f.done
		return f.nodeCount, f.sub, f.err
	}
	f := &externalSyncFlight{done: make(chan struct{})}
	externalSyncFlights[key] = f
	externalSyncFlightMu.Unlock()

	nodeCount, sub, err := fn()
	f.nodeCount, f.sub, f.err = nodeCount, sub, err
	close(f.done)

	externalSyncFlightMu.Lock()
	delete(externalSyncFlights, key)
	externalSyncFlightMu.Unlock()
	return nodeCount, sub, err
}

// nodeMatchIndex indexes only nodes from one source URL for O(1) matching.
type nodeMatchIndex struct {
	byName                  map[string]int
	byServerPort            map[string]int
	byTypeServerPort        map[string]int
	ambiguousServerPort     map[string]bool
	ambiguousTypeServerPort map[string]bool
}

func portKey(port any) string {
	return strings.TrimSpace(fmt.Sprint(port))
}

func buildSourceNodeMatchIndex(existing []storage.Node, sourceURL string) *nodeMatchIndex {
	idx := &nodeMatchIndex{
		byName:                  make(map[string]int, len(existing)),
		byServerPort:            make(map[string]int, len(existing)),
		byTypeServerPort:        make(map[string]int, len(existing)),
		ambiguousServerPort:     make(map[string]bool),
		ambiguousTypeServerPort: make(map[string]bool),
	}
	for i := range existing {
		if existing[i].RawURL != sourceURL {
			continue
		}
		var cfg map[string]any
		_ = json.Unmarshal([]byte(existing[i].ClashConfig), &cfg)
		if existing[i].NodeName != "" {
			// First wins; stable for duplicates within source
			if _, ok := idx.byName[existing[i].NodeName]; !ok {
				idx.byName[existing[i].NodeName] = i
			}
		}
		if cfg == nil {
			continue
		}
		server, _ := cfg["server"].(string)
		if existing[i].OriginalServer != "" {
			server = existing[i].OriginalServer
		}
		port := portKey(cfg["port"])
		typeName, _ := cfg["type"].(string)
		if server != "" && port != "" && port != "<nil>" {
			sp := strings.ToLower(server) + "\x00" + port
			if previous, ok := idx.byServerPort[sp]; ok && previous != i {
				idx.ambiguousServerPort[sp] = true
			} else if !ok {
				idx.byServerPort[sp] = i
			}
			if typeName != "" {
				tsp := strings.ToLower(typeName) + "\x00" + sp
				if previous, ok := idx.byTypeServerPort[tsp]; ok && previous != i {
					idx.ambiguousTypeServerPort[tsp] = true
				} else if !ok {
					idx.byTypeServerPort[tsp] = i
				}
			}
		}
	}
	return idx
}

// findUniqueEndpoint is a conservative fallback for source nodes whose imported
// names were suffixed to avoid a collision with another subscription. It never
// guesses when multiple nodes in the same source share the endpoint.
func (idx *nodeMatchIndex) findUniqueEndpoint(newConfig map[string]any) int {
	newServer, _ := newConfig["server"].(string)
	newPort := portKey(newConfig["port"])
	newType, _ := newConfig["type"].(string)
	if newServer == "" || newPort == "" || newPort == "<nil>" {
		return -1
	}
	sp := strings.ToLower(newServer) + "\x00" + newPort
	if newType != "" {
		tsp := strings.ToLower(newType) + "\x00" + sp
		if !idx.ambiguousTypeServerPort[tsp] {
			if i, ok := idx.byTypeServerPort[tsp]; ok {
				return i
			}
		}
	}
	if !idx.ambiguousServerPort[sp] {
		if i, ok := idx.byServerPort[sp]; ok {
			return i
		}
	}
	return -1
}

func nodeNameTakenOutsideSource(nodes []storage.Node, sourceURL, nodeName string) bool {
	for _, node := range nodes {
		if node.RawURL != sourceURL && node.NodeName == nodeName {
			return true
		}
	}
	return false
}

func shouldCleanupExternalSyncOrphans(syncScope string, deferNewNodes bool) bool {
	return syncScope == "all" && !deferNewNodes
}

func (idx *nodeMatchIndex) find(matchRule string, nodeName string, newCfg map[string]any) int {
	if idx == nil {
		return -1
	}
	newServer, _ := newCfg["server"].(string)
	newPort := portKey(newCfg["port"])
	newType, _ := newCfg["type"].(string)

	switch matchRule {
	case "type_server_port":
		if newServer != "" && newPort != "" && newPort != "<nil>" && newType != "" {
			key := strings.ToLower(newType) + "\x00" + strings.ToLower(newServer) + "\x00" + newPort
			if i, ok := idx.byTypeServerPort[key]; ok {
				return i
			}
		}
		return -1
	case "server_port":
		if newServer != "" && newPort != "" && newPort != "<nil>" {
			key := strings.ToLower(newServer) + "\x00" + newPort
			if i, ok := idx.byServerPort[key]; ok {
				return i
			}
		}
		return -1
	default:
		if i, ok := idx.byName[nodeName]; ok {
			return i
		}
		return -1
	}
}

// nodeSyncPayloadEqual reports whether an existing DB node already matches the
// incoming external node content (so UpdateNode can be skipped).
func nodeSyncPayloadEqual(existing storage.Node, incoming storage.Node, keepNodeName bool) bool {
	if existing.RawURL != incoming.RawURL {
		return false
	}
	if existing.Protocol != incoming.Protocol {
		return false
	}
	if existing.Enabled != incoming.Enabled {
		return false
	}
	wantName := incoming.NodeName
	if keepNodeName {
		wantName = existing.NodeName
	}
	if existing.NodeName != wantName {
		return false
	}
	if existing.Tag != incoming.Tag || !slices.Equal(existing.Tags, incoming.Tags) {
		return false
	}

	if !syncJSONEqual(existing.ClashConfig, incoming.ClashConfig, wantName) {
		return false
	}
	if !syncJSONEqual(existing.ParsedConfig, incoming.ParsedConfig, wantName) {
		return false
	}
	return true
}

func syncJSONEqual(existingRaw, incomingRaw, name string) bool {
	var existingCfg, incomingCfg map[string]any
	if err := json.Unmarshal([]byte(existingRaw), &existingCfg); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(incomingRaw), &incomingCfg); err != nil {
		return false
	}
	existingCfg = copyMapForSync(existingCfg)
	incomingCfg = copyMapForSync(incomingCfg)
	existingCfg["name"] = name
	incomingCfg["name"] = name
	eb, err1 := json.Marshal(existingCfg)
	ib, err2 := json.Marshal(incomingCfg)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(eb) == string(ib)
}
