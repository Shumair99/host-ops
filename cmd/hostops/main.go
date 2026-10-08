package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Shumair99/host-ops/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
