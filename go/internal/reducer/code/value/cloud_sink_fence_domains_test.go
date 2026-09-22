// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/value"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/materialized"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// cloudSinkChainRelationshipPattern extracts a Cypher relationship pattern's
// type, e.g. `[:RUNS_IN]` or `[sinkRel:CAN_PERFORM]`. Node-label patterns use
// `(var:Label` (a paren, never a bracket), so this never matches a label.
var cloudSinkChainRelationshipPattern = regexp.MustCompile(`\[[A-Za-z0-9_]*:([A-Z_]+)\]`)

// cloudSinkChainBracketPattern matches EVERY bracketed relationship pattern,
// including shapes the extraction regex above does not understand
// (`[:A|B]`, `[:A*1..2]`, lowercase types). The guard compares the two
// counts so a future statement edit that adds such a hop fails loudly
// instead of silently dropping the hop from the derived writer set.
var cloudSinkChainBracketPattern = regexp.MustCompile(`\[[^\]]*\]`)

// cloudSinkChainRelationshipTypes extracts every relationship type the two
// #6923 probe statements traverse, directly from the production consts (not
// a hand-copied list), so a future edit to either statement's chain is what
// this guard actually tracks.
func cloudSinkChainRelationshipTypes(t *testing.T) map[string]struct{} {
	t.Helper()
	types, err := relationshipTypesFromStatements(value.CloudSinkWorkloadRowsCypher, value.CloudSinkTargetsByPairCypher)
	if err != nil {
		t.Fatalf("%v; widen the regex before trusting the fence", err)
	}
	return types
}

// relationshipTypesFromStatements extracts the single-type relationship
// patterns of every statement and refuses (returns an error) when a
// statement contains a bracketed pattern the extraction regex did not
// understand, so an unparsed hop can never silently vanish from the set.
func relationshipTypesFromStatements(statements ...string) (map[string]struct{}, error) {
	types := make(map[string]struct{})
	for _, cypher := range statements {
		matched := cloudSinkChainRelationshipPattern.FindAllStringSubmatch(cypher, -1)
		if brackets := cloudSinkChainBracketPattern.FindAllString(cypher, -1); len(brackets) != len(matched) {
			return nil, fmt.Errorf("statement has %d bracketed relationship patterns but the extraction regex understood %d (%q)",
				len(brackets), len(matched), brackets)
		}
		for _, m := range matched {
			types[m[1]] = struct{}{}
		}
	}
	return types, nil
}

// TestRelationshipTypesFromStatementsRejectsUnparsedHop is the seeded
// violation for the bracket-count guard: a multi-type hop the extraction
// regex does not understand must be reported, not dropped.
func TestRelationshipTypesFromStatementsRejectsUnparsedHop(t *testing.T) {
	t.Parallel()

	if _, err := relationshipTypesFromStatements(value.CloudSinkWorkloadRowsCypher); err != nil {
		t.Fatalf("production statement must parse cleanly: %v", err)
	}
	_, err := relationshipTypesFromStatements("MATCH (fn:Function)-[:RUNS_IN|INSTANCE_OF]->(w:Workload) MATCH (w)<-[:USES]-(p) RETURN 1")
	if err == nil {
		t.Fatal("a [:A|B] hop must be rejected, not silently dropped")
	}
	if !strings.Contains(err.Error(), "2 bracketed relationship patterns but the extraction regex understood 1") {
		t.Fatalf("error = %v, want the bracket/extracted count mismatch", err)
	}
}

// relationshipFamilyOwner resolves a relationship type to the
// materialized-edge family that owns it, when the family registry
// (go/internal/storage/cypher/edge/materialized/families.go) tracks that
// type. That registry exists for the Ifá exhaustiveness gates and only
// catalogs single-relationship-type MERGE templates, so a miss here is
// expected for CAN_PERFORM and INSTANCE_OF -- see
// cloudSinkChainRelationshipOwners for those two.
func relationshipFamilyOwner(relType string) (string, bool) {
	for _, family := range materialized.SingleTypeMaterializedEdgeFamilyNames() {
		edgeTypes, ok := materialized.SingleTypeMaterializedEdgeTypes(family)
		if !ok {
			continue
		}
		if _, owns := edgeTypes[relType]; owns {
			return family, true
		}
	}
	return "", false
}

