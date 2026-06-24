// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"
)

// RepositoryScopedRuntimeInstanceLoader returns the materialized runtime
// instances for one or more repositories, regardless of which repository
// generation produced them. It is the runtime-family analogue of
// RepositoryScopedResolvedRelationshipLoader: the service catalog correlation
// handler uses it to source the runtime evidence family (#1986) for each
// correlated service's repository. The returned instances must carry only durable
// platform/environment/workload identity, never a resolution or materialization
// generation id, so the runtime service_evidence_key stays generation-stable.
type RepositoryScopedRuntimeInstanceLoader interface {
	GetRuntimeInstancesForRepos(
		ctx context.Context,
		repoIDs []string,
	) (map[string][]ServiceRuntimeInstance, error)
}

// Runtime evidence family (#1986, part of #1943/#1797). A service's runtime
// evidence is the set of materialized runtime instances of its workload. Each
// instance becomes one generation-stable service_evidence_snapshots row in the
// runtime family, reusing the Stage-1 lineage, payload-hash, and tombstone
// machinery verbatim.
//
// Stable identity (verified generation-independent). A runtime instance is keyed
// by its durable platform/environment/workload identity:
//
//	runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>
//
// The read model that surfaces runtime lanes
// (query.buildServiceDeploymentLanes over workloadContext["instances"]) reads
// these fields off the WorkloadInstance and Platform graph nodes:
// i.id (the durable instance id workload-instance:<workload_name>:<environment>),
// i.environment, p.kind, and p.name. The reducer projection
// (reducer.projection.go) constructs the instance id as
// "workload-instance:<workload_name>:<environment>" and the platform kind from
// the candidate's inferred runtime platform — neither embeds a resolution
// generation id or a materialization generation id. Unlike the deployment family
// (whose resolved_id digests the resolution generation) and the dependencies
// family, runtime instance identity carries no generation-bearing field, so the
// same logical instance keeps the same key across re-materializations and the
// FULL OUTER JOIN diff can classify updated vs unchanged. This is the
// identity-vs-generation distinction from design #1231.

// ServiceRuntimeInstance is one durable runtime instance of a service's workload,
// as read from the materialized runtime read model. The reducer converts it into
// a generation-stable runtime evidence row. Only the durable platform/
// environment/workload identity is keyed; the generation lives in the row, never
// in the key.
type ServiceRuntimeInstance struct {
	// PlatformKind is the durable runtime platform class (for example
	// "kubernetes" or "ecs"). It is part of the stable identity.
	PlatformKind string
	// Environment is the durable runtime environment (for example "prod"). It is
	// part of the stable identity.
	Environment string
	// WorkloadRef is the durable cluster/namespace/workload-name identity of the
	// instance (the WorkloadInstance id, for example
	// "workload-instance:checkout:prod"). It is part of the stable identity and
	// carries no resolution or materialization generation id.
	WorkloadRef string
	// PlatformName is the runtime platform name (observable, hashed into the
	// payload). It is not part of the identity.
	PlatformName string
	// WorkloadName is the workload's name (observable, hashed into the payload).
	WorkloadName string
	// Confidence is the materialization confidence of the instance (observable,
	// hashed into the payload so a confidence change flips the row to updated).
	Confidence float64
}

// ServiceRuntimeEvidence is one generation-stable runtime row for a service.
// Identity is the stable per-instance durable identity digest; the generation
// lives in the row, never in the key. A retired instance carries Retired=true so
// the delta classifies it explicitly rather than letting it vanish into
// unchanged.
type ServiceRuntimeEvidence struct {
	// Identity is the generation-independent per-instance identity
	// (see serviceRuntimeEvidenceIdentity). It is combined with the service id to
	// form the service_evidence_key.
	Identity string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It captures the instance's stable, observable fields.
	Payload map[string]any
	// Retired records an instance that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// ServiceRuntimeEvidenceKey returns the generation-independent identity for one
// runtime row: runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>.
// The generation is stored in a column, never embedded here.
func ServiceRuntimeEvidenceKey(serviceID string, instance ServiceRuntimeInstance) string {
	return ServiceRuntimeEvidenceKeyFromIdentity(serviceID, serviceRuntimeEvidenceIdentity(instance))
}

// ServiceRuntimeEvidenceKeyFromIdentity returns the runtime service_evidence_key
// for an already-computed identity: runtime:<service_id>:<identity>. It lets the
// writer and tests share the same key shape without recomputing the identity.
func ServiceRuntimeEvidenceKeyFromIdentity(serviceID, identity string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyRuntime,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(identity),
	}, ":")
}

