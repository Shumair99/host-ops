package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/collector"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/evidence"
	"github.com/Shumair99/host-ops/internal/health"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/Shumair99/host-ops/internal/tui"
)

const Help = `Amadeus HostOps - terminal-only production website operations

Usage: hostops <command> [site] [options]

Commands:
  connect TARGET    Save an SSH connection and discover sites automatically
  discover          Refresh and explain the automatic inventory
  dashboard         Open the terminal monitoring dashboard (default)
  sites             Refresh and list automatically discovered sites
  record <kind>     Import deployment/backup/restore/observations (--file FILE)
  status [site]     Inspect runtime health
  doctor [site]     Explain cross-layer observations with evidence
  metrics [site]    Collect current host or container metrics
  history [site]    Query SQLite health history (--db, --since)
  disk              Inspect disk directories and Docker usage without deleting
  events [site]     Read the operations timeline
  deployments       Read deployment notifications
  logs <site>       Read a bounded, redacted runtime log stream
  diff [site]       Read infrastructure changes (--since 24h)
  incidents         Read persisted outage and recovery evidence
  capsule <incident> Export a redacted incident evidence bundle as JSON
  readiness [site]  Explain the production readiness score
  security          Inspect posture, publications and listening sockets
  orphans           Review possible stopped/unregistered resource candidates
  ownership <name>  Find the site owning a container, domain or configured port
  costs             Explain configured cost allocation and estimated margins
  billing           Optional provider API / manual budget setup (billing help)
  capacity          Assess measured resource usage and history limits
  recovery [site]   Assess backup and reported restore evidence
  preflight <target> Read-only reboot or site maintenance assessment
  impact <resource> Simulate the registered dependency blast radius

Options:
  --config FILE     Manifest (default: HOSTOPS_CONFIG or hostops.json)
  --json            Machine-readable output

  --db FILE         Local SQLite history (default local/hostops.db)
  --since DURATION  Historical query window (default 24h)
  --file FILE       Evidence JSON; use - for standard input
  --demo            Offline fictional dashboard; no network access
  --once            Print one dashboard frame and exit
  --no-collect      Disable automatic cycles; inspection views still read live
  --interval VALUE  Public HTTP interval (default 30s; CPU/RAM stream every 1s)
  --page VIEW       Initial dashboard view, e.g. disk or readiness
  --color MODE      auto (default), always, or never; NO_COLOR is respected

Exit codes: 0 healthy/list, 1 usage/configuration error, 2 unhealthy or unknown.
`