// cloudSinkChainRelationshipOwners hand-documents the two cloud-sink chain
// writers the materialized-edge family registry does not catalog:
// CAN_PERFORM is written by iam_can_perform_materialization
// (go/internal/reducer/iamcan) and INSTANCE_OF by workload_materialization
// (go/internal/reducer/workload_materializer.go,
// workload_materialization_handler.go). Both already appear in
// postgres.ValueFlowInputsFenceReducerDomains.
var cloudSinkChainRelationshipOwners = map[string]reducer.Domain{
	"CAN_PERFORM": reducer.DomainIAMCanPerformMaterialization,
	"INSTANCE_OF": reducer.DomainWorkloadMaterialization,
}

// familyToFencedDomain maps a materialized-edge family name to the fenced
// domain that owns it, for every family the two cloud-sink probe statements
// actually traverse. runs_in/invokes_cloud_action are shared-projection
// domains (their family name IS the projection_domain value);
// workload_cloud_relationship is a fact_work_items domain under a shorter
// family slug.
var familyToFencedDomain = map[string]reducer.Domain{
	"runs_in":                     reducer.DomainRunsIn,
	"invokes_cloud_action":        reducer.DomainInvokesCloudAction,
	"workload_cloud_relationship": reducer.DomainWorkloadCloudRelationshipMaterialization,
}

// TestValueFlowInputsFenceDomainsCoverCloudSinkChain guards the #6923 fence's
// declared domain set (postgres.ValueFlowInputsFenceReducerDomains +
// ValueFlowInputsFenceSharedDomains) against drift from the two statements
// it fences: every relationship type CloudSinkWorkloadRowsCypher and
// CloudSinkTargetsByPairCypher traverse must resolve, through the
// materialized-edge family registry or the hand-documented map above, to a
// domain the fence declares. Seeded violation (run manually, not committed):
// deleting reducer.DomainRunsIn from
// postgres.ValueFlowInputsFenceSharedDomains turns this test RED.
func TestValueFlowInputsFenceDomainsCoverCloudSinkChain(t *testing.T) {
	t.Parallel()

	fenced := make(map[reducer.Domain]struct{})
	for _, d := range postgres.ValueFlowInputsFenceReducerDomains {
		fenced[d] = struct{}{}
	}
	for _, d := range postgres.ValueFlowInputsFenceSharedDomains {
		fenced[d] = struct{}{}
	}

	relTypes := cloudSinkChainRelationshipTypes(t)
	if len(relTypes) == 0 {
		t.Fatal("extracted zero relationship types from the cloud-sink probe statements; the regex is broken, not the fence")
	}

	for relType := range relTypes {
		if owner, ok := cloudSinkChainRelationshipOwners[relType]; ok {
			if _, covered := fenced[owner]; !covered {
				t.Errorf("relationship %s owner %q is not in the fence's declared domain set", relType, owner)
			}
			continue
		}
		family, ok := relationshipFamilyOwner(relType)
		if !ok {
			t.Fatalf(
				"relationship %s has no owner in the materialized-edge family registry or "+
					"cloudSinkChainRelationshipOwners; add one before trusting the fence", relType,
			)
		}
		domain, ok := familyToFencedDomain[family]
		if !ok {
			t.Fatalf(
				"relationship %s resolved to family %q, which has no entry in familyToFencedDomain; add one", relType, family,
			)
		}
		if _, covered := fenced[domain]; !covered {
			t.Errorf("relationship %s owner domain %q (family %q) is not in the fence's declared domain set", relType, domain, family)
		}
	}
}
