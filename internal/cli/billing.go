package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Shumair99/host-ops/internal/billing"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/storage"
)

func billingCommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "Usage: hostops billing list|set|sync|show [--provider hetzner|hostinger|manual|self-hosted] [--resource ID] [--monthly AMOUNT --currency GBP] [--token-env NAME] [--config FILE] [--db FILE] [--json]")
		fmt.Fprintln(out, "Provider tokens stay in environment variables. Only GET requests are used. Manual zero is an explicit budget; self-hosted without a budget disables costs.")
		return 0
	}
	action := args[0]
	if action != "list" && action != "set" && action != "sync" && action != "show" {
		fmt.Fprintln(errOut, "unknown billing action")
		return 1
	}
	flags := flag.NewFlagSet("billing", flag.ContinueOnError)
	flags.SetOutput(errOut)
	defaultConfig := os.Getenv("HOSTOPS_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "local/hostops.json"
		if _, err := os.Stat("hostops.json"); err == nil {
			defaultConfig = "hostops.json"
		}
	}
	file := flags.String("config", defaultConfig, "connection settings")
	dbPath := flags.String("db", "local/hostops.db", "local evidence database")
	provider := flags.String("provider", "", "billing source")
	resource := flags.String("resource", "", "exact provider server/subscription ID")
	tokenEnv := flags.String("token-env", "", "environment variable holding API token")
	monthly := flags.String("monthly", "", "manual monthly budget; zero is valid")
	currency := flags.String("currency", "", "ISO currency for manual budget")
	asJSON := flags.Bool("json", false, "JSON output")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return 1
	}
	if action == "list" {
		resources, err := billing.NewClient().Resources(ctx, *provider, *tokenEnv)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if *asJSON {
			return encodeBilling(out, resources)
		}
		fmt.Fprintln(out, "RESOURCE ID   NAME / PLAN / LOCATION")
		for _, r := range resources {
			fmt.Fprintf(out, "%s   %s / %s / %s\n", r.ID, console.Safe(r.Name), console.Safe(r.Plan), console.Safe(r.Location))
		}
		fmt.Fprintln(out, "Choose the resource belonging to this host, then run billing set --provider NAME --resource ID. Hardware size alone cannot prove billing identity.")
		return 0
	}
	c, err := config.Load(*file)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if action == "set" {
		b := &config.Billing{Provider: *provider, ResourceID: *resource, TokenEnv: *tokenEnv}
		if *monthly != "" {
			amount, err := strconv.ParseFloat(*monthly, 64)
			if err != nil {
				fmt.Fprintln(errOut, "invalid monthly budget")
				return 1
			}
			b.MonthlyAmount = &amount
		}
		if *currency != "" {
			c.Host.Currency = strings.ToUpper(*currency)
		}
		c.Host.Billing = b
		if err = c.Validate(); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			fmt.Fprintln(errOut, "billing settings cannot be encoded")
			return 1
		}
		// Publish a complete local manifest; no provider or VPS mutation occurs.
		tmp, err := os.CreateTemp(filepath.Dir(*file), ".hostops-billing-*")
		if err != nil {
			fmt.Fprintln(errOut, "cannot save billing settings")
			return 1
		}
		name := tmp.Name()
		defer os.Remove(name)
		if err = tmp.Chmod(0600); err == nil {
			_, err = tmp.Write(append(data, '\n'))
		}
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(name, *file)
		}
		if err != nil {
			fmt.Fprintln(errOut, "cannot publish billing settings")
			return 1
		}
		fmt.Fprintln(out, "Billing source saved. Restart the dashboard to load changed settings. API sources: run hostops billing sync to read and cache pricing.")
		return 0
	}
	if *provider != "" || *resource != "" || *monthly != "" || *currency != "" || *tokenEnv != "" {
		fmt.Fprintln(errOut, "sync/show uses the configured source; use billing set to change it")
		return 1
	}
	db, err := storage.Open(*dbPath)
	if err != nil {
		fmt.Fprintln(errOut, "billing evidence database unavailable")
		return 1
	}
	defer db.Close()
	r, err := billing.Resolve(ctx, c.Host, db, action == "sync")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if *asJSON {
		return encodeBilling(out, r)
	}
	data, _ := json.Marshal(r)
	fmt.Fprintln(out, strings.Join(console.FormatJSON("billing", data), "\n"))
	return 0
}

func encodeBilling(out io.Writer, value any) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(value) != nil {
		return 1
	}
	return 0
}
