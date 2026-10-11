package metrics

import "testing"

func TestStorageFilesystemPrefersNestedMountAndRejectsRelativePaths(t *testing.T) {
	mounts := []Filesystem{{Mount: "/", Type: "ext4"}, {Mount: "/srv", Type: "xfs"}, {Mount: "/srv/cache", Type: "tmpfs"}}
	for p, want := range map[string]string{"/srv/app": "/srv", "/srv/cache/a": "/srv/cache", "/srv-other/a": "/", "relative/path": ""} {
		if got := StorageFilesystem(p, mounts); got != want {
			t.Fatalf("%s: got %s want %s", p, got, want)
		}
	}
}
