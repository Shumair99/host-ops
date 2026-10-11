package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/Shumair99/host-ops/internal/aiexport"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/Shumair99/host-ops/internal/tui"
)

func exportAICommand(ctx context.Context, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("export-ai", flag.ContinueOnError)
	flags.SetOutput(errOut)
	manifest := flags.String("config", defaultConnectionConfig(), "site manifest")
	db := flags.String("db", "", "local history database")
	output := flags.String("output", "", "new Markdown file; existing files are kept")
	demo := flags.Bool("demo", false, "export isolated fictional observations without network access")
	asJSON := flags.Bool("json", false, "print saved-file metadata as JSON")
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() > 0 {
		fmt.Fprintln(errOut, "export-ai includes every site; it does not accept a site argument")
		return 1
	}
	var c config.Config
	var err error
	path := *db
	if *demo {
		c, path = tui.DemoConfig(), ":memory:"
	} else {
		c, err = config.Load(*manifest)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		var ok bool
		path, ok = resolveHistoryPath(path, errOut)
		if !ok {
			return 1
		}
	}
	store, err := storage.Open(path)
	if err != nil {
		fmt.Fprintln(errOut, "Export history unavailable:", err)
		return 1
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if *demo {
		if err = tui.SeedDemo(ctx, c, store, false); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	}
	source := tui.Source{Config: c, Store: store, Demo: *demo, MaxAge: model.DefaultMaxAge, Live: !*demo && c.Discovering()}
	doc, err := source.AIExport(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "Export could not capture observations:", err)
		return 1
	}
	data, err := aiexport.Render(doc)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	destination := *output
	if destination == "" {
		directory, err := aiexport.DefaultDirectory()
		if err != nil {
			fmt.Fprintln(errOut, "Choose a destination with --output FILE:", err)
			return 1
		}
		destination = filepath.Join(directory, aiexport.Filename(c.Host.ID, time.Now()))
	}
	saved, err := aiexport.Write(ctx, destination, data)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if *asJSON {
		if err = json.NewEncoder(out).Encode(map[string]any{"path": saved, "bytes": len(data), "views": len(doc.Sections), "demo": *demo}); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	} else {
		fmt.Fprintln(out, "Saved", saved)
	}
	return 0
}
