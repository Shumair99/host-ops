package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/storage"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func connect(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("connect", flag.ContinueOnError)
	flags.SetOutput(errOut)
	file := flags.String("config", "local/hostops.json", "connection settings")
	identity := flags.String("identity", "", "existing SSH private key file")
	port := flags.Int("port", 0, "SSH port; zero uses SSH configuration")
	collector := flags.String("collector", "", "Linux collector from the release bundle")
	db := flags.String("db", "", "inventory database (default: private per-user cache)")
	target := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
		args = args[1:]
	}
	if flags.Parse(args) != nil {
		return 1
	}
	if flags.NArg() == 1 && target == "" {
		target = flags.Arg(0)
	} else if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "connect accepts one SSH target")
		return 1
	}
	if target == "" {
		fmt.Fprintln(errOut, "Usage: hostops connect user@vps [--identity FILE]")
		return 1
	}
	if resolved, ok := resolveHistoryPath(*db, errOut); ok {
		*db = resolved
	} else {
		return 1
	}
	c := config.Config{Host: config.Host{ID: discovery.StableID("host", target), Name: target, SSH: target, IdentityFile: *identity}, Discovery: config.Discovery{CollectorFile: *collector}}
	c.Host.SSHPort = *port
	if old, err := config.Load(*file); err == nil && old.Host.SSH == target {
		c = old
		c.Discovery.Disabled = false
		if *port != 0 {
			c.Host.SSHPort = *port
		}
		if *identity != "" {
			c.Host.IdentityFile = *identity
		}
		if *collector != "" {
			c.Discovery.CollectorFile = *collector
		}
	}
	if err := c.Validate(); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	store, err := storage.Open(*db)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer store.Close()
	fmt.Fprintln(out, "Connecting and discovering supported routes and workloads...")
	if err = agent.CollectOnce(ctx, c, store); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if err = os.MkdirAll(filepath.Dir(*file), 0700); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	if err = os.WriteFile(*file, append(data, '\n'), 0600); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	resolved, err := agent.ResolveConfig(ctx, c, store)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	fmt.Fprintf(out, "Connected. %d sites are in the automatic inventory. Settings: %s\nRun hostops start to configure history and open the dashboard, or hostops dashboard to open it directly.\n", len(resolved.Sites), *file)
	return 0
}

func discover(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	flags.SetOutput(errOut)
	defaultFile := os.Getenv("HOSTOPS_CONFIG")
	if defaultFile == "" {
		defaultFile = "hostops.json"
		if _, err := os.Stat(defaultFile); os.IsNotExist(err) {
			defaultFile = "local/hostops.json"
		}
	}
	file := flags.String("config", defaultFile, "connection settings")
	db := flags.String("db", "", "inventory database (default: private per-user cache)")
	asJSON := flags.Bool("json", false, "JSON output")
	cached := flags.Bool("no-collect", false, "use existing inventory")
	allServices := flags.Bool("all-services", false, "include non-listening systemd services")
	if flags.Parse(args) != nil {
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "discover takes no site argument")
		return 1
	}
	if resolved, ok := resolveHistoryPath(*db, errOut); ok {
		*db = resolved
	} else {
		return 1
	}
	c, err := config.Load(*file)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	c.Host.AllServices = c.Host.AllServices || *allServices
	store, err := storage.Open(*db)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer store.Close()
	if !*cached {
		if err = agent.CollectOnce(ctx, c, store); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	registry, err := store.Inventory(ctx, c.Host.ID)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if *asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if encoder.Encode(registry) != nil {
			return 1
		}
	} else {
		fmt.Fprintf(out, "Inventory: %d sites, %d workloads, %d route patterns\n", len(registry.Sites), len(registry.Snapshot.Workloads), len(registry.Patterns))
		for _, site := range registry.Sites {
			fmt.Fprintf(out, "%-45s %s\n", console.Safe(site.Domain), site.State)
		}
		for _, site := range discovery.ConfigSites(registry, c).Sites {
			if site.Health.PublicURL == "" {
				fmt.Fprintf(out, "Warning: %s has no observed public endpoint\n", console.Safe(site.Name))
			}
		}
		fmt.Fprintln(out, "\nSource coverage:")
		for _, source := range registry.Snapshot.Sources {
			fmt.Fprintf(out, "%-12s %-12s %s %s\n", source.State, source.Mode, console.Safe(source.ID), console.Safe(source.Issue))
		}
	}
	return 0
}
