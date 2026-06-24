package cypher

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

const packageRegistryIdentitySlowLockWait = 100 * time.Millisecond

type packageRegistryIdentityLocks struct {
	mu    sync.Mutex
	locks map[string]*packageRegistryIdentityLock
}

type packageRegistryIdentityLock struct {
	mu   sync.Mutex
	refs int
}

type packageRegistryIdentityLockLease struct {
	keyCount int
	minKey   string
	wait     time.Duration
	unlock   func()
}

func newPackageRegistryIdentityLocks() *packageRegistryIdentityLocks {
	return &packageRegistryIdentityLocks{locks: make(map[string]*packageRegistryIdentityLock)}
}

func (w *CanonicalNodeWriter) lockPackageRegistryIdentities(
	mat projector.CanonicalMaterialization,
) packageRegistryIdentityLockLease {
	if w == nil || w.packageRegistryLocks == nil {
		return packageRegistryIdentityLockLease{unlock: func() {}}
	}
	return w.packageRegistryLocks.lock(packageRegistryIdentityLockKeys(mat))
}

func (l *packageRegistryIdentityLocks) lock(keys []string) packageRegistryIdentityLockLease {
	keys = uniqueSortedPackageRegistryIdentityKeys(keys)
	if l == nil || len(keys) == 0 {
		return packageRegistryIdentityLockLease{unlock: func() {}}
	}

	start := time.Now()
	locked := make([]*packageRegistryIdentityLock, 0, len(keys))
	for _, key := range keys {
		l.mu.Lock()
		lock := l.locks[key]
		if lock == nil {
			lock = &packageRegistryIdentityLock{}
			l.locks[key] = lock
		}
		lock.refs++
		l.mu.Unlock()

		lock.mu.Lock()
		locked = append(locked, lock)
	}

	return packageRegistryIdentityLockLease{
		keyCount: len(keys),
		minKey:   keys[0],
		wait:     time.Since(start),
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

func recordPackageRegistryIdentityLock(
	ctx context.Context,
	span trace.Span,
	mat projector.CanonicalMaterialization,
	lease packageRegistryIdentityLockLease,
) {
	if lease.keyCount == 0 {
		return
	}
	span.SetAttributes(
		attribute.Int("package_registry_identity_lock_key_count", lease.keyCount),
		attribute.Float64("package_registry_identity_lock_wait_seconds", lease.wait.Seconds()),
	)
	if lease.wait < packageRegistryIdentitySlowLockWait {
		return
	}
	slog.InfoContext(
		ctx, "canonical package registry identity lock acquired",
		"scope_id", mat.ScopeID,
		"repo_id", mat.RepoID,
		"generation_id", mat.GenerationID,
		"package_uid_min", lease.minKey,
		"package_uid_count", lease.keyCount,
		"wait_s", lease.wait.Seconds(),
	)
}

func packageRegistryIdentityLockKeys(mat projector.CanonicalMaterialization) []string {
	keys := make(
		[]string,
		0,
		len(mat.PackageRegistryPackages)+len(mat.PackageRegistryVersions)+2*len(mat.PackageRegistryDependencies),
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
	return keys
}

func uniqueSortedPackageRegistryIdentityKeys(keys []string) []string {
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
