//go:build !windows

package storage

func protectHistory(string, bool) error { return nil }
