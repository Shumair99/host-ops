package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/health"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
)

const Help = `Amadeus HostOps — read-only production website operations

Usage: hostops <command> [site] [options]

Commands:
  sites             List registered production properties
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
  capacity          Assess measured resource usage and history limits
  recovery [site]   Assess backup and reported restore evidence
  preflight <target> Read-only reboot or site maintenance assessment
  impact <resource> Simulate the registered dependency blast radius

Options:
  --config FILE     Manifest (default: HOSTOPS_CONFIG or hostops.json)
  --json            Machine-readable output

Exit codes: 0 healthy/list, 1 usage/configuration error, 2 unhealthy or unknown.
`

func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, Help)
		return 0
	}
	command := args[0]
	if command != "sites" && command != "status" && command != "doctor" && command != "metrics" && command != "history" && command != "disk" && command != "events" && command != "deployments" && command != "logs" && command != "diff" && command != "incidents" && command != "readiness" && command != "security" && command != "orphans" && command != "ownership" && command != "costs" && command != "capacity" && command != "recovery" && command != "impact" && command != "preflight" && command != "capsule" {
		fmt.Fprintf(errOut, "unknown command %q\n", command)
		return 1
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	defaultPath := os.Getenv("HOSTOPS_CONFIG")
	if defaultPath == "" {
		defaultPath = "hostops.json"
	}
	path := flags.String("config", defaultPath, "site manifest")
	asJSON := flags.Bool("json", false, "JSON output")
	db := flags.String("db", "local/hostops.db", "history database")
	since := flags.String("since", "24h", "history window")
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
	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	selectionID := id
	if command == "ownership" || command == "impact" || command == "preflight" || command == "capsule" {
		selectionID = ""
	}
	sites, err := c.Select(selectionID)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if command == "metrics" || command == "history" || command == "disk" || command == "events" || command == "deployments" || command == "logs" || command == "diff" || command == "incidents" || command == "readiness" || command == "security" || command == "orphans" || command == "ownership" || command == "costs" || command == "capacity" || command == "recovery" || command == "impact" || command == "preflight" || command == "capsule" {
		return query(ctx, c, command, id, *db, *since, out, errOut)
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
		r := health.Collect(ctx, c, s, platform.Command{Host: c.Host})
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
