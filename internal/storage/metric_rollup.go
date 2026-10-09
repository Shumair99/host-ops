package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
)

// Fixed-size tuples avoid repeating eight field names per metric/window.
// [count, mean, min, max, first, last, firstOffsetNS, lastOffsetNS].
type metricTuple [8]float64

// One/two measured values already determine their mean and extrema. Keep their
// endpoints once rather than storing the same numbers four more times.
func (v metricTuple) MarshalJSON() ([]byte, error) {
	if v[0] == 1 {
		return json.Marshal([3]float64{1, v[4], v[6]})
	}
	if v[0] == 2 {
		return json.Marshal([5]float64{2, v[4], v[5], v[6], v[7]})
	}
	return json.Marshal([8]float64(v))
}

func (v *metricTuple) UnmarshalJSON(data []byte) error {
	var values []float64
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	switch len(values) {
	case 3:
		if values[0] != 1 {
			return fmt.Errorf("invalid single-value metric tuple")
		}
		*v = metricTuple{1, values[1], values[1], values[1], values[1], values[1], values[2], values[2]}
	case 5:
		if values[0] != 2 {
			return fmt.Errorf("invalid two-value metric tuple")
		}
		first, last := values[1], values[2]
		*v = metricTuple{2, first*.5 + last*.5, min(first, last), max(first, last), first, last, values[3], values[4]}
	case 8:
		copy(v[:], values)
	default:
		return fmt.Errorf("invalid metric tuple length")
	}
	return nil
}

type metricWindow struct {
	N int                    `json:"n"`
	F int64                  `json:"f"`
	L int64                  `json:"l"`
	V map[string]metricTuple `json:"v"`
	C []byte                 `json:"c"`
	U map[string]int         `json:"u,omitempty"`
}

func summaryKind(kind string) (string, int, bool) {
	switch kind {
	case "metrics_5m", "host_metrics_5m":
		return strings.TrimSuffix(kind, "_5m"), 300, true
	case "metrics_30m", "host_metrics_30m":
		return strings.TrimSuffix(kind, "_30m"), 1800, true
	}
	return kind, 0, false
}

func summaryName(kind string, seconds int) string {
	if seconds == 300 {
		return kind + "_5m"
	}
	return kind + "_30m"
}

func rawMetric(sample observation, seconds int, owner ...string) (metricWindow, bool) {
	var value metrics.Sample
	decoder := json.NewDecoder(bytes.NewReader(sample.data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.Values == nil || !value.ObservedAt.Equal(sample.at) {
		// Unknown record shapes remain raw through the configured retention.
		return metricWindow{}, false
	}
	if len(owner) == 2 && (value.HostID != "" && value.HostID != owner[0] || value.SiteID != "" && value.SiteID != owner[1]) {
		return metricWindow{}, false
	}
	start := sample.at.Truncate(time.Duration(seconds) * time.Second)
	offset := sample.at.Sub(start).Nanoseconds()
	w := metricWindow{N: 1, F: offset, L: offset, V: map[string]metricTuple{}, C: make([]byte, (seconds+7)/8), U: map[string]int{}}
	position := int(offset / int64(time.Second))
	w.C[position/8] |= 1 << (position % 8)
	for name, n := range value.Values {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return metricWindow{}, false
		}
		w.V[name] = metricTuple{1, n, n, n, n, n, float64(offset), float64(offset)}
	}
	for _, reason := range value.Unavailable {
		w.U[reason] = 1
	}
	return w, true
}

func validateWindow(w metricWindow, seconds int) error {
	end := int64(seconds) * int64(time.Second)
	if w.N < 1 || w.N > 1_000_000_000 || w.F < 0 || w.L < w.F || w.L >= end || len(w.C) != (seconds+7)/8 || w.V == nil {
		return fmt.Errorf("invalid metric window bounds")
	}
	for _, value := range w.V {
		for _, n := range value {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return fmt.Errorf("non-finite metric summary")
			}
		}
		if value[0] < 1 || value[0] > float64(w.N) || value[0] != math.Trunc(value[0]) || value[2] > value[3] || value[1] < value[2] || value[1] > value[3] || value[4] < value[2] || value[4] > value[3] || value[5] < value[2] || value[5] > value[3] || value[6] < float64(w.F) || value[7] < value[6] || value[7] > float64(w.L) || value[6] != math.Trunc(value[6]) || value[7] != math.Trunc(value[7]) {
			return fmt.Errorf("invalid metric statistic")
		}
		if value[0] == 1 && (value[1] != value[4] || value[2] != value[4] || value[3] != value[4] || value[5] != value[4] || value[6] != value[7]) || value[0] == 2 && (value[1] != value[4]*.5+value[5]*.5 || value[2] != min(value[4], value[5]) || value[3] != max(value[4], value[5])) {
			return fmt.Errorf("invalid small-count metric statistic")
		}
		for _, offset := range []float64{value[6], value[7]} {
			bit := int(offset / float64(time.Second))
			if w.C[bit/8]&(1<<(bit%8)) == 0 {
				return fmt.Errorf("metric endpoint has no coverage")
			}
		}
	}
	for _, n := range w.U {
		if n < 1 || n > w.N {
			return fmt.Errorf("invalid unavailable count")
		}
	}
	coverage := 0
	for _, value := range w.C {
		coverage += bits.OnesCount8(value)
	}
	first, last := int(w.F/int64(time.Second)), int(w.L/int64(time.Second))
	if coverage < 1 || coverage > w.N || w.C[first/8]&(1<<(first%8)) == 0 || w.C[last/8]&(1<<(last%8)) == 0 {
		return fmt.Errorf("invalid metric coverage count")
	}
	for bit := seconds; bit < len(w.C)*8; bit++ {
		if w.C[bit/8]&(1<<(bit%8)) != 0 {
			return fmt.Errorf("invalid metric coverage")
		}
	}
	return nil
}

