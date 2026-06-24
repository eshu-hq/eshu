// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// deferredSelfExclusionSim reproduces the ORIGINAL #3659 boundary-regex SQL
// predicate of listDeferredScopedRelationshipFactRecordsQuery in pure Go. It is
// retained as the reference for the regex form so the LIKE-superset proof
// (TestDeferredLikeSupersetMatcherRefinesToBoundaryEvidence) can show the new
// LIKE form selects a strict superset of what the regex form selected.
//
// A fact is selected iff:
//
//	lower(payload::text) LIKE ANY($1 non-repo_id anchors)
//	OR EXISTS rid IN $2 repo_id values: rid <> own_repo_id AND payload contains rid
//	  at a token boundary
//
// nonRepoIDAnchors is CatalogPayloadAnchors over each entry's NON-first aliases
// (name/slug) plus the ArgoCD markers — mirroring backfillNonRepoIDAnchorTerms.
// repoIDValues is CatalogRepoIDValues over the full catalog (full repo_id
// strings). The $2 arm is the self-aware EXISTS test: it matches a catalog
// repo_id that is NOT the row's own, so a target repo_id that merely CONTAINS
// the source repo_id as a substring is still matched (no blind replace()
// corruption that the prior implementation suffered).
//
// NOTE: production SQL now uses the LIKE-substring form modeled by
// deferredLikeSupersetSim (issue #3710). This regex sim stays as the narrower
// reference set; the matcher refines both forms to the identical evidence.
func deferredSelfExclusionSim(
	t *testing.T,
	envelope facts.Envelope,
	nonRepoIDAnchors, repoIDValues []string,
) bool {
	t.Helper()
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	text := strings.ToLower(string(raw))

	for _, anchor := range nonRepoIDAnchors {
		anchor = strings.ToLower(strings.TrimSpace(anchor))
		if anchor != "" && strings.Contains(text, anchor) {
			return true
		}
	}

	// $2 arm: EXISTS a catalog repo_id value that is NOT the row's own and
	// appears in the payload as a TOKEN-BOUNDARY-delimited substring (mirrors the
	// SQL `~ (^|[^a-z0-9._-])value($|[^a-z0-9._-])` test). Boundary matching is
	// required so repo_id "repo-fleet-1" does not spuriously match
	// "repo-fleet-15", matching the in-memory catalogMatcher's whole-token
	// semantics while still matching a distinct longer repo_id referenced verbatim.
	ownRepoID, _ := envelope.Payload["repo_id"].(string)
	ownRepoID = strings.ToLower(strings.TrimSpace(ownRepoID))
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == ownRepoID {
			continue
		}
		if containsRepoIDAtBoundary(text, value) {
			return true
		}
	}
	return false
}

// isRepoIDTokenChar reports whether r is part of a catalog match token (mirrors
// isCatalogTokenChar and the SQL [a-z0-9._-] boundary class).
func isRepoIDTokenChar(r byte) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
		r == '.' || r == '_' || r == '-'
}

// containsRepoIDAtBoundary reports whether value occurs in text delimited by a
// non-token char (or string start/end) on both sides — the Go mirror of the
// boundary regex the deferred query uses for the repo_id arm.
func containsRepoIDAtBoundary(text, value string) bool {
	if value == "" {
		return false
	}
	from := 0
	for {
		idx := strings.Index(text[from:], value)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(value)
		leftOK := start == 0 || !isRepoIDTokenChar(text[start-1])
		rightOK := end == len(text) || !isRepoIDTokenChar(text[end])
		if leftOK && rightOK {
			return true
		}
		from = start + 1
	}
}

// nonRepoIDAnchorsSim mirrors backfillNonRepoIDAnchorTerms in the postgres
// package: CatalogPayloadAnchors over each entry's aliases with the first alias
// (the repo_id) stripped, unioned with the ArgoCD over-select markers.
func nonRepoIDAnchorsSim(catalog []CatalogEntry) []string {
	stripped := make([]CatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if len(entry.Aliases) <= 1 {
			continue
		}
		stripped = append(stripped, CatalogEntry{
			RepoID:  entry.RepoID,
			Aliases: entry.Aliases[1:],
		})
	}
	anchors := CatalogPayloadAnchors(stripped)
	if len(anchors) == 0 {
		return nil
	}
	return append(append([]string(nil), anchors...), argoCDOverSelectMarkersSim...)
}

