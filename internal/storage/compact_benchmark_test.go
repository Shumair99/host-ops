package storage

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func benchmarkSamples() []observation {
	var samples []observation
	start := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 128; i++ {
		at := start.Add(time.Duration(i)*30*time.Second + time.Duration(i*7919)*time.Nanosecond)
		data, _ := json.Marshal(metrics.Sample{HostID: "h", SiteID: "s", ObservedAt: at, Values: map[string]float64{"cpu_percent": 5 + 4*math.Sin(float64(i)*0.17), "memory_used_bytes": float64(100000000 + (i*7919)%5000*4096)}, Unavailable: []string{}})
		samples = append(samples, observation{id: int64(i + 1), at: at, data: data})
	}
	return samples
}

func BenchmarkBlockCompressionLevels(b *testing.B) {
	raw := encodeFrames(benchmarkSamples())
	for _, level := range []int{1, 3, 6, 9} {
		b.Run(fmt.Sprint(level), func(b *testing.B) {
			var buffer bytes.Buffer
			w, err := zlib.NewWriterLevel(&buffer, level)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buffer.Reset()
				w.Reset(&buffer)
				w.Write(raw)
				w.Close()
			}
			b.ReportMetric(float64(buffer.Len()), "bytes/block")
		})
	}
}

func BenchmarkCompactStore(b *testing.B) {
	for _, operation := range []string{"append", "recent1000"} {
		b.Run(operation, func(b *testing.B) {
			s, err := Open(":memory:")
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			start := time.Now().UTC()
			if operation == "recent1000" {
				for i := 0; i < 4096; i++ {
					_, err = s.Add(ctx, "h", "s", "metrics", start.Add(time.Duration(i)*time.Second), map[string]any{"cpu_percent": float64(i % 99), "memory_used_bytes": 100000000 + i*4096})
					if err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if operation == "append" {
					_, err = s.Add(ctx, "h", "s", "metrics", start.Add(time.Duration(i)*time.Second), map[string]any{"cpu_percent": float64(i % 99), "memory_used_bytes": 100000000 + i*4096})
				} else {
					_, err = s.Query(ctx, "h", "s", "metrics", time.Time{}, 1000)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
