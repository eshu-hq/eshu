package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestPlatformGraphConflictKeyPartitionsByDomain proves that the platform_graph
// conflict domain is now partitioned by domain so that intents for different
// platform-graph domains in the same scope get distinct conflict keys and can
// drain concurrently, while same-domain same-scope intents still share a key
// and serialize correctly. This addresses the serialization bug in #3672 where
// all five platform_graph domains shared a single coarse conflict key, causing
// ~26k workload materialization intents to drain serially.
//
// Conflict-domain model (see reducerConflictDomainKey):
//   - domain separates graph-write families that do NOT share NornicDB hot spots
//   - conflict key within a domain partitions by scope so ordering is preserved
//
// True write-conflict analysis per domain pair:
//   - WorkloadMaterialization ↔ DeploymentMapping: DM writes PROVISIONS_PLATFORM
//     edges protected by pg_advisory_xact_lock (PlatformGraphLocker). WM reads
//     committed platform data from InfrastructurePlatformLookup. No shared MERGE
//     target; queue-level serialization is unnecessary.
//   - WorkloadIdentity: writes to a separate canonical writer (service_uid keyspace).
//   - DeployableUnitCorrelation: writes correlation edges (deployable_unit_uid
//     keyspace). No shared MERGE target with WM or DM.
//   - CloudAssetResolution: writes to a separate canonical writer (cloud_resource_uid
//     keyspace). No shared MERGE target with WM, DM, WI, or DUC.
//   - Same-domain same-scope: always shares a conflict key so concurrent
//     handlers for the same scope still serialize (idempotency is NOT required
//     to waive this; it is the correct serialization).
func TestPlatformGraphConflictKeyPartitionsByDomain(t *testing.T) {
	t.Parallel()

	const testScope = "scope:repo:acme:backend"

	intentFor := func(domain reducer.Domain) projector.ReducerIntent {
		return projector.ReducerIntent{
			ScopeID: testScope,
			Domain:  domain,
		}
	}

	domainWM, keyWM := reducerConflictDomainKey(intentFor(reducer.DomainWorkloadMaterialization))
	domainDM, keyDM := reducerConflictDomainKey(intentFor(reducer.DomainDeploymentMapping))
	domainWI, keyWI := reducerConflictDomainKey(intentFor(reducer.DomainWorkloadIdentity))
	domainDUC, keyDUC := reducerConflictDomainKey(intentFor(reducer.DomainDeployableUnitCorrelation))
	domainCAR, keyCAR := reducerConflictDomainKey(intentFor(reducer.DomainCloudAssetResolution))

	// All five domains must still classify under platform_graph conflict domain.
	for _, got := range []string{domainWM, domainDM, domainWI, domainDUC, domainCAR} {
		if got != reducerConflictDomainPlatformGraph {
			t.Errorf("conflict domain = %q, want %q", got, reducerConflictDomainPlatformGraph)
		}
	}

	// Different domains for the same scope MUST produce different conflict keys
	// so the queue can drain them concurrently (not serialize them).
	pairs := []struct {
		nameA, nameB string
		keyA, keyB   string
	}{
		{"workload_materialization", "deployment_mapping", keyWM, keyDM},
		{"workload_materialization", "workload_identity", keyWM, keyWI},
		{"workload_materialization", "deployable_unit_correlation", keyWM, keyDUC},
		{"workload_materialization", "cloud_asset_resolution", keyWM, keyCAR},
		{"deployment_mapping", "workload_identity", keyDM, keyWI},
		{"deployment_mapping", "deployable_unit_correlation", keyDM, keyDUC},
		{"deployment_mapping", "cloud_asset_resolution", keyDM, keyCAR},
		{"workload_identity", "deployable_unit_correlation", keyWI, keyDUC},
		{"workload_identity", "cloud_asset_resolution", keyWI, keyCAR},
		{"deployable_unit_correlation", "cloud_asset_resolution", keyDUC, keyCAR},
	}
	for _, pair := range pairs {
		if pair.keyA == pair.keyB {
			t.Errorf(
				"domains %q and %q share conflict key %q for scope %q; "+
					"this serializes non-conflicting intents (serialization bug #3672)",
				pair.nameA, pair.nameB, pair.keyA, testScope,
			)
		}
	}
}

// TestPlatformGraphConflictKeySameDomainSameScopeSerializes proves that two
// intents for the SAME platform-graph domain and the SAME scope still share a
// conflict key so the queue serializes them correctly. This is the idempotency
// and ordering half of the partition proof.
func TestPlatformGraphConflictKeySameDomainSameScopeSerializes(t *testing.T) {
	t.Parallel()

	const testScope = "scope:repo:acme:backend"
	domains := []reducer.Domain{
		reducer.DomainWorkloadMaterialization,
		reducer.DomainDeploymentMapping,
		reducer.DomainWorkloadIdentity,
		reducer.DomainDeployableUnitCorrelation,
		reducer.DomainCloudAssetResolution,
	}

	for _, domain := range domains {
		intent := projector.ReducerIntent{ScopeID: testScope, Domain: domain}
		_, key1 := reducerConflictDomainKey(intent)
		_, key2 := reducerConflictDomainKey(intent)
		if key1 != key2 {
			t.Errorf(
				"domain %q scope %q produced different conflict keys %q and %q; "+
					"conflict key must be deterministic for same-domain same-scope serialization",
				domain, testScope, key1, key2,
			)
		}
	}
}

// TestPlatformGraphConflictKeyDistinctScopesAlwaysDistinct proves that the
// same domain with different scopes always produces different conflict keys,
// preserving ordering within each scope independently.
func TestPlatformGraphConflictKeyDistinctScopesAlwaysDistinct(t *testing.T) {
	t.Parallel()

	domains := []reducer.Domain{
		reducer.DomainWorkloadMaterialization,
		reducer.DomainDeploymentMapping,
		reducer.DomainWorkloadIdentity,
		reducer.DomainDeployableUnitCorrelation,
		reducer.DomainCloudAssetResolution,
	}

	scopes := []string{"scope:repo:acme:backend", "scope:repo:acme:frontend"}

	for _, domain := range domains {
		_, keyA := reducerConflictDomainKey(projector.ReducerIntent{ScopeID: scopes[0], Domain: domain})
		_, keyB := reducerConflictDomainKey(projector.ReducerIntent{ScopeID: scopes[1], Domain: domain})
		if keyA == keyB {
			t.Errorf(
				"domain %q: distinct scopes %q and %q produced the same conflict key %q; "+
					"distinct-scope concurrency would be lost",
				domain, scopes[0], scopes[1], keyA,
			)
		}
	}
}

// TestPlatformGraphConflictKeyDoesNotLeakRawScopeID proves that the conflict
// key does not embed the raw scope ID string (it must be hashed or prefixed to
// avoid accidental key collisions and to bound key length).
func TestPlatformGraphConflictKeyDoesNotLeakRawScopeID(t *testing.T) {
	t.Parallel()

	const sensitiveScope = "scope:repo:acme:backend:secret-infra"
	domains := []reducer.Domain{
		reducer.DomainWorkloadMaterialization,
		reducer.DomainDeploymentMapping,
	}
	for _, domain := range domains {
		_, key := reducerConflictDomainKey(projector.ReducerIntent{
			ScopeID: sensitiveScope,
			Domain:  domain,
		})
		if strings.Contains(key, "secret-infra") {
			t.Errorf("conflict key %q leaks raw scope value %q", key, "secret-infra")
		}
	}
}
