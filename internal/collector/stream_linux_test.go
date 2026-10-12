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
			// A complete inventory scan can take longer than a few seconds on a
			// busy CI runner. Stop explicitly once the stream has demonstrated the
			// requested behavior, and retain a generous deadline for regressions.
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
			inventoryScans := 0
			refreshRequested, canceledDuringRefresh := false, false
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
					inventoryScans++
					if refreshRequested && inventoryScans > 1 {
						canceledDuringRefresh = true
						cancel()
					}
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
				if !refreshRequested && inventory && workloads > 0 && (skip || metrics > 0) {
					if err := json.NewEncoder(inW).Encode(Control{Kind: "refresh"}); err != nil {
						t.Fatalf("request inventory refresh: %v", err)
					}
					refreshRequested = true
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			outR.Close()
			inR.Close()
			if !inventory || workloads == 0 || (skip && metrics != 0) || (!skip && metrics == 0) || !refreshRequested || !canceledDuringRefresh {
				t.Fatalf("inventory=%v host frames=%d workload batches=%d skip=%v refresh=%v canceled during refresh=%v", inventory, metrics, workloads, skip, refreshRequested, canceledDuringRefresh)
			}
		})
	}
}
