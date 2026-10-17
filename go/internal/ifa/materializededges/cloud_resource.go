// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	materialized "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
)

// cloudResourceFamily is the materialized-edge family key this guard asserts
// (#8358), matching the predicate-scoped registration in
// cypher/edge/materialized/predicate_families.go and the domain
// `eshu-ifa assert-edges -domain cloud_resource` addresses.
const cloudResourceFamily = "cloud_resource"

// cloudResourceExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const cloudResourceExpectedEdgesRelPath = "go/internal/ifa/testdata/cloudresource/ifa-cloud-resource-family-expected-edges.json"

// cloudResourceExpectedEdgesPath joins repoRoot onto the fixture path.
func cloudResourceExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, cloudResourceExpectedEdgesRelPath)
}

// loadPredicateFamilyExpectedEdges loads a predicate-scoped family's
// expected-edge fixture and validates it against the registered predicate:
// the set is non-empty (an empty set passes vacuously), every edge pairs on
// ONE registered writer (its type carries that writer's prefix and its
// asserted evidence_source is that writer's stamp), and every registered
// writer pairing is exercised at least once.
//
// The pairing-coverage check is this family's analogue of
// missingDirectFamilyExpectedTypes. A finite-set guard fails when the
// fixture stops naming a registry type; here the registry is three
// (prefix, stamp) pairings, so a fixture that stopped exercising one
// provider — after, say, its extractor regressed to zero rows and the
// fixture was trimmed to match — fails instead of continuing to report the
// family covered on two of its three writers.
func loadPredicateFamilyExpectedEdges(path, family, oduName string) ([]ExpectedEdge, materialized.PredicateScopedEdgeFamily, string) {
	predicate, ok := MaterializedEdgeDomainPredicate(family)
	if !ok {
		return nil, materialized.PredicateScopedEdgeFamily{}, fmt.Sprintf("odù %q: family %q is not registered as predicate-scoped", oduName, family)
	}
	expected, err := LoadExpectedEdges(path, family)
	if err != nil {
		return nil, materialized.PredicateScopedEdgeFamily{}, err.Error()
	}
	if len(expected) == 0 {
		return nil, materialized.PredicateScopedEdgeFamily{}, fmt.Sprintf("odù %q: %s expected edges %s declares no edges; an empty expected set would make the gate vacuous", oduName, family, path)
	}
	exercised := make([]bool, len(predicate.Writers))
	for i, edge := range expected {
		stamp := edge.Properties["evidence_source"]
		paired := false
		for w, writer := range predicate.Writers {
			if writer.EvidenceSource == stamp && strings.HasPrefix(edge.RelationshipType, writer.TypePrefix) {
				exercised[w] = true
				paired = true
				break
			}
		}
		if !paired {
			return nil, materialized.PredicateScopedEdgeFamily{}, fmt.Sprintf("odù %q: expected edge %d (%s, evidence_source=%q) pairs on no registered writer of %s; fix the fixture's type prefix or stamp", oduName, i, edge.RelationshipType, stamp, family)
		}
	}
	for w, writer := range predicate.Writers {
		if !exercised[w] {
			return nil, materialized.PredicateScopedEdgeFamily{}, fmt.Sprintf("odù %q: expected-edge set never exercises writer pairing (prefix %q, evidence_source %q) of %s; a provider that stopped materializing would pass unnoticed", oduName, writer.TypePrefix, writer.EvidenceSource, family)
		}
	}
	return expected, predicate, ""
}

// cloudResourceScopeFacts partitions the Odù's facts for one provider's
// extractor and requires every fact (resources and relationships) to share
// one scope — the handlers load a single scope generation, and the AWS/GCP
// extractors refuse cross-scope endpoints (#6162), so a mixed-scope fixture
// would understate its own claim instead of mirroring an intent.
func cloudResourceScopeFacts(odu ifa.Odu, resourceKind, relationshipKind string) (resources, relationships []facts.Envelope, scopeID string, problem string) {
	for _, env := range odu.Facts {
		switch env.FactKind {
		case resourceKind:
			resources = append(resources, env)
		case relationshipKind:
			relationships = append(relationships, env)
		default:
			continue
		}
		if scopeID == "" {
			scopeID = env.ScopeID
		} else if env.ScopeID != scopeID {
			return nil, nil, "", fmt.Sprintf("odù %q: %s/%s facts span scopes %q and %q; one reducer intent loads one scope generation", odu.Name, resourceKind, relationshipKind, scopeID, env.ScopeID)
		}
	}
	if len(relationships) == 0 {
		return nil, nil, "", fmt.Sprintf("odù %q: carries no %s facts; this fixture cannot prove that writer's half of the predicate", odu.Name, relationshipKind)
	}
	return resources, relationships, scopeID, ""
}

// cloudResourceRowsToExpectedEdges converts one extractor's rows into the
// graph edges its writer MERGEs: the type gains the writer's registered
// prefix (the writer prepends its own prefix const to row.relationship_type,
// so the guard asserting prefix+verb catches a writer that stopped
// prefixing), and the mutable properties the templates SET from compile-time
// or row values ride along — evidence_source from the writer's registered
// stamp (a handler constant, knowable offline like the iam guard's),
// resolution_mode and support_state from the row when present. scope_id and
// generation_id are per-run intent values the writer stamps, so they are not
// part of this offline assertion; writer unit tests cover them.
func cloudResourceRowsToExpectedEdges(rows []map[string]any, writer materialized.PredicateScopedEdgeWriter) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: writer.TypePrefix + anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["source_uid"]),
			TargetEntityID:   anyToStringValue(row["target_uid"]),
			Properties: map[string]string{
				"evidence_source": writer.EvidenceSource,
			},
		}
		if mode := anyToStringValue(row["resolution_mode"]); mode != "" {
			edge.Properties["resolution_mode"] = mode
		}
		if state := anyToStringValue(row["support_state"]); state != "" {
			edge.Properties["support_state"] = state
		}
		edges = append(edges, edge)
	}
	return edges
}

