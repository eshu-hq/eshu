// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha1" //nolint:gosec // sha1 is a non-cryptographic identity digest, matching the resolved-relationship and deployment-artifact identity contracts.
	"encoding/hex"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Dependencies evidence family (#1987, part of #1943/#1797). A service's
// dependency evidence is the set of resolved dependency relationships that
// involve the service's repository. Each relationship becomes one
// generation-stable service_evidence_snapshots row in the dependencies family,
// reusing the Stage-1 lineage, payload-hash, and tombstone machinery verbatim.
// It shares deployment's source verbatim — the same resolved_relationships
// Postgres path and the same RepositoryScopedResolvedRelationshipLoader — and
// differs only in which relationship types it admits and its evidence_family
// label.
//
// Stable identity: the resolved relationship's Postgres primary key
// (resolved_id) embeds the resolution generation id (see
// relationships.ResolvedRelationshipID, which digests the generation into the
// id), and the query-layer dependency read model surfaces that same generation-
// bearing resolved_id. Neither is usable as a changed-since diff key: the same
// logical relationship would get a new key every resolution generation,
// producing 100% churn — exactly the false-churn failure design #1943 warns
// about. The dependency identity is therefore derived from the relationship's
// generation-INDEPENDENT natural key
// (relationship_type, source_repo_id, target_repo_id, source_entity_id,
// target_entity_id), digested with the same sha1 natural-key contract the
// deployment family uses, so the same relationship keeps the same key across
// generations and the FULL OUTER JOIN diff can classify updated vs unchanged.
// This is the identity-vs-generation distinction from design #1231.

// ServiceDependencyEvidence is one generation-stable dependency row for a
// service. Identity is the stable per-relationship natural-key digest; the
// generation lives in the row, never in the key. A retired relationship carries
// Retired=true so the delta classifies it explicitly rather than letting it
// vanish into unchanged.
type ServiceDependencyEvidence struct {
	// Identity is the generation-independent per-relationship identity digest
	// (see serviceDependencyEvidenceIdentity). It is combined with the service id
	// to form the service_evidence_key.
	Identity string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It captures the relationship's stable, observable fields.
	Payload map[string]any
	// Retired records a relationship that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// serviceDependencyRelationshipTypes is the closed set of relationship types
// that constitute dependency evidence for a service. It is the complement of the
// deployment family within the resolved cross-repo relationship space: DEPENDS_ON
// (generic dependency), USES_MODULE (consumes a target module repository), and
// READS_CONFIG_FROM (granted read access to target config). A relationship inside
// the deployment set (DEPLOYS_FROM / DISCOVERS_CONFIG_IN / PROVISIONS_DEPENDENCY_FOR
// / RUNS_ON) is deployment evidence, never admitted into the dependencies family.
func serviceDependencyRelationshipTypes() map[relationships.RelationshipType]struct{} {
	return map[relationships.RelationshipType]struct{}{
		relationships.RelDependsOn:       {},
		relationships.RelUsesModule:      {},
		relationships.RelReadsConfigFrom: {},
	}
}

// isServiceDependencyRelationship reports whether a resolved relationship belongs
// to the dependency evidence family.
func isServiceDependencyRelationship(rel relationships.ResolvedRelationship) bool {
	_, ok := serviceDependencyRelationshipTypes()[rel.RelationshipType]
	return ok
}

// ServiceDependencyEvidenceKey returns the generation-independent identity for one
// dependency row: dependencies:<service_id>:<identity>. The identity is the
// per-relationship natural-key digest; the generation is stored in a column,
// never embedded here.
func ServiceDependencyEvidenceKey(serviceID, identity string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyDependencies,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(identity),
	}, ":")
}

