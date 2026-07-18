// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestResourceInvestigationResourceAnchorOmitsEmptyArnGuard guards the #5287
// fix: the resource identity predicate must not reintroduce the
// `$resource_arn <> ”` guard disjunct, which mis-evaluates to zero rows on the
// pinned NornicDB build. The arn disjunct is present only when the resolved
// candidate carries an arn.
func TestResourceInvestigationResourceAnchorOmitsEmptyArnGuard(t *testing.T) {
	t.Parallel()

	noArn := resourceInvestigationResourceAnchor("resource", false)
	if strings.Contains(noArn, "resource.arn") || strings.Contains(noArn, " OR ") {
		t.Fatalf("no-arn anchor must not reference arn or OR: %q", noArn)
	}
	if !strings.Contains(noArn, "coalesce(resource.id, resource.uid, resource.resource_id, resource.name) = $resource_id") {
		t.Fatalf("no-arn anchor missing coalesce identity predicate: %q", noArn)
	}

	withArn := resourceInvestigationResourceAnchor("resource", true)
	if !strings.Contains(withArn, "OR resource.arn = $resource_arn") {
		t.Fatalf("arn anchor missing arn disjunct: %q", withArn)
	}
	if strings.Contains(withArn, "<> ''") {
		t.Fatalf("arn anchor must not reintroduce the empty-string guard (#5287): %q", withArn)
	}
}

// TestResourceInvestigationAnchorParamsBindArnOnlyWhenPresent keeps the params
// in lockstep with the predicate: $resource_arn is bound exactly when the arn
// disjunct is rendered.
func TestResourceInvestigationAnchorParamsBindArnOnlyWhenPresent(t *testing.T) {
	t.Parallel()

	without := resourceInvestigationAnchorParams(&resourceInvestigationCandidate{ID: "r1"}, nil)
	if _, ok := without["resource_arn"]; ok {
		t.Fatalf("resource_arn must not be bound without an arn: %#v", without)
	}
	if without["resource_id"] != "r1" {
		t.Fatalf("resource_id = %#v, want r1", without["resource_id"])
	}

	with := resourceInvestigationAnchorParams(&resourceInvestigationCandidate{ID: "r1", Arn: "arn:aws:x"}, map[string]any{"limit": 5})
	if with["resource_arn"] != "arn:aws:x" {
		t.Fatalf("resource_arn = %#v, want arn:aws:x", with["resource_arn"])
	}
	if with["limit"] != 5 {
		t.Fatalf("extra param limit = %#v, want 5", with["limit"])
	}
}

// TestResourceInvestigationHopListDecodesBothBackends proves the hop decoder
// unwinds a raw relationships(path) value from both the Neo4j driver
// (neo4j.Relationship) and NornicDB (map[string]any with a nested properties
// map) shapes, since the prior map-valued comprehension corrupts on NornicDB.
func TestResourceInvestigationHopListDecodesBothBackends(t *testing.T) {
	t.Parallel()

	cases := map[string]any{
		"neo4j-relationship": []any{
			neo4jdriver.Relationship{Type: "BELONGS_TO", Props: map[string]any{"confidence": 0.77, "reason": "provisioned-by"}},
		},
		"nornicdb-map": []any{
			map[string]any{"type": "BELONGS_TO", "properties": map[string]any{"confidence": 0.77, "reason": "provisioned-by"}},
		},
	}
	for name, raw := range cases {
		hops := resourceInvestigationHopList(raw)
		if len(hops) != 1 {
			t.Fatalf("%s: hop count = %d, want 1", name, len(hops))
		}
		hop := hops[0]
		if hop["type"] != "BELONGS_TO" {
			t.Fatalf("%s: hop type = %#v, want BELONGS_TO", name, hop["type"])
		}
		if hop["confidence"] != 0.77 {
			t.Fatalf("%s: hop confidence = %#v, want 0.77", name, hop["confidence"])
		}
		if hop["reason"] != "provisioned-by" {
			t.Fatalf("%s: hop reason = %#v, want provisioned-by", name, hop["reason"])
		}
	}
}

// TestResourceInvestigationHopReasonFallsBackToEvidenceType keeps the reason
// coalesce(reason, evidence_type, ”) semantics of the prior projection.
func TestResourceInvestigationHopReasonFallsBackToEvidenceType(t *testing.T) {
	t.Parallel()

	if got := resourceInvestigationHopReason(map[string]any{"reason": "a", "evidence_type": "b"}); got != "a" {
		t.Fatalf("reason present = %q, want a", got)
	}
	if got := resourceInvestigationHopReason(map[string]any{"evidence_type": "b"}); got != "b" {
		t.Fatalf("reason empty falls back = %q, want b", got)
	}
	if got := resourceInvestigationHopReason(nil); got != "" {
		t.Fatalf("nil props = %q, want empty", got)
	}
}
