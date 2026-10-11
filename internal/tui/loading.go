package tui

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/agent"
)

func (m dashboard) connectionCmd() tea.Cmd {
	return m.jobs.command(func() tea.Msg {
		data, err := m.source.Store.State(m.ctx, m.source.Config.Host.ID, "", "connection")
		var connection agent.Connection
		if err == nil && len(data) > 0 {
			err = json.Unmarshal(data, &connection)
		}
		return connectionMsg{connection: connection, err: err}
	})
}

// Propagation belongs only to startup. Once the first load settles, refreshes,
// navigation and later reconnects must never restart the header animation.
func (m dashboard) propagating() bool {
	return !m.startupComplete
}

func (m *dashboard) finishInitialLoad() {
	if m.startupComplete || m.snapshot.LoadedAt.IsZero() {
		return
	}
	if m.source.Live {
		connection := m.snapshot.Connection
		// Retained status from an earlier run cannot complete this startup.
		if connection.ObservedAt.IsZero() || connection.ObservedAt.Before(m.source.LiveStartedAt) || len(connection.Pending) > 0 {
			return
		}
		if !connection.Connected && connection.Issue == "" {
			return
		}
	} else if m.collecting {
		return
	}
	// Missing evidence is a completed result too, not perpetual loading.
	m.startupComplete = true
}
