package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBillingSavePreservesLatestUnrelatedSettingsAndRejectsInvalidEdits(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	c := Config{Host: Host{ID: "host", SSH: "operator@example.invalid", IdentityFile: "private-key", SSHPort: 2222, DiskRoots: []string{"/srv"}, HistoryMode: "session"}}
	data, _ := json.Marshal(c)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	got, err := SaveBilling(file, "host", &Billing{Provider: "manual", MonthlyAmount: &zero}, "GBP")
	if err != nil || got.Host.Billing == nil || *got.Host.Billing.MonthlyAmount != 0 || got.Host.SSH != c.Host.SSH || got.Host.SSHPort != 2222 || got.Host.IdentityFile != c.Host.IdentityFile || !reflect.DeepEqual(got.Host.DiskRoots, c.Host.DiskRoots) {
		t.Fatal("billing edit changed connection settings or lost explicit zero", err, got)
	}
	before, _ := os.ReadFile(file)
	for _, edit := range []struct {
		host string
		b    *Billing
	}{{"another-host", &Billing{Provider: "self-hosted"}}, {"host", &Billing{Provider: "hetzner", ResourceID: "../invalid"}}} {
		if _, err := SaveBilling(file, edit.host, edit.b, "GBP"); err == nil {
			t.Fatal("invalid edit was accepted")
		}
		after, _ := os.ReadFile(file)
		if string(before) != string(after) {
			t.Fatal("rejected edit changed settings")
		}
	}
}
