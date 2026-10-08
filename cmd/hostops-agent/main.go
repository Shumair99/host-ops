package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
)

func main() {
	manifest := flag.String("config", "hostops.json", "site manifest")
	db := flag.String("db", "local/hostops.db", "SQLite history")
	interval := flag.Duration("interval", 30*time.Second, "collection interval")
	once := flag.Bool("once", false, "collect once and exit")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	c, err := config.Load(*manifest)
	if err != nil {
		log.Fatal(err)
	}
	store, err := storage.Open(*db)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if *once {
		err = agent.Cycle(ctx, c, store)
	} else {
		log.Printf("headless collection for %s; Ctrl+C stops collection", c.Host.ID)
		err = agent.Run(ctx, c, store, *interval)
	}
	if err != nil {
		log.Fatal(err)
	}
}
