// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	azurerelationship "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/azure/relationship"
	gcprelationship "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/gcp/relationship"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/resource"
)

// Predicate-scoped materialized-edge families (#8358, route 2 of the #6228
// arbiter verdict's cloud_resource follow-up).
//
// Every family in families.go owns a finite relationship-type set, and
// `eshu-ifa assert-edges` scopes its scan to that set. cloud_resource cannot
// register that way: its three writers prefix an open per-provider verb
// vocabulary (AWS_<verb>, GCP_<verb>, AZURE_<verb> — 702 AWS and 161 GCP
// collector verbs at the #8358 census, growing with every scanner), so no
// finite type list exists that is not an observed-type snapshot. The verdict
// rejected both registering such a list and scoping a bare `[rel]` by the
// mutable evidence_source property alone.
//
// A predicate-scoped family instead scopes the scan by the conjunction the
// writers themselves guarantee on every edge they create:
//
//   - the relationship type carries one of the writers' type prefixes (the
//     type is immutable once created, unlike a property);
//   - the edge's evidence_source equals the paired handler's stamp;
//   - both endpoints are :CloudResource (both upsert MATCH clauses).
//
// The conjunction is exact under the sole-creator census in
// go/internal/ifa/materializededges (cloud_resource_census_test.go): only
// these three writers emit prefixed types with these stamps, and only their
// handlers pass these evidence_source values to a graph write. Under that
// census the production retract set — bare `[rel]` + evidence_source +
// scope over :CloudResource sources — reaps exactly the scanned set, because
// every edge carrying one of these stamps was created by its writer and so
// carries its prefix and endpoints. The retract Cypher deliberately keeps
// its proven shape: the scan is evaluated client-side over the graph dump,
// so no `type(rel) STARTS WITH` predicate reaches a backend (the pinned
// NornicDB build mis-evaluates STARTS WITH in multi-node MATCHes, #6546).
//
// Each entry holds production consts, never copies: the prefix is the
// writer's own exported prefix const, the evidence_source is the handler's
// exported accessor value resolved at init, and the templates are the exact
// upsert/retract consts the writer executes. TestPredicateScoped*
// (predicate_families_test.go) pins the predicate conjuncts to those
// templates so a writer change that widens or narrows the family fails here.

// PredicateScopedEdgeWriter is one writer's half of a predicate-scoped
// family: the type prefix it prepends, the evidence_source its handler
// stamps, and the production templates that create and reap its edges.
type PredicateScopedEdgeWriter struct {
	// TypePrefix is the relationship-type prefix every type this writer
	// emits carries (the writer's own exported prefix const).
	TypePrefix string
	// EvidenceSource is the rel.evidence_source value the writer's handler
	// stamps on every edge (the handler's exported accessor value).
	EvidenceSource string
	// UpsertCypherFormat is the production write template (its %s takes the
	// prefixed type). Holding the real const is what makes the template
	// drift guard meaningful.
	UpsertCypherFormat string
	// RetractCyphers holds EVERY production retract variant for this writer
	// (scope-anchored and ledger-anchored), so a new variant cannot reap
	// edges outside the registered predicate unnoticed.
	RetractCyphers []string
}

// PredicateScopedEdgeFamily is one family's predicate: the endpoint labels
// every writer MATCHes, plus one writer record per (prefix, evidence_source)
// pairing. The pairing is load-bearing — a GCP_-prefixed edge stamped with
// the AWS evidence_source is not this family's edge, so the scan matches
// prefix and stamp from the SAME writer record, never cross-product.
type PredicateScopedEdgeFamily struct {
	// FromLabel is the required source node label.
	FromLabel string
	// ToLabel is the required target node label.
	ToLabel string
	// Writers are the family's (prefix, evidence_source) pairings.
	Writers []PredicateScopedEdgeWriter
}

