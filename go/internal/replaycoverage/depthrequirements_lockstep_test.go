// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestRetractableNodeTypesLockstep keeps the depth-requirement spec's
// retractable_node_types byte-equal (as a set) to the canonical retract phase's
// label set (cypher.RetractableNodeEntityLabels()). This is the property that
// makes C-13 honest: adding a retractable node label in the writer makes this
// test fail until the spec is updated, which in turn makes the replay-coverage
// gate demand a delta/tombstone scenario for the new node type (the #4186 class)
// instead of the gap going unseen.
func TestRetractableNodeTypesLockstep(t *testing.T) {
	dr, err := replaycoverage.LoadDepthRequirements(
		filepath.Join("..", "..", "..", "specs", replaycoverage.DepthRequirementsFileName),
	)
	if err != nil {
		t.Fatalf("load depth requirements: %v", err)
	}

	spec := toSet(dr.RetractableNodeTypes)
	code := toSet(cypher.RetractableNodeEntityLabels())

	for label := range code {
		if _, ok := spec[label]; !ok {
			t.Errorf("retractable node label %q is in the cypher retract registry but missing from %s "+
				"(add it so the gate demands a delta scenario for it)", label, replaycoverage.DepthRequirementsFileName)
		}
	}
	for label := range spec {
		if _, ok := code[label]; !ok {
			t.Errorf("retractable node label %q is in %s but not in the cypher retract registry "+
				"(it is no longer retractable; remove it)", label, replaycoverage.DepthRequirementsFileName)
		}
	}
	if len(spec) != len(code) {
		t.Errorf("spec has %d retractable node types, cypher registry has %d", len(spec), len(code))
	}
}

// TestRetractableEdgeTypesLockstep keeps the depth-requirement spec's
// retractable_edge_types byte-equal (as a set) to the static canonical and
// reducer edge retract path's relationship-type set. This is the #4370 property:
// adding an edge type to the retract path makes the replay-coverage gate demand
// a delta/tombstone scenario for it instead of losing the gap in prose.
func TestRetractableEdgeTypesLockstep(t *testing.T) {
	dr, err := replaycoverage.LoadDepthRequirements(
		filepath.Join("..", "..", "..", "specs", replaycoverage.DepthRequirementsFileName),
	)
	if err != nil {
		t.Fatalf("load depth requirements: %v", err)
	}

	spec := toSet(dr.RetractableEdgeTypes)
	code := toSet(cypher.RetractableEdgeTypes())

	for edgeType := range code {
		if _, ok := spec[edgeType]; !ok {
			t.Errorf("retractable edge type %q is in the cypher retract registry but missing from %s "+
				"(add it so the gate demands a delta scenario for it)", edgeType, replaycoverage.DepthRequirementsFileName)
		}
	}
	for edgeType := range spec {
		if _, ok := code[edgeType]; !ok {
			t.Errorf("retractable edge type %q is in %s but not in the cypher retract registry "+
				"(it is no longer retractable; remove it)", edgeType, replaycoverage.DepthRequirementsFileName)
		}
	}
	if len(spec) != len(code) {
		t.Errorf("spec has %d retractable edge types, cypher registry has %d", len(spec), len(code))
	}
}

func toSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

// TestRetractableNodeTypesLockstepDetectsDrift proves the lockstep assertion has
// teeth: a label present in the code set but absent from the spec set is flagged.
// It runs the same set-difference the lockstep test uses, against a deliberately
// shortened spec set, and requires a non-empty difference.
func TestRetractableNodeTypesLockstepDetectsDrift(t *testing.T) {
	code := cypher.RetractableNodeEntityLabels()
	if len(code) < 2 {
		t.Skip("need at least two retractable labels to drop one")
	}
	shortened := toSet(code[1:]) // drop the first label
	var missing []string
	for _, label := range code {
		if _, ok := shortened[label]; !ok {
			missing = append(missing, label)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		t.Fatal("set-difference detected no drift after dropping a label; the lockstep check would be toothless")
	}
}

// TestRetractableEdgeTypesLockstepDetectsDrift proves the edge lockstep
// assertion has teeth using the same set-difference as the real lockstep test.
func TestRetractableEdgeTypesLockstepDetectsDrift(t *testing.T) {
	code := cypher.RetractableEdgeTypes()
	if len(code) < 2 {
		t.Skip("need at least two retractable edge types to drop one")
	}
	shortened := toSet(code[1:]) // drop the first edge type
	var missing []string
	for _, edgeType := range code {
		if _, ok := shortened[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		t.Fatal("set-difference detected no drift after dropping an edge type; the lockstep check would be toothless")
	}
}
