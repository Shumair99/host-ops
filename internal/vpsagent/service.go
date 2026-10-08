package vpsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

const (
	DefaultHistoryPath = "/var/lib/hostops/history.json"
	DefaultSocketPath  = "/run/hostops/agent.sock"
)

type server struct {
	hostID  string
	mu      sync.Mutex
	history []metrics.Sample
	clients map[chan metrics.Sample]struct{}
}

func newServer(hostID string, history []metrics.Sample) *server {
	return &server{hostID: hostID, history: retained(history, hostID, time.Now()), clients: map[chan metrics.Sample]struct{}{}}
}

func (s *server) subscribe() ([]metrics.Sample, chan metrics.Sample, func()) {
	s.mu.Lock()
	history := append([]metrics.Sample(nil), s.history...)
	updates := make(chan metrics.Sample, 1)
	s.clients[updates] = struct{}{}
	s.mu.Unlock()
	return history, updates, func() {
		s.mu.Lock()
		delete(s.clients, updates)
		s.mu.Unlock()
	}
}

func (s *server) publish(sample metrics.Sample, save bool) error {
	now := time.Now()
	if err := ValidateSample(sample, s.hostID, now); err != nil {
		return err
	}
	s.mu.Lock()
	if save {
		s.history = retained(append(s.history, sample), s.hostID, now)
	}
	for client := range s.clients {
		select {
		case client <- sample:
		default:
			select {
			case <-client:
			default:
			}
			select {
			case client <- sample:
			default:
			}
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *server) historySnapshot() []metrics.Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]metrics.Sample(nil), s.history...)
}

func (s *server) serveClient(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	requestData, err := readRequest(conn)
	if err != nil {
		return
	}
	if _, err = decodeRequest(requestData, s.hostID); err != nil {
		return
	}
	history, updates, unsubscribe := s.subscribe()
	defer unsubscribe()
	if err = writeFrame(conn, Frame{Version: ProtocolVersion, HostID: s.hostID, Kind: "history", Samples: history}); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case sample := <-updates:
			if err = writeFrame(conn, Frame{Version: ProtocolVersion, HostID: s.hostID, Kind: "host_metrics", Sample: &sample}); err != nil {
				return
			}
		}
	}
}

func (s *server) accept(ctx context.Context, listener net.Listener) error {
	var clients sync.WaitGroup
	clientSlots := make(chan struct{}, 4)
	defer clients.Wait()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("VPS collector socket accept failed")
		}
		select {
		case clientSlots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		clients.Add(1)
		go func() {
			defer clients.Done()
			defer func() { <-clientSlots }()
			s.serveClient(ctx, conn)
		}()
	}
}

// Run starts the low-overhead service. Only fixed /proc counters and root
// filesystem metadata are sampled; discovery, Docker, HTTP probes and file
// content scans are intentionally outside this service.
func Run(ctx context.Context, hostID, historyPath, socketPath string) error {
	if !hostIDPattern.MatchString(hostID) {
		return fmt.Errorf("invalid host id")
	}
	if historyPath == "" {
		historyPath = DefaultHistoryPath
	}
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	if err := validateServicePaths(historyPath, socketPath); err != nil {
		return err
	}
	unlock, err := lockService(socketPath + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if err = cleanupHistoryTemps(historyPath); err != nil {
		return err
	}
	history, err := loadHistory(historyPath, hostID, time.Now())
	if err != nil {
		return err
	}
	s := newServer(hostID, history)
	listener, err := listenSocket(socketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	serviceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- s.accept(serviceCtx, listener) }()
	sampler, err := NewSampler(hostID)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(SampleEvery)
	defer ticker.Stop()
	lastPersist := time.Now()
	for {
		select {
		case <-ctx.Done():
			final := sampler.Sample(context.Background())
			if err = s.publish(final, true); err == nil {
				err = saveHistory(historyPath, hostID, s.historySnapshot(), time.Now())
			}
			cancel()
			<-serveDone
			return err
		case err = <-serveDone:
			cancel()
			return err
		case <-ticker.C:
			sample := sampler.Sample(serviceCtx)
			persist := time.Since(lastPersist) >= HistoryEvery
			if err = s.publish(sample, persist); err != nil {
				cancel()
				<-serveDone
				return fmt.Errorf("VPS collector produced an invalid sample")
			}
			if persist {
				snapshot := s.historySnapshot()
				if err = saveHistory(historyPath, hostID, snapshot, time.Now()); err != nil {
					cancel()
					<-serveDone
					return err
				}
				lastPersist = time.Now()
			}
		}
	}
}

func validateServicePaths(historyPath, socketPath string) error {
	for _, path := range []string{historyPath, socketPath} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") || filepath.Clean(path) != path {
			return fmt.Errorf("VPS collector path rejected")
		}
	}
	if historyPath == socketPath {
		return fmt.Errorf("VPS collector paths overlap")
	}
	for _, dir := range []string{filepath.Dir(historyPath), filepath.Dir(socketPath)} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || !ownedServiceDirectory(info) {
			return fmt.Errorf("VPS collector directory permissions rejected")
		}
	}
	return nil
}

func readRequest(conn net.Conn) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(conn, 1025))
	if err != nil || len(data) == 0 || len(data) > 1024 {
		return nil, fmt.Errorf("collector request rejected")
	}
	return data, nil
}

func writeFrame(conn net.Conn, frame Frame) error {
	data, err := json.Marshal(frame)
	if err != nil || len(data)+1 > MaxFrameBytes {
		return fmt.Errorf("collector frame exceeded its limit")
	}
	data = append(data, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	for len(data) > 0 {
		n, writeErr := conn.Write(data)
		if writeErr != nil {
			return fmt.Errorf("collector frame write failed")
		}
		if n == 0 {
			return fmt.Errorf("collector frame write failed")
		}
		data = data[n:]
	}
	return nil
}

func DecodeRequest(data []byte, hostID string) (Request, error) { return decodeRequest(data, hostID) }

func DecodeFrame(data []byte, hostID string, now time.Time) (Frame, error) {
	var frame Frame
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return frame, fmt.Errorf("collector frame rejected")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frame); err != nil {
		return Frame{}, fmt.Errorf("collector frame rejected")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Frame{}, fmt.Errorf("collector frame rejected")
	}
	if frame.Version != ProtocolVersion || frame.HostID != hostID {
		return Frame{}, fmt.Errorf("collector host or protocol mismatch")
	}
	switch frame.Kind {
	case "history":
		if frame.Sample != nil || len(frame.Samples) > MaxHistory {
			return Frame{}, fmt.Errorf("collector history frame rejected")
		}
		var previous time.Time
		for i, sample := range frame.Samples {
			if ValidateSample(sample, hostID, now) != nil {
				return Frame{}, fmt.Errorf("collector history sample rejected")
			}
			if i > 0 && !sample.ObservedAt.After(previous) {
				return Frame{}, fmt.Errorf("collector history ordering rejected")
			}
			previous = sample.ObservedAt
		}
	case "host_metrics":
		if frame.Sample == nil || len(frame.Samples) != 0 || ValidateSample(*frame.Sample, hostID, now) != nil {
			return Frame{}, fmt.Errorf("collector telemetry frame rejected")
		}
	default:
		return Frame{}, fmt.Errorf("collector frame kind rejected")
	}
	return frame, nil
}
