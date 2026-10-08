package vpsagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func retained(samples []metrics.Sample, hostID string, now time.Time) []metrics.Sample {
	out := make([]metrics.Sample, 0, min(len(samples), MaxHistory))
	for _, sample := range samples {
		if ValidateSample(sample, hostID, now) != nil || sample.ObservedAt.Before(now.Add(-HistoryWindow)) {
			continue
		}
		out = append(out, sample)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObservedAt.Before(out[j].ObservedAt) })
	if len(out) > MaxHistory {
		out = out[len(out)-MaxHistory:]
	}
	return out
}

func loadHistory(path, hostID string, now time.Time) ([]metrics.Sample, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return []metrics.Sample{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxHistoryBytes {
		return nil, fmt.Errorf("VPS history file is unavailable or invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("VPS history file is unavailable")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("VPS history file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxHistoryBytes+1))
	if err != nil || len(data) > MaxHistoryBytes {
		return nil, fmt.Errorf("VPS history file exceeded its size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file historyFile
	if err = decoder.Decode(&file); err != nil || file.Version != ProtocolVersion || file.HostID != hostID || len(file.Samples) > MaxHistory {
		return nil, fmt.Errorf("VPS history file format rejected")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("VPS history file has trailing data")
	}
	for _, sample := range file.Samples {
		if err = ValidateSample(sample, hostID, now); err != nil {
			return nil, fmt.Errorf("VPS history sample rejected")
		}
	}
	return retained(file.Samples, hostID, now), nil
}

// cleanupHistoryTemps removes only regular files left by an interrupted
// atomic checkpoint. The service owns its state directory; unrelated entries
// and symlinks are preserved.
func cleanupHistoryTemps(historyPath string) error {
	dir := filepath.Dir(historyPath)
	opened, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot inspect VPS history directory")
	}
	entries, err := opened.ReadDir(65)
	closeErr := opened.Close()
	if err != nil && err != io.EOF {
		return fmt.Errorf("cannot inspect VPS history directory")
	}
	if closeErr != nil || len(entries) > 64 {
		return fmt.Errorf("VPS history directory exceeded its entry limit")
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".hostops-history-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("cannot inspect stale VPS history checkpoint")
		}
		if info.Mode().IsRegular() {
			if err = os.Remove(path); err != nil {
				return fmt.Errorf("cannot remove stale VPS history checkpoint")
			}
		}
	}
	return nil
}

func saveHistory(path, hostID string, samples []metrics.Sample, now time.Time) error {
	samples = retained(samples, hostID, now)
	data, err := json.Marshal(historyFile{Version: ProtocolVersion, HostID: hostID, Samples: samples})
	if err != nil || len(data) > MaxHistoryBytes {
		return fmt.Errorf("VPS history exceeded its size limit")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".hostops-history-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create VPS history checkpoint")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0640); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("cannot write VPS history checkpoint")
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("cannot publish VPS history checkpoint")
	}
	if directory, openErr := os.Open(dir); openErr == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
