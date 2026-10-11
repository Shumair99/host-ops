package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSettingsFollowUserAcrossDirectories(t *testing.T) {
	profile := t.TempDir()
	t.Setenv("HOSTOPS_CONFIG", "")
	t.Setenv("APPDATA", profile)
	t.Setenv("XDG_CONFIG_HOME", profile)
	t.Setenv("HOME", profile)
	first, second := t.TempDir(), t.TempDir()
	t.Chdir(first)
	path, err := DefaultPath()
	if err != nil || path != filepath.Join(profile, "HostOps", "hostops.json") {
		t.Fatal(path, err)
	}
	t.Chdir(second)
	reused, err := DefaultPath()
	if err != nil || reused != path {
		t.Fatal(reused, err)
	}
	if err = os.MkdirAll("local", 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join("local", "hostops.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, _ := DefaultPath(); got != filepath.Join("local", "hostops.json") {
		t.Fatal(got)
	}
	if err = os.WriteFile("hostops.json", []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, _ := DefaultPath(); got != "hostops.json" {
		t.Fatal(got)
	}
	t.Setenv("HOSTOPS_CONFIG", filepath.Join(profile, "explicit.json"))
	if got, _ := DefaultPath(); got != filepath.Join(profile, "explicit.json") {
		t.Fatal(got)
	}
}

func TestSaveFailurePreservesPreviousSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "hostops.json")
	c := Config{Host: Host{ID: "host", SSH: "ops@example.test", HistoryMode: "session"}}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Host.SSH = "-oProxyCommand=evil"
	if Save(path, c) == nil {
		t.Fatal("invalid settings saved")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("previous settings changed", err)
	}
	c.Host.SSH = "ops@example.test"
	c.Host.Name = "Saved again"
	if err = Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Host.Name != c.Host.Name {
		t.Fatal(loaded, err)
	}
	if err = Save(filepath.Dir(path), c); err == nil {
		t.Fatal("directory accepted as settings file")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary settings leaked", entries, err)
	}
}
