// Package collector provides a versioned inventory/telemetry stream over SSH stdio.
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/platform"
)

type Request struct {
	Version int         `json:"version"`
	Host    config.Host `json:"host"`
}
type Control struct {
	Kind string `json:"kind"`
}
type Message struct {
	Version    int                 `json:"version"`
	HostID     string              `json:"host_id"`
	Kind       string              `json:"kind"`
	ObservedAt time.Time           `json:"observed_at"`
	Inventory  *discovery.Snapshot `json:"inventory,omitempty"`
	Sample     *metrics.Sample     `json:"sample,omitempty"`
	Samples    []metrics.Sample    `json:"samples,omitempty"`
}

func Serve(parent context.Context, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("collector requires a protocol request")
	}
	if request.Version != discovery.ProtocolVersion {
		return fmt.Errorf("collector protocol version mismatch")
	}
	request.Host.SSH, request.Host.IdentityFile = "", ""
	c := config.Config{Host: request.Host}
	if err := c.Validate(); err != nil {
		return err
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("collector requires a Linux VPS")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	frames := make(chan Message, 8)
	wake := make(chan struct{}, 1)
	var wg sync.WaitGroup
	send := func(message Message) bool {
		message.Version, message.HostID = discovery.ProtocolVersion, c.Host.ID
		if message.ObservedAt.IsZero() {
			message.ObservedAt = time.Now().UTC()
		}
		select {
		case frames <- message:
			return true
		case <-ctx.Done():
			return false
		}
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		runner := platform.Command{Host: c.Host}
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		disk := map[string]float64{}
		lastDisk := time.Time{}
		for {
			sample := metrics.HostFast(ctx, runner, c.Host)
			if time.Since(lastDisk) >= 30*time.Second {
				values := metrics.RootDisk(ctx, runner)
				if len(values) > 0 {
					disk = values
					lastDisk = time.Now()
				}
			}
			for key, value := range disk {
				sample.Values[key] = value
			}
			if !send(Message{Kind: "host_metrics", Sample: &sample, ObservedAt: sample.ObservedAt}) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
	go func() {
		defer wg.Done()
		timer := time.NewTicker(30 * time.Second)
		defer timer.Stop()
		for {
			snapshot := Scan(ctx, c.Host)
			if !send(Message{Kind: "inventory", Inventory: &snapshot, ObservedAt: snapshot.ObservedAt}) {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			case <-wake:
			}
		}
	}()
	go func() {
		for {
			var control Control
			if decoder.Decode(&control) != nil {
				cancel()
				return
			}
			if control.Kind == "refresh" {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}
	}()
	encoder := json.NewEncoder(output)
	send(Message{Kind: "hello"})
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case frame := <-frames:
			if err := encoder.Encode(frame); err != nil {
				cancel()
				wg.Wait()
				return fmt.Errorf("collector stream closed")
			}
		}
	}
}
