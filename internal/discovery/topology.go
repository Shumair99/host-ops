package discovery

import (
	"net"
	"sort"
	"strconv"
)

type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	State string `json:"state,omitempty"`
}
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Kind     string `json:"kind"`
	Evidence string `json:"evidence"`
}
type Topology struct {
	Nodes []Node   `json:"nodes"`
	Edges []Edge   `json:"edges"`
	Gaps  []string `json:"gaps"`
}

func Graph(reg Registry) Topology {
	g := Topology{Nodes: []Node{}, Edges: []Edge{}, Gaps: []string{}}
	nodes := map[string]bool{}
	edges := map[string]bool{}
	addNode := func(n Node) {
		if n.ID != "" && !nodes[n.ID] {
			nodes[n.ID] = true
			g.Nodes = append(g.Nodes, n)
		}
	}
	addEdge := func(e Edge) {
		key := e.From + "|" + e.To + "|" + e.Kind
		if e.From != "" && e.To != "" && !edges[key] {
			edges[key] = true
			g.Edges = append(g.Edges, e)
		}
	}
	host := "host:" + reg.Snapshot.HostID
	addNode(Node{ID: host, Label: reg.Snapshot.HostID, Kind: "host"})
	sources := map[string]bool{}
	for _, s := range reg.Snapshot.Sources {
		sources[s.ID] = s.State == "ready"
	}
	for _, w := range reg.Snapshot.Workloads {
		state := w.Status
		if !sources[w.SourceID] {
			state = "retained / unverified"
		}
		addNode(Node{ID: w.ID, Label: w.Name, Kind: w.Type, State: state})
		addEdge(Edge{From: w.ID, To: host, Kind: "host", Evidence: "Runtime inventory"})
		for n := range w.Networks {
			id := "network:" + n
			addNode(Node{ID: id, Label: n, Kind: "network"})
			addEdge(Edge{From: w.ID, To: id, Kind: "network", Evidence: "Observed network attachment"})
		}
		for _, m := range w.Mounts {
			id := "storage:" + m.Source
			addNode(Node{ID: id, Label: m.Source, Kind: "storage"})
			addEdge(Edge{From: w.ID, To: id, Kind: "storage", Evidence: "Mounted at " + m.Destination})
		}
	}
	for _, c := range reg.Snapshot.Connections {
		if c.Kind == "peer" && nodes[c.From] {
			label := net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
			id := "endpoint:" + label
			addNode(Node{ID: id, Label: label, Kind: "endpoint"})
			addEdge(Edge{From: c.From, To: id, Kind: "peer", Evidence: "Observed outbound TCP peer; service/ownership not identified"})
			continue
		}
		if !nodes[c.From] || !nodes[c.To] {
			continue
		}
		evidence := "Compose startup dependency"
		if c.Kind == "established" {
			evidence = "Established TCP connection to listening port " + strconv.Itoa(c.Port)
		}
		addEdge(Edge{From: c.From, To: c.To, Kind: c.Kind, Evidence: evidence})
	}
	for _, site := range reg.Sites {
		if site.State == "removed" {
			continue
		}
		addNode(Node{ID: site.ID, Label: site.Domain, Kind: "domain", State: site.State})
		for _, r := range site.Routes {
			addEdge(Edge{From: site.ID, To: r.ProxyID, Kind: "proxy", Evidence: "Observed proxy route"})
			if r.Kind == "redirect" {
				for _, target := range reg.Sites {
					if redirectHost(r.RedirectTo) == target.Domain && target.ID != site.ID {
						addEdge(Edge{From: site.ID, To: target.ID, Kind: "redirect", Evidence: "Configured redirect"})
					}
				}
			}
		}
		for _, id := range site.WorkloadIDs {
			addEdge(Edge{From: site.ID, To: id, Kind: "backend", Evidence: "Resolved route upstream"})
		}
		if site.State == "unresolved" {
			g.Gaps = append(g.Gaps, site.Domain+": backend resolution incomplete")
		}
	}
	for _, s := range reg.Snapshot.Sources {
		if s.State == "unavailable" {
			g.Gaps = append(g.Gaps, s.Kind+": "+s.Issue)
		}
	}
	if reg.Snapshot.ConnectionIssue != "" {
		g.Gaps = append(g.Gaps, reg.Snapshot.ConnectionIssue)
	}
	g.Gaps = append(g.Gaps, "Configured startup dependencies and current local TCP connections are observed; idle, external and dynamically generated dependencies cannot be proven complete")
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].Label < g.Nodes[j].Label })
	sort.Slice(g.Edges, func(i, j int) bool {
		return g.Edges[i].From+g.Edges[i].To+g.Edges[i].Kind < g.Edges[j].From+g.Edges[j].To+g.Edges[j].Kind
	})
	return g
}

// Site dependencies start with its actual upstreams. Proxy connections to
// other websites must not make every website depend on every other website.
func WorkloadDependencies(snapshot Snapshot, starts []string) []string {
	seen := map[string]bool{}
	queue := append([]string(nil), starts...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, c := range snapshot.Connections {
			if c.From == id && c.To != "" {
				queue = append(queue, c.To)
			}
		}
	}
	out := []string{}
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
