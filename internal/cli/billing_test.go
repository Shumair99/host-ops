package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
)

func TestBillingSetPreservesConnectionAndAcceptsExplicitZeroBudget(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hostops.json")
	c := config.Config{Host: config.Host{ID: "h", SSH: "ops@server", IdentityFile: "private-key-reference", SSHPort: 2222}}
	data, _ := json.Marshal(c)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"billing", "set", "--provider", "manual", "--monthly", "0", "--currency", "GBP", "--config", file}, &out, &errOut)
	if code != 0 {
		t.Fatal(code, errOut.String())
	}
	got, err := config.Load(file)
	if err != nil || got.Host.SSH != c.Host.SSH || got.Host.IdentityFile != c.Host.IdentityFile || got.Host.SSHPort != 2222 || got.Host.Billing == nil || got.Host.Billing.MonthlyAmount == nil || *got.Host.Billing.MonthlyAmount != 0 {
		t.Fatal("billing setup lost connection settings or explicit zero", err, got)
	}
	before, _ := os.ReadFile(file)
	code = Run(context.Background(), []string{"billing", "set", "--provider", "hetzner", "--resource", "../../bad", "--config", file}, &out, &errOut)
	after, _ := os.ReadFile(file)
	if code == 0 || !bytes.Equal(before, after) {
		t.Fatal("invalid billing configuration changed the manifest")
	}
}
