package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/Shumair99/host-ops/internal/watcher"
)

func main() {
	manifest := flag.String("config", "hostops.json", "site manifest")
	db := flag.String("db", "local/watcher.db", "independent SQLite history")
	interval := flag.Duration("interval", 30*time.Second, "check interval")
	failures := flag.Int("failures", 3, "consecutive failures to open incident")
	recoveries := flag.Int("recoveries", 2, "consecutive healthy checks to resolve")
	once := flag.Bool("once", false, "one collection cycle")
	endpoint := flag.String("api", "", "optional private agent/tunnel URL")
	flag.Parse()
	if *interval < 5*time.Second || *failures < 1 || *recoveries < 1 {
		log.Fatal("interval >=5s and thresholds >=1 are required")
	}
	c, err := config.Load(*manifest)
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.Open(*db)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	for {
		reports, err := watcher.Cycle(ctx, c, store, *failures, *recoveries, os.Getenv("HOSTOPS_WEBHOOK"))
		if err != nil {
			log.Fatal(err)
		}
		if *endpoint != "" {
			if err = watcher.Publish(ctx, *endpoint, os.Getenv("HOSTOPS_TOKEN"), reports); err != nil {
				log.Print(err)
			}
		}
		if *once {
			json.NewEncoder(os.Stdout).Encode(reports)
			return
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
