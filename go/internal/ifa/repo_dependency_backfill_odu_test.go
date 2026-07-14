// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type backfillScopeShape struct {
	alias      string
	sourceRows int
	candidates int
}

var retainedBackfillScopeShapes = []backfillScopeShape{
	{alias: "source-01", sourceRows: 14190, candidates: 12},
	{alias: "source-02", sourceRows: 11322, candidates: 24},
	{alias: "source-03", sourceRows: 4818, candidates: 4},
	{alias: "source-04", sourceRows: 164, candidates: 2},
	{alias: "source-05", sourceRows: 14190, candidates: 12},
	{alias: "source-06", sourceRows: 11322, candidates: 24},
	{alias: "source-07", sourceRows: 4818, candidates: 4},
	{alias: "source-08", sourceRows: 164, candidates: 2},
}

func TestRepoDependencyBackfillProofOduRetainsWorstScopeShape(t *testing.T) {
	odu := RepoDependencyBackfillProofOdu()
	byRepo := make(map[string][]facts.Envelope, len(retainedBackfillScopeShapes))
	for _, fact := range odu.Facts {
		if !isRelationshipBackfillSourceFact(fact) {
			continue
		}
		repoID, _ := fact.Payload["repo_id"].(string)
		byRepo[repoID] = append(byRepo[repoID], fact)
	}

	var totalSourceRows int
	var totalCandidates int
	for _, shape := range retainedBackfillScopeShapes {
		repoID := repoDependencyRepoID(shape.alias)
		rows := byRepo[repoID]
		if got := len(rows); got != shape.sourceRows {
			t.Errorf("%s source rows = %d, want %d", shape.alias, got, shape.sourceRows)
		}
		generic := 0
		for _, fact := range rows {
			if strings.HasPrefix(fact.StableFactKey, repoDependencyBackfillGenericStableKeyPrefix) {
				generic++
			}
		}
		candidates := len(rows) - generic
		if candidates != shape.candidates {
			t.Errorf("%s relationship-family candidates = %d, want %d", shape.alias, candidates, shape.candidates)
		}
		totalSourceRows += len(rows)
		totalCandidates += candidates
	}
	if totalSourceRows != 60988 {
		t.Errorf("total source rows = %d, want 60988", totalSourceRows)
	}
	if totalCandidates != 84 {
		t.Errorf("total relationship-family candidates = %d, want 84", totalCandidates)
	}
	if generic := totalSourceRows - totalCandidates; generic != 60904 {
		t.Errorf("generic distractors = %d, want 60904", generic)
	}
}

func TestRepoDependencyBackfillProofOduKeepsEightIndependentSourceCoordinates(t *testing.T) {
	odu := RepoDependencyBackfillProofOdu()
	coordinates := make(map[string]string, len(retainedBackfillScopeShapes))
	stableKeys := make(map[string]string, len(odu.Facts))
	for _, fact := range odu.Facts {
		if owner, duplicate := stableKeys[fact.StableFactKey]; duplicate {
			t.Fatalf("stable fact key %q is shared by scopes %q and %q", fact.StableFactKey, owner, fact.ScopeID)
		}
		stableKeys[fact.StableFactKey] = fact.ScopeID

		repoID, _ := fact.Payload["repo_id"].(string)
		if !strings.HasPrefix(repoID, "repository:source-") {
			continue
		}
		coordinate := fact.ScopeID + "\x00" + fact.GenerationID
		if existing, ok := coordinates[repoID]; ok && existing != coordinate {
			t.Fatalf("source %q spans coordinates %q and %q", repoID, existing, coordinate)
		}
		coordinates[repoID] = coordinate
	}
	if got := len(coordinates); got != 8 {
		t.Fatalf("source coordinates = %d, want 8: %#v", got, coordinates)
	}
	unique := make(map[string]struct{}, len(coordinates))
	for source, coordinate := range coordinates {
		if _, duplicate := unique[coordinate]; duplicate {
			t.Fatalf("source %q reuses coordinate %q", source, coordinate)
		}
		unique[coordinate] = struct{}{}
	}
}

func TestRepoDependencyBackfillProofOduProductionEvidenceExact(t *testing.T) {
	odu := RepoDependencyBackfillProofOdu()
	assertRepoDependencyEvidence(t, DiscoveredEvidence(odu))
}

func TestRepoDependencyBackfillProofOduGenericDistractorsAreTruthInert(t *testing.T) {
	odu := RepoDependencyBackfillProofOdu()
	withoutGeneric := odu
	withoutGeneric.Facts = slices.DeleteFunc(cloneFacts(odu.Facts), func(fact facts.Envelope) bool {
		return strings.HasPrefix(fact.StableFactKey, repoDependencyBackfillGenericStableKeyPrefix)
	})
	withEvidence := DiscoveredEvidence(odu)
	withoutEvidence := DiscoveredEvidence(withoutGeneric)
	if !reflect.DeepEqual(withEvidence, withoutEvidence) {
		t.Fatalf("generic distractors changed relationship truth:\nwith=%#v\nwithout=%#v", withEvidence, withoutEvidence)
	}
}

func TestRepoDependencyBackfillProofOduCarriesOneDualArmFact(t *testing.T) {
	odu := RepoDependencyBackfillProofOdu()
	var dualArm []facts.Envelope
	for _, fact := range odu.Facts {
		if _, ok := fact.Payload["linked_repo_id"]; ok {
			dualArm = append(dualArm, fact)
		}
	}
	if len(dualArm) != 1 {
		t.Fatalf("dual-arm facts = %d, want 1", len(dualArm))
	}
	linkedRepoID, _ := dualArm[0].Payload["linked_repo_id"].(string)
	contentBody, _ := dualArm[0].Payload["content_body"].(string)
	if linkedRepoID == "" || !strings.Contains(contentBody, "source-06") {
		t.Fatalf("dual-arm fact must carry a target repo_id and non-repo alias: payload=%#v", dualArm[0].Payload)
	}
}

func TestRepoDependencyBackfillProofOduCanonicalBytesAreDeterministic(t *testing.T) {
	first := RepoDependencyBackfillProofOdu()
	second := RepoDependencyBackfillProofOdu()
	slices.Reverse(second.Facts)
	firstBytes, err := CanonicalizeOdu(context.Background(), first, nil)
	if err != nil {
		t.Fatalf("CanonicalizeOdu(first) error = %v", err)
	}
	secondBytes, err := CanonicalizeOdu(context.Background(), second, nil)
	if err != nil {
		t.Fatalf("CanonicalizeOdu(reversed) error = %v", err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("proof Odù canonical bytes changed with input fact order")
	}
}

func isRelationshipBackfillSourceFact(fact facts.Envelope) bool {
	switch fact.FactKind {
	case contentFactKind, "file", facts.GCPCloudRelationshipFactKind:
		return true
	default:
		return false
	}
}