// Merge by measured sample count, retaining peaks and exact measured endpoints.
// Coverage is unioned, so an interrupted collector never turns into filled time.
func mergeWindow(dst *metricWindow, src metricWindow, offset time.Duration, seconds int) error {
	shift := offset.Nanoseconds()
	if shift < 0 || src.L+shift >= int64(seconds)*int64(time.Second) {
		return fmt.Errorf("metric window does not fit target")
	}
	if dst.N == 0 {
		dst.F, dst.L, dst.V, dst.C, dst.U = src.F+shift, src.L+shift, map[string]metricTuple{}, make([]byte, (seconds+7)/8), map[string]int{}
	}
	if dst.N > 1_000_000_000-src.N {
		return fmt.Errorf("metric summary count exceeds limit")
	}
	dst.N += src.N
	dst.F, dst.L = min(dst.F, src.F+shift), max(dst.L, src.L+shift)
	for name, value := range src.V {
		value[6] += float64(shift)
		value[7] += float64(shift)
		previous, exists := dst.V[name]
		if !exists {
			dst.V[name] = value
			continue
		}
		count := previous[0] + value[0]
		previous[1] = previous[1]*(previous[0]/count) + value[1]*(value[0]/count)
		previous[2], previous[3] = min(previous[2], value[2]), max(previous[3], value[3])
		// Clamp only round-off in a derived mean, never any original extrema.
		previous[1] = max(previous[2], min(previous[3], previous[1]))
		if value[6] < previous[6] {
			previous[4], previous[6] = value[4], value[6]
		}
		if value[7] >= previous[7] {
			previous[5], previous[7] = value[5], value[7]
		}
		previous[0] = count
		dst.V[name] = previous
	}
	for position := 0; position < len(src.C)*8; position++ {
		if src.C[position/8]&(1<<(position%8)) != 0 {
			bit := position + int(offset/time.Second)
			if bit >= seconds {
				return fmt.Errorf("metric coverage exceeds target")
			}
			dst.C[bit/8] |= 1 << (bit % 8)
		}
	}
	for name, count := range src.U {
		dst.U[name] += count
	}
	return validateWindow(*dst, seconds)
}

func decodeWindow(sample observation, seconds int) (metricWindow, error) {
	var w metricWindow
	if err := json.Unmarshal(sample.data, &w); err != nil {
		return w, err
	}
	return w, validateWindow(w, seconds)
}

func summaryAt(ctx context.Context, tx *sql.Tx, host, site, kind string, at time.Time) (block, []observation, int, error) {
	timestamp := at.UTC().Format(timestampLayout)
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.stream_id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding FROM blocks b JOIN streams s ON s.id=b.stream_id WHERE s.host_id=? AND s.site_id=? AND s.kind=? AND b.first_at<=? AND b.last_at>=?`, host, site, kind, timestamp, timestamp)
	if err != nil {
		return block{}, nil, -1, err
	}
	defer rows.Close()
	for rows.Next() {
		var b block
		if err = rows.Scan(&b.id, &b.streamID, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding); err != nil {
			return b, nil, -1, err
		}
		if err = loadPayload(ctx, tx, &b); err != nil {
			return b, nil, -1, err
		}
		samples, err := b.observations()
		if err != nil {
			return b, nil, -1, err
		}
		for i, sample := range samples {
			if sample.at.Equal(at) {
				return b, samples, i, nil
			}
		}
	}
	return block{}, nil, -1, rows.Err()
}

func saveWindow(ctx context.Context, tx *sql.Tx, host, site, kind string, at time.Time, seconds int, w metricWindow) error {
	kind = summaryName(kind, seconds)
	b, samples, index, err := summaryAt(ctx, tx, host, site, kind, at)
	if err != nil {
		return err
	}
	if index >= 0 {
		previous, err := decodeWindow(samples[index], seconds)
		if err != nil {
			return err
		}
		if err = mergeWindow(&previous, w, 0, seconds); err != nil {
			return err
		}
		w = previous
	}
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	if index < 0 {
		_, err = insertObservation(ctx, tx, host, site, kind, at, data, 0)
		return err
	}
	samples[index].data = data
	if err = replaceBlock(ctx, tx, b, samples); err != nil {
		return err
	}
	packed, err := packBlock(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE streams SET latest_data=? WHERE id=? AND latest_id=?`, packed, b.streamID, samples[index].id)
	return err
}

