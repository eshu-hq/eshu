//go:build windows

package eshulocal

import "fmt"

// ErrWorkspaceOwned indicates another process already owns the workspace lock.
var ErrWorkspaceOwned = fmt.Errorf("workspace ownership is not implemented on windows")

// OwnerLock guards one workspace root at a time.
type OwnerLock struct{}

// AcquireOwnerLock is not yet implemented on Windows.
func AcquireOwnerLock(path string) (*OwnerLock, error) {
	return nil, fmt.Errorf("workspace ownership is not implemented on windows")
}

// Close releases the owner lock.
func (l *OwnerLock) Close() error {
	if l == nil {
		return nil
	}
	return nil
}