// representativeDeferredFixture is a content/file fact whose payload carries its
// own repo_id (the realistic shape the #3655 benchmark omitted), plus the
// catalog entry the source repo references. repo_id is Aliases[0]; the
// human-facing name/slug follow.
type representativeDeferredFixture struct {
	name              string
	envelope          facts.Envelope
	sourceCatalog     CatalogEntry // the SOURCE repo's own catalog entry (self)
	targetCatalog     CatalogEntry // the repo the fact references (cross-repo)
	crossRepoByRepoID bool         // the reference is the target's repo_id (not name)
}

// representativeDeferredFixtures returns fixtures whose payloads DO carry
// repo_id, so self-matches genuinely fire. Some reference the target by name,
// one references the target by the target's repo_id (proving cross-repo repo_id
// references survive the self-exclusion). Identifiers are generic.
func representativeDeferredFixtures() []representativeDeferredFixture {
	// Repo_ids are this system's realistic "repo-<name>" / "repository:r_<id>"
	// shape (surveyed from the fact corpus), where the human-facing name is NOT
	// a substring of the repo_id, so a fact's own name does not self-match the
	// $1 (non-repo_id) arm — the only self-match channel is the repo_id field.
	return []representativeDeferredFixture{
		{
			name: "terraform_app_repo_by_name",
			envelope: facts.Envelope{
				ScopeID: "scope:infra",
				Payload: map[string]any{
					"repo_id":       "repo-infra",
					"artifact_type": "terraform",
					"relative_path": "main.tf",
					"content":       `app_repo = "payments-service"`,
				},
			},
			sourceCatalog: CatalogEntry{RepoID: "repo-infra", Aliases: []string{"repo-infra", "platform-infra"}},
			targetCatalog: CatalogEntry{RepoID: "repo-payments", Aliases: []string{"repo-payments", "payments-service"}},
		},
		{
			name: "helm_values_by_name",
			envelope: facts.Envelope{
				ScopeID: "scope:charts",
				Payload: map[string]any{
					"repo_id":       "repo-charts",
					"artifact_type": "helm",
					"relative_path": "values.yaml",
					"content":       "image:\n  repository: billing-service\n",
				},
			},
			sourceCatalog: CatalogEntry{RepoID: "repo-charts", Aliases: []string{"repo-charts", "helm-charts"}},
			targetCatalog: CatalogEntry{RepoID: "repo-billing", Aliases: []string{"repo-billing", "billing-service"}},
		},
		{
			name: "github_actions_reusable_by_repo_id",
			envelope: facts.Envelope{
				ScopeID: "scope:app",
				Payload: map[string]any{
					"repo_id":       "repo-app",
					"artifact_type": "github_actions_workflow",
					"relative_path": ".github/workflows/ci.yaml",
					// References the TARGET repo by its repo_id (deploy-toolkit), the
					// legitimate cross-repo repo_id reference the self-exclusion must
					// NOT drop. The repo_id is the verbatim org/deploy-toolkit ref the
					// reusable-workflow extractor resolves to the target.
					"content": "jobs:\n  build:\n    uses: org/deploy-toolkit/.github/workflows/deploy.yaml@main\n",
				},
			},
			sourceCatalog:     CatalogEntry{RepoID: "repo-app", Aliases: []string{"repo-app", "edge-app"}},
			targetCatalog:     CatalogEntry{RepoID: "deploy-toolkit", Aliases: []string{"deploy-toolkit"}},
			crossRepoByRepoID: true,
		},
		{
			name: "overlapping_repo_id_prefix_reference",
			envelope: facts.Envelope{
				ScopeID: "scope:app",
				Payload: map[string]any{
					"repo_id":       "github.com/org/app",
					"artifact_type": "github_actions_workflow",
					"relative_path": ".github/workflows/deploy.yaml",
					"content":       "uses: github.com/org/app-config/.github/workflows/deploy.yaml@main",
				},
			},
			sourceCatalog:     CatalogEntry{RepoID: "github.com/org/app", Aliases: []string{"github.com/org/app", "app"}},
			targetCatalog:     CatalogEntry{RepoID: "github.com/org/app-config", Aliases: []string{"github.com/org/app-config"}},
			crossRepoByRepoID: true,
		},
	}
}

