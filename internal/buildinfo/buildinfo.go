package buildinfo

import "fmt"

// Release builders set these strings with -ldflags; source builds remain dev.
var Version = "dev"
var Commit = "unknown"
var BuiltAt = "unknown"

func String() string {
	return fmt.Sprintf("HostOps %s (commit %s, built %s)", Version, Commit, BuiltAt)
}
