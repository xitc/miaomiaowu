package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"miaomiaowu/internal/storage"
)

type subscriptionRequestStartKey struct{}

// Bound automatic refresh work across requests, rather than per HTTP request.
var referencedSyncFlights = struct {
	sync.Mutex
	running   map[string]*externalSyncFlight
	completed map[string]*externalSyncFlight
	slots     chan struct{}
}{running: make(map[string]*externalSyncFlight), completed: make(map[string]*externalSyncFlight), slots: make(chan struct{}, 3)}

func syncReferencedSource(ctx context.Context, client *http.Client, repo *storage.TrafficRepository, dir, username string, sub storage.ExternalSubscription, settings storage.UserSettings) (int, storage.ExternalSubscription, error) {
	if err := ctx.Err(); err != nil {
		return 0, sub, err
	}
	started, ok := ctx.Value(subscriptionRequestStartKey{}).(time.Time)
	if !ok {
		started = time.Now()
	}
	inputs, err := json.Marshal([]any{dir, username, sub.ID, sub.Name, sub.URL, sub.UserAgent, sub.Upload, sub.Download, sub.Total, sub.Expire, sub.TrafficMode, settings})
	if err != nil {
		return 0, sub, err
	}
	key := fmt.Sprintf("%p:%x", repo, sha256.Sum256(inputs))
	referencedSyncFlights.Lock()
	flight := referencedSyncFlights.running[key]
	if flight == nil {
		// A request that began before this refresh completed may arrive here late
		// after rendering. Share that result, but never skip a later cache=0 request.
		if recent := referencedSyncFlights.completed[key]; recent != nil && recent.sub.LastSyncAt != nil && recent.sub.LastSyncAt.After(started) {
			referencedSyncFlights.Unlock()
			return recent.nodeCount, recent.sub, nil
		}
		flight = &externalSyncFlight{done: make(chan struct{})}
		referencedSyncFlights.running[key] = flight
		// One disconnected client must not cancel work needed by other clients.
		workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		go func() {
			defer cancel()
			defer func() {
				if recover() != nil {
					flight.err = fmt.Errorf("external subscription refresh failed")
				}
				referencedSyncFlights.Lock()
				if flight.err == nil {
					if len(referencedSyncFlights.completed) >= 128 {
						clear(referencedSyncFlights.completed)
					}
					referencedSyncFlights.completed[key] = flight
				}
				delete(referencedSyncFlights.running, key)
				close(flight.done)
				referencedSyncFlights.Unlock()
			}()
			select {
			case referencedSyncFlights.slots <- struct{}{}:
			case <-workCtx.Done():
				flight.err = workCtx.Err()
				return
			}
			defer func() { <-referencedSyncFlights.slots }()
			flight.nodeCount, flight.sub, flight.err = syncSingleExternalSubscription(workCtx, client, repo, dir, username, sub, settings)
			if flight.err != nil {
				return
			}
			now := time.Now()
			flight.sub.LastSyncAt = &now
			flight.sub.NodeCount = flight.nodeCount
			// Commit once, before releasing the shared result to all waiting requests.
			flight.err = repo.UpdateExternalSubscription(workCtx, flight.sub)
		}()
	}
	referencedSyncFlights.Unlock()
	select {
	case <-ctx.Done():
		return 0, sub, ctx.Err()
	case <-flight.done:
		return flight.nodeCount, flight.sub, flight.err
	}
}