// cloudResourceWriterFor finds the registered writer pairing carrying stamp.
func cloudResourceWriterFor(predicate materialized.PredicateScopedEdgeFamily, stamp string) (materialized.PredicateScopedEdgeWriter, bool) {
	for _, writer := range predicate.Writers {
		if writer.EvidenceSource == stamp {
			return writer, true
		}
	}
	return materialized.PredicateScopedEdgeWriter{}, false
}

// resolveCloudResourceMaterializedEdges is cloud_resource's named vacuity
// guard (#8358). It runs all three production extractors over the Odù's
// per-provider facts and compares the union of their rows EXACTLY (by
// multiplicity) against the hand-derived fixture, so the fixture proves each
// extractor's resolution behaviour: AWS dedup and self-loop/unresolved
// drops across two distinct verbs, GCP support-state gating, and Azure's
// closed-verb screening inside the open family.
//
// A quarantined fact from any extractor is fatal, for the same reason as
// every sibling guard: a fixture that stopped decoding against a provider
// contract cannot prove the set it names.
func resolveCloudResourceMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, predicate, problem := loadPredicateFamilyExpectedEdges(expectedEdgesPath, cloudResourceFamily, odu.Name)
	if problem != "" {
		return false, problem
	}
	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}

	awsWriter, ok := cloudResourceWriterFor(predicate, reducer.AWSRelationshipEvidenceSource())
	if !ok {
		return false, fmt.Sprintf("odù %q: predicate registers no writer for the AWS evidence_source", odu.Name)
	}
	gcpWriter, ok := cloudResourceWriterFor(predicate, reducer.GCPRelationshipEvidenceSource())
	if !ok {
		return false, fmt.Sprintf("odù %q: predicate registers no writer for the GCP evidence_source", odu.Name)
	}
	azureWriter, ok := cloudResourceWriterFor(predicate, reducer.AzureRelationshipEvidenceSource())
	if !ok {
		return false, fmt.Sprintf("odù %q: predicate registers no writer for the Azure evidence_source", odu.Name)
	}

	var actual []ExpectedEdge

	awsResources, awsRelationships, awsScope, problem := cloudResourceScopeFacts(odu, cloud.AWSResourceFactKind, cloud.AWSRelationshipFactKind)
	if problem != "" {
		return false, problem
	}
	awsRows, _, awsQuarantined, err := reducer.ExtractAWSRelationshipEdgeRows(awsResources, awsRelationships, awsScope)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractAWSRelationshipEdgeRows failed: %v", odu.Name, err)
	}
	if len(awsQuarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d AWS fact(s) quarantined by the decoder; the fixture no longer validates against the aws contracts", odu.Name, len(awsQuarantined))
	}
	if len(awsRows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractAWSRelationshipEdgeRows produced zero rows; this fixture cannot prove anything", odu.Name)
	}
	actual = append(actual, cloudResourceRowsToExpectedEdges(awsRows, awsWriter)...)

	gcpResources, gcpRelationships, gcpScope, problem := cloudResourceScopeFacts(odu, cloud.GCPCloudResourceFactKind, cloud.GCPCloudRelationshipFactKind)
	if problem != "" {
		return false, problem
	}
	gcpRows, _, gcpQuarantined, err := reducer.ExtractGCPRelationshipEdgeRows(gcpResources, gcpRelationships, gcpScope)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractGCPRelationshipEdgeRows failed: %v", odu.Name, err)
	}
	if len(gcpQuarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d GCP fact(s) quarantined by the decoder; the fixture no longer validates against the gcp contracts", odu.Name, len(gcpQuarantined))
	}
	if len(gcpRows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractGCPRelationshipEdgeRows produced zero rows; this fixture cannot prove anything", odu.Name)
	}
	actual = append(actual, cloudResourceRowsToExpectedEdges(gcpRows, gcpWriter)...)

	azureResources, azureRelationships, _, problem := cloudResourceScopeFacts(odu, cloud.AzureCloudResourceFactKind, cloud.AzureCloudRelationshipFactKind)
	if problem != "" {
		return false, problem
	}
	azureRows, _, azureQuarantined, err := reducer.ExtractAzureRelationshipEdgeRows(azureResources, azureRelationships)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractAzureRelationshipEdgeRows failed: %v", odu.Name, err)
	}
	if len(azureQuarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d Azure fact(s) quarantined by the decoder; the fixture no longer validates against the azure contracts", odu.Name, len(azureQuarantined))
	}
	if len(azureRows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractAzureRelationshipEdgeRows produced zero rows; this fixture cannot prove anything", odu.Name)
	}
	actual = append(actual, cloudResourceRowsToExpectedEdges(azureRows, azureWriter)...)

	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, cloudResourceFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: the AWS, GCP, and Azure extractors reproduce the expected %d-edge predicate set exactly across all %d writer pairing(s) and %d fact envelope(s); duplicate, unresolved, self-loop, partial, and unsupported-verb controls produced no spurious rows",
		odu.Name, len(expected), len(predicate.Writers), len(odu.Facts),
	)
}
