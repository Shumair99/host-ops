package collector

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/discovery"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupArtifactsAreRecognizedDeduplicatedAndNeverCalledVerified(t *testing.T) {
	root := t.TempDir()
	backup := filepath.Join(root, "data", "backups")
	if err := os.MkdirAll(backup, 0700); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{
		filepath.Join(backup, "copy.db"): append([]byte("SQLite format 3\x00"), make([]byte, 512)...),
		filepath.Join(backup, "fake.db"): []byte("This is not a SQLite backup"),
		filepath.Join(root, "live.db"):   append([]byte("SQLite format 3\x00"), make([]byte, 512)...),
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := discovery.Snapshot{HostID: "h", ObservedAt: time.Now(), Workloads: []discovery.Workload{{ID: "w", Name: "app", Directory: root, Mounts: []discovery.Mount{{Source: filepath.Join(root, "data")}}}}}
	readBackupArtifacts(context.Background(), &snapshot)
	if len(snapshot.BackupArtifacts) != 1 || snapshot.BackupArtifacts[0].Format != "SQLite copy" {
		t.Fatal(snapshot.BackupArtifacts)
	}
}

func TestAutomaticRecoveryReportsProjectOutUnknownSecretFields(t *testing.T) {
	root := t.TempDir()
	data := map[string]any{"version": 1, "kind": "backup", "domains": []string{"example.com"}, "record": map[string]any{"id": "real-job", "status": "succeeded", "destination": "local", "completed_at": time.Now().Add(-time.Minute), "size_bytes": 512, "password": "never-persist-this"}}
	encoded, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(root, "report.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := discovery.Snapshot{HostID: "h", ObservedAt: time.Now()}
	readRecoveryReports(root, &snapshot)
	result, _ := json.Marshal(snapshot)
	if len(snapshot.RecoveryReports) != 1 || strings.Contains(string(result), "never-persist") {
		t.Fatal(string(result))
	}
}

func TestProcessUIDMappingAndNginxRolesAreCollectedWithoutArguments(t *testing.T) {
	root := t.TempDir()
	for pid, cmd := range map[string]string{"10": "nginx: master process nginx -g daemon off; SECRET=hidden", "11": "nginx: worker process"} {
		if err := os.MkdirAll(filepath.Join(root, pid), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "cmdline"), []byte(cmd), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "10", "uid_map"), []byte("0 100000 65536\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := readIdentities(root, 10, "PID PPID EUID COMMAND\n10 1 100000 nginx\n11 10 100101 nginx\n")
	data, _ := json.Marshal(p)
	if !p.Complete || len(p.Items) != 2 || p.Items[0].UID != 0 || p.Items[1].UID != 101 || p.Items[1].Role != "nginx-worker" || strings.Contains(string(data), "hidden") {
		t.Fatal(string(data))
	}
}

func TestDNSAnswersDoNotMistakeResolverAddressForBackend(t *testing.T) {
	answers := dnsAnswers("Server: 127.0.0.11\nAddress: 127.0.0.11:53\nName: app\nAddress: 172.19.0.2\n", "app")
	if len(answers) != 1 || answers[0] != "172.19.0.2" {
		t.Fatal(answers)
	}
}

func TestTCPDecodingPreservesIPv4AndIPv6Ports(t *testing.T) {
	ip, port, err := tcpAddress("020013AC:1538")
	if err != nil || ip != "172.19.0.2" || port != 5432 {
		t.Fatal(ip, port, err)
	}
	ip, port, err = tcpAddress("00000000000000000000000001000000:01BB")
	if err != nil || ip != "::1" || port != 443 {
		t.Fatal(ip, port, err)
	}
}

func TestEstablishedConnectionsRequireAListenerAndExcludeIncomingPeers(t *testing.T) {
	root := t.TempDir()
	for pid, table := range map[string]string{
		"10": "0: 030013AC:1538 00000000:0000 0A\n1: 030013AC:1538 020013AC:C350 01\n",
		"11": "0: 020013AC:C350 030013AC:1538 01\n1: 020013AC:C351 040013AC:1538 01\n",
		"12": "",
	} {
		dir := filepath.Join(root, pid, "net")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tcp"), []byte(table), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tcp6"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := discovery.Snapshot{Workloads: []discovery.Workload{
		{ID: "db", Name: "db", Type: "docker", Status: "running", PID: 10, Networks: map[string]discovery.Network{"n": {IP: "172.19.0.3"}}},
		{ID: "app", Name: "app", Type: "docker", Status: "running", PID: 11, Networks: map[string]discovery.Network{"n": {IP: "172.19.0.2"}}},
		{ID: "unlistening", Name: "unlistening", Type: "docker", Status: "running", PID: 12, Networks: map[string]discovery.Network{"n": {IP: "172.19.0.4"}}},
	}}
	observeConnections(root, &snapshot)
	if len(snapshot.Connections) != 2 {
		t.Fatal(snapshot.Connections)
	}
	for _, c := range snapshot.Connections {
		if c.From != "app" {
			t.Fatal("incoming client was treated as a dependency", c)
		}
		if c.To != "" && (c.To != "db" || c.Kind != "established") {
			t.Fatal(c)
		}
	}
}
