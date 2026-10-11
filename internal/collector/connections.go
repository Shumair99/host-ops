package collector

import (
	"encoding/hex"
	"fmt"
	"github.com/Shumair99/host-ops/internal/discovery"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type tcpEntry struct {
	localIP, remoteIP     string
	localPort, remotePort int
	state                 string
}

const maxSocketTableBytes = 2 << 20
const maxObservedConnections = 512

func readSocketTable(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSocketTableBytes+1))
	if len(data) > maxSocketTableBytes {
		// Discard the incomplete last line; publish a coverage gap below.
		data = data[:maxSocketTableBytes]
		if end := strings.LastIndexByte(string(data), '\n'); end >= 0 {
			data = data[:end]
		}
		return data, false, err
	}
	return data, true, err
}

func tcpAddress(raw string) (string, int, error) {
	fields := strings.Split(raw, ":")
	if len(fields) != 2 {
		return "", 0, fmt.Errorf("invalid TCP endpoint")
	}
	data, err := hex.DecodeString(fields[0])
	if err != nil || len(data) != 4 && len(data) != 16 {
		return "", 0, fmt.Errorf("invalid IP")
	}
	for i := 0; i < len(data); i += 4 {
		data[i], data[i+3] = data[i+3], data[i]
		data[i+1], data[i+2] = data[i+2], data[i+1]
	}
	port, err := strconv.ParseInt(fields[1], 16, 32)
	return net.IP(data).String(), int(port), err
}

func parseTCP(data string) []tcpEntry {
	result := []tcpEntry{}
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		lip, lp, e1 := tcpAddress(f[1])
		rip, rp, e2 := tcpAddress(f[2])
		if e1 != nil || e2 != nil {
			continue
		}
		result = append(result, tcpEntry{lip, rip, lp, rp, f[3]})
	}
	return result
}

// A connection becomes a local dependency only when its remote address names
// one workload on a shared network and that workload has the port listening.
// Network co-membership alone is never treated as a service dependency.
func observeConnections(proc string, snapshot *discovery.Snapshot) {
	tables := map[string][]tcpEntry{}
	issues := []string{}
	for _, w := range snapshot.Workloads {
		if w.Status != "running" || w.PID == 0 || w.Type != "docker" && w.Type != "podman" {
			continue
		}
		if w.NetworkMode == "host" || strings.HasPrefix(w.NetworkMode, "container:") {
			issues = append(issues, w.Name+": shared namespace requires socket ownership")
			continue
		}
		complete := true
		for _, file := range []string{"tcp", "tcp6"} {
			data, bounded, err := readSocketTable(filepath.Join(proc, strconv.Itoa(w.PID), "net", file))
			if err != nil {
				complete = false
				continue
			}
			if !bounded {
				issues = append(issues, w.Name+": connection table limit reached")
			}
			tables[w.ID] = append(tables[w.ID], parseTCP(string(data))...)
		}
		if !complete {
			issues = append(issues, w.Name+": connection table unavailable")
		}
	}
	seen := map[string]bool{}
	limited := false
	for _, from := range snapshot.Workloads {
		for _, socket := range tables[from.ID] {
			if len(snapshot.Connections) >= maxObservedConnections {
				limited = true
				break
			}
			if socket.state != "01" || socket.remotePort == 0 {
				continue
			}
			incoming := false
			for _, listener := range tables[from.ID] {
				if listener.state == "0A" && listener.localPort == socket.localPort {
					incoming = true
				}
			}
			if incoming {
				continue
			}
			candidates := []string{}
			for _, to := range snapshot.Workloads {
				if to.ID == from.ID {
					continue
				}
				shared := false
				for network, details := range to.Networks {
					if _, ok := from.Networks[network]; ok && details.IP == socket.remoteIP {
						shared = true
					}
				}
				if !shared {
					continue
				}
				for _, listener := range tables[to.ID] {
					if listener.state == "0A" && listener.localPort == socket.remotePort {
						candidates = append(candidates, to.ID)
						break
					}
				}
			}
			if len(candidates) != 1 {
				// Retain an unmapped outbound peer as an endpoint, without
				// guessing its service, owner or role from its port number.
				key := from.ID + "|peer|" + socket.remoteIP + "|" + strconv.Itoa(socket.remotePort)
				if !seen[key] {
					seen[key] = true
					snapshot.Connections = append(snapshot.Connections, discovery.Connection{From: from.ID, Address: socket.remoteIP, Port: socket.remotePort, Kind: "peer"})
				}
				continue
			}
			key := from.ID + "|" + candidates[0] + "|" + strconv.Itoa(socket.remotePort)
			if seen[key] {
				continue
			}
			seen[key] = true
			snapshot.Connections = append(snapshot.Connections, discovery.Connection{From: from.ID, To: candidates[0], Address: socket.remoteIP, Port: socket.remotePort, Kind: "established"})
		}
	}
	if limited {
		issues = append(issues, "Connection observation limit reached")
	}
	sort.Strings(issues)
	snapshot.ConnectionIssue = strings.Join(issues, "; ")
}
