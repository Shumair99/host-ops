//go:build !linux

package vpsagent

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
)

func ownedServiceDirectory(os.FileInfo) bool { return true }

func lockService(string) (func(), error) { return nil, fmt.Errorf("VPS collector requires Linux") }
func listenSocket(string) (net.Listener, error) {
	return nil, fmt.Errorf("VPS collector requires Linux")
}
func Bridge(context.Context, string, io.Reader, io.Writer) error {
	return fmt.Errorf("VPS collector requires Linux")
}
