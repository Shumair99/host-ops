package storage

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/discovery"
	"time"
)

func (s *Store) Inventory(ctx context.Context, host string) (discovery.Registry, error) {
	var registry discovery.Registry
	data, err := s.State(ctx, host, "", "inventory")
	if err != nil {
		return registry, err
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &registry)
	}
	return registry, err
}

func (s *Store) ReconcileInventory(ctx context.Context, snapshot discovery.Snapshot) (discovery.Registry, error) {
	unlock := s.LockState(snapshot.HostID + ":inventory")
	defer unlock()
	previous, err := s.Inventory(ctx, snapshot.HostID)
	if err != nil {
		return previous, err
	}
	registry := discovery.Reconcile(snapshot, previous)
	var event any
	if inventoryFingerprint(previous) != inventoryFingerprint(registry) {
		event = registry
	}
	err = s.Transition(ctx, snapshot.HostID, "", "inventory", registry, "inventory", snapshot.ObservedAt, event)
	return registry, err
}

// Sample timestamps express freshness, not a topology change.
func inventoryFingerprint(registry discovery.Registry) string {
	data, _ := json.Marshal(registry)
	var copy discovery.Registry
	json.Unmarshal(data, &copy)
	copy.Snapshot.ObservedAt = time.Time{}
	for i := range copy.Snapshot.Sources {
		copy.Snapshot.Sources[i].ObservedAt = time.Time{}
	}
	for i := range copy.Sites {
		copy.Sites[i].LastSeen = time.Time{}
	}
	data, _ = json.Marshal(copy)
	return string(data)
}