// serviceRuntimeEvidenceIdentity derives the generation-independent identity for
// one runtime instance from its durable platform/environment/workload identity.
// It must not include any resolution or materialization generation id. The fields
// are joined with the runtime key separator so the same logical instance hashes
// to the same identity across re-materializations.
func serviceRuntimeEvidenceIdentity(instance ServiceRuntimeInstance) string {
	parts := []string{
		strings.TrimSpace(instance.PlatformKind),
		strings.TrimSpace(instance.Environment),
		strings.TrimSpace(instance.WorkloadRef),
	}
	return strings.Join(parts, ":")
}

// serviceRuntimeEvidencePayload captures the stable, observable fields of a
// runtime instance whose change should flip the row to updated. It deliberately
// excludes any generation/resolution id and the raw instance_id so an unchanged
// instance across re-materializations hashes identically and classifies as
// unchanged.
func serviceRuntimeEvidencePayload(instance ServiceRuntimeInstance) map[string]any {
	return map[string]any{
		"platform_kind": strings.TrimSpace(instance.PlatformKind),
		"platform_name": strings.TrimSpace(instance.PlatformName),
		"environment":   strings.TrimSpace(instance.Environment),
		"workload_ref":  strings.TrimSpace(instance.WorkloadRef),
		"workload_name": strings.TrimSpace(instance.WorkloadName),
		"confidence":    instance.Confidence,
	}
}

// runtimeInstanceHasDurableIdentity reports whether an instance carries enough
// durable identity to be keyed: a workload ref plus at least one of platform kind
// or environment. An instance without a durable workload ref cannot produce a
// stable diff key and is dropped rather than keyed on an empty identity.
func runtimeInstanceHasDurableIdentity(instance ServiceRuntimeInstance) bool {
	if strings.TrimSpace(instance.WorkloadRef) == "" {
		return false
	}
	return strings.TrimSpace(instance.PlatformKind) != "" || strings.TrimSpace(instance.Environment) != ""
}

// buildServiceRuntimeEvidence converts the service's materialized runtime
// instances into deterministic, deduped runtime evidence rows. Instances without
// a durable identity are dropped; instances are deduped by stable identity (a
// later entry for the same identity wins) and ordered by identity so the
// generation fingerprint is input-order-independent.
func buildServiceRuntimeEvidence(instances []ServiceRuntimeInstance) []ServiceRuntimeEvidence {
	deduped := make(map[string]ServiceRuntimeEvidence, len(instances))
	for _, instance := range instances {
		if !runtimeInstanceHasDurableIdentity(instance) {
			continue
		}
		identity := serviceRuntimeEvidenceIdentity(instance)
		deduped[identity] = ServiceRuntimeEvidence{
			Identity: identity,
			Payload:  serviceRuntimeEvidencePayload(instance),
		}
	}
	rows := make([]ServiceRuntimeEvidence, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Identity < rows[j].Identity
	})
	return rows
}

// addServiceRuntimeEvidence normalizes runtime evidence into the shared snapshot
// row map keyed by service_evidence_key. It mirrors addServiceDeploymentEvidence:
// a later non-retired entry for the same identity wins, and an explicit
// retirement always wins so a re-materialization cannot resurrect a removed
// instance.
func addServiceRuntimeEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceRuntimeEvidence,
) {
	for _, item := range evidence {
		identity := strings.TrimSpace(item.Identity)
		if identity == "" {
			continue
		}
		key := ServiceRuntimeEvidenceKeyFromIdentity(serviceID, identity)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyRuntime,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}
