// Package platform is the single boundary for read-only host inspection.
package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type Command struct{ Host config.Host }

func Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

type boundedBuffer struct {
	data     []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 4*1024*1024 - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.overflow = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (r Command) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	includeStderr := name == "docker" && len(args) > 0 && args[0] == "logs"
	partialAccounting := name == "du"
	if r.Host.SSH != "" {
		parts := []string{Quote(name)}
		for _, arg := range args {
			parts = append(parts, Quote(arg))
		}
		sshArgs := []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=5"}
		if r.Host.IdentityFile != "" {
			sshArgs = append(sshArgs, "-i", r.Host.IdentityFile)
		}
		sshArgs = append(sshArgs, "--", r.Host.SSH, strings.Join(parts, " "))
		name, args = "ssh", sshArgs
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// Do not echo arbitrary command stderr: it can contain application secrets.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("inspection timed out or was cancelled")
		}
		if partialAccounting && !stdout.overflow {
			return stdout.data, fmt.Errorf("directory accounting incomplete")
		}
		return nil, fmt.Errorf("inspection command failed (%s)", err)
	}
	if includeStderr {
		stdout.Write(stderr.data)
	}
	if stdout.overflow || (includeStderr && stderr.overflow) {
		return nil, fmt.Errorf("inspection output exceeded 4 MiB")
	}
	return stdout.data, nil
}