// serviceDependencyEvidenceIdentity derives the generation-independent identity
// digest for one resolved dependency relationship from its natural key. It must
// not include the resolution generation id or the resolved_id, both of which are
// generation-bearing. Entity ids are included so a relationship that resolves to
// a specific entity stays distinct from a repo-level one. The digest mirrors the
// deployment family's natural-key sha1 contract so both families key the same way.
func serviceDependencyEvidenceIdentity(rel relationships.ResolvedRelationship) string {
	parts := []string{
		string(rel.RelationshipType),
		strings.TrimSpace(rel.SourceRepoID),
		strings.TrimSpace(rel.TargetRepoID),
		strings.TrimSpace(rel.SourceEntityID),
		strings.TrimSpace(rel.TargetEntityID),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\x00"))) //nolint:gosec // non-cryptographic identity digest.
	return hex.EncodeToString(sum[:])
}

// serviceDependencyEvidencePayload captures the stable, observable fields of a
// resolved dependency relationship whose change should flip the row to updated.
// It deliberately excludes the resolution generation id and resolved_id so an
// unchanged relationship across re-materializations hashes identically and
// classifies as unchanged.
func serviceDependencyEvidencePayload(rel relationships.ResolvedRelationship) map[string]any {
	return map[string]any{
		"relationship_type": string(rel.RelationshipType),
		"source_repo_id":    strings.TrimSpace(rel.SourceRepoID),
		"target_repo_id":    strings.TrimSpace(rel.TargetRepoID),
		"source_entity_id":  strings.TrimSpace(rel.SourceEntityID),
		"target_entity_id":  strings.TrimSpace(rel.TargetEntityID),
		"confidence":        rel.Confidence,
		"resolution_source": string(rel.ResolutionSource),
	}
}

// buildServiceDependencyEvidence converts the service's resolved dependency
// relationships into deterministic, deduped dependency evidence rows. Only
// dependency-family relationship types contribute; relationships are deduped by
// stable identity (a later entry for the same identity wins) and ordered by
// identity so the generation fingerprint is input-order-independent.
func buildServiceDependencyEvidence(resolved []relationships.ResolvedRelationship) []ServiceDependencyEvidence {
	deduped := make(map[string]ServiceDependencyEvidence, len(resolved))
	for _, rel := range resolved {
		if !isServiceDependencyRelationship(rel) {
			continue
		}
		identity := serviceDependencyEvidenceIdentity(rel)
		deduped[identity] = ServiceDependencyEvidence{
			Identity: identity,
			Payload:  serviceDependencyEvidencePayload(rel),
		}
	}
	rows := make([]ServiceDependencyEvidence, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Identity < rows[j].Identity
	})
	return rows
}

// addServiceDependencyEvidence normalizes dependency evidence into the shared
// snapshot row map keyed by service_evidence_key. It mirrors
// addServiceDeploymentEvidence: a later non-retired entry for the same identity
// wins, and an explicit retirement always wins so a re-materialization cannot
// resurrect a removed relationship.
func addServiceDependencyEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceDependencyEvidence,
) {
	for _, item := range evidence {
		identity := strings.TrimSpace(item.Identity)
		if identity == "" {
			continue
		}
		key := ServiceDependencyEvidenceKey(serviceID, identity)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyDependencies,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}

// groupDependencyRelationshipsByRepo buckets resolved dependency relationships by
// the repository that owns the dependency evidence. A service depends on its
// targets from the source side (the service repo consumes a module, depends on a
// repo, or reads config from a repo), so the source repo is the dependency-
// evidence owner for the service. Relationships are bucketed under the source repo
// id, mirroring groupDeploymentRelationshipsByRepo.
func groupDependencyRelationshipsByRepo(
	resolved []relationships.ResolvedRelationship,
) map[string][]relationships.ResolvedRelationship {
	byRepo := map[string][]relationships.ResolvedRelationship{}
	for _, rel := range resolved {
		if !isServiceDependencyRelationship(rel) {
			continue
		}
		source := strings.TrimSpace(rel.SourceRepoID)
		if source == "" {
			continue
		}
		byRepo[source] = append(byRepo[source], rel)
	}
	return byRepo
}
