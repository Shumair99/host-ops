package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func windowPoint(host, site string, start time.Time, seconds int, w metricWindow) metrics.HistoryPoint {
	p := metrics.HistoryPoint{HostID: host, SiteID: site, Start: start, End: start.Add(time.Duration(seconds) * time.Second), ResolutionSeconds: seconds, Samples: w.N, FirstAt: start.Add(time.Duration(w.F)), LastAt: start.Add(time.Duration(w.L)), Values: map[string]metrics.Statistic{}, ObservedSeconds: []int{}, Unavailable: w.U}
	for key, value := range w.V {
		p.Values[key] = metrics.Statistic{Count: int(value[0]), Mean: value[1], Min: value[2], Max: value[3], First: value[4], Last: value[5], FirstAt: start.Add(time.Duration(value[6])), LastAt: start.Add(time.Duration(value[7]))}
	}
	for i := 0; i < seconds; i++ {
		if w.C[i/8]&(1<<(i%8)) != 0 {
			p.ObservedSeconds = append(p.ObservedSeconds, i)
		}
	}
	return p
}

func pointFromRecord(r Record) (metrics.HistoryPoint, bool, error) {
	_, seconds, summary := summaryKind(r.Kind)
	sample := observation{id: r.ID, at: r.ObservedAt, data: r.Data}
	if summary {
		w, err := decodeWindow(sample, seconds)
		if err != nil {
			return metrics.HistoryPoint{}, false, err
		}
		return windowPoint(r.HostID, r.SiteID, r.ObservedAt, seconds, w), true, nil
	}
	w, usable := rawMetric(sample, 60, r.HostID, r.SiteID)
	if !usable {
		return metrics.HistoryPoint{}, false, nil
	}
	p := windowPoint(r.HostID, r.SiteID, r.ObservedAt.Truncate(time.Minute), 60, w)
	p.Start, p.End, p.ResolutionSeconds, p.ObservedSeconds = r.ObservedAt, r.ObservedAt, 0, []int{0}
	return p, true, nil
}

// MetricHistory returns explicitly typed raw readings and/or summaries, newest
// measured time first. Intersecting summary windows are returned whole; their
// statistics are not invented for the narrower requested time range.
func (s *Store) MetricHistory(ctx context.Context, host, site, kind string, since time.Time, resolution string, limit int) ([]metrics.HistoryPoint, error) {
	if kind != "metrics" && kind != "host_metrics" {
		return nil, fmt.Errorf("metric history kind must be metrics or host_metrics")
	}
	kinds := []string{kind, kind + "_5m", kind + "_30m"}
	switch resolution {
	case "", "auto":
	case "raw":
		kinds = kinds[:1]
	case "5m":
		kinds = kinds[1:2]
	case "30m":
		kinds = kinds[2:]
	default:
		return nil, fmt.Errorf("metric history resolution must be auto, raw, 5m or 30m")
	}
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := []metrics.HistoryPoint{}
	for _, k := range kinds {
		_, seconds, _ := summaryKind(k)
		records, err := queryBlocksTx(ctx, tx, host, site, k, since.Add(-time.Duration(seconds)*time.Second), limit, false, false, true)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			p, usable, err := pointFromRecord(r)
			if err != nil {
				return nil, err
			}
			if usable && !p.LastAt.Before(since) {
				out = append(out, p)
			}
		}
	}
	// The protected last raw reading can share a time window with older folded
	// readings. Fold that one reading into the returned window, without changing
	// the saved raw head or counting it twice in historical analysis.
	if resolution == "" || resolution == "auto" {
		for i := 0; i < len(out); i++ {
			p := out[i]
			if p.ResolutionSeconds != 0 {
				continue
			}
			for j := 0; j < len(out); j++ {
				q := &out[j]
				if q.ResolutionSeconds == 0 || p.SiteID != q.SiteID || p.Start.Before(q.Start) || !p.Start.Before(q.End) {
					continue
				}
				w := pointWindow(*q)
				source := pointWindow(p)
				if err := mergeWindow(&w, source, p.Start.Sub(q.Start), q.ResolutionSeconds); err != nil {
					return nil, err
				}
				*q = windowPoint(q.HostID, q.SiteID, q.Start, q.ResolutionSeconds, w)
				out = append(out[:i], out[i+1:]...)
				i--
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastAt.After(out[j].LastAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func pointWindow(p metrics.HistoryPoint) metricWindow {
	seconds := p.ResolutionSeconds
	if seconds == 0 {
		seconds = 1
	}
	w := metricWindow{N: p.Samples, F: p.FirstAt.Sub(p.Start).Nanoseconds(), L: p.LastAt.Sub(p.Start).Nanoseconds(), V: map[string]metricTuple{}, C: make([]byte, (seconds+7)/8), U: p.Unavailable}
	for key, v := range p.Values {
		w.V[key] = metricTuple{float64(v.Count), v.Mean, v.Min, v.Max, v.First, v.Last, float64(v.FirstAt.Sub(p.Start).Nanoseconds()), float64(v.LastAt.Sub(p.Start).Nanoseconds())}
	}
	for _, bit := range p.ObservedSeconds {
		w.C[bit/8] |= 1 << (bit % 8)
	}
	return w
}

func (s *Store) OldestMetricPoint(ctx context.Context, host, kind string, since time.Time) (*metrics.HistoryPoint, error) {
	if kind != "metrics" && kind != "host_metrics" {
		return nil, fmt.Errorf("invalid metric history kind")
	}
	var oldest *metrics.HistoryPoint
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, k := range []string{kind, kind + "_5m", kind + "_30m"} {
		_, seconds, _ := summaryKind(k)
		records, err := queryBlocksTx(ctx, tx, host, "", k, since.Add(-time.Duration(seconds)*time.Second), 1, true, true, false)
		if err != nil {
			return nil, err
		}
		for _, r := range records {
			p, usable, err := pointFromRecord(r)
			if err != nil {
				return nil, err
			}
			if usable && !p.LastAt.Before(since) && (oldest == nil || p.FirstAt.Before(oldest.FirstAt)) {
				oldest = &p
			}
		}
	}
	return oldest, nil
}
