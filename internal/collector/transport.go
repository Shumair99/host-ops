package collector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
)

type Session struct {
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	host    string
	mu      sync.Mutex
	once    sync.Once
}

func sshArgs(h config.Host) []string {
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2"}
	if h.SSHPort > 0 {
		args = append(args, "-p", strconv.Itoa(h.SSHPort))
	}
	if h.IdentityFile != "" {
		args = append(args, "-i", h.IdentityFile)
	}
	return append(args, "--", h.SSH)
}

// Bootstrap installs only the version-matched collector in the user's private
// cache. It never alters services, runtimes, proxy configuration or host packages.
func Bootstrap(ctx context.Context, c config.Config) (string, error) {
	if c.Host.SSH == "" {
		path, err := os.Executable()
		return path, err
	}
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "uname", "-m")
	if err != nil {
		return "", err
	}
	arch := ""
	switch strings.TrimSpace(string(data)) {
	case "x86_64":
		arch = "amd64"
	case "aarch64", "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("VPS architecture is not supported by this release")
	}
	file := c.Discovery.CollectorFile
	if file == "" {
		exe, _ := os.Executable()
		file = filepath.Join(filepath.Dir(exe), "collectors", "linux-"+arch, "hostops")
	}
	binary, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("Linux collector missing: build the release bundle or set discovery.collector_file")
	}
	if len(binary) < 20 || string(binary[:4]) != "\x7fELF" {
		return "", fmt.Errorf("collector file must be a Linux executable")
	}
	machine := int(binary[18]) | int(binary[19])<<8
	if binary[5] != 1 || arch == "amd64" && machine != 62 || arch == "arm64" && machine != 183 {
		return "", fmt.Errorf("collector executable does not match the VPS architecture")
	}
	sum := sha256.Sum256(binary)
	hash := hex.EncodeToString(sum[:])
	// Fixed script, with the content-addressed name as the only interpolated value.
	command := uploadCommand(hash)
	cmd := exec.CommandContext(ctx, "ssh", append(sshArgs(c.Host), command)...)
	cmd.Stdin = bytes.NewReader(binary)
	var out limitedWriter
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return "", fmt.Errorf("collector upload or checksum verification failed; check SSH access")
	}
	remote := strings.TrimSpace(string(out.data))
	if !strings.HasPrefix(remote, "/") || strings.ContainsAny(remote, "\r\n\x00") {
		return "", fmt.Errorf("invalid collector cache path")
	}
	return remote, nil
}

func uploadCommand(hash string) string {
	name := "collector-" + hash
	return `umask 077; taskCache="${XDG_CACHE_HOME:-$HOME/.cache}/hostops"; test ! -L "$taskCache" && mkdir -p "$taskCache" && chmod 700 "$taskCache" || exit 1; taskTmp="$taskCache/` + name + `.upload-$$"; trap 'rm -f -- "$taskTmp"' EXIT; cat > "$taskTmp" && printf '%s  %s\n' '` + hash + `' "$taskTmp" | sha256sum -c - >/dev/null && chmod 700 "$taskTmp" && mv -- "$taskTmp" "$taskCache/` + name + `" && printf '%s' "$taskCache/` + name + `"`
}

type limitedWriter struct{ data []byte }

func (w *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(w.data)+n > 8192 {
		return 0, fmt.Errorf("collector startup output too large")
	}
	w.data = append(w.data, p...)
	return n, nil
}

func Start(ctx context.Context, c config.Config, path string) (*Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	var cmd *exec.Cmd
	if c.Host.SSH != "" {
		cmd = exec.CommandContext(ctx, "ssh", append(sshArgs(c.Host), platform.Quote(path)+" collect")...)
	} else {
		if runtime.GOOS != "linux" {
			cancel()
			return nil, fmt.Errorf("local discovery requires Linux; connect a VPS over SSH")
		}
		cmd = exec.CommandContext(ctx, path, "collect")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		stdin.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		return nil, fmt.Errorf("collector launch failed")
	}
	s := &Session{cancel: cancel, cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), host: c.Host.ID}
	s.scanner.Buffer(make([]byte, 65536), 8*1024*1024)
	host := c.Host
	host.SSH, host.IdentityFile = "", ""
	if err = json.NewEncoder(stdin).Encode(Request{Version: discovery.ProtocolVersion, Host: host}); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) Next() (Message, error) {
	var m Message
	if !s.scanner.Scan() {
		return m, fmt.Errorf("collector disconnected or exceeded frame limit")
	}
	if json.Unmarshal(s.scanner.Bytes(), &m) != nil || m.Version != discovery.ProtocolVersion || m.HostID != s.host {
		return m, fmt.Errorf("collector protocol or host mismatch")
	}
	return m, nil
}
func (s *Session) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.NewEncoder(s.stdin).Encode(Control{Kind: "refresh"})
}
func (s *Session) Close() { s.once.Do(func() { s.cancel(); s.stdin.Close(); _ = s.cmd.Wait() }) }
