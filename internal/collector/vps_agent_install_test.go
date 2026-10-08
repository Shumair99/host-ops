package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVPSAgentUnitSubstitutesValidatedHostID(t *testing.T) {
	unit, hash, err := vpsAgentUnit("host-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unit, "@HOST_ID@") || !strings.Contains(unit, "--host-id host-1") || len(hash) != 64 {
		t.Fatalf("service unit was not generated as expected: %q %q", unit, hash)
	}
	for _, invalid := range []string{"host\nExecStart=/bin/sh", "", "-bad"} {
		if _, _, err := vpsAgentUnit(invalid); err == nil {
			t.Fatalf("invalid host ID %q was accepted", invalid)
		}
	}
}

func TestVPSAgentUnitKeepsPrivilegeAndNetworkBoundaries(t *testing.T) {
	unit, _, err := vpsAgentUnit("host-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"User=hostops", "Group=hostops-readers", "NoNewPrivileges=yes",
		"ProtectSystem=strict", "ProtectHome=yes", "RestrictAddressFamilies=AF_UNIX",
		"CapabilityBoundingSet=", "AmbientCapabilities=",
		"ReadWritePaths=/run/hostops /var/lib/hostops",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("service unit lacks security boundary %q", required)
		}
	}
	if strings.Contains(unit, "0.0.0.0:") || strings.Contains(unit, "127.0.0.1:") || strings.Contains(unit, "CAP_") {
		t.Fatal("service unit exposes a network listener or Linux capability")
	}
}

func TestVPSAgentBinaryMustMatchTargetArchitecture(t *testing.T) {
	for _, tc := range []struct {
		arch    string
		machine byte
		wantErr bool
	}{{"amd64", 62, false}, {"arm64", 183, false}, {"arm64", 62, true}} {
		path := filepath.Join(t.TempDir(), "agent")
		data := make([]byte, 100_001)
		copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1})
		data[18] = tc.machine
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := vpsAgentBinary(tc.arch, path)
		if (err != nil) != tc.wantErr {
			t.Fatalf("architecture %s machine %d error = %v", tc.arch, tc.machine, err)
		}
	}
}
