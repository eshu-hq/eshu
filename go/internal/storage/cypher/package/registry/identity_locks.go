// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

const identitySlowLockWait = 100 * time.Millisecond

// IdentityLocks serializes concurrent canonical writes that MERGE the same
// package identity: per-UID mutexes keyed by the package UIDs a
// materialization touches. The parent cypher package holds one set on its
// CanonicalNodeWriter and locks it around the package-registry phases.
type IdentityLocks struct {
	mu    sync.Mutex
	locks map[string]*identityLock
}

type identityLock struct {
	mu   sync.Mutex
	refs int
}

// Lease is one Lock acquisition: the key count, the smallest key (for slow
// lock logs), and how long acquisition waited.
type Lease struct {
	KeyCount int
	MinKey   string
	Wait     time.Duration
	unlock   func()
}

// NewIdentityLocks returns an empty lock set.
func NewIdentityLocks() *IdentityLocks {
	return &IdentityLocks{locks: make(map[string]*identityLock)}
}

// Lock acquires the per-UID mutexes for keys in sorted order (deadlock-free
// across writers) and returns a Lease the caller must Unlock.
func (l *IdentityLocks) Lock(keys []string) Lease {
	keys = UniqueSortedIdentityKeys(keys)
	if l == nil || len(keys) == 0 {
		return Lease{unlock: func() {}}
	}

	start := time.Now()
	locked := make([]*identityLock, 0, len(keys))
	for _, key := range keys {
		l.mu.Lock()
		lock := l.locks[key]
		if lock == nil {
			lock = &identityLock{}
			l.locks[key] = lock
		}
		lock.refs++
		l.mu.Unlock()

		lock.mu.Lock()
		locked = append(locked, lock)
	}

	return Lease{
		KeyCount: len(keys),
		MinKey:   keys[0],
		Wait:     time.Since(start),
		unlock: func() {
			for index := len(locked) - 1; index >= 0; index-- {
				lock := locked[index]
				lock.mu.Unlock()

				l.mu.Lock()
				lock.refs--
				if lock.refs == 0 {
					delete(l.locks, keys[index])
				}
				l.mu.Unlock()
			}
		},
	}
}

// Unlock releases a Lease. A zero Lease is a no-op.
func (l Lease) Unlock() {
	if l.unlock != nil {
		l.unlock()
	}
}

// RefCount reports how many writers currently hold key. The concurrency
// tests poll it to prove a second write queues on a held identity lock.
func (l *IdentityLocks) RefCount(key string) int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if lock := l.locks[key]; lock != nil {
		return lock.refs
	}
	return 0
}

// SlowLockWait reports whether acquisition waited long enough to log.
func (l Lease) SlowLockWait() bool {
	return l.Wait >= identitySlowLockWait
}

// LockKeys lists every package UID a materialization touches: the identity
// set the writer must hold while its phases run.
func LockKeys(mat canonical.CanonicalMaterialization) []string {
	keys := make(
		[]string,
		0,
		len(mat.PackageRegistryPackages)+len(mat.PackageRegistryVersions)+
			2*len(mat.PackageRegistryDependencies)+len(mat.PackageRegistryArtifacts)+
			len(mat.PackageRegistryEvents),
	)
	for _, row := range mat.PackageRegistryPackages {
		keys = append(keys, row.UID)
	}
	for _, row := range mat.PackageRegistryVersions {
		keys = append(keys, row.PackageID)
	}
	for _, row := range mat.PackageRegistryDependencies {
		keys = append(keys, row.PackageID, row.DependencyPackageID)
	}
	for _, row := range mat.PackageRegistryArtifacts {
		keys = append(keys, row.PackageID)
	}
	for _, row := range mat.PackageRegistryEvents {
		keys = append(keys, row.PackageID)
	}
	return keys
}

// UniqueSortedIdentityKeys deduplicates keys, drops blanks, and sorts the
// remainder: the deterministic acquisition order every writer shares.
func UniqueSortedIdentityKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
