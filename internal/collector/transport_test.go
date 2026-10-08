package collector

import (
	"bytes"
	"context"
	"crypto/sha256"
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

func TestConcurrentCollectorUploadsPublishCompleteExecutable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("collector upload shell runs on Linux")
	}
	cache := t.TempDir()
	data := bytes.Repeat([]byte("collector fixture\n"), 8192)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
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
