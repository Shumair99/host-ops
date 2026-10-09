package collector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/vpsagent"
)

type Session struct {
	cancel     context.CancelFunc
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	host       string
	mu         sync.Mutex
	once       sync.Once
	persistent bool
}

const maxCollectorBytes = 50_000_000

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
		relative := "collectors/linux-" + arch + "/hostops"
		file = bundledAsset(relative)
		if file == "" {
			file = sourceAsset(relative)
		}
		if file == "" {
			file, err = buildSourceAsset(ctx, relative, "./cmd/hostops", arch, "matching Linux discovery collector")
			if err != nil {
				return "", err
			}
		}
	}
	binary, err := readCollectorBinary(file, arch)
	if err != nil {
		return "", err
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

func readCollectorBinary(file, arch string) ([]byte, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return nil, fmt.Errorf("Linux collector missing: build the release bundle or set discovery.collector_file")
	}
	if !info.Mode().IsRegular() || info.Size() < 64 || info.Size() > maxCollectorBytes {
		return nil, fmt.Errorf("collector file must be a regular Linux executable no larger than 50 MB")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("cannot open Linux collector")
	}
	opened, statErr := f.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, fmt.Errorf("collector file changed while opening")
	}
	binary, readErr := io.ReadAll(io.LimitReader(f, maxCollectorBytes+1))
	closeErr := f.Close()
	if readErr == nil {
		readErr = closeErr
	}
	if readErr != nil {
		return nil, fmt.Errorf("cannot read Linux collector")
	}
	if len(binary) < 64 || int64(len(binary)) > maxCollectorBytes {
		return nil, fmt.Errorf("collector file must be a regular Linux executable no larger than 50 MB")
	}
	if err := validateLinuxExecutable(binary, arch); err != nil {
		return nil, err
	}
	return binary, nil
}

func validateLinuxExecutable(data []byte, arch string) error {
	executable, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("collector file is not a valid Linux executable")
	}
	defer executable.Close()
	if executable.Class != elf.ELFCLASS64 || executable.Data != elf.ELFDATA2LSB || (executable.Type != elf.ET_EXEC && executable.Type != elf.ET_DYN) {
		return fmt.Errorf("collector file must be a 64-bit little-endian Linux executable")
	}
	var machine elf.Machine
	switch arch {
	case "amd64":
		machine = elf.EM_X86_64
	case "arm64":
		machine = elf.EM_AARCH64
	default:
		return fmt.Errorf("collector architecture is not supported")
	}
	if executable.Machine != machine {
		return fmt.Errorf("collector executable does not match the VPS architecture")
	}
	if executable.Entry == 0 {
		return fmt.Errorf("collector executable has no entry point")
	}
	entryMapped := false
	for _, program := range executable.Progs {
		if program.Type == elf.PT_LOAD && program.Flags&elf.PF_X != 0 && executable.Entry >= program.Vaddr && executable.Entry-program.Vaddr < program.Memsz {
			entryMapped = true
			break
		}
	}
	if !entryMapped {
		return fmt.Errorf("collector entry point is not in an executable segment")
	}
	return nil
}

func uploadCommand(hash string) string {
	name := "collector-" + hash
	// Keep recent versions during the upload/start handoff so another HostOps
	// process cannot remove a binary that it has uploaded but not launched yet.
	return `umask 077; taskCache="${XDG_CACHE_HOME:-$HOME/.cache}/hostops"; test ! -L "$taskCache" && mkdir -p "$taskCache" && chmod 700 "$taskCache" || exit 1; taskTmp="$taskCache/` + name + `.upload-$$"; trap 'rm -f -- "$taskTmp"' EXIT; cat > "$taskTmp" && printf '%s  %s\n' '` + hash + `' "$taskTmp" | sha256sum -c - >/dev/null && chmod 700 "$taskTmp" && mv -- "$taskTmp" "$taskCache/` + name + `" && { find "$taskCache" -maxdepth 1 -type f -regextype posix-extended -regex '.*/collector-[0-9a-f]{64}' ! -name '` + name + `' -mmin +60 -delete 2>/dev/null || :; } && printf '%s' "$taskCache/` + name + `"`
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

func Start(ctx context.Context, c config.Config, path string, skipHostMetrics bool) (*Session, error) {
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
	for _, site := range c.Sites {
		if site.Runtime.Type == "systemd" {
			host.ServiceUnits = append(host.ServiceUnits, site.Runtime.Unit)
		}
	}
	if err = json.NewEncoder(stdin).Encode(Request{Version: discovery.ProtocolVersion, Host: host, SkipHostMetrics: skipHostMetrics}); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// StartPersistent attaches to the installed VPS collector through the existing
// strict-host-key SSH session. The fixed bridge command only relays the
// read-only Unix-socket stream; it accepts no command or path from the client.
func StartPersistent(ctx context.Context, c config.Config) (*Session, error) {
	if c.Host.SSH == "" || c.Host.HistoryMode != "continuous" {
		return nil, fmt.Errorf("persistent VPS history is not enabled for this host")
	}
	ctx, cancel := context.WithCancel(ctx)
	remote := platform.Quote("/usr/local/libexec/hostops-vps-agent") + " bridge"
	args := sshArgs(c.Host)
	// This is a new login so a group added by the installer is applied even
	// when the user's SSH configuration normally reuses a ControlMaster.
	for i, arg := range args {
		if arg == "--" {
			args = append(args[:i], append([]string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}, args[i:]...)...)
			break
		}
	}
	cmd := exec.CommandContext(ctx, "ssh", append(args, remote)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		return nil, fmt.Errorf("persistent VPS collector launch failed")
	}
	s := &Session{cancel: cancel, cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), host: c.Host.ID, persistent: true}
	s.scanner.Buffer(make([]byte, 4096), vpsagent.MaxFrameBytes)
	if err = json.NewEncoder(stdin).Encode(vpsagent.Request{Version: vpsagent.ProtocolVersion, HostID: c.Host.ID}); err != nil {
		s.Close()
		return nil, fmt.Errorf("persistent VPS collector request failed")
	}
	if err = stdin.Close(); err != nil {
		s.Close()
		return nil, fmt.Errorf("persistent VPS collector request failed")
	}
	return s, nil
}

func (s *Session) Next() (Message, error) {
	var m Message
	if !s.scanner.Scan() {
		return m, fmt.Errorf("collector disconnected or exceeded frame limit")
	}
	if s.persistent {
		frame, err := vpsagent.DecodeFrame(s.scanner.Bytes(), s.host, time.Now())
		if err != nil {
			return m, err
		}
		return Message{Version: frame.Version, HostID: frame.HostID, Kind: frame.Kind, Sample: frame.Sample, Samples: frame.Samples}, nil
	}
	if json.Unmarshal(s.scanner.Bytes(), &m) != nil || m.Version != discovery.ProtocolVersion || m.HostID != s.host {
		return m, fmt.Errorf("collector protocol or host mismatch")
	}
	return m, nil
}
func (s *Session) Refresh() error {
	if s.persistent {
		return fmt.Errorf("persistent collector does not accept control commands")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.NewEncoder(s.stdin).Encode(Control{Kind: "refresh"})
}
func (s *Session) Close() { s.once.Do(func() { s.cancel(); s.stdin.Close(); _ = s.cmd.Wait() }) }
