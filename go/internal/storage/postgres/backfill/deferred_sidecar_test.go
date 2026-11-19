// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestDeferredRelationshipFamilySidecarSharesCandidateLiterals pins the
// no-drift contract between the fact-payload candidate predicate and the
// version-1 sidecar predicate (issue #10875): the artifact list, the path
// alternation, and the yaml-suffix gate are shared fragments, so a change to
// one predicate without the other fails here instead of silently
// under-selecting the keyed load.
func TestDeferredRelationshipFamilySidecarSharesCandidateLiterals(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		deferredRelationshipFamilyArtifactTypeListSQL,
		deferredRelationshipFamilyPathPatternSQL,
	} {
		if !strings.Contains(DeferredRelationshipFamilyCandidatePredicateSQL, fragment) {
			t.Error("candidate predicate lost shared fragment; keyed and legacy family arms drift")
		}
		if !strings.Contains(DeferredRelationshipFamilySidecarPredicateSQL, fragment) {
			t.Error("sidecar predicate lost shared fragment; keyed and legacy family arms drift")
		}
	}
	for _, marker := range []string{
		deferredRelationshipFamilyArgoCDContentMarkerSQL,
		DeferredRelationshipFamilySaltGitfsContentMarkerSQL,
	} {
		if !strings.Contains(marker, deferredRelationshipFamilyYamlSuffixPatternSQL) {
			t.Error("content marker lost the shared yaml-suffix gate")
		}
	}
	if !strings.Contains(DeferredRelationshipFamilySidecarPredicateSQL, deferredRelationshipFamilyYamlSuffixPatternSQL) {
		t.Error("sidecar predicate lost the shared yaml-suffix gate")
	}
}

// TestDeferredScopedKeyedQueryBindsRawAnchors proves the shipped deferred
// query carries the keyed arms and their $8 bind: version-gated sidecar
// selection, the position() anchor arm, the um_* marker arm, the legacy
// fallback gate for versioned rows, and the $8 parameter the loader binds.
func TestDeferredScopedKeyedQueryBindsRawAnchors(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"keyed_reference_keys AS MATERIALIZED",
		"ref.keys_version >= 1",
		"keyed_anchor_matches AS MATERIALIZED",
		"keyed_marker_matches AS MATERIALIZED",
		"FROM keyed_anchor_matches AS ref",
		"FROM keyed_marker_matches AS ref",
		"FROM unnest($8::text[]) AS anchor(value)",
		"position(anchor.value in ref.reference_key) > 0",
		"FROM keyed_reference_keys AS keyed",
		"keyed.fact_id = fact.fact_id",
	} {
		if !strings.Contains(ListDeferredScopedRelationshipFactRecordsQuery, want) {
			t.Errorf("shipped deferred query lacks keyed fragment %q", want)
		}
	}
	if got := strings.Count(ListDeferredScopedRelationshipFactRecordsQuery, DeferredRelationshipFamilySidecarPredicateSQL); got != 2 {
		t.Errorf("sidecar predicate appears %d times, want 2 (anchor and marker arms)", got)
	}
}

// TestBackfillNonRepoIDAnchorRawTermsExcludesMarkersAndEmpties proves the
// $8 raw anchors are exactly the alias anchors the $1 LIKE terms wrap:
// no unconditional markers (the keyed marker arm covers those), no LIKE
// wrapping or escaping, no empty strings (position(” in x) matches every
// row), and every raw anchor appears inside some $1 LIKE term.
func TestBackfillNonRepoIDAnchorRawTermsExcludesMarkersAndEmpties(t *testing.T) {
	t.Parallel()

	catalog := []relationships.CatalogEntry{{
		RepoID:  "github.com/acme/app",
		Aliases: []string{"github.com/acme/app", "app", "  ", "APP"},
	}}
	raw := backfillNonRepoIDAnchorRawTerms(catalog)
	if len(raw) == 0 {
		t.Fatal("backfillNonRepoIDAnchorRawTerms() = nil, want anchors")
	}
	for _, anchor := range raw {
		if anchor == "" {
			t.Error("raw anchors contain an empty string, which matches every sidecar row")
		}
		if strings.ContainsAny(anchor, "%_\\") {
			t.Errorf("raw anchor %q carries LIKE syntax; $8 is matched with position(), not LIKE", anchor)
		}
	}
	for _, marker := range relationships.UnconditionalPayloadMarkers() {
		for _, anchor := range raw {
			if anchor == strings.ToLower(marker) {
				t.Errorf("raw anchors contain marker %q; markers belong to the um_* arm", marker)
			}
		}
	}
	likeTerms := buildPayloadAnchorLikeTerms(backfillNonRepoIDAnchorTerms(catalog))
	for _, anchor := range raw {
		found := false
		for _, term := range likeTerms {
			if strings.Contains(term, anchor) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("raw anchor %q appears in no $1 LIKE term; $8 drifted from $1", anchor)
		}
	}
}
