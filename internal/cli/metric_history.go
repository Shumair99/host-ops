package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/storage"
)

func metricHistoryExport(ctx context.Context, c config.Config, site, path, since, resolution string, asJSON bool, out, errOut io.Writer) int {
	duration, err := time.ParseDuration(since)
	if err != nil || duration <= 0 {
		fmt.Fprintln(errOut, "since must be a positive duration")
		return 1
	}
	if resolution != "auto" && resolution != "raw" && resolution != "5m" && resolution != "30m" {
		fmt.Fprintln(errOut, "resolution must be auto, raw, 5m or 30m")
		return 1
	}
	if _, err = os.Stat(path); err != nil {
		fmt.Fprintln(errOut, "History database does not exist; run the agent first")
		return 1
	}
	s, err := storage.Open(path)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer s.Close()
	if err = s.Prune(ctx, c.History); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	kind := "host_metrics"
	if site != "" {
		kind = "metrics"
	}
	points, err := s.MetricHistory(ctx, c.Host.ID, site, kind, time.Now().Add(-duration), resolution, 1000)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err = encoder.Encode(points); err != nil {
			return 1
		}
		return 0
	}
	if len(points) == 0 {
		fmt.Fprintln(out, "No retained metrics at this resolution.")
		return 0
	}
	fmt.Fprintln(out, "Saved metrics: newest 1,000 points; summary windows intersecting --since are shown whole.")
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "OBSERVED\tDETAIL\tSAMPLES\tMETRIC\tMEAN\tPEAK\tKNOWN")
	for _, point := range points {
		detail := "raw"
		if point.ResolutionSeconds > 0 {
			detail = (time.Duration(point.ResolutionSeconds) * time.Second).String()
		}
		keys := make([]string, 0, len(point.Values))
		for key := range point.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := point.Values[key]
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%.6g\t%.6g\t%d\n", point.LastAt.UTC().Format(time.RFC3339), detail, point.Samples, console.Safe(key), value.Mean, value.Max, value.Count)
		}
		if len(keys) == 0 {
			fmt.Fprintf(w, "%s\t%s\t%d\tUnavailable\t-\t-\t0\n", point.LastAt.UTC().Format(time.RFC3339), detail, point.Samples)
		}
	}
	if err = w.Flush(); err != nil {
		return 1
	}
	return 0
}
