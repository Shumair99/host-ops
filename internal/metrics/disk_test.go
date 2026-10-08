package metrics

import (
	"context"
	"fmt"
	"github.com/Shumair99/host-ops/internal/config"
	"testing"
)

type partialDiskRunner struct{}

func (partialDiskRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name == "timeout" {
		return []byte("1024\t/srv/apps/site\x002048\t/srv/apps\x00"), fmt.Errorf("partial")
	}
	return []byte(`{"Type":"Build Cache","Size":"6GB","Reclaimable":"4GB"}`), nil
}
func TestPartialDirectoryAccountingIsPreserved(t *testing.T) {
	r := Disk(context.Background(), partialDiskRunner{}, config.Host{ID: "h", DiskRoots: []string{"/srv/apps"}})
	if len(r.Directories) != 2 || !r.Directories[0].Partial || len(r.Unavailable) != 1 || len(r.Docker) != 1 {
		t.Fatal(r)
	}
}
