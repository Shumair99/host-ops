package agent

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Shumair99/host-ops/internal/storage"
)

func TestStreamProgressTracksPendingWorkUntilEveryOperationCompletes(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := newStreamProgress(ctx, db, "h")
	read := func(want ...string) {
		t.Helper()
		data, err := db.State(ctx, "h", "", "connection")
		if err != nil {
			t.Fatal(err)
		}
		var got Connection
		if err = json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !got.Connected || !slices.Equal(got.Pending, want) {
			t.Fatalf("connected=%v pending=%v want=%v", got.Connected, got.Pending, want)
		}
	}
	if err = p.ready(); err != nil {
		t.Fatal(err)
	}
	read("host_metrics", "inventory", "workload_metrics")
	if err = p.set("public_checks", true); err != nil {
		t.Fatal(err)
	}
	if err = p.set("inventory", false); err != nil {
		t.Fatal(err)
	}
	if err = p.set("host_metrics", false); err != nil {
		t.Fatal(err)
	}
	read("public_checks", "workload_metrics")
	// Completion does not depend on having useful or non-empty results.
	if err = p.set("workload_metrics", false); err != nil {
		t.Fatal(err)
	}
	read("public_checks")
	if err = p.set("public_checks", false); err != nil {
		t.Fatal(err)
	}
	read()
	// A second persistent-stream hello must not restart completed stages.
	if err = p.ready(); err != nil {
		t.Fatal(err)
	}
	read()
	if err = p.set("inventory", true); err != nil {
		t.Fatal(err)
	}
	read("inventory")
	if err = p.set("inventory", false); err != nil {
		t.Fatal(err)
	}
	read()
	if err = connection(ctx, db, "h", false, "Collector disconnected"); err != nil {
		t.Fatal(err)
	}
	data, _ := db.State(ctx, "h", "", "connection")
	var disconnected Connection
	if err = json.Unmarshal(data, &disconnected); err != nil || disconnected.Connected || len(disconnected.Pending) != 0 || disconnected.Issue == "" {
		t.Fatal("disconnect left a loading operation behind", disconnected, err)
	}
}

func TestHostTelemetryArrivingBeforeHelloIsNotResetToPending(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := newStreamProgress(ctx, db, "h")
	if err = p.set("host_metrics", false); err != nil {
		t.Fatal(err)
	}
	if err = p.ready(); err != nil {
		t.Fatal(err)
	}
	data, err := db.State(ctx, "h", "", "connection")
	if err != nil {
		t.Fatal(err)
	}
	var c Connection
	if err = json.Unmarshal(data, &c); err != nil || slices.Contains(c.Pending, "host_metrics") || len(c.Pending) != 2 {
		t.Fatal("handshake concealed already received host telemetry", c, err)
	}
}
