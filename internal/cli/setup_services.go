package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/Shumair99/host-ops/internal/tui"
	"github.com/charmbracelet/x/term"
)

type setupServices struct {
	PrepareSSH func(context.Context, config.Config, io.Reader, io.Writer, io.Writer) error
	Discover   func(context.Context, config.Config, string) (discovery.Registry, error)
	Dashboard  func(context.Context, config.Config, string, tui.Options, io.Reader, io.Writer) error
}

func setupSSHArgs(h config.Host, interactive bool) []string {
	args := []string{"-T", "-o", "ConnectTimeout=10", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no"}
	if interactive {
		// OpenSSH performs the actual host-key confirmation. Changed keys still
		// fail; no keyscan result is silently trusted or written by HostOps.
		args = append(args, "-o", "BatchMode=no", "-o", "StrictHostKeyChecking=ask", "-o", "AddKeysToAgent=yes")
	} else {
		args = append(args, "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes")
	}
	if h.SSHPort > 0 {
		args = append(args, "-p", strconv.Itoa(h.SSHPort))
	}
	if h.IdentityFile != "" {
		args = append(args, "-i", h.IdentityFile)
	}
	return append(args, "--", h.SSH, "true")
}

func prepareSetupSSH(ctx context.Context, c config.Config, input io.Reader, out, errOut io.Writer) error {
	if _, err := exec.LookPath("ssh"); err != nil {
		return fmt.Errorf("OpenSSH client is missing. Install your operating system's OpenSSH client, then run hostops start again")
	}
	interactive := false
	if file, ok := input.(*os.File); ok {
		interactive = term.IsTerminal(file.Fd())
	}
	if interactive {
		fmt.Fprintln(out, "SSH may ask you to confirm your server's fingerprint. Check it against your provider before accepting.")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "ssh", setupSSHArgs(c.Host, interactive)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, out, errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SSH connection was not verified. Check the host, port, authorized SSH key and host fingerprint, then run hostops start again. Password-only login is not supported")
	}
	if _, err := (platform.Command{Host: c.Host}).Run(checkCtx, "true"); err != nil {
		return fmt.Errorf("SSH connected, but unattended key authentication is unavailable. Unlock your key in your SSH agent, then run hostops start again; HostOps does not store key passphrases")
	}
	return nil
}

func discoverSetup(ctx context.Context, c config.Config, db string) (discovery.Registry, error) {
	store, err := storage.Open(db)
	if err != nil {
		return discovery.Registry{}, err
	}
	defer store.Close()
	if err = agent.CollectOnce(ctx, c, store); err != nil {
		return discovery.Registry{}, fmt.Errorf("VPS discovery could not finish: %w", err)
	}
	return store.Inventory(ctx, c.Host.ID)
}

func printSetupInventory(out io.Writer, r discovery.Registry) {
	fmt.Fprintf(out, "Detected %s / %s · %d addresses · %d workloads · %d proxy routes\n", console.Safe(r.Snapshot.OS), console.Safe(r.Snapshot.Architecture), len(r.Sites), len(r.Snapshot.Workloads), len(r.Snapshot.Routes))
	gaps := 0
	for _, source := range r.Snapshot.Sources {
		if source.State == "unavailable" {
			gaps++
			fmt.Fprintf(out, "  Coverage gap: %s — %s\n", console.Safe(source.Kind), console.Safe(source.Issue))
		}
	}
	if len(r.Sites) == 0 {
		fmt.Fprintln(out, "No website addresses were observed. Inventory will show available sources and discovery gaps.")
	} else if gaps == 0 {
		fmt.Fprintln(out, "Discovery finished. Open Inventory to review observed routes and source coverage.")
	}
}
