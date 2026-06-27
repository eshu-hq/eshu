// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	reducerConflictDomainScope             = "scope"
	reducerConflictDomainCodeGraph         = "code_graph"
	reducerConflictDomainPlatformGraph     = "platform_graph"
	reducerConflictDomainResourceScope     = "resource_scope"
	reducerConflictDomainCloudResourceNode = "cloud_resource_node"

	reducerConflictKeyPrefixResourceScope     = "resource-scope:v1:"
	reducerConflictKeyPrefixCloudResourceNode = "cloud-resource-node:v1:"

	// reducerConflictKeyPrefixPlatformGraph is the versioned prefix for all
	// platform_graph conflict keys. The key is hashed over a partition token plus
	// the scope so that non-conflicting platform-graph work drains concurrently
	// while true same-target conflicts still serialize.
	//
	// Two partition tokens exist under this prefix:
	//   - "platform-node-writer:<scope>" — shared by WorkloadMaterialization and
	//     DeploymentMapping (both MERGE the same (p:Platform {id}) node and
	//     WorkloadMaterialization holds no advisory lock, so they must serialize).
	//   - "<domain>:<scope>" — one key per non-Platform-writing domain
	//     (WorkloadIdentity, CloudAssetResolution, DeployableUnitCorrelation),
	//     which drain concurrently with each other and with the Platform pair.
	//
	// Version history:
	//   v1 (pre-#3672): raw scopeKey — all 5 platform-graph domains shared one
	//     coarse key per scope, serializing ~26k intents into a 25-min queue wait.
	//   v2 (#3672): partitioned by graph-write target — Platform-node writers share
	//     one key; non-Platform domains get per-domain keys. Removes the false
	//     serialization while preserving the real same-Platform-node MERGE fence.
	reducerConflictKeyPrefixPlatformGraph = "platform-graph:v2:"
)

type reducerResourceConflictStatus string

const (
	reducerResourceConflictStatusSafe    reducerResourceConflictStatus = "safe"
	reducerResourceConflictStatusRisky   reducerResourceConflictStatus = "risky"
	reducerResourceConflictStatusBlocked reducerResourceConflictStatus = "blocked"
)

type reducerResourceConflictPolicy struct {
	Domain   reducer.Domain
	Status   reducerResourceConflictStatus
	Evidence string
}

var reducerResourceConflictPolicies = []reducerResourceConflictPolicy{
	{
		Domain:   reducer.DomainAWSResourceMaterialization,
		Status:   reducerResourceConflictStatusSafe,
		Evidence: "scope-generation node writer is idempotent by CloudResource uid and has no scope-wide retract",
	},
	{
		Domain:   reducer.DomainGCPResourceMaterialization,
		Status:   reducerResourceConflictStatusRisky,
		Evidence: "scope-generation node writer is idempotent, but promotion needs provider-specific contention proof",
	},
	{
		Domain:   reducer.DomainAzureResourceMaterialization,
		Status:   reducerResourceConflictStatusRisky,
		Evidence: "scope-generation node writer is idempotent, but promotion needs Azure case-fold contention proof",
	},
	{
		Domain:   reducer.DomainEC2InstanceNodeMaterialization,
		Status:   reducerResourceConflictStatusRisky,
		Evidence: "scope-generation EC2 node writer needs partition-filtered load proof before promotion",
	},
	{
		Domain:   reducer.DomainKubernetesWorkloadMaterialization,
		Status:   reducerResourceConflictStatusRisky,
		Evidence: "scope-generation workload node writer needs namespace/uid partition proof before promotion",
	},
	{
		Domain:   reducer.DomainSecurityGroupCidrMaterialization,
		Status:   reducerResourceConflictStatusRisky,
		Evidence: "endpoint-node materialization shares the security-group scope trigger until partitioned loads exist",
	},
	{
		Domain:   reducer.DomainSecurityGroupRuleMaterialization,
		Status:   reducerResourceConflictStatusRisky,
		Evidence: "rule-node materialization shares the security-group scope trigger until partitioned loads exist",
	},
	blockedResourceConflictPolicy(reducer.DomainAWSRelationshipMaterialization),
	blockedResourceConflictPolicy(reducer.DomainGCPRelationshipMaterialization),
	blockedResourceConflictPolicy(reducer.DomainAzureRelationshipMaterialization),
	blockedResourceConflictPolicy(reducer.DomainWorkloadCloudRelationshipMaterialization),
	blockedResourceConflictPolicy(reducer.DomainIAMCanAssumeMaterialization),
	blockedResourceConflictPolicy(reducer.DomainIAMEscalationMaterialization),
	blockedResourceConflictPolicy(reducer.DomainIAMCanPerformMaterialization),
	blockedResourceConflictPolicy(reducer.DomainS3LogsToMaterialization),
	blockedResourceConflictPolicy(reducer.DomainS3ExternalPrincipalGrantMaterialization),
	blockedResourceConflictPolicy(reducer.DomainS3InternetExposureMaterialization),
	blockedResourceConflictPolicy(reducer.DomainRDSPostureMaterialization),
	blockedResourceConflictPolicy(reducer.DomainEC2UsesProfileMaterialization),
	blockedResourceConflictPolicy(reducer.DomainIAMInstanceProfileRoleMaterialization),
	blockedResourceConflictPolicy(reducer.DomainEC2InternetExposureMaterialization),
	blockedResourceConflictPolicy(reducer.DomainEC2BlockDeviceKMSPostureMaterialization),
	blockedResourceConflictPolicy(reducer.DomainKubernetesCorrelationMaterialization),
	blockedResourceConflictPolicy(reducer.DomainSecurityGroupReachabilityMaterialization),
}