// Matches reports whether an edge with the given type, evidence_source, and
// endpoint labels belongs to the family: some single writer record supplies
// both the type prefix and the evidence_source, and both endpoint labels
// match. It is the one definition the live scan and the vacuity guard share.
func (f PredicateScopedEdgeFamily) Matches(edgeType, evidenceSource string, fromLabels, toLabels []string) bool {
	if !predicateHasLabel(fromLabels, f.FromLabel) || !predicateHasLabel(toLabels, f.ToLabel) {
		return false
	}
	for _, writer := range f.Writers {
		if writer.EvidenceSource == evidenceSource && strings.HasPrefix(edgeType, writer.TypePrefix) {
			return true
		}
	}
	return false
}

// predicateHasLabel reports whether labels contains label.
func predicateHasLabel(labels []string, label string) bool {
	for _, candidate := range labels {
		if candidate == label {
			return true
		}
	}
	return false
}

// predicateScopedMaterializedEdgeFamilies indexes the predicate-scoped
// families by name. cloud_resource is the sole entry: the one genuinely open
// vocabulary among the #6228 direct families (arbiter verdict 2026-10-17).
var predicateScopedMaterializedEdgeFamilies = map[string]PredicateScopedEdgeFamily{
	"cloud_resource": {
		FromLabel: "CloudResource",
		ToLabel:   "CloudResource",
		Writers: []PredicateScopedEdgeWriter{
			{
				TypePrefix:         resource.AWSRelationshipCypherTypePrefix,
				EvidenceSource:     reducer.AWSRelationshipEvidenceSource(),
				UpsertCypherFormat: resource.CanonicalCloudResourceEdgeUpsertCypherFormat,
				RetractCyphers: []string{
					resource.RetractCloudResourceEdgesCypher,
					resource.RetractCloudResourceEdgesByUIDsCypher,
				},
			},
			{
				TypePrefix:         gcprelationship.GCPRelationshipCypherTypePrefix,
				EvidenceSource:     reducer.GCPRelationshipEvidenceSource(),
				UpsertCypherFormat: gcprelationship.CanonicalGCPCloudResourceEdgeUpsertCypherFormat,
				RetractCyphers: []string{
					gcprelationship.RetractGCPCloudResourceEdgesCypher,
					gcprelationship.RetractGCPCloudResourceEdgesByUIDsCypher,
				},
			},
			{
				TypePrefix:         azurerelationship.AzureRelationshipCypherTypePrefix,
				EvidenceSource:     reducer.AzureRelationshipEvidenceSource(),
				UpsertCypherFormat: azurerelationship.CanonicalAzureCloudResourceEdgeUpsertCypherFormat,
				RetractCyphers: []string{
					azurerelationship.RetractAzureCloudResourceEdgesCypher,
					azurerelationship.RetractAzureCloudResourceEdgesByUIDsCypher,
				},
			},
		},
	},
}

// PredicateScopedMaterializedEdgeFamily returns the named family's predicate.
//
// The second return is false for a family that is not predicate-scoped.
// Callers MUST fail closed on it, mirroring SingleTypeMaterializedEdgeTypes:
// an empty predicate would assert zero edges and pass any graph vacuously.
//
// The returned value is a deep copy, so a caller cannot mutate the
// package-level table through it.
func PredicateScopedMaterializedEdgeFamily(family string) (PredicateScopedEdgeFamily, bool) {
	entry, ok := predicateScopedMaterializedEdgeFamilies[family]
	if !ok {
		return PredicateScopedEdgeFamily{}, false
	}
	out := PredicateScopedEdgeFamily{
		FromLabel: entry.FromLabel,
		ToLabel:   entry.ToLabel,
		Writers:   make([]PredicateScopedEdgeWriter, len(entry.Writers)),
	}
	for i, writer := range entry.Writers {
		out.Writers[i] = writer
		out.Writers[i].RetractCyphers = append([]string(nil), writer.RetractCyphers...)
	}
	return out, true
}

// PredicateScopedMaterializedEdgeFamilyNames returns the registered
// predicate-scoped family names, so callers and tests can enumerate coverage
// without importing the table itself.
func PredicateScopedMaterializedEdgeFamilyNames() []string {
	out := make([]string, 0, len(predicateScopedMaterializedEdgeFamilies))
	for family := range predicateScopedMaterializedEdgeFamilies {
		out = append(out, family)
	}
	return out
}
