//go:build linux

package vpsagent

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

func ownedServiceDirectory(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func lockService(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("VPS collector lock unavailable")
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("VPS collector is already running")
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func listenSocket(path string) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("VPS collector socket path is occupied")
		}
		if err = os.Remove(path); err != nil {
			return nil, fmt.Errorf("VPS collector socket unavailable")
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("VPS collector socket unavailable")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("VPS collector socket unavailable")
	}
	listener.SetUnlinkOnClose(true)
	if err = os.Chmod(path, 0660); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("VPS collector socket permissions could not be set")
	}
	return listener, nil
}

func Bridge(ctx context.Context, socketPath string, input io.Reader, output io.Writer) error {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return fmt.Errorf("VPS collector is unavailable")
	}
	defer conn.Close()
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(conn, io.LimitReader(input, 1025))
		if closeErr := conn.CloseWrite(); copyErr == nil {
			copyErr = closeErr
		}
		copyDone <- copyErr
	}()
	copyOut := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(output, conn)
		copyOut <- copyErr
	}()
	select {
	case <-ctx.Done():
		_ = conn.Close()
		return nil
	case inErr := <-copyDone:
		if inErr != nil {
			_ = conn.Close()
			return fmt.Errorf("VPS collector request transfer failed")
		}
		outErr := <-copyOut
		if outErr != nil {
			return fmt.Errorf("VPS collector stream ended")
		}
		return nil
	}
}
