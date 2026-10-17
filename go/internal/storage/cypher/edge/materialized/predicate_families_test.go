// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestPredicateScopedCloudResourceFamilyMatchesItsWriters is the exactness
// proof the #8358 route-2 registration owes: the predicate (relationship-type
// prefix + evidence_source + CloudResource endpoints) must describe exactly
// the edges the three cloud_resource writers create and retract, read off the
// production consts the registry holds rather than copied literals.
//
// The writers hold the other half of each pairing: the prefix comes from the
// writer's own exported prefix const (the same const its
// canonical*RelationshipCypherType prepends), and the evidence_source comes
// from the reducer handler's exported accessor (the same value the handler
// passes to WriteCloudResourceEdges). A writer that changed either one
// without this registry changing in step fails here instead of silently
// narrowing what `eshu-ifa assert-edges -domain cloud_resource` asserts.
func TestPredicateScopedCloudResourceFamilyMatchesItsWriters(t *testing.T) {
	t.Parallel()

	family, ok := PredicateScopedMaterializedEdgeFamily("cloud_resource")
	if !ok {
		t.Fatal("cloud_resource is not registered as a predicate-scoped family")
	}
	if _, ok := PredicateScopedMaterializedEdgeFamily("no_such_family"); ok {
		t.Fatal("unknown family resolved as predicate-scoped; registration must fail closed")
	}
	if family.FromLabel != "CloudResource" || family.ToLabel != "CloudResource" {
		t.Fatalf("endpoint labels = %q -> %q, want CloudResource -> CloudResource", family.FromLabel, family.ToLabel)
	}

	wantWriters := []struct {
		prefix         string
		evidenceSource string
		upsert         string
		retracts       []string
	}{
		{
			prefix:         sourcecypher.AWSRelationshipCypherTypePrefix,
			evidenceSource: reducer.AWSRelationshipEvidenceSource(),
			upsert:         sourcecypher.CanonicalCloudResourceEdgeUpsertCypherFormat,
			retracts: []string{
				sourcecypher.RetractCloudResourceEdgesCypher,
				sourcecypher.RetractCloudResourceEdgesByUIDsCypher,
			},
		},
		{
			prefix:         sourcecypher.GCPRelationshipCypherTypePrefix,
			evidenceSource: reducer.GCPRelationshipEvidenceSource(),
			upsert:         sourcecypher.CanonicalGCPCloudResourceEdgeUpsertCypherFormat,
			retracts: []string{
				sourcecypher.RetractGCPCloudResourceEdgesCypher,
				sourcecypher.RetractGCPCloudResourceEdgesByUIDsCypher,
			},
		},
		{
			prefix:         sourcecypher.AzureRelationshipCypherTypePrefix,
			evidenceSource: reducer.AzureRelationshipEvidenceSource(),
			upsert:         sourcecypher.CanonicalAzureCloudResourceEdgeUpsertCypherFormat,
			retracts: []string{
				sourcecypher.RetractAzureCloudResourceEdgesCypher,
				sourcecypher.RetractAzureCloudResourceEdgesByUIDsCypher,
			},
		},
	}
	if len(family.Writers) != len(wantWriters) {
		t.Fatalf("registered %d writers, want %d (AWS, GCP, Azure)", len(family.Writers), len(wantWriters))
	}
	for i, want := range wantWriters {
		got := family.Writers[i]
		if got.TypePrefix != want.prefix || got.TypePrefix == "" {
			t.Errorf("writer %d prefix = %q, want the writer's own prefix const %q", i, got.TypePrefix, want.prefix)
		}
		if got.EvidenceSource != want.evidenceSource || got.EvidenceSource == "" {
			t.Errorf("writer %d evidence_source = %q, want the handler accessor value %q", i, got.EvidenceSource, want.evidenceSource)
		}
		if got.UpsertCypherFormat != want.upsert {
			t.Errorf("writer %d does not hold its production upsert const", i)
		}
		if len(got.RetractCyphers) != len(want.retracts) {
			t.Errorf("writer %d holds %d retract variants, want %d (scope + ByUIDs)", i, len(got.RetractCyphers), len(want.retracts))
			continue
		}
		for j := range want.retracts {
			if got.RetractCyphers[j] != want.retracts[j] {
				t.Errorf("writer %d retract %d does not hold its production retract const", i, j)
			}
		}
	}
}

