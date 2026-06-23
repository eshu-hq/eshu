package relationships

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// deferredSelfExclusionSim reproduces the SQL predicate of
// listDeferredScopedRelationshipFactRecordsQuery (issue #3659) in pure Go so the
// truth-equivalence and real-scoping gates can run without a live Postgres.
//
// A fact is selected iff:
//
//	lower(payload::text) LIKE ANY($1 non-repo_id anchors)
//	OR (
//	    lower(payload::text) LIKE ANY($2 repo_id anchors)
//	    AND lower(payload->>'repo_id') NOT IN ($3 repo_id values)
//	)
//
// nonRepoIDAnchors is CatalogPayloadAnchors over each entry's NON-first aliases
// (name/slug) plus the ArgoCD markers — mirroring backfillNonRepoIDAnchorTerms.
// repoIDTokens / repoIDValues are CatalogRepoIDAnchors over the full catalog.
func deferredSelfExclusionSim(
	t *testing.T,
	envelope facts.Envelope,
	nonRepoIDAnchors, repoIDTokens, repoIDValues []string,
) bool {
	t.Helper()
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	text := strings.ToLower(string(raw))

	matchesAny := func(anchors []string) bool {
		for _, anchor := range anchors {
			anchor = strings.ToLower(strings.TrimSpace(anchor))
			if anchor != "" && strings.Contains(text, anchor) {
				return true
			}
		}
		return false
	}

	if matchesAny(nonRepoIDAnchors) {
		return true
	}
	if !matchesAny(repoIDTokens) {
		return false
	}
	// Self-exclusion: drop when the fact's own repo_id is in the exclusion set.
	ownRepoID, _ := envelope.Payload["repo_id"].(string)
	ownRepoID = strings.ToLower(strings.TrimSpace(ownRepoID))
	for _, value := range repoIDValues {
		if ownRepoID == strings.ToLower(strings.TrimSpace(value)) {
			return false
		}
	}
	return true
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
	name             string
	envelope         facts.Envelope
	sourceCatalog    CatalogEntry // the SOURCE repo's own catalog entry (self)
	targetCatalog    CatalogEntry // the repo the fact references (cross-repo)
	crossRepoByRepoID bool        // the reference is the target's repo_id (not name)
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
					// References the TARGET repo by its full repo_id URL, the
					// legitimate cross-repo repo_id reference the self-exclusion must
					// NOT drop. The target repo_id's longest token (deploy-toolkit)
					// is selective and appears verbatim in the workflow content.
					"content": "jobs:\n  build:\n    uses: github.com/org/deploy-toolkit/.github/workflows/deploy.yaml@main\n",
				},
			},
			sourceCatalog:     CatalogEntry{RepoID: "repo-app", Aliases: []string{"repo-app", "edge-app"}},
			targetCatalog:     CatalogEntry{RepoID: "github.com/org/deploy-toolkit", Aliases: []string{"github.com/org/deploy-toolkit", "deploy-toolkit"}},
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
	repoIDTokens, repoIDValues := CatalogRepoIDAnchors(fullCatalog)

	var scopedLoad []facts.Envelope
	for _, envelope := range fullCorpus {
		if deferredSelfExclusionSim(t, envelope, nonRepoID, repoIDTokens, repoIDValues) {
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
	repoIDTokens, repoIDValues := CatalogRepoIDAnchors(catalog)

	if !deferredSelfExclusionSim(t, crossFixture.envelope, nonRepoID, repoIDTokens, repoIDValues) {
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
	repoIDTokens, repoIDValues := CatalogRepoIDAnchors(catalog)

	if deferredSelfExclusionSim(t, selfEnvelope, nonRepoID, repoIDTokens, repoIDValues) {
		t.Fatal("pure self-match fact was loaded; the self-exclusion predicate is not bounding the load")
	}

	// Truth-equivalence: discovering over the self-fact produces no evidence,
	// so excluding it loses nothing.
	evidence := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{selfEnvelope}, catalog))
	if len(evidence) != 0 {
		t.Fatalf("pure self-match fact produced evidence %v; self-exclusion would have dropped it", canonicalEvidence(evidence))
	}
}
