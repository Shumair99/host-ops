package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestStoredStatusAndDoctorRejectStaleHealthyEvidence(t *testing.T) {
	dir := t.TempDir()
	settings, path := filepath.Join(dir, "settings.json"), filepath.Join(dir, "private", "history.db")
	data, _ := json.Marshal(config.Config{Host: config.Host{ID: "h"}})
	if err := os.WriteFile(settings, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := s.ReconcileInventory(context.Background(), discovery.Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []discovery.Source{{ID: "p", State: "ready", Mode: "active"}}, Routes: []discovery.Route{{ID: "r", SourceID: "p", Hosts: []string{"example.com"}, Kind: "response", Scheme: "https"}}})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Reports(context.Background(), "h", []model.Report{{HostID: "h", SiteID: registry.Sites[0].ID, Overall: "healthy", ObservedAt: time.Now().Add(-time.Hour), Checks: []model.Check{{Layer: "public_http", State: model.Pass}}}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	for _, command := range []string{"status", "doctor"} {
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{command, "--config", settings, "--db", path, "--no-collect", "--json"}, &out, &errOut)
		var reports []model.Report
		if code != 2 || json.Unmarshal(out.Bytes(), &reports) != nil || len(reports) != 1 || reports[0].Overall != "unknown" || reports[0].Checks[0].State != model.Unknown {
			t.Fatal(code, out.String(), errOut.String())
		}
		out.Reset()
		errOut.Reset()
		if code := Run(context.Background(), []string{command, "--config", settings, "--db", path, "--no-collect", "--max-age", "2h", "--json"}, &out, &errOut); code != 0 {
			t.Fatal("max-age ignored", code, errOut.String())
		}
	}
}

func TestCLIEventsUsesSameMergedTimelineAsDashboard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, kind := range []string{"event", "change", "deployment", "incident"} {
		if _, err := s.Add(ctx, "h", "s", kind, time.Now().Add(time.Duration(-i)*time.Minute), map[string]string{"id": kind}); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := s.Activity(ctx, "h", "", time.Now().Add(-time.Hour), 1000)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	var out, errOut bytes.Buffer
	if code := query(ctx, config.Config{Host: config.Host{ID: "h"}}, "events", "", path, "1h", &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var got []storage.Record
	if json.Unmarshal(out.Bytes(), &got) != nil || len(got) != len(expected) || len(got) != 4 {
		t.Fatal(out.String())
	}
	for i := range got {
		if got[i].ID != expected[i].ID {
			t.Fatal("timeline differs", got, expected)
		}
	}
}