// TestPredicateScopedCloudResourceTemplatesCarryThePredicate pins the three
// predicate conjuncts to the production templates: every upsert MATCHes both
// CloudResource endpoints and stamps evidence_source, and every retract is
// scoped by that same evidence_source (and the edge's own scope_id). Under
// the sole-creator census (materializededges) those are the only edges the
// retract can reap, which is what makes the retract set and the scanned set
// the same set.
func TestPredicateScopedCloudResourceTemplatesCarryThePredicate(t *testing.T) {
	t.Parallel()

	family, ok := PredicateScopedMaterializedEdgeFamily("cloud_resource")
	if !ok {
		t.Fatal("cloud_resource is not registered as a predicate-scoped family")
	}
	for i, writer := range family.Writers {
		upsert := writer.UpsertCypherFormat
		for _, needle := range []string{
			"MATCH (source:CloudResource {uid: row.source_uid})",
			"MATCH (target:CloudResource {uid: row.target_uid})",
			"MERGE (source)-[rel:%s]->(target)",
			"rel.evidence_source = row.evidence_source",
			"rel.scope_id = row.scope_id",
		} {
			if !strings.Contains(upsert, needle) {
				t.Errorf("writer %d upsert missing %q:\n%s", i, needle, upsert)
			}
		}
		if strings.Contains(upsert, "[rel:%s {") {
			t.Errorf("writer %d upsert MERGEs with a relationship property map; the family would gain identity properties this registration does not declare:\n%s", i, upsert)
		}
		for j, retract := range writer.RetractCyphers {
			for _, needle := range []string{
				"(source:CloudResource)-[rel]->",
				"rel.scope_id IN $scope_ids",
				"rel.evidence_source = $evidence_source",
				"DELETE rel",
			} {
				if !strings.Contains(retract, needle) {
					t.Errorf("writer %d retract %d missing %q:\n%s", i, j, needle, retract)
				}
			}
		}
		// One variant anchors on both endpoint labels, the other on the
		// ledger-enumerated source uids; both shapes must stay registered or
		// the retract set the predicate is proven against silently shrinks.
		var sawEndpointAnchored, sawUIDAnchored bool
		for _, retract := range writer.RetractCyphers {
			sawEndpointAnchored = sawEndpointAnchored || strings.Contains(retract, "->(:CloudResource)")
			sawUIDAnchored = sawUIDAnchored || strings.Contains(retract, "source.uid IN $source_uids")
		}
		if !sawEndpointAnchored || !sawUIDAnchored {
			t.Errorf("writer %d retracts: endpoint-anchored=%v uid-anchored=%v, want both variants", i, sawEndpointAnchored, sawUIDAnchored)
		}
	}
}

// TestPredicateScopedCloudResourceIdentityIsEndpointOnly pins the identity
// contract the expected-edge fixture loader resolves through: the writers
// MERGE on (source, type, target) alone, so the family declares an explicit
// empty identity rather than failing as unregistered.
func TestPredicateScopedCloudResourceIdentityIsEndpointOnly(t *testing.T) {
	t.Parallel()

	identity, err := MaterializedEdgeIdentityProperties("cloud_resource")
	if err != nil {
		t.Fatalf("MaterializedEdgeIdentityProperties(cloud_resource): %v", err)
	}
	if identity == nil || len(identity) != 0 {
		t.Fatalf("identity = %v, want an explicit empty declaration", identity)
	}
}
