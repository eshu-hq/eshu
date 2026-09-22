// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package value_test

import (
	"regexp"
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

// cloudSinkChainRelationshipTypes extracts every relationship type the two
// #6923 probe statements traverse, directly from the production consts (not
// a hand-copied list), so a future edit to either statement's chain is what
// this guard actually tracks.
func cloudSinkChainRelationshipTypes() map[string]struct{} {
	types := make(map[string]struct{})
	for _, cypher := range []string{value.CloudSinkWorkloadRowsCypher, value.CloudSinkTargetsByPairCypher} {
		for _, m := range cloudSinkChainRelationshipPattern.FindAllStringSubmatch(cypher, -1) {
			types[m[1]] = struct{}{}
		}
	}
	return types
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

	relTypes := cloudSinkChainRelationshipTypes()
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
