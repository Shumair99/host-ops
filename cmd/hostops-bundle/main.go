// hostops-bundle builds self-contained workstation installation archives.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	Schema      int     `json:"schema"`
	Application string  `json:"application"`
	Version     string  `json:"version"`
	Commit      string  `json:"commit"`
	BuiltAt     string  `json:"built_at"`
	Target      string  `json:"target"`
	Files       []asset `json:"files"`
}

var supported = map[string]bool{"windows-amd64": true, "windows-arm64": true, "linux-amd64": true, "linux-arm64": true, "darwin-amd64": true, "darwin-arm64": true}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	output := flag.String("output", "output/bundles", "archive destination")
	targetList := flag.String("targets", "windows-amd64,windows-arm64,linux-amd64,linux-arm64,darwin-amd64,darwin-arm64", "comma-separated workstation targets")
	version := flag.String("version", "dev", "release version")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *version == "" || strings.ContainsAny(*version, " \t\r\n'\"") {
		return fmt.Errorf("version must be a single identifier")
	}
	targets := strings.Split(*targetList, ",")
	seen := map[string]bool{}
	for _, target := range targets {
		if !supported[target] || seen[target] {
			return fmt.Errorf("unsupported or repeated target %q", target)
		}
		seen[target] = true
	}
	if _, err := os.Stat("packaging/GETTING-STARTED.md"); err != nil {
		return fmt.Errorf("run the bundle builder from the HostOps source directory")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("building bundles requires Go; installed users do not need it")
	}
	if err = os.MkdirAll(*output, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(*output, ".build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage) // Only the fresh directory returned by MkdirTemp.
	commit := "unknown"
	if b, gitErr := exec.Command("git", "rev-parse", "--short=12", "HEAD").Output(); gitErr == nil {
		commit = strings.TrimSpace(string(b))
		if b, statusErr := exec.Command("git", "status", "--porcelain", "--untracked-files=normal").Output(); statusErr == nil && len(b) > 0 {
			commit += "-dirty"
		}
	}
	built := time.Now().UTC().Format(time.RFC3339)
	ld := "-s -w -X github.com/Shumair99/host-ops/internal/buildinfo.Version=" + *version + " -X github.com/Shumair99/host-ops/internal/buildinfo.Commit=" + commit + " -X github.com/Shumair99/host-ops/internal/buildinfo.BuiltAt=" + built
	build := func(target, program, destination string) error {
		parts := strings.Split(target, "-")
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		cmd := exec.Command(goTool, "build", "-trimpath", "-ldflags", ld, "-o", destination, "./cmd/"+program)
		for _, value := range os.Environ() {
			key := strings.ToUpper(strings.SplitN(value, "=", 2)[0])
			if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "GOOS="+parts[0], "GOARCH="+parts[1], "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	shared := filepath.Join(stage, "helpers")
	for _, arch := range []string{"amd64", "arm64"} {
		fmt.Println("Building bundled Linux tools:", arch)
		for _, item := range []struct{ program, path string }{{"hostops", "collectors/linux-" + arch + "/hostops"}, {"hostops-vps-agent", "agents/vps/linux-" + arch + "/hostops-vps-agent"}} {
			if err = build("linux-"+arch, item.program, filepath.Join(shared, filepath.FromSlash(item.path))); err != nil {
				return err
			}
		}
	}
	var sums strings.Builder
	for _, target := range targets {
		fmt.Println("Packaging", target)
		name := "hostops-" + target
		dir := filepath.Join(stage, name)
		if err = copyTree(shared, dir); err != nil {
			return err
		}
		suffix := ""
		if strings.HasPrefix(target, "windows-") {
			suffix = ".exe"
		}
		for _, program := range []string{"hostops", "hostops-agent", "hostops-watcher"} {
			if err = build(target, program, filepath.Join(dir, program+suffix)); err != nil {
				return err
			}
		}
		// A fixed allowlist keeps personal settings, databases and keys out of releases.
		for _, path := range []string{"LICENSE", "README.md", "SECURITY.md", "examples"} {
			if err = copyTree(path, filepath.Join(dir, path)); err != nil {
				return err
			}
		}
		for _, name := range []string{"install", "ai-export", "changes", "deployment-principles", "deployments", "diagnostics", "discovery", "economics", "exposure", "history", "incidents", "monitoring", "operations-tools", "preflight", "readiness", "recovery", "security", "storage", "terminal-guide", "watcher"} {
			path := "docs/" + name + ".md"
			if err = copyTree(path, filepath.Join(dir, filepath.FromSlash(path))); err != nil {
				return err
			}
		}
		installers := []string{"GETTING-STARTED.md"}
		if suffix != "" {
			installers = append(installers, "Install.cmd", "Install-HostOps.ps1", "Uninstall-HostOps.ps1")
		} else {
			installers = append(installers, "install.sh", "uninstall.sh")
		}
		for _, path := range installers {
			if err = copyTree(filepath.Join("packaging", path), filepath.Join(dir, path)); err != nil {
				return err
			}
		}
		m := manifest{Schema: 1, Application: "HostOps", Version: *version, Commit: commit, BuiltAt: built, Target: target}
		if err = writeManifest(dir, m); err != nil {
			return err
		}
		ext := ".tar.gz"
		if suffix != "" {
			ext = ".zip"
		}
		archive := filepath.Join(stage, name+ext)
		if err = writeArchive(archive, dir, suffix != ""); err != nil {
			return err
		}
		digest, hashErr := hashFile(archive)
		if hashErr != nil {
			return hashErr
		}
		destination := filepath.Join(*output, name+ext)
		if err = os.Rename(archive, destination); err != nil {
			return err
		}
		if suffix != "" {
			// Earlier bundles used tar.gz on Windows. Do not leave the obsolete
			// generated format next to the installable ZIP for the same target.
			if removeErr := os.Remove(filepath.Join(*output, name+".tar.gz")); removeErr != nil && !os.IsNotExist(removeErr) {
				return removeErr
			}
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name+ext)
		fmt.Println("Created", destination)
	}
	return os.WriteFile(filepath.Join(*output, "SHA256SUMS"), []byte(sums.String()), 0644)
}

func copyTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		in, err := os.Open(source)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	if !info.IsDir() {
		return fmt.Errorf("bundle assets must be regular files: %s", source)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err = copyTree(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func pathsIn(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular asset %s", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(paths)
	return paths, err
}
func writeManifest(dir string, m manifest) error {
	paths, err := pathsIn(dir)
	if err != nil {
		return err
	}
	var sums strings.Builder
	for _, path := range paths {
		if path == "bundle.json" || path == "CHECKSUMS.sha256" {
			continue
		}
		digest, err := hashFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		m.Files = append(m.Files, asset{path, digest})
		fmt.Fprintf(&sums, "%s  %s\n", digest, path)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "bundle.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	digest, err := hashFile(filepath.Join(dir, "bundle.json"))
	if err != nil {
		return err
	}
	fmt.Fprintf(&sums, "%s  bundle.json\n", digest)
	return os.WriteFile(filepath.Join(dir, "CHECKSUMS.sha256"), []byte(sums.String()), 0644)
}
func executableAsset(path string) bool {
	base := filepath.Base(path)
	return base == "hostops" || base == "hostops-agent" || base == "hostops-watcher" || base == "hostops-vps-agent" || strings.HasSuffix(base, ".sh")
}
func writeArchive(destination, dir string, windows bool) (err error) {
	f, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	paths, err := pathsIn(dir)
	if err != nil {
		return err
	}
	if windows {
		z := zip.NewWriter(f)
		for _, path := range paths {
			entry, err := z.Create(filepath.Base(dir) + "/" + path)
			if err != nil {
				return err
			}
			if err = writeAsset(entry, dir, path); err != nil {
				return err
			}
		}
		return z.Close()
	}
	g := gzip.NewWriter(f)
	t := tar.NewWriter(g)
	for _, path := range paths {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		mode := int64(0644)
		if executableAsset(path) {
			mode = 0755
		}
		if err = t.WriteHeader(&tar.Header{Name: filepath.Base(dir) + "/" + path, Mode: mode, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
			return err
		}
		if err = writeAsset(t, dir, path); err != nil {
			return err
		}
	}
	if err = t.Close(); err != nil {
		return err
	}
	return g.Close()
}
func writeAsset(out io.Writer, dir, path string) error {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(path)))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(out, f)
	return err
}
