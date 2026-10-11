package collector

import (
	"context"
	"fmt"
	"os"
)

// The Linux collector samples kernel counters directly, without forking cat
// processes or opening additional SSH connections for the one-second stream.
type hostFiles struct{}

func (hostFiles) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name != "cat" || len(args) != 1 {
		return nil, fmt.Errorf("unsupported kernel counter read")
	}
	switch args[0] {
	case "/proc/stat", "/proc/meminfo", "/proc/uptime", "/proc/loadavg", "/proc/net/dev", "/proc/pressure/cpu", "/proc/pressure/memory", "/proc/pressure/io":
		return os.ReadFile(args[0])
	}
	return nil, fmt.Errorf("unsupported kernel counter path")
}
