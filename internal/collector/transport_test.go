package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testExecutableELF(machine uint16, size int) []byte {
	if size < 120 {
		size = 120
	}
	data := make([]byte, size)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:18], 2) // ET_EXEC
	binary.LittleEndian.PutUint16(data[18:20], machine)
	binary.LittleEndian.PutUint32(data[20:24], 1)
	binary.LittleEndian.PutUint64(data[24:32], 0x400070)
	binary.LittleEndian.PutUint64(data[32:40], 64)
	binary.LittleEndian.PutUint16(data[52:54], 64)
	binary.LittleEndian.PutUint16(data[54:56], 56)
	binary.LittleEndian.PutUint16(data[56:58], 1)
	binary.LittleEndian.PutUint16(data[58:60], 64)
	binary.LittleEndian.PutUint32(data[64:68], 1) // PT_LOAD
	binary.LittleEndian.PutUint32(data[68:72], 5) // PF_R | PF_X
	binary.LittleEndian.PutUint64(data[80:88], 0x400000)
	binary.LittleEndian.PutUint64(data[96:104], uint64(size))
	binary.LittleEndian.PutUint64(data[104:112], uint64(size))
	binary.LittleEndian.PutUint64(data[112:120], 0x1000)
	return data
}

func TestConcurrentCollectorUploadsPublishCompleteExecutable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("collector upload shell runs on Linux")
	}
	cache := t.TempDir()
	cacheDir := filepath.Join(cache, "hostops")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("collector fixture\n"), 8192)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	staleHash := strings.Repeat("a", 64)
	if staleHash == hash {
		staleHash = strings.Repeat("b", 64)
	}
	stalePath := filepath.Join(cacheDir, "collector-"+staleHash)
	if err := os.WriteFile(stalePath, []byte("old collector"), 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatal(err)
	}
	graceHash := strings.Repeat("c", 64)
	if graceHash == hash || graceHash == staleHash {
		graceHash = strings.Repeat("d", 64)
	}
	gracePath := filepath.Join(cacheDir, "collector-"+graceHash)
	if err := os.WriteFile(gracePath, []byte("recent collector"), 0700); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(cacheDir, "collector-manual-backup")
	if err := os.WriteFile(otherPath, []byte("unrecognized file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(otherPath, old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, "sh", "-c", uploadCommand(hash))
			cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cache)
			cmd.Stdin = bytes.NewReader(data)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != filepath.Join(cache, "hostops", "collector-"+hash) {
				t.Errorf("concurrent upload failed: %v %s", err, out)
			}
		}()
	}
	wg.Wait()
	got, err := os.ReadFile(filepath.Join(cache, "hostops", "collector-"+hash))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("published collector was missing or incomplete", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("old content-addressed collector was not pruned", err)
	}
	for _, path := range []string{gracePath, otherPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("recent or unrecognized cache file was removed: %s: %v", path, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(cache, "hostops"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".upload-") {
			t.Fatal("upload temporary file was left behind")
		}
	}
}

func TestReadCollectorBinaryBoundsAndValidatesELF(t *testing.T) {
	tests := []struct {
		name    string
		arch    string
		data    []byte
		wantErr bool
	}{
		{name: "amd64", arch: "amd64", data: testExecutableELF(62, 120)},
		{name: "arm64", arch: "arm64", data: testExecutableELF(183, 120)},
		{name: "wrong machine", arch: "amd64", data: testExecutableELF(183, 120), wantErr: true},
		{name: "wrong class", arch: "amd64", data: testExecutableELF(62, 120), wantErr: true},
		{name: "truncated header", arch: "amd64", data: []byte("ELF"), wantErr: true},
		{name: "unsupported architecture", arch: "386", data: testExecutableELF(62, 120), wantErr: true},
	}
	tests[3].data[4] = 1
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "collector")
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readCollectorBinary(path, tc.arch)
			if (err != nil) != tc.wantErr {
				t.Fatalf("readCollectorBinary() error = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && !bytes.Equal(got, tc.data) {
				t.Fatal("collector bytes changed while validating")
			}
		})
	}
	t.Run("oversized file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "collector")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(maxCollectorBytes + 1); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = readCollectorBinary(path, "amd64"); err == nil {
			t.Fatal("oversized collector was accepted")
		}
	})
}
