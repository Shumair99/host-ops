package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Shumair99/host-ops/internal/vpsagent"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: hostops-vps-agent serve --host-id ID | bridge")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		hostID := flags.String("host-id", "", "configured HostOps host identifier")
		if err := flags.Parse(os.Args[2:]); err != nil || flags.NArg() != 0 {
			os.Exit(2)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := vpsagent.Run(ctx, *hostID, vpsagent.DefaultHistoryPath, vpsagent.DefaultSocketPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "bridge":
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "bridge takes no arguments")
			os.Exit(2)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		if err := vpsagent.Bridge(ctx, vpsagent.DefaultSocketPath, os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown host agent command")
		os.Exit(2)
	}
}
