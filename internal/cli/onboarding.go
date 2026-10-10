package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/collector"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/Shumair99/host-ops/internal/tui"
)

func defaultConnectionConfig() string {
	if path := os.Getenv("HOSTOPS_CONFIG"); path != "" {
		return path
	}
	if _, err := os.Stat("hostops.json"); err == nil {
		return "hostops.json"
	}
	return "local/hostops.json"
}

func startCommand(ctx context.Context, args []string, input io.Reader, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(errOut)
	path := flags.String("config", defaultConnectionConfig(), "connection settings")
	db := flags.String("db", "", "local history database (default: private per-user cache)")
	identity := flags.String("identity", "", "existing SSH private key file")
	port := flags.Int("port", 0, "SSH port; zero uses SSH configuration")
	collectorFile := flags.String("collector", "", "Linux discovery collector from the release bundle")
	vpsAgent := flags.String("vps-agent", "", "VPS history agent binary (normally bundled beside hostops)")
	interval := flags.Duration("interval", 30*time.Second, "public HTTP check interval")
	page := flags.String("page", "overview", "initial dashboard view")
	color := flags.String("color", "auto", "terminal color: auto, always, never")
	once := flags.Bool("once", false, "print one dashboard frame and exit")
	noCollect := flags.Bool("no-collect", false, "disable automatic cycles")
	target := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target, args = args[0], args[1:]
	}
	if flags.Parse(args) != nil {
		return 1
	}
	if flags.NArg() == 1 && target == "" {
		target = flags.Arg(0)
	} else if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "start accepts one SSH target")
		return 1
	}
	if *interval < 5*time.Second {
		fmt.Fprintln(errOut, "public HTTP check interval must be at least 5s")
		return 1
	}
	if resolved, ok := resolveHistoryPath(*db, errOut); ok {
		*db = resolved
	} else {
		return 1
	}

	c, err := config.Load(*path)
	newConnection := errors.Is(err, fs.ErrNotExist)
	if err != nil && !newConnection {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if newConnection {
		fmt.Fprintln(out, "HostOps uses SSH keys from your agent/config and a host key already trusted by SSH; it does not ask for or store SSH passwords.")
		if target == "" {
			target, err = askLine(input, out, "VPS SSH target (user@host): ")
			if err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			if target == "" {
				fmt.Fprintln(errOut, "SSH target is required")
				return 1
			}
		}
		if *identity == "" {
			*identity, err = askLine(input, out, "SSH private key path (blank uses SSH config/agent): ")
			if err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
		}
		c = config.Config{
			Host:      config.Host{ID: discovery.StableID("host", target), Name: target, SSH: target, IdentityFile: *identity, SSHPort: *port},
			Discovery: config.Discovery{CollectorFile: *collectorFile},
		}
	} else {
		if target != "" && target != c.Host.SSH {
			fmt.Fprintln(errOut, "this config already points to a different VPS; use a separate --config file")
			return 1
		}
		if *identity != "" {
			c.Host.IdentityFile = *identity
		}
		if *port != 0 {
			c.Host.SSHPort = *port
		}
		if *collectorFile != "" {
			c.Discovery.CollectorFile = *collectorFile
		}
	}
	if c.Host.SSH == "" {
		fmt.Fprintln(errOut, "start requires a configured SSH target")
		return 1
	}
	if err = c.Validate(); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}

	if newConnection {
		fmt.Fprintln(out, "Connecting over SSH and discovering VPS workloads. SSH key authentication and a trusted host key are required.")
		store, openErr := storage.Open(*db)
		if openErr != nil {
			fmt.Fprintln(errOut, openErr)
			return 1
		}
		err = agent.CollectOnce(ctx, c, store)
		siteCount := 0
		if err == nil {
			var resolved config.Config
			resolved, err = agent.ResolveConfig(ctx, c, store)
			if err == nil {
				siteCount = len(resolved.Sites)
			}
		}
		closeErr := store.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		fmt.Fprintf(out, "Discovered %d production properties.\n", siteCount)
	}

	if c.Host.HistoryMode == "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Optional VPS history")
		fmt.Fprintln(out, "  Keeps a rolling 30-minute window of CPU, memory, load, and root-disk samples.")
		fmt.Fprintln(out, "  Samples once per second; checkpoints every 15 seconds; opens no network port.")
		fmt.Fprintln(out, "  Installation uses root privileges to create a locked account and install the service.")
		fmt.Fprintln(out, "  HostOps never saves your sudo password.")
		fmt.Fprintln(out)
		continuous, promptErr := askYesNo(input, out, "Enable always-on history while the dashboard is closed? [y/N] ", false)
		if promptErr != nil {
			fmt.Fprintln(errOut, promptErr)
			return 1
		}
		if continuous {
			c.Host.HistoryMode = "continuous"
		} else {
			c.Host.HistoryMode = "session"
		}
		if err = saveConnectionConfig(*path, c); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if continuous {
			if err = collector.InstallVPSAgent(ctx, c, *vpsAgent, input, out, errOut); err != nil {
				c.Host.HistoryMode = "session"
				if saveErr := saveConnectionConfig(*path, c); saveErr != nil {
					fmt.Fprintf(errOut, "Also could not save session-only mode: %v\n", saveErr)
					fmt.Fprintf(errOut, "VPS history service was not enabled: %v\n", err)
					return 1
				}
				fmt.Fprintf(errOut, "Always-on history was not enabled: %v\n", err)
				fmt.Fprintln(out, "Continuing with session-only history. You can retry later with `hostops agent install`.")
			}
		}
	}
	if c.Host.HistoryMode == "continuous" {
		installed, statusErr := collector.VPSAgentInstalled(ctx, c)
		active, activeErr := collector.VPSAgentActive(ctx, c)
		if statusErr != nil || activeErr != nil || !installed || !active {
			fmt.Fprintln(errOut, "always-on history is configured but the VPS service is not active; run `hostops agent status` and `hostops agent install` to repair it")
			return 1
		}
	}
	if err = tui.Run(ctx, c, *db, tui.Options{Interval: *interval, Once: *once, NoCollect: *noCollect, Page: *page, Color: *color}, input, out); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func agentCommand(ctx context.Context, args []string, input io.Reader, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "Usage: hostops agent install|status|uninstall [--config FILE]")
		return 1
	}
	action := args[0]
	if action != "install" && action != "status" && action != "uninstall" {
		fmt.Fprintf(errOut, "unknown agent action %q\n", action)
		return 1
	}
	flags := flag.NewFlagSet("agent "+action, flag.ContinueOnError)
	flags.SetOutput(errOut)
	path := flags.String("config", defaultConnectionConfig(), "connection settings")
	vpsAgent := flags.String("vps-agent", "", "VPS history agent binary (normally bundled beside hostops)")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return 1
	}
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	switch action {
	case "status":
		if c.Host.SSH == "" {
			fmt.Fprintln(errOut, "VPS agent commands require a configured SSH target")
			return 1
		}
		installed, err := collector.VPSAgentInstalled(ctx, c)
		if err != nil {
			fmt.Fprintln(errOut, "Could not check the VPS installation marker")
			return 1
		}
		if !installed {
			fmt.Fprintln(out, "VPS history service: not installed")
			return 0
		}
		active, err := collector.VPSAgentActive(ctx, c)
		if err != nil {
			fmt.Fprintln(errOut, "Installed, but systemd status could not be read")
			return 1
		}
		if active {
			fmt.Fprintln(out, "VPS history service: installed and active (30-minute rolling window)")
		} else {
			fmt.Fprintln(out, "VPS history service: installed but inactive")
		}
	case "install":
		if c.Host.SSH == "" {
			fmt.Fprintln(errOut, "VPS agent installation requires a configured SSH target")
			return 1
		}
		installed, statusErr := collector.VPSAgentInstalled(ctx, c)
		if statusErr != nil {
			fmt.Fprintln(errOut, statusErr)
			return 1
		}
		if installed {
			active, activeErr := collector.VPSAgentActive(ctx, c)
			if activeErr != nil {
				fmt.Fprintln(errOut, "Installed service status could not be checked")
				return 1
			}
			if active {
				c.Host.HistoryMode = "continuous"
				if err = saveConnectionConfig(*path, c); err != nil {
					fmt.Fprintln(errOut, err)
					return 1
				}
				fmt.Fprintln(out, "VPS history service is already installed and active.")
				return 0
			}
			fmt.Fprintln(errOut, "VPS history service is installed but inactive. Inspect it with `hostops agent status` before changing the installation.")
			return 1
		}
		c.Host.HistoryMode = "continuous"
		if err = c.Validate(); err == nil {
			err = saveConnectionConfig(*path, c)
		}
		if err == nil {
			err = collector.InstallVPSAgent(ctx, c, *vpsAgent, input, out, errOut)
		}
		if err != nil {
			c.Host.HistoryMode = "session"
			_ = saveConnectionConfig(*path, c)
			fmt.Fprintln(errOut, err)
			return 1
		}
		fmt.Fprintln(out, "Always-on history enabled. Run `hostops start` to open the dashboard.")
	case "uninstall":
		if c.Host.SSH == "" {
			fmt.Fprintln(errOut, "VPS agent removal requires a configured SSH target")
			return 1
		}
		confirmed, promptErr := askYesNo(input, out, "Remove the VPS service and its retained 30-minute history? [y/N] ", false)
		if promptErr != nil {
			fmt.Fprintln(errOut, promptErr)
			return 1
		}
		if !confirmed {
			fmt.Fprintln(out, "Cancelled; nothing was removed.")
			return 0
		}
		if err = collector.RemoveVPSAgent(ctx, c, input, out, errOut); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		c.Host.HistoryMode = "session"
		if err = saveConnectionConfig(*path, c); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	return 0
}

func askLine(input io.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	var line strings.Builder
	for line.Len() <= 4096 {
		var next [1]byte
		_, err := io.ReadFull(input, next[:])
		if err != nil {
			if err == io.EOF && line.Len() > 0 {
				break
			}
			return "", fmt.Errorf("interactive setup needs terminal input")
		}
		if next[0] == '\n' {
			break
		}
		if next[0] != '\r' {
			line.WriteByte(next[0])
		}
	}
	if line.Len() > 4096 {
		return "", fmt.Errorf("setup answer is too long")
	}
	return strings.TrimSpace(line.String()), nil
}

func askYesNo(input io.Reader, out io.Writer, prompt string, defaultYes bool) (bool, error) {
	answer, err := askLine(input, out, prompt)
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultYes, nil
	}
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	if answer == "n" || answer == "no" {
		return false, nil
	}
	return false, fmt.Errorf("answer yes or no")
}

func saveConnectionConfig(path string, c config.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode connection settings: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create connection settings directory: %w", err)
	}
	if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("save connection settings: %w", err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("secure connection settings: %w", err)
	}
	return nil
}
