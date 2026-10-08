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
	listen := flag.String("listen", "127.0.0.1:9090", "private management API")
	interval := flag.Duration("interval", 30*time.Second, "collection interval")
	once := flag.Bool("once", false, "collect once without serving API")
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
		err = agent.Run(ctx, c, store, *listen, os.Getenv("HOSTOPS_TOKEN"), *interval)
	}
	if err != nil {
		log.Fatal(err)
	}
}
