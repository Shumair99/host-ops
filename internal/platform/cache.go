package platform

import (
	"context"
	"strings"
	"sync"
)

// Cache deduplicates read-only observations within one collection cycle only.
type Cache struct {
	Runner  Runner
	mu      sync.Mutex
	entries map[string]*entry
}
type entry struct {
	done chan struct{}
	data []byte
	err  error
}

func (c *Cache) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	key := name + "\x00" + strings.Join(args, "\x00")
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*entry{}
	}
	if found, ok := c.entries[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-found.done:
			return found.data, found.err
		}
	}
	current := &entry{done: make(chan struct{})}
	c.entries[key] = current
	c.mu.Unlock()
	current.data, current.err = c.Runner.Run(ctx, name, args...)
	close(current.done)
	return current.data, current.err
}
