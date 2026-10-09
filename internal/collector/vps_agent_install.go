package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/platform"
)

//go:embed hostops-vps-agent.service install-vps-agent.sh remove-vps-agent.sh
var vpsAgentFiles embed.FS

var remoteLoginPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`)

func vpsAgentUnit(hostID string) (string, string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`).MatchString(hostID) {
		return "", "", fmt.Errorf("host id is invalid")
	}
	template, err := vpsAgentFiles.ReadFile("hostops-vps-agent.service")
	if err != nil {
		return "", "", fmt.Errorf("VPS service template unavailable")
	}
	unit := strings.ReplaceAll(string(template), "@HOST_ID@", hostID)
	sum := sha256.Sum256([]byte(unit))
	return unit, hex.EncodeToString(sum[:]), nil
}

func remoteArchitecture(ctx context.Context, c config.Config) (string, error) {
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "uname", "-m")
	if err != nil {
		return "", fmt.Errorf("cannot identify the VPS architecture")
	}
	switch strings.TrimSpace(string(data)) {
	case "x86_64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("always-on history supports Linux amd64 and arm64 VPS hosts")
	}
}

func vpsAgentBinary(architecture, override string) ([]byte, error) {
	path := override
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("cannot locate the HostOps release bundle")
		}
		path = filepath.Join(filepath.Dir(exe), "agents", "vps", "linux-"+architecture, "hostops-vps-agent")
	}
	info, statErr := os.Lstat(path)
	if statErr != nil || !info.Mode().IsRegular() || info.Size() < 100_000 || info.Size() > 50_000_000 {
		return nil, fmt.Errorf("matching Linux VPS collector is missing or invalid; build the HostOps release bundle")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("matching Linux VPS collector is missing or invalid; build the HostOps release bundle")
	}
	opened, statErr := f.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		_ = f.Close()
		return nil, fmt.Errorf("matching Linux VPS collector changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCollectorBytes+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || len(data) < 100_000 || int64(len(data)) > maxCollectorBytes {
		return nil, fmt.Errorf("matching Linux VPS collector is missing or invalid; build the HostOps release bundle")
	}
	if err := validateLinuxExecutable(data, architecture); err != nil {
		return nil, fmt.Errorf("VPS collector executable is invalid: %w", err)
	}
	return data, nil
}

func preparedVPSAgentBinary(ctx context.Context, architecture, override string, out io.Writer) ([]byte, error) {
	if override != "" {
		return vpsAgentBinary(architecture, override)
	}
	relative := path.Join("agents", "vps", "linux-"+architecture, "hostops-vps-agent")
	asset := bundledAsset(relative)
	if asset == "" {
		asset = sourceAsset(relative)
	}
	if asset == "" {
		if sourceRoot() != "" {
			fmt.Fprintf(out, "Preparing the Linux VPS history agent for %s from this checkout...\n", architecture)
		}
		var err error
		asset, err = buildSourceAsset(ctx, relative, "./cmd/hostops-vps-agent", architecture, "matching Linux VPS history agent")
		if err != nil {
			return nil, err
		}
	}
	return vpsAgentBinary(architecture, asset)
}

func remoteAgentCache(ctx context.Context, c config.Config) (string, error) {
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "sh", "-c", `printf '%s' "${XDG_CACHE_HOME:-$HOME/.cache}/hostops"`)
	if err != nil {
		return "", fmt.Errorf("cannot locate the private VPS collector cache")
	}
	root := string(data)
	if !path.IsAbs(root) || path.Clean(root) != root || strings.ContainsAny(root, "\x00\r\n") || len(root) > 4096 {
		return "", fmt.Errorf("VPS collector cache path is invalid")
	}
	return root, nil
}

func uploadVPSAgent(ctx context.Context, c config.Config, binary []byte, cacheRoot string) (string, string, error) {
	sum := sha256.Sum256(binary)
	hash := hex.EncodeToString(sum[:])
	cmd := exec.CommandContext(ctx, "ssh", append(sshArgs(c.Host), uploadCommand(hash))...)
	cmd.Stdin = bytes.NewReader(binary)
	var out limitedWriter
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("VPS collector upload or checksum verification failed; check SSH access")
	}
	remote := string(out.data)
	if remote != path.Join(cacheRoot, "collector-"+hash) || !path.IsAbs(remote) || strings.ContainsAny(remote, "\r\n\x00") || len(remote) > 4096 {
		return "", "", fmt.Errorf("VPS collector upload returned an invalid cache path")
	}
	return remote, hash, nil
}

func loginName(ctx context.Context, c config.Config) (string, error) {
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "id", "-un")
	name := strings.TrimSpace(string(data))
	if err != nil || !remoteLoginPattern.MatchString(name) {
		return "", fmt.Errorf("SSH account name could not be validated")
	}
	return name, nil
}

func remoteUID(ctx context.Context, c config.Config) (string, error) {
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "id", "-u")
	uid := strings.TrimSpace(string(data))
	if err != nil || uid == "" {
		return "", fmt.Errorf("SSH account privilege level could not be checked")
	}
	if _, err = strconv.ParseUint(uid, 10, 32); err != nil {
		return "", fmt.Errorf("SSH account privilege level could not be checked")
	}
	return uid, nil
}

