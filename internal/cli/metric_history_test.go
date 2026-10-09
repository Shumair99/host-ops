package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestSavedMetricExportUsesLocalHistoryWithLabelledSummary(t *testing.T) {
	c, err := config.Load("../../examples/sites/docker.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hostops.json")
	data, _ := json.Marshal(c)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	dbpath := filepath.Join(dir, "history", "metrics.db")
	db, err := storage.Open(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(5 * time.Minute)
	old := now.Add(-2 * 24 * time.Hour)
	for _, pair := range []struct {
		at    time.Time
		value float64
	}{{old, 0}, {old.Add(15 * time.Second), 99}, {now, 1}} {
		_, err = db.Add(ctx, c.Host.ID, "", "host_metrics", pair.at, metrics.Sample{HostID: c.Host.ID, ObservedAt: pair.at, Values: map[string]float64{"cpu_percent": pair.value}})
		if err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	for _, asJSON := range []bool{false, true} {
		args := []string{"metrics", "--history", "--config", path, "--db", dbpath, "--since", "72h", "--resolution", "5m"}
		if asJSON {
			args = append(args, "--json")
		}
		var out, errOut bytes.Buffer
		if code := Run(ctx, args, &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
		if asJSON {
			var points []metrics.HistoryPoint
			if err = json.Unmarshal(out.Bytes(), &points); err != nil || len(points) != 1 || points[0].ResolutionSeconds != 300 || points[0].Samples != 2 || points[0].Values["cpu_percent"].Max != 99 || points[0].Values["cpu_percent"].Mean != 49.5 {
				t.Fatal(out.String(), err)
			}
		} else if !strings.Contains(out.String(), "5m0s") || !strings.Contains(out.String(), "PEAK") || !strings.Contains(out.String(), "49.5") {
			t.Fatal(out.String())
		}
	}
}
