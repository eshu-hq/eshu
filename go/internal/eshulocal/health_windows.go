//go:build windows

package eshulocal

import "fmt"

// DefaultReclaimDeps returns placeholder reclaim probes for Windows hosts.
func DefaultReclaimDeps() ReclaimDeps {
	return ReclaimDeps{
		PIDAlive:      ProcessAlive,
		SocketHealthy: SocketHealthy,
		StopPostgres:  StopEmbeddedPostgres,
	}
}

// ProcessAlive is not yet implemented on Windows.
func ProcessAlive(pid int) bool {
	return false
}

// SocketHealthy is not yet implemented on Windows.
func SocketHealthy(path string) bool {
	return false
}

// StopEmbeddedPostgres is not yet implemented on Windows.
func StopEmbeddedPostgres(dataDir string) error {
	return fmt.Errorf("embedded postgres stop is not implemented on windows")
}
