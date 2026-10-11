//go:build windows

package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectHistory(path string, created bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid.String()
	dir := filepath.Dir(path)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("history directory must be a regular directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if created || len(entries) == 0 {
		if err = setPrivateACL(dir, sid); err != nil {
			return fmt.Errorf("protect history directory: %w", err)
		}
	} else {
		sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			return err
		}
		if !privateACL(sd.String(), sid) {
			if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && cacheDir != "" {
				privatePath := filepath.Join(cacheDir, "HostOps", "history.db")
				return fmt.Errorf("history directory grants access to other users; choose a private database path with --db, for example %s", privatePath)
			}
			return fmt.Errorf("history directory grants access to other users; choose a private directory and pass its database path with --db")
		}
		if err = setPrivateACL(dir, sid); err != nil {
			return fmt.Errorf("protect history directory: %w", err)
		}
	}
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Lstat(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("history path must be a regular file")
		}
		if err = setPrivateACL(file, sid); err != nil {
			return fmt.Errorf("protect history file: %w", err)
		}
	}
	return nil
}

func setPrivateACL(path, sid string) error {
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func privateACL(sddl, sid string) bool {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return false
	}
	defer runtime.KeepAlive(sd)
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return false
	}
	user, err := windows.StringToSid(sid)
	if err != nil {
		return false
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil || ace == nil {
			return false
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		// The Windows parser resolves aliases such as LA and SY to real SIDs.
		// SidStart is the first word of the variable-length SID in this ACE.
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !trustee.Equals(user) && !trustee.Equals(system) && !trustee.Equals(admins) {
			return false
		}
	}
	return true
}
