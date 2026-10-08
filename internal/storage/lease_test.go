package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectionLeaseCoordinatesIndependentDatabaseConnections(t *testing.T) {
	file := filepath.Join(t.TempDir(), "history.db")
	a, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := context.Background()
	now := time.Now()
	if ok, err := a.AcquireLease(ctx, "h", "disk", "a", now, time.Minute); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := b.AcquireLease(ctx, "h", "disk", "b", now, time.Minute); ok || err != nil {
		t.Fatal("two processes acquired one scan", ok, err)
	}
	if err := b.ReleaseLease(ctx, "h", "disk", "b"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := b.AcquireLease(ctx, "h", "disk", "b", now, time.Minute); ok {
		t.Fatal("wrong owner released another process's lease")
	}
	if ok, err := b.AcquireLease(ctx, "h", "disk", "b", now.Add(2*time.Minute), time.Minute); !ok || err != nil {
		t.Fatal("crashed owner's lease did not expire", ok, err)
	}
}