// TestDeferredSelfExclusionTruthEquivalence is the issue #3659 eshu-correlation-
// truth gate. Over a representative corpus where every payload carries its own
// repo_id, discovering evidence over the deferred self-exclusion fact load must
// produce EXACTLY the same evidence as discovering over the entire fact corpus —
// no cross-repo edge dropped. The self-exclusion only removes facts whose sole
// match is their own repo_id, which the in-memory matcher already skips, so the
// emitted evidence is unchanged.
func TestDeferredSelfExclusionTruthEquivalence(t *testing.T) {
	t.Parallel()

	fixtures := representativeDeferredFixtures()

	var fullCorpus []facts.Envelope
	var fullCatalog []CatalogEntry
	seenRepo := make(map[string]bool)
	addCatalog := func(entry CatalogEntry) {
		if seenRepo[entry.RepoID] {
			return
		}
		seenRepo[entry.RepoID] = true
		fullCatalog = append(fullCatalog, entry)
	}
	for _, fixture := range fixtures {
		fullCorpus = append(fullCorpus, fixture.envelope)
		addCatalog(fixture.sourceCatalog)
		addCatalog(fixture.targetCatalog)
	}

	// Add a self-referential noise fact: its content references ONLY its own
	// repo_id, so it self-matches the repo_id anchor and must be excluded by the
	// deferred predicate while producing no evidence in the full load. The name
	// (secret-store) is deliberately NOT a substring of the repo_id (repo-vault)
	// so the only self-match channel is the repo_id field.
	fullCorpus = append(fullCorpus, facts.Envelope{
		ScopeID: "scope:vault",
		Payload: map[string]any{
			"repo_id":       "repo-vault",
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			// References only its own repo_id; no other catalog token present.
			"content": `source = "repo-vault//modules/local"`,
		},
	})
	addCatalog(CatalogEntry{RepoID: "repo-vault", Aliases: []string{"repo-vault", "secret-store"}})

	nonRepoID := nonRepoIDAnchorsSim(fullCatalog)
	repoIDValues := CatalogRepoIDValues(fullCatalog)

	var scopedLoad []facts.Envelope
	for _, envelope := range fullCorpus {
		if deferredSelfExclusionSim(t, envelope, nonRepoID, repoIDValues) {
			scopedLoad = append(scopedLoad, envelope)
		}
	}

	// Real scoping: the self-only fact must be dropped, so the scoped load is
	// strictly smaller than the full corpus.
	if len(scopedLoad) >= len(fullCorpus) {
		t.Fatalf("deferred scoped load (%d) did not exclude the self-only fact from the corpus (%d)", len(scopedLoad), len(fullCorpus))
	}

	fullEvidence := DedupeEvidenceFacts(DiscoverEvidence(fullCorpus, fullCatalog))
	scopedEvidence := DedupeEvidenceFacts(DiscoverEvidence(scopedLoad, fullCatalog))

	if !reflect.DeepEqual(canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence)) {
		t.Fatalf("deferred scoped evidence != full evidence\nfull:   %v\nscoped: %v",
			canonicalEvidence(fullEvidence), canonicalEvidence(scopedEvidence))
	}
	if len(fullEvidence) == 0 {
		t.Fatal("expected non-empty evidence from the representative corpus")
	}
}

// TestDeferredSelfExclusionKeepsCrossRepoRepoIDReference proves the documented
// superset guarantee: a fact that references ANOTHER repo's repo_id (not the
// fact's own) survives the self-exclusion and is loaded. This is the cross-repo
// edge the issue warns must not be dropped.
func TestDeferredSelfExclusionKeepsCrossRepoRepoIDReference(t *testing.T) {
	t.Parallel()

	var crossFixture representativeDeferredFixture
	for _, fixture := range representativeDeferredFixtures() {
		if fixture.crossRepoByRepoID {
			crossFixture = fixture
			break
		}
	}
	if crossFixture.name == "" {
		t.Fatal("no cross-repo-by-repo_id fixture defined")
	}

	catalog := []CatalogEntry{crossFixture.sourceCatalog, crossFixture.targetCatalog}
	nonRepoID := nonRepoIDAnchorsSim(catalog)
	repoIDValues := CatalogRepoIDValues(catalog)

	if !deferredSelfExclusionSim(t, crossFixture.envelope, nonRepoID, repoIDValues) {
		t.Fatalf("cross-repo repo_id reference %q was dropped by the self-exclusion predicate", crossFixture.name)
	}

	// And it must actually produce a cross-repo edge under discovery.
	evidence := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{crossFixture.envelope}, catalog))
	found := false
	for _, fact := range evidence {
		if fact.TargetRepoID == crossFixture.targetCatalog.RepoID && fact.SourceRepoID == crossFixture.sourceCatalog.RepoID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a %s -> %s cross-repo edge, got %v",
			crossFixture.sourceCatalog.RepoID, crossFixture.targetCatalog.RepoID, canonicalEvidence(evidence))
	}
}

