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
	case reducer.DomainWorkloadIdentity,
		reducer.DomainDeployableUnitCorrelation,
		reducer.DomainCloudAssetResolution,
		reducer.DomainDeploymentMapping,
		reducer.DomainWorkloadMaterialization:
		return reducerConflictDomainPlatformGraph, scopeKey
	default:
		return reducerConflictDomainScope, scopeKey
	}
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
