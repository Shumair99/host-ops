package storage

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/discovery"
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
	err = s.Transition(ctx, snapshot.HostID, "", "inventory", registry, "inventory", snapshot.ObservedAt, registry)
	return registry, err
}
