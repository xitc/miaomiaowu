package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"miaomiaowu/internal/storage"
)

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
	byName           map[string]int
	byServerPort     map[string]int
	byTypeServerPort map[string]int
}

func portKey(port any) string {
	return strings.TrimSpace(fmt.Sprint(port))
}

func buildSourceNodeMatchIndex(existing []storage.Node, sourceURL string) *nodeMatchIndex {
	idx := &nodeMatchIndex{
		byName:           make(map[string]int, len(existing)),
		byServerPort:     make(map[string]int, len(existing)),
		byTypeServerPort: make(map[string]int, len(existing)),
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
			if _, ok := idx.byServerPort[sp]; !ok {
				idx.byServerPort[sp] = i
			}
			if typeName != "" {
				tsp := strings.ToLower(typeName) + "\x00" + sp
				if _, ok := idx.byTypeServerPort[tsp]; !ok {
					idx.byTypeServerPort[tsp] = i
				}
			}
		}
	}
	return idx
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
