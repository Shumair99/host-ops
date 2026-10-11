package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleManifestAndArchivesPreserveAssets(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "hostops-linux-arm64")
	assets := map[string]string{"hostops": "workstation", "collectors/linux-arm64/hostops": "remote collector", "install.sh": "installer", "docs/install.md": "guide"}
	for path, content := range assets {
		file := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeManifest(dir, manifest{Schema: 1, Application: "HostOps", Version: "test", Target: "linux-arm64"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != len(assets) || m.Application != "HostOps" {
		t.Fatal(m)
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "CHECKSUMS.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range m.Files {
		digest, err := hashFile(filepath.Join(dir, filepath.FromSlash(asset.Path)))
		if err != nil || digest != asset.SHA256 {
			t.Fatal(asset, err)
		}
		if !strings.Contains(string(checksums), digest+"  "+asset.Path+"\n") {
			t.Fatal("missing installer checksum", asset)
		}
	}
	if err = writeArchive(filepath.Join(root, "test.tar.gz"), dir, false); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(root, "test.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	reader := tar.NewReader(g)
	seen := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		path := strings.TrimPrefix(header.Name, filepath.Base(dir)+"/")
		seen[path] = true
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if expected, ok := assets[path]; ok {
			if string(content) != expected {
				t.Fatal("archive lost bytes", path)
			}
			if executableAsset(path) && header.Mode != 0755 {
				t.Fatal("missing executable bit", path, header.Mode)
			}
		}
	}
	if len(seen) != len(assets)+2 {
		t.Fatal(seen)
	}
	if err = writeArchive(filepath.Join(root, "test.zip"), dir, true); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(filepath.Join(root, "test.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != len(seen) {
		t.Fatal("ZIP assets differ")
	}
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, filepath.Base(dir)+"/") || strings.Contains(f.Name, "\\") {
			t.Fatal("nonportable ZIP path", f.Name)
		}
	}
}
