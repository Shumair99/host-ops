//go:build windows

package aiexport

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestExportFileHasPrivateProtectedACL(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{"default", "shared"} {
		t.Run(parent, func(t *testing.T) {
			dir := t.TempDir()
			if parent == "shared" {
				sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
				if err != nil {
					t.Fatal(err)
				}
				acl, _, err := sd.DACL()
				if err != nil {
					t.Fatal(err)
				}
				if err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "review.md")
			if _, err := Write(context.Background(), path, []byte("private evidence")); err != nil {
				t.Fatal(err)
			}
			sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkPrivateExportACL(sd, user.User.Sid); err != nil {
				t.Fatalf("export ACL %s: %v", sd.String(), err)
			}
		})
	}
}

func TestExportACLChecksAccountIdentityAndPermissions(t *testing.T) {
	admin, err := windows.SecurityDescriptorFromString("O:LA")
	if err != nil {
		t.Fatal(err)
	}
	adminSID, _, err := admin.Owner()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, sddl string
		private    bool
	}{
		{"runner aliases with auto-inherited flag", "D:PAI(A;;FA;;;SY)(A;;FA;;;LA)", true},
		{"numeric SIDs", "D:P(A;;FA;;;S-1-5-18)(A;;FA;;;" + adminSID.String() + ")", true},
		{"reversed ACE order", "D:P(A;;FA;;;LA)(A;;FA;;;SY)", true},
		{"unprotected DACL", "D:AI(A;;FA;;;SY)(A;;FA;;;LA)", false},
		{"inherited ACE", "D:P(A;ID;FA;;;SY)(A;;FA;;;LA)", false},
		{"inherit-only ACE", "D:P(A;;FA;;;SY)(A;IO;FA;;;LA)", false},
		{"wrong user", "D:P(A;;FA;;;SY)(A;;FA;;;S-1-5-21-1-2-3-1001)", false},
		{"extra principal", "D:P(A;;FA;;;SY)(A;;FA;;;LA)(A;;FA;;;WD)", false},
		{"missing SYSTEM", "D:P(A;;FA;;;LA)(A;;FA;;;LA)", false},
		{"missing user", "D:P(A;;FA;;;SY)(A;;FA;;;SY)", false},
		{"insufficient rights", "D:P(A;;FA;;;SY)(A;;FR;;;LA)", false},
		{"deny ACE", "D:P(D;;FA;;;SY)(A;;FA;;;LA)", false},
		{"empty DACL", "D:P", false},
		{"null DACL", "D:PNO_ACCESS_CONTROL", false},
		{"missing DACL", "O:LA", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(test.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if err = checkPrivateExportACL(sd, adminSID); (err == nil) != test.private {
				t.Fatalf("private=%t for %s: %v", test.private, test.sddl, err)
			}
		})
	}
}

func checkPrivateExportACL(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID) error {
	defer runtime.KeepAlive(sd)
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("DACL is not protected from parent inheritance")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil || acl.AceCount != 2 {
		return fmt.Errorf("DACL must contain exactly two ACEs")
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	// FILE_ALL_ACCESS from the Windows SDK; x/sys does not export this constant.
	const fileAllAccess = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	var hasUser, hasSystem bool
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != 0 || ace.Mask != fileAllAccess {
			return fmt.Errorf("ACE %d must explicitly grant full file access", i)
		}
		// Compare the SID itself: SDDL can abbreviate it to LA or SY.
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		isUser, isSystem := trustee.Equals(user), trustee.Equals(system)
		if !isUser && !isSystem {
			return fmt.Errorf("ACE %d grants access to an unexpected account", i)
		}
		hasUser, hasSystem = hasUser || isUser, hasSystem || isSystem
	}
	if !hasUser || !hasSystem {
		return fmt.Errorf("DACL must grant access to both the current user and SYSTEM")
	}
	return nil
}