func runInteractiveSSH(ctx context.Context, c config.Config, remote string, input io.Reader, out, errOut io.Writer) error {
	args := sshArgs(c.Host)
	args[0] = "-tt"
	cmd := exec.CommandContext(ctx, "ssh", append(args, remote)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, out, errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("VPS service installation did not complete")
	}
	return nil
}

func runRootScript(ctx context.Context, c config.Config, script string, args []string, useSudo bool, input io.Reader, out, errOut io.Writer) error {
	parts := []string{}
	if useSudo {
		parts = append(parts, "sudo", "-tt")
	}
	parts = append(parts, "/bin/sh", "-c", platform.Quote(script), "hostops-root-operation")
	for _, arg := range args {
		parts = append(parts, platform.Quote(arg))
	}
	return runInteractiveSSH(ctx, c, strings.Join(parts, " "), input, out, errOut)
}

// InstallVPSAgent installs the bounded telemetry service after the user
// explicitly chooses always-on history. Root access is used only for creating
// the dedicated service account, group, files and systemd unit. The installed
// process has no capabilities and no network address family except Unix IPC.
func InstallVPSAgent(ctx context.Context, c config.Config, override string, input io.Reader, out, errOut io.Writer) error {
	if c.Host.SSH == "" {
		return fmt.Errorf("always-on history requires an SSH connection to a Linux VPS")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if _, err := (platform.Command{Host: c.Host}).Run(ctx, "test", "-d", "/run/systemd/system"); err != nil {
		return fmt.Errorf("always-on history requires systemd on the VPS")
	}
	architecture, err := remoteArchitecture(ctx, c)
	if err != nil {
		return err
	}
	binary, err := preparedVPSAgentBinary(ctx, architecture, override, out)
	if err != nil {
		return err
	}
	cacheRoot, err := remoteAgentCache(ctx, c)
	if err != nil {
		return err
	}
	sshUser, err := loginName(ctx, c)
	if err != nil {
		return err
	}
	uid, err := remoteUID(ctx, c)
	if err != nil {
		return err
	}
	unit, unitHash, err := vpsAgentUnit(c.Host.ID)
	if err != nil {
		return err
	}
	script, err := vpsAgentFiles.ReadFile("install-vps-agent.sh")
	if err != nil {
		return fmt.Errorf("VPS installer unavailable")
	}
	remoteBinary, binaryHash, err := uploadVPSAgent(ctx, c, binary, cacheRoot)
	if err != nil {
		return err
	}
	args := []string{remoteBinary, binaryHash, strconv.Itoa(len(binary)), c.Host.ID, sshUser, unit, unitHash}
	if err = runRootScript(ctx, c, string(script), args, uid != "0", input, out, errOut); err != nil {
		return err
	}
	_, _ = (platform.Command{Host: c.Host}).Run(ctx, "rm", "-f", remoteBinary)
	active, err := VPSAgentActive(ctx, c)
	if err != nil || !active {
		return fmt.Errorf("VPS collector installation completed but its service is not active; run hostops agent status")
	}
	return nil
}

// VPSAgentActive reports whether the installed systemd unit is currently
// active. It does not expose journal or application log contents.
func VPSAgentActive(ctx context.Context, c config.Config) (bool, error) {
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "systemctl", "show", "--property=ActiveState", "--value", "hostops-vps-agent.service")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) == "active", nil
}

// VPSAgentInstalled checks the root-owned installation marker without reading
// the retained telemetry file or the service's logs.
func VPSAgentInstalled(ctx context.Context, c config.Config) (bool, error) {
	check := `if [ -f /etc/hostops-vps-agent.managed ] && [ ! -L /etc/hostops-vps-agent.managed ]; then cat -- /etc/hostops-vps-agent.managed; else printf 'not-installed'; fi`
	data, err := (platform.Command{Host: c.Host}).Run(ctx, "sh", "-c", check)
	if err != nil {
		return false, fmt.Errorf("cannot check VPS collector installation")
	}
	fields := strings.Fields(string(data))
	if len(fields) == 1 && fields[0] == "not-installed" {
		return false, nil
	}
	return len(fields) == 5 && fields[0] == "hostops-managed-v2" && fields[1] == c.Host.ID, nil
}

func RemoveVPSAgent(ctx context.Context, c config.Config, input io.Reader, out, errOut io.Writer) error {
	if c.Host.SSH == "" {
		return fmt.Errorf("VPS collector removal requires SSH")
	}
	sshUser, err := loginName(ctx, c)
	if err != nil {
		return err
	}
	uid, err := remoteUID(ctx, c)
	if err != nil {
		return err
	}
	_, unitHash, err := vpsAgentUnit(c.Host.ID)
	if err != nil {
		return err
	}
	script, err := vpsAgentFiles.ReadFile("remove-vps-agent.sh")
	if err != nil {
		return fmt.Errorf("VPS remover unavailable")
	}
	return runRootScript(ctx, c, string(script), []string{c.Host.ID, sshUser, unitHash}, uid != "0", input, out, errOut)
}
