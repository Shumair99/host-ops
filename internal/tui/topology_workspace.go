package tui

import (
	"fmt"
	"github.com/Shumair99/host-ops/internal/preflight"
	"sort"
	"strings"
)

func (m dashboard) topologyPanel(v preflight.Impact, w int) []string {
	t := m.theme
	graph := v.Graph
	labels := map[string]string{}
	for _, n := range graph.Nodes {
		labels[n.ID] = n.Label
	}
	rows := []string{t.strong(fmt.Sprintf("%d affected properties", len(groupSites(v.Affected))), amber)}
	for _, group := range groupSites(v.Affected) {
		for _, s := range group.domains() {
			backend := s.Runtime.Container
			if backend == "" {
				backend = s.Runtime.Unit
			}
			if s.Proxy.RedirectTo != "" {
				backend = "redirect → " + s.Proxy.RedirectTo
			}
			if backend == "" {
				backend = s.Runtime.Type
			}
			rows = append(rows, wrap([]string{siteLabel(s) + " → " + clean(backend)}, w-4)...)
		}
	}
	out := t.panel("Impact / "+clean(v.Resource), rows, w)
	rows = []string{}
	for _, n := range graph.Nodes {
		if n.Kind == "domain" || n.Kind == "host" || n.Kind == "network" || n.Kind == "storage" || n.Kind == "endpoint" {
			continue
		}
		rows = append(rows, t.strong(clean(n.Label), cyan)+"  "+t.paint(clean(n.State), muted))
		networks := []string{}
		for _, e := range graph.Edges {
			if e.From != n.ID {
				continue
			}
			if e.Kind == "network" {
				networks = append(networks, strings.TrimPrefix(strings.TrimPrefix(labels[e.To], "docker:"), "podman:"))
				continue
			}
			if e.Kind == "host" {
				continue
			}
			rows = append(rows, wrap([]string{"  → " + clean(labels[e.To]) + "  " + t.paint(clean(e.Evidence), muted)}, w-4)...)
		}
		if len(networks) > 0 {
			sort.Strings(networks)
			rows = append(rows, wrap([]string{t.paint("  Networks: "+strings.Join(networks, ", "), muted)}, w-4)...)
		}
	}
	out = append(out, t.panel("Runtime / databases / storage", rows, w)...)
	gaps := []string{}
	for _, gap := range graph.Gaps {
		gaps = append(gaps, wrap([]string{t.paint(clean(gap), muted)}, w-4)...)
	}
	return append(out, t.panel("Observation coverage", gaps, w)...)
}
