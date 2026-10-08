package collector

import (
	"context"
	"errors"
	"testing"
)

func TestKernelReaderRejectsCommandsAndNonCounterFiles(t *testing.T) {
	for _, probe := range []struct{ name, path string }{{"cat", "/etc/passwd"}, {"sh", "/proc/stat"}, {"cat", "/proc/../etc/passwd"}} {
		if data, err := (hostFiles{}).Run(context.Background(), probe.name, probe.path); err == nil || len(data) > 0 {
			t.Fatal("kernel sampler could execute a command or read unrelated files")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (hostFiles{}).Run(ctx, "cat", "/proc/stat"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled sampling continued")
	}
}
