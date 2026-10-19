// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/secretsiam"
)

// usesServiceAccountFamily is the materialized-edge family key this guard
// asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const usesServiceAccountFamily = "uses_service_account"

// usesServiceAccountRelationshipType is the single relationship type the
// workload -> service-account edge MERGEs: the static
// SECRETS_IAM_USES_SERVICE_ACCOUNT token baked into
// CanonicalSecretsIAMUsesServiceAccountEdgeUpsertCypher, read off the
// template rather than derived from the port or family name.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const usesServiceAccountRelationshipType = "SECRETS_IAM_USES_SERVICE_ACCOUNT"

// usesServiceAccountConfidence is the confidence every produced row stamps.
// Exact-only admission is the family contract: the extractor emits a
// workload edge only for state=exact chains, so pinning the stamped value
// catches a future second confidence silently changing what the rows claim.
const usesServiceAccountConfidence = "exact"

// usesServiceAccountExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const usesServiceAccountExpectedEdgesRelPath = "go/internal/ifa/testdata/usesserviceaccount/ifa-uses-service-account-family-expected-edges.json"

// usesServiceAccountExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func usesServiceAccountExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, usesServiceAccountExpectedEdgesRelPath)
}

// resolveUsesServiceAccountMaterializedEdges is uses_service_account's named
// vacuity guard (#6228).
//
// The production extractor feeding this port takes the Odù's facts whole —
// ExtractSecretsIAMGraphRows switches on the read-model kinds itself and
// ignores every other kind — so there is no partition step to mirror, only
// the extractor call the SecretsIAMGraphProjectionHandler makes. What the
// fixture proves is the extractor's exact-only admission: two exact chains
// carrying workload ids emit one edge each, while a partial chain, an exact
// chain with no workload id, and a tombstoned chain each resolve to nothing
// rather than to a fabricated edge.
//
// The extractor is pure and total: it never quarantines and never errors,
// counting every skip in its tally instead. The guard therefore proves
// restraint through the exact-set comparison (a ghost edge or a dropped
// producer reds here either way) rather than through a quarantine tripwire,
// which has no meaning for schemaless read-model payloads.
func resolveUsesServiceAccountMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, usesServiceAccountFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows := secretsiam.ExtractSecretsIAMGraphRows(odu.Facts)
	if len(rows.UsesServiceAccountEdges) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractSecretsIAMGraphRows produced zero SECRETS_IAM_USES_SERVICE_ACCOUNT rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows.UsesServiceAccountEdges {
		if got := anyToStringValue(row["confidence"]); got != usesServiceAccountConfidence {
			return false, fmt.Sprintf("odù %q: row %d carries confidence %q, want %q (the exact-only admission contract)", odu.Name, index, got, usesServiceAccountConfidence)
		}
	}

	actual := usesServiceAccountRowsToExpectedEdges(rows.UsesServiceAccountEdges)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, usesServiceAccountFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractSecretsIAMGraphRows reproduces the expected %d-edge SECRETS_IAM_USES_SERVICE_ACCOUNT set exactly across all %d registry type(s) and %d fact envelope(s); the partial, workload-less, and tombstoned chains produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// usesServiceAccountRowsToExpectedEdges converts the extractor's workload
// rows one-for-one into the edge identity the write template MERGEs.
//
// workload_uid and service_account_uid are the template's two MATCH anchors
// (the KubernetesWorkload node and the SecretsIAMServiceAccount node), so
// they are the edge's source and target identity. confidence is SET after
// the MERGE — deliberately outside the relationship key — and is asserted
// here as a mutable property so a second confidence cannot pass on edge
// identity alone.
//
// scope_id, generation_id, evidence_source, and evidence_fact_ids are
// stamped from the read-model payload and the writer's per-run intent
// arguments; no static set can pin per-run values, and opaque fact ids prove
// nothing about production, so neither is part of what this offline guard
// asserts. The writer unit test asserting the annotated row contents covers
// their stamping instead.
func usesServiceAccountRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: usesServiceAccountRelationshipType,
			SourceEntityID:   anyToStringValue(row["workload_uid"]),
			TargetEntityID:   anyToStringValue(row["service_account_uid"]),
			Properties: map[string]string{
				"confidence": anyToStringValue(row["confidence"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