// TestDeferredSelfExclusionExcludesPureSelfMatch proves a fact whose ONLY
// matching anchor is its own repo_id is excluded by the deferred predicate (the
// scope-bounding win), and that excluding it drops no evidence (the in-memory
// matcher skips the self-match anyway).
func TestDeferredSelfExclusionExcludesPureSelfMatch(t *testing.T) {
	t.Parallel()

	// repo_id "repo-orders"; name "order-gateway" is NOT a substring of the
	// repo_id, so the only self-match channel is the repo_id field.
	selfEnvelope := facts.Envelope{
		ScopeID: "scope:orders",
		Payload: map[string]any{
			"repo_id":       "repo-orders",
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `tags = { repo = "repo-orders" }`,
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-orders", Aliases: []string{"repo-orders", "order-gateway"}},
	}

	nonRepoID := nonRepoIDAnchorsSim(catalog)
	repoIDValues := CatalogRepoIDValues(catalog)

	if deferredSelfExclusionSim(t, selfEnvelope, nonRepoID, repoIDValues) {
		t.Fatal("pure self-match fact was loaded; the self-exclusion predicate is not bounding the load")
	}

	// Truth-equivalence: discovering over the self-fact produces no evidence,
	// so excluding it loses nothing.
	evidence := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{selfEnvelope}, catalog))
	if len(evidence) != 0 {
		t.Fatalf("pure self-match fact produced evidence %v; self-exclusion would have dropped it", canonicalEvidence(evidence))
	}
}

// TestDeferredSelfExclusionKeepsPrefixCollidingTargetRepoID is the regression
// gate for the PR #3668 P2 bot finding: when a fact's OWN repo_id is a
// PREFIX/substring of ANOTHER repo's repo_id, the deferred self-exclusion must
// still load the fact when it references that longer repo_id by its full value.
//
// The earlier blind-replace implementation
// (replace(payload, own_repo_id, "")) corrupted the target reference: stripping
// "github.com/org/app" from "github.com/org/app-config" left "-config", so the
// "%github.com/org/app-config%" term never matched and the cross-repo edge was
// dropped — a truth-equivalence violation, because full-corpus DiscoverEvidence
// only skips the EXACT self entry. The EXISTS(value <> own) test compares whole
// repo_id values, so the longer repo_id is a distinct value and still matches.
func TestDeferredSelfExclusionKeepsPrefixCollidingTargetRepoID(t *testing.T) {
	t.Parallel()

	// Source repo_id is a strict prefix of the target repo_id.
	sourceRepoID := "github.com/org/app"
	targetRepoID := "github.com/org/app-config"

	// Source references the target ONLY by the target's full repo_id, not by any
	// name/slug alias.
	envelope := facts.Envelope{
		ScopeID: "scope:app",
		Payload: map[string]any{
			"repo_id":       sourceRepoID,
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `app_repo = "github.com/org/app-config"`,
		},
	}
	catalog := []CatalogEntry{
		// Target has only its repo_id alias, so the $1 (name/slug) arm cannot
		// cover it — the load relies entirely on the $2 repo_id arm.
		{RepoID: targetRepoID, Aliases: []string{targetRepoID}},
		{RepoID: sourceRepoID, Aliases: []string{sourceRepoID, "edge-app"}},
	}

	nonRepoID := nonRepoIDAnchorsSim(catalog)
	repoIDValues := CatalogRepoIDValues(catalog)

	// The deferred predicate MUST load this fact — the target repo_id
	// (github.com/org/app-config) is a distinct value from the source repo_id
	// (github.com/org/app) and is a literal substring of the payload.
	if !deferredSelfExclusionSim(t, envelope, nonRepoID, repoIDValues) {
		t.Fatalf("prefix-colliding cross-repo reference %q -> %q was DROPPED by the self-exclusion predicate; the blind-replace truth-equivalence bug is present",
			sourceRepoID, targetRepoID)
	}

	// Truth-equivalence: full-corpus discovery DOES match the edge, so the
	// deferred load must not have dropped it.
	fullEvidence := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{envelope}, catalog))
	foundTarget := false
	for _, fact := range fullEvidence {
		if fact.TargetRepoID == targetRepoID && fact.SourceRepoID == sourceRepoID {
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		t.Fatalf("full-corpus discovery did not produce the %s -> %s edge, fixture is not exercising the bug; evidence=%v",
			sourceRepoID, targetRepoID, canonicalEvidence(fullEvidence))
	}

	// And the source fact's OWN repo_id (the prefix) must NOT be the reason it
	// loads: a fact that references ONLY its own prefix repo_id is still excluded.
	selfOnly := facts.Envelope{
		ScopeID: "scope:app",
		Payload: map[string]any{
			"repo_id":       sourceRepoID,
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `tags = { repo = "github.com/org/app" }`,
		},
	}
	if deferredSelfExclusionSim(t, selfOnly, nonRepoID, repoIDValues) {
		t.Fatal("a fact referencing only its own prefix repo_id was loaded; the self-exclusion is over-loading")
	}
}
