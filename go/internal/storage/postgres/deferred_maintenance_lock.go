package postgres

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// deferredMaintenanceLockNamespace namespaces the deferred-relationship
// maintenance advisory locks. Postgres advisory locks share one global keyspace;
// the namespace keeps these per-repository locks from colliding with unrelated
// advisory-lock families that hash into the same space.
const deferredMaintenanceLockNamespace = "deferred_relationship_maintenance"

// deferredMaintenancePartitionedSharedLockSQL takes a transaction-level shared
// advisory lock on one repository partition. Generation commits hold this so a
// concurrent maintenance pass for the same repository waits, while commits and
// maintenance for disjoint repositories proceed in parallel.
const deferredMaintenancePartitionedSharedLockSQL = `SELECT pg_advisory_xact_lock_shared(hashtext($1), hashtext($2))`

// deferredMaintenancePartitionedExclusiveLockSQL takes a transaction-level
// exclusive advisory lock on one repository partition. The maintenance pass
// holds this per source repository it backfills, so a repository's maintenance
// only blocks commits for that same repository, never the whole fleet.
const deferredMaintenancePartitionedExclusiveLockSQL = `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`

// deferredMaintenanceRepoLockKey returns the partition key used to fence a
// generation commit against deferred maintenance for the same repository. It is
// the scope partition key when present, falling back to the scope source key so
// non-repository scopes still take a stable, non-empty partition.
func deferredMaintenanceRepoLockKey(scopeValue scope.IngestionScope) string {
	if key := scopeValue.PartitionKey; key != "" {
		return key
	}
	return scopeSourceKey(scopeValue)
}

// deferredMaintenanceRepoLockKeyFromID returns the partition key for a known
// repository identifier. The maintenance leader uses this for every source
// repository it backfills so its exclusive lock matches the shared lock the
// committing ingester takes via deferredMaintenanceRepoLockKey.
func deferredMaintenanceRepoLockKeyFromID(repoID string) string {
	return repoID
}

// acquireDeferredMaintenanceRepoSharedLock fences a commit against deferred
// maintenance for one repository partition only.
func acquireDeferredMaintenanceRepoSharedLock(ctx context.Context, db ExecQueryer, repoKey string) error {
	_, err := db.ExecContext(ctx, deferredMaintenancePartitionedSharedLockSQL, deferredMaintenanceLockNamespace, repoKey)
	return err
}

// acquireDeferredMaintenanceRepoExclusiveLocks takes the per-repository
// exclusive maintenance locks in deterministic sorted order. Acquiring every
// lock in the same global order across all callers prevents lock-ordering
// deadlock between concurrent maintenance leaders and commits that touch
// overlapping repository sets. Duplicate keys are collapsed so a key is locked
// at most once per transaction.
func acquireDeferredMaintenanceRepoExclusiveLocks(ctx context.Context, db ExecQueryer, repoKeys []string) error {
	ordered := sortedUniqueRepoKeys(repoKeys)
	for _, repoKey := range ordered {
		if _, err := db.ExecContext(
			ctx,
			deferredMaintenancePartitionedExclusiveLockSQL,
			deferredMaintenanceLockNamespace,
			repoKey,
		); err != nil {
			return err
		}
	}
	return nil
}

// sortedUniqueRepoKeys returns the distinct, non-empty repository keys in sorted
// order. Sorting yields the consistent lock-acquisition order that keeps
// multi-repository maintenance deadlock-free.
func sortedUniqueRepoKeys(repoKeys []string) []string {
	seen := make(map[string]struct{}, len(repoKeys))
	ordered := make([]string, 0, len(repoKeys))
	for _, repoKey := range repoKeys {
		if repoKey == "" {
			continue
		}
		if _, ok := seen[repoKey]; ok {
			continue
		}
		seen[repoKey] = struct{}{}
		ordered = append(ordered, repoKey)
	}
	sort.Strings(ordered)
	return ordered
}
