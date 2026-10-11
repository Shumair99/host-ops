//go:build linux

package collector

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"io"
	"testing"
	"time"
)

// Exercise the actual stream, including cancellation while probes are active.
// Keeping stdin open matches an attached dashboard rather than an EOF shutdown.
func TestStreamCanOmitHostSamplingWhileKeepingInventory(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{false: "session", true: "continuous"}[skip], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			inR, inW := io.Pipe()
			outR, outW := io.Pipe()
			done := make(chan error, 1)
			go func() { done <- Serve(ctx, inR, outW); outW.Close() }()
			go func() { <-ctx.Done(); inW.Close() }()
			if err := json.NewEncoder(inW).Encode(Request{Version: discovery.ProtocolVersion, Host: config.Host{ID: "h"}, SkipHostMetrics: skip}); err != nil {
				t.Fatal(err)
			}
			inventory, metrics, workloads := false, 0, 0
			inventoryPending, workloadPending := false, false
			decoder := json.NewDecoder(outR)
			for {
				var frame Message
				if decoder.Decode(&frame) != nil {
					break
				}
				if frame.Kind == "inventory" {
					if !inventoryPending {
						t.Fatal("inventory arrived without announcing the pending scan")
					}
					inventory = true
				}
				if frame.Kind == "inventory_pending" {
					inventoryPending = true
				}
				if frame.Kind == "workload_pending" {
					workloadPending = true
				}
				if frame.Kind == "workload_metrics" {
					if !workloadPending {
						t.Fatal("workload batch arrived without announcing loading")
					}
					workloads++
				}
				if frame.Kind == "host_metrics" {
					metrics++
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			outR.Close()
			inR.Close()
			if !inventory || workloads == 0 || (skip && metrics != 0) || (!skip && metrics == 0) {
				t.Fatalf("inventory=%v host frames=%d workload batches=%d skip=%v", inventory, metrics, workloads, skip)
			}
		})
	}
}
