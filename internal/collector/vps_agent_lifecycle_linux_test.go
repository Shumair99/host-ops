//go:build linux

package collector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/vpsagent"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This test changes system accounts/units. Three independent conditions keep
// it confined to the deliberately created disposable container, never a VPS.
func TestVPSAgentDisposableSystemdLifecycle(t *testing.T) {
	if os.Getenv("HOSTOPS_SYSTEMD_LAB") != "1" {
		t.Skip("requires disposable systemd lab")
	}
	if os.Geteuid() != 0 {
		t.Fatal("lab requires container root")
	}
	if _, err := os.Stat("/run/hostops-disposable-lab"); err != nil {
		t.Fatal("disposable lab marker missing")
	}
	if out, err := exec.Command("systemd-detect-virt", "--container").Output(); err != nil || strings.TrimSpace(string(out)) == "none" {
		t.Fatal("not a disposable container")
	}
	command := func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	must := func(name string, args ...string) []byte {
		t.Helper()
		out, err := command(name, args...)
		if err != nil {
			t.Fatalf("%s failed: %v %s", name, err, out)
		}
		return out
	}
	binary := "/opt/hostops-vps-agent"
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	unit, unitHash, err := vpsAgentUnit("lab-host")
	if err != nil {
		t.Fatal(err)
	}
	install, _ := vpsAgentFiles.ReadFile("install-vps-agent.sh")
	remove, _ := vpsAgentFiles.ReadFile("remove-vps-agent.sh")
	args := []string{"-c", string(install), "lab", binary, hash, strconv.Itoa(len(data)), "lab-host", "lab-ops", unit, unitHash}
	must("useradd", "--no-create-home", "lab-ops")
	defer command("userdel", "lab-ops")
	// A checksum failure after account creation must roll everything back.
	bad := append([]string(nil), args...)
	bad[4] = strings.Repeat("0", 64)
	if out, err := command("sh", bad...); err == nil {
		t.Fatalf("bad checksum accepted: %s", out)
	}
	for _, p := range []string{"/etc/hostops-vps-agent.managed", "/usr/local/libexec/hostops-vps-agent", "/etc/systemd/system/hostops-vps-agent.service", "/var/lib/hostops"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("rollback left path", p, err)
		}
	}
	if _, err := command("id", "hostops"); err == nil {
		t.Fatal("rollback left account")
	}
	must("sh", args...)
	removeArgs := []string{"-c", string(remove), "lab", "lab-host", "lab-ops", unitHash}
	defer command("sh", removeArgs...)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(vpsagent.DefaultSocketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket did not appear", string(must("journalctl", "-u", "hostops-vps-agent", "--no-pager")))
		}
		time.Sleep(100 * time.Millisecond)
	}
	pid := strings.TrimSpace(string(must("systemctl", "show", "hostops-vps-agent", "-p", "MainPID", "--value")))
	status := string(must("cat", "/proc/"+pid+"/status"))
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "Uid:") && strings.Fields(line)[1] == "0" {
			t.Fatal("service runs as root")
		}
		if strings.HasPrefix(line, "CapEff:") && strings.Fields(line)[1] != "0000000000000000" {
			t.Fatal("service has capabilities")
		}
	}
	if !strings.Contains(status, "NoNewPrivs:\t1") {
		t.Fatal("NoNewPrivileges absent")
	}
	if sockets := string(must("ss", "-lntp")); strings.Contains(sockets, "pid="+pid+",") {
		t.Fatal("service opens TCP listener", sockets)
	}
	readHistory := func(user string) (vpsagent.Frame, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "runuser", "-u", user, "--", "/usr/local/libexec/hostops-vps-agent", "bridge")
		req, _ := json.Marshal(vpsagent.Request{Version: vpsagent.ProtocolVersion, HostID: "lab-host"})
		cmd.Stdin = bytes.NewReader(append(req, '\n'))
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return vpsagent.Frame{}, err
		}
		if err = cmd.Start(); err != nil {
			return vpsagent.Frame{}, err
		}
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), vpsagent.MaxFrameBytes)
		if !scanner.Scan() {
			cancel()
			_ = cmd.Wait()
			return vpsagent.Frame{}, os.ErrPermission
		}
		frame, err := vpsagent.DecodeFrame(scanner.Bytes(), "lab-host", time.Now())
		cancel()
		_ = cmd.Wait()
		return frame, err
	}
	if _, err := readHistory("nobody"); err == nil {
		t.Fatal("unauthorized socket access accepted")
	}
	before, err := readHistory("lab-ops")
	if err != nil {
		t.Fatal("reader group denied", err)
	}
	cpuBefore := strings.TrimSpace(string(must("systemctl", "show", "hostops-vps-agent", "-p", "CPUUsageNSec", "--value")))
	started := time.Now()
	time.Sleep(32 * time.Second) // no dashboard/bridge during this interval
	after, err := readHistory("lab-ops")
	if err != nil || len(after.Samples) <= len(before.Samples) {
		t.Fatal("closed-dashboard history did not advance", len(before.Samples), len(after.Samples), err)
	}
	if len(after.Samples) > vpsagent.MaxHistory {
		t.Fatal("unbounded history")
	}
	info, err := os.Stat(vpsagent.DefaultHistoryPath)
	if err != nil || info.Size() > vpsagent.MaxHistoryBytes {
		t.Fatal("history file bounds", info, err)
	}
	cpuAfter := strings.TrimSpace(string(must("systemctl", "show", "hostops-vps-agent", "-p", "CPUUsageNSec", "--value")))
	first, _ := strconv.ParseUint(cpuBefore, 10, 64)
	last, _ := strconv.ParseUint(cpuAfter, 10, 64)
	t.Logf("32-second idle window: %.3f%% of one core; RSS %s KiB; history %d bytes / %d samples", float64(last-first)/float64(time.Since(started).Nanoseconds())*100, strings.TrimSpace(string(must("ps", "-p", pid, "-o", "rss="))), info.Size(), len(after.Samples))
	oldest := after.Samples[0].ObservedAt
	must("systemctl", "restart", "hostops-vps-agent")
	time.Sleep(time.Second)
	restored, err := readHistory("lab-ops")
	if err != nil || len(restored.Samples) == 0 || !restored.Samples[0].ObservedAt.Equal(oldest) {
		t.Fatal("restart lost checkpoint history", err)
	}
	// Modified managed files are not silently deleted by uninstall.
	unitPath := "/etc/systemd/system/hostops-vps-agent.service"
	if err := os.WriteFile(unitPath, []byte(unit+"\n# tampered\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := command("sh", removeArgs...); err == nil {
		t.Fatal("tampered unit removed")
	}
	must("systemctl", "is-active", "--quiet", "hostops-vps-agent")
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		t.Fatal(err)
	}
	must("sh", removeArgs...)
	for _, p := range []string{unitPath, "/usr/local/libexec/hostops-vps-agent", "/etc/hostops-vps-agent.managed", vpsagent.DefaultHistoryPath, vpsagent.DefaultSocketPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("uninstall left path", p, err)
		}
	}
	if _, err := command("id", "hostops"); err == nil {
		t.Fatal("uninstall left service account")
	}
}
