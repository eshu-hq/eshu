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

// Deployment evidence family (#1985, part of #1943/#1797). A service's deployment
// evidence is the set of resolved deployment relationships that involve the
// service's repository. Each relationship becomes one generation-stable
// service_evidence_snapshots row in the deployment family, reusing the Stage-1
// lineage, payload-hash, and tombstone machinery verbatim.
//
// Stable identity: the resolved relationship's Postgres primary key
// (resolved_id) and the query-layer artifact_id BOTH embed the resolution
// generation id (see relationships.ResolvedRelationshipID, which digests the
// generation into the id, and query.deploymentEvidenceArtifactID, which digests
// resolved_id). Neither is usable as a changed-since diff key: the same logical
// relationship would get a new key every resolution generation, producing 100%
// churn — exactly the failure the design doc warns about for the ownership
// correlation key. The deployment identity is therefore derived from the
// relationship's generation-INDEPENDENT natural key
// (relationship_type, source_repo_id, target_repo_id, source_entity_id,
// target_entity_id), so the same relationship keeps the same key across
// generations and the FULL OUTER JOIN diff can classify updated vs unchanged.
// This is the identity-vs-generation distinction from design #1231.

// ServiceDeploymentEvidence is one generation-stable deployment row for a
// service. Identity is the stable per-relationship natural-key digest; the
// generation lives in the row, never in the key. A retired relationship carries
// Retired=true so the delta classifies it explicitly rather than letting it
// vanish into unchanged.
type ServiceDeploymentEvidence struct {
	// Identity is the generation-independent per-relationship identity digest
	// (see serviceDeploymentEvidenceIdentity). It is combined with the service id
	// to form the service_evidence_key.
	Identity string
	// Payload is the durable evidence body whose hash drives updated-vs-unchanged
	// classification. It captures the relationship's stable, observable fields.
	Payload map[string]any
	// Retired records a relationship that was explicitly removed in this
	// re-materialization. It is written as a tombstone row.
	Retired bool
}

// serviceDeploymentRelationshipTypes is the closed set of relationship types
// that constitute deployment evidence for a service. It mirrors the deployment
// relationship classes the cross-repo resolution writer routes as deployment
// edges (DEPLOYS_FROM / DISCOVERS_CONFIG_IN / PROVISIONS_DEPENDENCY_FOR /
// RUNS_ON). A relationship outside this set (for example DEPENDS_ON or
// USES_MODULE) is dependency evidence, a separate follow-up family, and is never
// admitted into the deployment family.
func serviceDeploymentRelationshipTypes() map[relationships.RelationshipType]struct{} {
	return map[relationships.RelationshipType]struct{}{
		relationships.RelDeploysFrom:             {},
		relationships.RelDiscoversConfigIn:       {},
		relationships.RelProvisionsDependencyFor: {},
		relationships.RelRunsOn:                  {},
	}
}

// isServiceDeploymentRelationship reports whether a resolved relationship belongs
// to the deployment evidence family.
func isServiceDeploymentRelationship(rel relationships.ResolvedRelationship) bool {
	_, ok := serviceDeploymentRelationshipTypes()[rel.RelationshipType]
	return ok
}

// ServiceDeploymentEvidenceKey returns the generation-independent identity for one
// deployment row: deployment:<service_id>:<identity>. The identity is the
// per-relationship natural-key digest; the generation is stored in a column,
// never embedded here.
func ServiceDeploymentEvidenceKey(serviceID, identity string) string {
	return strings.Join([]string{
		ServiceEvidenceFamilyDeployment,
		strings.TrimSpace(serviceID),
		strings.TrimSpace(identity),
	}, ":")
}

// serviceDeploymentEvidenceIdentity derives the generation-independent identity
// digest for one resolved deployment relationship from its natural key. It must
// not include the resolution generation id or the resolved_id, both of which are
// generation-bearing. Entity ids are included so a relationship that resolves to
// a specific entity (for example a RUNS_ON platform) stays distinct from a
// repo-level one.
func serviceDeploymentEvidenceIdentity(rel relationships.ResolvedRelationship) string {
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

// serviceDeploymentEvidencePayload captures the stable, observable fields of a
// resolved deployment relationship whose change should flip the row to updated.
// It deliberately excludes the resolution generation id and resolved_id so an
// unchanged relationship across re-materializations hashes identically and
// classifies as unchanged.
func serviceDeploymentEvidencePayload(rel relationships.ResolvedRelationship) map[string]any {
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

// buildServiceDeploymentEvidence converts the service's resolved deployment
// relationships into deterministic, deduped deployment evidence rows. Only
// deployment-family relationship types contribute; relationships are deduped by
// stable identity (a later entry for the same identity wins) and ordered by
// identity so the generation fingerprint is input-order-independent.
func buildServiceDeploymentEvidence(resolved []relationships.ResolvedRelationship) []ServiceDeploymentEvidence {
	deduped := make(map[string]ServiceDeploymentEvidence, len(resolved))
	for _, rel := range resolved {
		if !isServiceDeploymentRelationship(rel) {
			continue
		}
		identity := serviceDeploymentEvidenceIdentity(rel)
		deduped[identity] = ServiceDeploymentEvidence{
			Identity: identity,
			Payload:  serviceDeploymentEvidencePayload(rel),
		}
	}
	rows := make([]ServiceDeploymentEvidence, 0, len(deduped))
	for _, row := range deduped {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Identity < rows[j].Identity
	})
	return rows
}

// addServiceDeploymentEvidence normalizes deployment evidence into the shared
// snapshot row map keyed by service_evidence_key. It mirrors
// addServiceOwnershipEvidence: a later non-retired entry for the same identity
// wins, and an explicit retirement always wins so a re-materialization cannot
// resurrect a removed relationship.
func addServiceDeploymentEvidence(
	deduped map[string]serviceEvidenceRow,
	serviceID string,
	evidence []ServiceDeploymentEvidence,
) {
	for _, item := range evidence {
		identity := strings.TrimSpace(item.Identity)
		if identity == "" {
			continue
		}
		key := ServiceDeploymentEvidenceKey(serviceID, identity)
		payload := item.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		existing, ok := deduped[key]
		if ok && existing.tombstone && !item.Retired {
			continue
		}
		deduped[key] = serviceEvidenceRow{
			family:      ServiceEvidenceFamilyDeployment,
			evidenceKey: key,
			payloadHash: ServiceEvidencePayloadHash(payload),
			tombstone:   item.Retired,
			payload:     payload,
		}
	}
}
