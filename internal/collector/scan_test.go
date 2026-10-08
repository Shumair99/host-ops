package collector

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"testing"
)

func TestRelevantServiceInventoryKeepsListenersProxiesAndExplicitUnits(t *testing.T) {
	s := discovery.Snapshot{Workloads: []discovery.Workload{{ID: "docker", Type: "docker"}, {ID: "listener", Type: "systemd", Name: "web.service", PID: 10}, {ID: "proxy", Type: "process", ProxyKind: "nginx"}, {ID: "backup", Type: "systemd", Name: "backup.service"}, {ID: "noise", Type: "systemd", Name: "apt-daily.service"}, {ID: "tmux", Type: "process", PID: 99}}, Listeners: []discovery.Listener{{Address: "127.0.0.1:8080", PID: 10, Unit: "web.service"}, {Address: "/tmp/tmux-1000/default", PID: 99}}}
	all := s
	filterWorkloads(&s, config.Host{ServiceUnits: []string{"backup.service"}})
	if len(s.Workloads) != 4 {
		t.Fatal("noise retained or relevant workload removed", s.Workloads)
	}
	filterWorkloads(&all, config.Host{AllServices: true})
	if len(all.Workloads) != 6 {
		t.Fatal("all-services did not preserve full inventory")
	}
	known := rememberServices(config.Host{}, s)
	stopped := discovery.Snapshot{Workloads: []discovery.Workload{{ID: "listener", Type: "systemd", Name: "web.service", Status: "inactive"}, {ID: "noise", Type: "systemd", Name: "apt-daily.service", Status: "inactive"}}}
	filterWorkloads(&stopped, known)
	if len(stopped.Workloads) != 1 || stopped.Workloads[0].ID != "listener" {
		t.Fatal("stopped known service lost with its listener", stopped.Workloads)
	}
}