// Summaries and source removal commit together. A failed consolidation leaves
// all original readings intact. Working memory is bounded to one source block.
func rollupMetrics(ctx context.Context, tx *sql.Tx, policy config.History, now time.Time) error {
	rawEnd := now.Add(-24 * time.Hour).UTC().Truncate(5 * time.Minute)
	minuteEnd := now.Add(-7 * 24 * time.Hour).UTC().Truncate(30 * time.Minute)
	retainedAfter := now.Add(-time.Duration(policy.MetricsDays) * 24 * time.Hour)
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.stream_id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,s.host_id,s.site_id,s.kind,s.latest_id FROM blocks b JOIN streams s ON s.id=b.stream_id WHERE (?=1 AND s.kind IN ('metrics','host_metrics') AND b.first_at<?) OR (s.kind IN ('metrics_5m','host_metrics_5m') AND b.first_at<?) ORDER BY b.first_at,b.first_id`, policy.MetricDetail == "tiered", rawEnd.Format(timestampLayout), minuteEnd.Format(timestampLayout))
	if err != nil {
		return err
	}
	type candidate struct {
		block
		protected int64
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.streamID, &c.firstID, &c.lastID, &c.firstAt, &c.lastAt, &c.count, &c.encoding, &c.host, &c.site, &c.kind, &c.protected); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range candidates {
		if err = ctx.Err(); err != nil {
			return err
		}
		// Earlier source blocks can merge into a summary block that was also
		// selected above. Refresh its descriptor after those writes.
		err = tx.QueryRowContext(ctx, `SELECT first_id,last_id,first_at,last_at,samples,encoding FROM blocks WHERE id=?`, c.id).Scan(&c.firstID, &c.lastID, &c.firstAt, &c.lastAt, &c.count, &c.encoding)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if err = loadPayload(ctx, tx, &c.block); err != nil {
			return err
		}
		samples, err := c.observations()
		if err != nil {
			return err
		}
		base, width, isSummary := summaryKind(c.kind)
		type key struct {
			start   time.Time
			seconds int
		}
		windows := map[key]*metricWindow{}
		remaining := make([]observation, 0, len(samples))
		removed := []any{}
		for _, sample := range samples {
			if !isSummary && sample.id == c.protected || sample.at.Before(retainedAfter) || !isSummary && !sample.at.Before(rawEnd) || isSummary && !sample.at.Before(minuteEnd) {
				remaining = append(remaining, sample)
				continue
			}
			seconds := 300
			if sample.at.Before(minuteEnd) {
				seconds = 1800
			}
			start := sample.at.Truncate(time.Duration(seconds) * time.Second)
			var w metricWindow
			origin := start
			if isSummary {
				w, err = decodeWindow(sample, width)
				if err != nil {
					return err
				}
				origin = sample.at
			} else {
				var usable bool
				w, usable = rawMetric(sample, seconds, c.host, c.site)
				if !usable {
					remaining = append(remaining, sample)
					continue
				}
			}
			k := key{start, seconds}
			target := windows[k]
			if target == nil {
				target = &metricWindow{}
				windows[k] = target
			}
			if err = mergeWindow(target, w, origin.Sub(start), seconds); err != nil {
				return err
			}
			removed = append(removed, sample.id)
		}
		if len(removed) == 0 {
			continue
		}
		keys := make([]key, 0, len(windows))
		for k := range windows {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].start.Before(keys[j].start) })
		for _, k := range keys {
			if err = saveWindow(ctx, tx, c.host, c.site, base, k.start, k.seconds, *windows[k]); err != nil {
				return err
			}
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(removed)), ",")
		if _, err = tx.ExecContext(ctx, `DELETE FROM dedup WHERE record_id IN (`+marks+`)`, removed...); err != nil {
			return err
		}
		if err = replaceBlock(ctx, tx, c.block, remaining); err != nil {
			return err
		}
	}
	return compactMetricActives(ctx, tx)
}

// Older windows are closed to individual imports. Compress their partial
// blocks once after maintenance; the next maintenance can reopen them for
// appending/merging, instead of leaving days of incomplete blocks uncompressed.
func compactMetricActives(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT b.id FROM blocks b JOIN streams s ON s.id=b.stream_id WHERE b.encoding=0 AND s.kind IN ('metrics_5m','host_metrics_5m','metrics_30m','host_metrics_30m')`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT data FROM active_payloads WHERE block_id=?`, id).Scan(&raw); err != nil {
			return err
		}
		data, err := packSealedBlock(raw)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE active_payloads SET data=? WHERE block_id=?`, data, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE blocks SET encoding=2 WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}
