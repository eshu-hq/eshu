// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

// Sixteen common trigram-bearing topic terms for the #8370 16-term pins.
// Every term carries a run of at least three ASCII letters, so the pg_trgm
// guard hides its file-content bind on repo-scoped searches.
func sixteenTrigramTopicTerms() []string {
	return []string{
		"auth", "sync", "repo", "token",
		"config", "handler", "query", "filter",
		"cache", "retry", "scope", "route",
		"worker", "index", "store", "probe",
	}
}

// TestInvestigateCodeTopicSixteenTermSerialFallbackHidesTrigramBinds pins
// the #8370 serial shape: a 16-term repo-scoped search with trigram-bearing
// terms hides every file-content bind behind MATERIALIZED term_param on the
// reservation-timeout fallback path. The digit-term golden
// (scoped_sixteen_terms_serial.sql) pins the all-visible shape for
// trigram-less terms; this pins the opposite corner so a guard regression at
// 16 terms cannot slip through.
func TestInvestigateCodeTopicSixteenTermSerialFallbackHidesTrigramBinds(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{
			"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
			"entity_type", "language", "start_line", "end_line", "matched_terms",
			"score", "pool_truncated",
		},
	}})
	_, err := NewContentReader(db).InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		RepoID: "repo-1", Terms: sixteenTrigramTopicTerms(), Limit: 26,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(recorder.queries); got != 1 {
		t.Fatalf("serial fallback statements = %d, want 1", got)
	}
	query := recorder.queries[0]
	if got := strings.Count(query, "term_param AS MATERIALIZED"); got != 16 {
		t.Fatalf("sixteen-term trigram repo search term CTEs = %d, want 16 hidden content binds", got)
	}
	if got := strings.Count(query, "f.content ILIKE '%' || $"); got != 0 {
		t.Fatalf("sixteen-term trigram repo direct content binds = %d, want hidden", got)
	}
}

// TestCodeTopicParallelProbeHidesScopedTrigramBinds proves the #8370
// parallel fix: a 16-term repo-scoped partition probe hides each
// trigram-bearing term's file-content bind, matching the serial
// codeTopicFileBranch shape that measured 10.1s to 1.3s on the largest repo.
func TestCodeTopicParallelProbeHidesScopedTrigramBinds(t *testing.T) {
	t.Parallel()

	req := CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: sixteenTrigramTopicTerms()}
	filters, baseArgs, _ := codeTopicFilters(req)
	probe, args := codetopicparallel.ProbeSQL(req, codeTopicCandidateCap(len(req.Terms)), filters, baseArgs)
	if got := strings.Count(probe, "term_param AS MATERIALIZED"); got != 16 {
		t.Fatalf("sixteen-term scoped probe term CTEs = %d, want 16 hidden content binds", got)
	}
	if got := strings.Count(probe, "f.content ILIKE '%' || $"); got != 0 {
		t.Fatalf("sixteen-term scoped probe direct content binds = %d, want hidden", got)
	}
	if got, want := len(args), 17; got != want {
		t.Fatalf("len(probe args) = %d, want %d (repo_id + 16 terms)", got, want)
	}
	if got, want := args[0], "repo-1"; got != want {
		t.Fatalf("repo arg = %#v, want %#v", got, want)
	}
}

// TestCodeTopicParallelProbeKeepsNoTrigramBindVisible proves the per-term
// #7246 guard survives the #8370 parallel threading: a term with no
// extractable trigram keeps its visible content bind inside a repo-scoped
// 16-term partition probe while trigram-bearing siblings hide.
func TestCodeTopicParallelProbeKeepsNoTrigramBindVisible(t *testing.T) {
	t.Parallel()

	terms := append(sixteenTrigramTopicTerms()[:15], "db_")
	req := CodeTopicInvestigationRequest{RepoID: "repo-1", Terms: terms}
	filters, baseArgs, _ := codeTopicFilters(req)
	probe, _ := codetopicparallel.ProbeSQL(req, codeTopicCandidateCap(len(req.Terms)), filters, baseArgs)
	if got := strings.Count(probe, "term_param AS MATERIALIZED"); got != 15 {
		t.Fatalf("mixed-trigram scoped probe term CTEs = %d, want 15 (db_ visible)", got)
	}
	if got := strings.Count(probe, "f.content ILIKE '%' || $"); got != 1 {
		t.Fatalf("mixed-trigram scoped probe direct content binds = %d, want 1 (db_ visible)", got)
	}
}

// TestCodeTopicParallelScopedBranchMatchesSerialBranch pins the #8370 twin
// builders together: the parallel hidden branch must stay byte-identical to
// the serial one for the same inputs, so a pool-accounting or predicate edit
// lands in both or fails here.
func TestCodeTopicParallelScopedBranchMatchesSerialBranch(t *testing.T) {
	t.Parallel()

	for _, where := range []string{"AND repo_id = $1", "AND repo_id = $1 AND coalesce(language, '') = $2"} {
		if got, want := codetopicparallel.ScopedFileBranch(4, where, 250), scopedCodeTopicFileBranch(4, where, 250); got != want {
			t.Fatalf("scoped branch drift for where %q:\nparallel:\n%s\nserial:\n%s", where, got, want)
		}
	}
}

// TestCodeTopicParallelProbeKeepsUnscopedBindsVisible guards against
// over-hiding: corpus-wide, grant-list, and language-filtered 16-term probes
// keep the shared upstream visible branch even when every term carries a
// trigram.
func TestCodeTopicParallelProbeKeepsUnscopedBindsVisible(t *testing.T) {
	t.Parallel()

	for _, req := range []CodeTopicInvestigationRequest{
		{Terms: sixteenTrigramTopicTerms()},
		{AllowedRepositoryIDs: []string{"repo-1"}, Terms: sixteenTrigramTopicTerms()},
		{RepoID: "repo-1", Language: "go", Terms: sixteenTrigramTopicTerms()},
	} {
		filters, baseArgs, _ := codeTopicFilters(req)
		probe, _ := codetopicparallel.ProbeSQL(req, codeTopicCandidateCap(len(req.Terms)), filters, baseArgs)
		if strings.Contains(probe, "term_param AS MATERIALIZED") {
			t.Fatalf("req %+v hides binds, want the upstream visible branch", req)
		}
		if got := strings.Count(probe, "f.content ILIKE '%' || $"); got != 16 {
			t.Fatalf("req %+v direct content binds = %d, want 16 visible", req, got)
		}
	}
}
