package security

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
)

func TestLoopbackAndPublicDatabasePublication(t *testing.T) {
	site := config.Site{ID: "s", Runtime: config.Runtime{Container: "db"}}
	runtime := model.Runtime{Ports: map[string][]model.Port{"5432/tcp": {{HostIP: "127.0.0.1", HostPort: "5432"}, {HostIP: "0.0.0.0", HostPort: "5432"}}}}
	exposure := Published(site, runtime, "caddy")
	if len(exposure) != 2 || exposure[0].State != model.Fail || exposure[1].State != model.Pass {
		t.Fatal(exposure)
	}
}
