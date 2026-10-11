//go:build windows

package aiexport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestExportFileHasPrivateProtectedACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.md")
	if _, err := Write(context.Background(), path, []byte("private evidence")); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sddl := sd.String()
	if !strings.Contains(sddl, "D:P") || !strings.Contains(sddl, user.User.Sid.String()) || strings.Count(sddl, "(A;") != 2 {
		t.Fatal("export did not restrict access to current user and SYSTEM", sddl)
	}
}