func blockedResourceConflictPolicy(domain reducer.Domain) reducerResourceConflictPolicy {
	return reducerResourceConflictPolicy{
		Domain:   domain,
		Status:   reducerResourceConflictStatusBlocked,
		Evidence: "handler still performs scope-wide load, readiness, write, or retract semantics",
	}
}

// reducerConflictDomainKey returns the durable claim fence for one reducer
// intent. The key remains scope-scoped so newer generations cannot overtake
// older work for the same repo, while the domain separates graph families that
// do not share the same NornicDB write-hot spots.
//
// Platform-graph partition (#3672): before this change all five platform-graph
// domains shared a single coarse (platform_graph, scopeKey) pair, so only one
// of them could be in-flight per scope. ~26k intents drained serially (~25-min
// queue wait). The partition splits the conflict key by the actual graph-write
// target so non-conflicting work drains concurrently while true write conflicts
// still serialize.
//
// Verified write-target analysis (source-confirmed, see the per-domain table in
// go/internal/storage/postgres/README.md):
//
//	| Domain                      | Write target                              | MERGEs :Platform {id}? |
//	|-----------------------------|-------------------------------------------|------------------------|
//	| WorkloadMaterialization     | MERGE (p:Platform {id}) + Workload/Endpoint| YES (NO advisory lock) |
//	| DeploymentMapping           | MERGE (p:Platform {id}) + PROVISIONS_PLATFORM | YES (PlatformGraphLocker) |
//	| WorkloadIdentity            | Postgres fact_records ON CONFLICT (fact_id)| no (idempotent upsert) |
//	| CloudAssetResolution        | Postgres fact_records ON CONFLICT (fact_id)| no (idempotent upsert) |
//	| DeployableUnitCorrelation   | MERGE (Repository)-[:CORRELATES_DEPLOYABLE_UNIT]->(Repository) | no |
//
// WorkloadMaterialization and DeploymentMapping BOTH run
// MERGE (p:Platform {id: row.platform_id}) over the same platform_id namespace,
// and WorkloadMaterialization does NOT hold the PlatformGraphLocker advisory
// lock that DeploymentMapping uses. Running them concurrently for the same scope
// would race two unprotected MERGEs on the same Platform node, producing
// commit-time uniqueness conflicts / retries / eventual dead-letter. They MUST
// stay serialized against each other (#3672 review P1).
//
// Therefore the two Platform-node writers share ONE conflict key (the
// platform-node-writer group token + scope) so the queue fence still serializes
// them exactly as before. The three non-Platform-writing domains each get their
// own per-domain key so they drain concurrently with each other and with the
// platform-node-writer pair. This is the smallest provably-correct partition:
// it removes the false serialization between the graph-edge / Postgres-fact
// domains while preserving the real same-target Platform MERGE serialization.
func reducerConflictDomainKey(intent projector.ReducerIntent) (string, string) {
	scopeKey := strings.TrimSpace(intent.ScopeID)
	if policy, ok := reducerResourceConflictPolicyFor(intent.Domain); ok {
		if policy.Status == reducerResourceConflictStatusSafe {
			if key, ok := reducerCloudResourceNodeConflictKey(intent); ok {
				return reducerConflictDomainCloudResourceNode, key
			}
		}
		return reducerConflictDomainResourceScope, reducerResourceScopeConflictKey(scopeKey)
	}
	switch intent.Domain {
	case reducer.DomainCodeCallMaterialization,
		reducer.DomainSemanticEntityMaterialization,
		reducer.DomainSQLRelationshipMaterialization,
		reducer.DomainShellExecMaterialization,
		reducer.DomainInheritanceMaterialization:
		return reducerConflictDomainCodeGraph, scopeKey
	case reducer.DomainWorkloadMaterialization,
		reducer.DomainPlatformInfraMaterialization,
		reducer.DomainDeploymentMapping:
		// One shared, scope-keyed conflict key serializes three domains that must
		// not overlap for the same scope:
		//   - WorkloadMaterialization and PlatformInfraMaterialization both
		//     MERGE (p:Platform {id}) over the same platform_id namespace, and
		//     WorkloadMaterialization holds no advisory lock, so they must
		//     serialize to avoid a commit-time MERGE race (#3672 review P1).
		//   - DeploymentMapping does not MERGE Platform, but its cross-repo
		//     resolution requeues workload materialization via
		//     ReplayWorkloadMaterialization, which can only reopen a SUCCEEDED
		//     workload row; if a same-scope workload item is claimed/running the
		//     replay's ON CONFLICT DO NOTHING re-enqueue is silently lost and the
		//     in-flight workload commits without the stronger deployment evidence.
		//     Sharing this key keeps DeploymentMapping and WorkloadMaterialization
		//     from running concurrently for a scope, preserving replay ordering.
		return reducerConflictDomainPlatformGraph, reducerPlatformNodeWriterConflictKey(scopeKey)
	case reducer.DomainWorkloadIdentity,
		reducer.DomainDeployableUnitCorrelation,
		reducer.DomainCloudAssetResolution:
		// None of these MERGE a :Platform node and none requeue workload
		// materialization: WorkloadIdentity and CloudAssetResolution upsert
		// Postgres fact_records keyed by intent id (idempotent), and
		// DeployableUnitCorrelation MERGEs only
		// (Repository)-[:CORRELATES_DEPLOYABLE_UNIT]->(Repository) edges. They do
		// not conflict with the Platform-node writers or with each other, so each
		// gets its own per-domain key and drains concurrently.
		return reducerConflictDomainPlatformGraph, reducerPlatformGraphConflictKey(intent.Domain, scopeKey)
	default:
		return reducerConflictDomainScope, scopeKey
	}
}