func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	return RunWithInput(ctx, args, os.Stdin, out, errOut)
}
func RunWithInput(ctx context.Context, args []string, input io.Reader, out, errOut io.Writer) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help") {
		fmt.Fprint(out, Help)
		return 0
	}
	if len(args) == 0 {
		args = []string{"dashboard"}
	}
	command := args[0]
	if command == "connect" {
		return connect(ctx, args[1:], out, errOut)
	}
	if command == "discover" {
		return discover(ctx, args[1:], out, errOut)
	}
	if command == "billing" {
		return billingCommand(ctx, args[1:], out, errOut)
	}
	if command == "collect" {
		if err := collector.Serve(ctx, input, out); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	if command != "sites" && command != "status" && command != "doctor" && command != "metrics" && command != "history" && command != "disk" && command != "events" && command != "deployments" && command != "logs" && command != "diff" && command != "incidents" && command != "readiness" && command != "security" && command != "orphans" && command != "ownership" && command != "costs" && command != "capacity" && command != "recovery" && command != "impact" && command != "preflight" && command != "capsule" && command != "record" && command != "dashboard" {
		fmt.Fprintf(errOut, "unknown command %q\n", command)
		return 1
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	defaultPath := os.Getenv("HOSTOPS_CONFIG")
	if defaultPath == "" {
		defaultPath = "hostops.json"
		if _, err := os.Stat(defaultPath); os.IsNotExist(err) {
			if _, err = os.Stat("local/hostops.json"); err == nil {
				defaultPath = "local/hostops.json"
			}
		}
	}
	path := flags.String("config", defaultPath, "site manifest")
	asJSON := flags.Bool("json", false, "JSON output")
	db := flags.String("db", "local/hostops.db", "history database")
	since := flags.String("since", "24h", "history window")
	file := flags.String("file", "", "evidence input file")
	demo := flags.Bool("demo", false, "offline demo")
	once := flags.Bool("once", false, "one dashboard frame")
	noCollect := flags.Bool("no-collect", false, "history only")
	interval := flags.Duration("interval", 30*time.Second, "public HTTP check interval")
	page := flags.String("page", "overview", "initial dashboard view")
	color := flags.String("color", "auto", "terminal color: auto, always, never")
	id := ""
	flagArgs := append([]string(nil), args[1:]...)
	if len(flagArgs) > 0 && !strings.HasPrefix(flagArgs[0], "-") {
		id = flagArgs[0]
		flagArgs = flagArgs[1:]
	}
	if err := flags.Parse(flagArgs); err != nil {
		return 1
	}
	if flags.NArg() > 0 {
		if id != "" || flags.NArg() > 1 {
			fmt.Fprintln(errOut, "too many arguments")
			return 1
		}
		id = flags.Arg(0)
	}
	if command == "dashboard" {
		if id != "" {
			fmt.Fprintln(errOut, "dashboard does not accept a site argument; choose it inside the dashboard")
			return 1
		}
		var c config.Config
		var err error
		if !*demo {
			c, err = config.Load(*path)
			if err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
		}
		err = tui.Run(ctx, c, *db, tui.Options{Interval: *interval, Demo: *demo, Once: *once, NoCollect: *noCollect, Page: *page, Color: *color}, input, out)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	var observed []model.Report
	if c.Discovering() {
		store, openErr := storage.Open(*db)
		if openErr != nil {
			fmt.Fprintln(errOut, openErr)
			return 1
		}
		if !*noCollect && (command == "sites" || command == "status" || command == "doctor") {
			if err = agent.CollectOnce(ctx, c, store); err != nil {
				store.Close()
				fmt.Fprintln(errOut, err)
				return 1
			}
		}
		c, err = agent.ResolveConfig(ctx, c, store)
		if err == nil {
			observed, err = store.Latest(ctx, c.Host.ID)
		}
		store.Close()
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	selectionID := id
	if command == "ownership" || command == "impact" || command == "preflight" || command == "capsule" || command == "record" {
		selectionID = ""
	}
	sites, err := c.Select(selectionID)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if command == "record" {
		if *file == "" {
			fmt.Fprintln(errOut, "record requires --file FILE (or --file - for stdin)")
			return 1
		}
		if *file != "-" {
			f, err := os.Open(*file)
			if err != nil {
				fmt.Fprintln(errOut, "Evidence input unavailable")
				return 1
			}
			defer f.Close()
			input = f
		}
		store, err := storage.Open(*db)
		if err != nil {
			fmt.Fprintln(errOut, "Evidence database unavailable")
			return 1
		}
		defer store.Close()
		count, err := evidence.Record(ctx, c, store, id, input)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if *asJSON {
			encoder.Encode(map[string]any{"accepted": count, "kind": id})
		} else {
			fmt.Fprintf(out, "%d new %s record(s) accepted. Repeated records are ignored.\n", count, id)
		}
		return 0
	}
	if command == "metrics" || command == "history" || command == "disk" || command == "events" || command == "deployments" || command == "logs" || command == "diff" || command == "incidents" || command == "readiness" || command == "security" || command == "orphans" || command == "ownership" || command == "costs" || command == "capacity" || command == "recovery" || command == "impact" || command == "preflight" || command == "capsule" {
		if *asJSON || command == "logs" || command == "capsule" {
			return query(ctx, c, command, id, *db, *since, out, errOut)
		}
		var data bytes.Buffer
		code := query(ctx, c, command, id, *db, *since, &data, errOut)
		if data.Len() > 0 {
			fmt.Fprintln(out, strings.Join(console.FormatJSON(command, data.Bytes()), "\n"))
		}
		return code
	}
	if command == "sites" {
		if *asJSON {
			if encoder.Encode(sites) != nil {
				return 1
			}
			return 0
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SITE\tNAME\tHOST\tRUNTIME\tDOMAIN")
		for _, s := range sites {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.HostID, s.Runtime.Type, s.Proxy.Domain)
		}
		w.Flush()
		return 0
	}
	reports := make([]model.Report, 0, len(sites))
	code := 0
	for _, s := range sites {
		var r model.Report
		if c.Discovering() {
			r = model.Report{HostID: s.HostID, SiteID: s.ID, Name: s.Name, Overall: "unknown"}
			for _, item := range observed {
				if item.SiteID == s.ID {
					r = item
					break
				}
			}
		} else {
			r = health.Collect(ctx, c, s, platform.Command{Host: c.Host})
		}
		reports = append(reports, r)
		if r.Overall != "healthy" {
			code = 2
		}
	}
	if *asJSON {
		if encoder.Encode(reports) != nil {
			return 1
		}
		return code
	}
	for _, r := range reports {
		fmt.Fprintf(out, "\n%s  %s\nObserved %s | %s\n", r.Name, strings.ToUpper(r.Overall), r.ObservedAt.Format("2006-01-02T15:04:05Z"), r.Vantage)
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, check := range r.Checks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", check.Layer, strings.ToUpper(string(check.State)), check.Summary)
		}
		w.Flush()
		if command == "doctor" {
			for _, d := range r.Diagnoses {
				fmt.Fprintf(out, "\nDiagnosis: %s\nConfidence: %s | Layer: %s\n", d.Finding, d.Confidence, d.Layer)
				for _, ev := range d.Evidence {
					fmt.Fprintf(out, "  %s\n", ev)
				}
				fmt.Fprintln(out, d.Investigation)
			}
		}
	}
	return code
}
