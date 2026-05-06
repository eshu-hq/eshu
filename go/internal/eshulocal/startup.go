package eshulocal

import "fmt"

// StartupDeps composes the startup admission contract for a local workspace owner.
type StartupDeps struct {
	AcquireLock       func(path string) (*OwnerLock, error)
	ReleaseLock       func(lock *OwnerLock) error
	EnsureVersion     func(layout Layout, currentVersion string) error
	ValidateOrReclaim func(layout Layout, currentVersion string, deps ReclaimDeps) error
	ReclaimDeps       ReclaimDeps
}

// PrepareWorkspace runs the documented startup admission order:
// acquire owner.lock, validate VERSION, then validate or reclaim owner.json.
func PrepareWorkspace(layout Layout, currentVersion string, deps StartupDeps) (*OwnerLock, error) {
	acquireLock := deps.AcquireLock
	if acquireLock == nil {
		acquireLock = AcquireOwnerLock
	}
	releaseLock := deps.ReleaseLock
	if releaseLock == nil {
		releaseLock = func(lock *OwnerLock) error { return lock.Close() }
	}
	ensureVersion := deps.EnsureVersion
	if ensureVersion == nil {
		ensureVersion = EnsureLayoutVersion
	}
	validateOrReclaim := deps.ValidateOrReclaim
	if validateOrReclaim == nil {
		validateOrReclaim = ValidateOrReclaimOwner
	}

	lock, err := acquireLock(layout.OwnerLockPath)
	if err != nil {
		return nil, err
	}

	if err := ensureVersion(layout, currentVersion); err != nil {
		return nil, releaseAfterFailure(lock, releaseLock, err)
	}
	if err := validateOrReclaim(layout, currentVersion, deps.ReclaimDeps); err != nil {
		return nil, releaseAfterFailure(lock, releaseLock, err)
	}
	return lock, nil
}

func releaseAfterFailure(lock *OwnerLock, releaseLock func(lock *OwnerLock) error, originalErr error) error {
	if err := releaseLock(lock); err != nil {
		return fmt.Errorf("%v; release owner lock: %w", originalErr, err)
	}
	return originalErr
}