// reducerPlatformNodeWriterConflictKey returns the shared, scope-keyed conflict
// key for the reducer domains that must serialize for a scope:
// WorkloadMaterialization, PlatformInfraMaterialization, and DeploymentMapping.
// All three produce the IDENTICAL key for a scope so the queue fence keeps them
// from being claimed concurrently. WorkloadMaterialization and
// PlatformInfraMaterialization both MERGE (p:Platform {id}); WorkloadMaterialization
// holds no advisory lock, so this fence prevents an unprotected concurrent MERGE
// on the same Platform node (#3672 review P1). DeploymentMapping does not MERGE
// Platform but requeues workload materialization via ReplayWorkloadMaterialization
// (reopen-succeeded-only), so it must not overlap a same-scope in-flight workload
// item or the replay is silently lost.
func reducerPlatformNodeWriterConflictKey(scopeKey string) string {
	return reducerHashedConflictKey(
		reducerConflictKeyPrefixPlatformGraph,
		"platform-node-writer:"+strings.TrimSpace(scopeKey),
	)
}

// reducerPlatformGraphConflictKey returns a versioned hashed conflict key for a
// non-Platform-writing platform-graph domain. It encodes both the domain name
// and the scope so that different such domains for the same scope produce
// distinct conflict keys (enabling concurrent drain) while the same domain and
// scope always produce the same key (preserving same-scope serialization within
// a domain). It MUST NOT be used for the Platform-node writers
// (WorkloadMaterialization, DeploymentMapping) — those share one key via
// reducerPlatformNodeWriterConflictKey.
func reducerPlatformGraphConflictKey(domain reducer.Domain, scopeKey string) string {
	return reducerHashedConflictKey(
		reducerConflictKeyPrefixPlatformGraph,
		string(domain)+":"+strings.TrimSpace(scopeKey),
	)
}

func reducerResourceConflictPolicyFor(domain reducer.Domain) (reducerResourceConflictPolicy, bool) {
	for _, policy := range reducerResourceConflictPolicies {
		if policy.Domain == domain {
			return policy, true
		}
	}
	return reducerResourceConflictPolicy{}, false
}

func reducerCloudResourceNodeConflictKey(intent projector.ReducerIntent) (string, bool) {
	entityKey := strings.TrimSpace(intent.EntityKey)
	if !strings.HasPrefix(entityKey, "aws_resource_materialization:") {
		return "", false
	}
	if reducerConflictValueHasRawLocator(entityKey) {
		return "", false
	}
	return reducerHashedConflictKey(reducerConflictKeyPrefixCloudResourceNode, entityKey), true
}

func reducerResourceScopeConflictKey(scopeKey string) string {
	return reducerHashedConflictKey(reducerConflictKeyPrefixResourceScope, scopeKey)
}

func reducerHashedConflictKey(prefix string, value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return prefix + hex.EncodeToString(sum[:])
}

func reducerConflictValueHasRawLocator(value string) bool {
	raw := strings.TrimSpace(strings.ToLower(value))
	for _, marker := range []string{
		"arn" + ":",
		"/sub" + "scriptions/",
		"//",
		"/",
		"credential",
		"secret",
		"token",
	} {
		if strings.Contains(raw, marker) {
			return true
		}
	}
	return reducerConflictValueHasIPv4(raw)
}

func reducerConflictValueHasIPv4(value string) bool {
	candidates := strings.FieldsFunc(value, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	})
	for _, candidate := range candidates {
		if strings.Count(candidate, ".") != 3 {
			continue
		}
		ip := net.ParseIP(candidate)
		if ip != nil && ip.To4() != nil {
			return true
		}
	}
	return false
}
