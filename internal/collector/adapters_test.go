package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeExecutable(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

type adapterRunner struct {
	run func(string, []string) ([]byte, error)
}

func (r adapterRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	return r.run(name, args)
}
func TestRuntimeInventoryBatchesAndPreservesPartialFailure(t *testing.T) {
	fakeExecutable(t, "docker")
	for _, partial := range []bool{false, true} {
		inspected := 0
		s := discovery.Snapshot{HostID: "h", ObservedAt: time.Now()}
		runner := adapterRunner{run: func(_ string, args []string) ([]byte, error) {
			if args[0] == "ps" {
				ids := []string{}
				for i := 0; i < 40; i++ {
					ids = append(ids, fmt.Sprintf("id%d", i))
				}
				return []byte(strings.Join(ids, "\n")), nil
			}
			inspected++
			if partial && inspected == 2 {
				return nil, errors.New("secret-daemon-error")
			}
			var out strings.Builder
			for _, id := range args[5:] {
				meta := containerMeta{ID: id, Name: "/" + id, Engine: "docker"}
				meta.State.Status = "running"
				meta.Config.Image = "nginx:1"
				meta.Config.Labels = map[string]string{"com.docker.compose.project": "tenant"}
				meta.NetworkSettings.Networks = map[string]struct {
					IPAddress string
					Aliases   []string
				}{"web": {IPAddress: "10.0.0.2", Aliases: []string{"app"}}}
				data, _ := json.Marshal(meta)
				out.Write(data)
				out.WriteByte('\n')
			}
			return []byte(out.String()), nil
		}}
		readRuntime(context.Background(), runner, "docker", &s)
		want := 40
		if partial {
			want = 32
		}
		if inspected != 2 || len(s.Workloads) != want || len(s.Sources) != 1 {
			t.Fatal(inspected, len(s.Workloads), s.Sources)
		}
		state := "ready"
		if partial {
			state = "unavailable"
		}
		if s.Sources[0].State != state || strings.Contains(s.Sources[0].Issue, "secret") {
			t.Fatal(s.Sources)
		}
		if s.Workloads[0].Networks["docker:web"].IP != "10.0.0.2" || s.Workloads[0].Name[0] == '/' {
			t.Fatal(s.Workloads[0])
		}
	}
}
func TestSystemdUnavailableJSONIsNotAnEmptyReadyInventory(t *testing.T) {
	fakeExecutable(t, "systemctl")
	for _, tc := range []struct {
		data  string
		fail  bool
		state string
		count int
	}{{`[{"unit":"getty@tty1.service","active":"inactive"}]`, false, "ready", 1}, {"invalid", false, "unavailable", 0}, {"", true, "unavailable", 0}} {
		s := discovery.Snapshot{HostID: "h"}
		readServices(context.Background(), adapterRunner{run: func(string, []string) ([]byte, error) {
			if tc.fail {
				return nil, errors.New("denied")
			}
			return []byte(tc.data), nil
		}}, &s)
		if s.Sources[0].State != tc.state || len(s.Workloads) != tc.count {
			t.Fatal(s)
		}
	}
}
func TestCaddyLoadedAndDeclaredEvidenceStaySeparateWhenActiveReadFails(t *testing.T) {
	instance := proxyInstance{kind: "caddy", binary: "caddy", args: []string{"--config", "/etc/Caddyfile"}, workload: discovery.Workload{ID: "p", SourceID: "runtime"}}
	s := discovery.Snapshot{HostID: "h"}
	runner := adapterRunner{run: func(name string, args []string) ([]byte, error) {
		if name == "curl" {
			return nil, errors.New("API denied")
		}
		return []byte(`{"apps":{"http":{"servers":{"srv":{"routes":[{"match":[{"host":["app.example"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"app:80"}]}]}]}}}}}`), nil
	}}
	readCaddy(context.Background(), runner, config.Host{}, instance, &s)
	if len(s.Sources) != 2 || s.Sources[0].State != "unavailable" || s.Sources[1].State != "ready" || len(s.Routes) != 1 || s.Routes[0].SourceID != s.Sources[1].ID {
		t.Fatal(s)
	}
}
