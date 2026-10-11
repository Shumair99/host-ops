package agent

import (
	"context"
	"sort"

	"github.com/Shumair99/host-ops/internal/storage"
)

// Track operations, not their outcomes. An empty or unavailable result still
// finishes its operation; missing evidence is not perpetual loading.
type streamProgress struct {
	ctx       context.Context
	store     *storage.Store
	host      string
	connected bool
	pending   map[string]bool
}

func newStreamProgress(ctx context.Context, store *storage.Store, host string) *streamProgress {
	return &streamProgress{ctx: ctx, store: store, host: host, pending: map[string]bool{"inventory": true, "host_metrics": true, "workload_metrics": true}}
}

func (p *streamProgress) ready() error {
	p.connected = true
	return p.publish()
}

func (p *streamProgress) set(stage string, pending bool) error {
	if p.pending[stage] == pending {
		return nil
	}
	p.pending[stage] = pending
	if !p.connected {
		return nil
	}
	return p.publish()
}

func (p *streamProgress) publish() error {
	pending := []string{}
	for stage, busy := range p.pending {
		if busy {
			pending = append(pending, stage)
		}
	}
	sort.Strings(pending)
	return connection(p.ctx, p.store, p.host, p.connected, "", pending...)
}
