//go:build windows

package storage

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsHistoryDirectoryAndFilesArePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Dir(path), path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue
		}
		sd, err := windows.GetNamedSecurityInfo(file, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil || !privateACL(sd.String(), user.User.Sid.String()) {
			t.Fatal("history not private", file, err)
		}
	}
	if privateACL("D:(A;;FA;;;WD)", user.User.Sid.String()) {
		t.Fatal("Everyone access accepted")
	}
	if privateACL("D:NO_ACCESS_CONTROL", user.User.Sid.String()) {
		t.Fatal("null DACL accepted")
	}
}

func TestWindowsRefusesSharedCustomHistoryDirectory(t *testing.T) {
	for _, name := range []string{"shared", "local"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("keep shared"), 0600); err != nil {
				t.Fatal(err)
			}
			sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
				t.Fatal(err)
			}
			before, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil {
				t.Fatal(err)
			}
			if s, err := Open(filepath.Join(dir, "history.db")); err == nil {
				s.Close()
				t.Fatal("shared custom directory accepted")
			} else if !strings.Contains(err.Error(), "--db") {
				t.Fatalf("refusal did not explain how to choose a private history path: %v", err)
			}
			after, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil || after.String() != before.String() {
				t.Fatalf("refusal modified shared ACL: %v %s", err, after.String())
			}
			if _, err := os.Stat(filepath.Join(dir, "history.db")); !os.IsNotExist(err) {
				t.Fatal("refusal created a database", err)
			}
		})
	}
}
