// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// POST /api/v0/code/dead-code/cross-repo bounds its consumer reads by what the
// request asked for, and that is what this file pins.
//
// A scoped caller that names no consumers gets two statements. The evidence
// page carries the grant, so its 1001-row cap falls on consumers the caller may
// see. The ungranted-consumer probe answers the other half -- is there a
// consumer this caller cannot see? -- and answers it per producer entity,
// bounded by that entity's own index seeks rather than by a shared row budget.
//
// A request that names consumers in consumer_repo_ids gets one statement, bound
// to those consumers. The row cap then falls where the question is: bound to the
// whole grant instead, a thousand rows from a repository the caller did not ask
// about filled the page and pushed the requested consumer off it, and the
// candidate came back unknown_needs_evidence for a symbol that consumer proves
// live. The probe is skipped for the same request, because every selector entry
// the grant admits is inside the grant -- there is nothing left for it to say,
// and not running it is what makes that structural.
//
// The count the probe contributes is one per producer entity that has an
// out-of-grant consumer, not one per such consumer. The classification only
// depends on whether there is one, and stopping at the first is what keeps the
// probe from reading a producer entity's whole fan-in group.

// TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe pins both
// statements a scoped request sends. The page must carry the grant ahead of its
// LIMIT; the second must be the ungranted-consumer probe, with the grant bound
// as its own argument rather than left off the statement entirely.
func TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id"}},
	})
	reader := NewContentReader(db)
	if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"entity-1", "entity-2"},
		crossRepoDeadCodeConsumerReads{
			PageRepositoryIDs: []string{codeGrantConsumerRepo},
			SignalGrant:       []string{codeGrantConsumerRepo, codeGrantGrantedRepo},
		},
	); err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if len(recorder.queries) != 2 {
		t.Fatalf("query count = %d, want 2 (grant-bound evidence page plus ungranted-consumer probe)", len(recorder.queries))
	}

	page := recorder.queries[0]
	grant := "AND row.repository_id = ANY($4)"
	if !strings.Contains(page, grant) {
		t.Fatalf("evidence page is missing %q, so its LIMIT is drawn from every tenant's rows:\n%s", grant, page)
	}
	if strings.Index(page, grant) > strings.Index(page, "LIMIT") {
		t.Fatalf("the grant sits after the LIMIT, so the page is still cut from a mixed set:\n%s", page)
	}

	probe := recorder.queries[1]
	if probe != crossRepoDeadCodeUngrantedConsumerProbeQuery {
		t.Fatalf("second statement is not the ungranted-consumer probe:\n%s", probe)
	}
	// The whole point of the probe is that it stops early, and it stops early
	// because it walks one producer entity's DISTINCT consumer repositories in
	// index order and quits at the first one the grant does not contain. Each
	// piece of that is pinned: the recursive walk, the per-step seek strictly
	// past the last repository, the one-row limits that keep each seek a seek,
	// and the continue-condition that ends the walk.
	for _, want := range []string{
		"WITH RECURSIVE page AS (",
		"AND (row.repository_id, row.scope_id) > (walk.repository_id, walk.scope_id)",
		"ORDER BY row.repository_id, row.scope_id\n      LIMIT 1) AS pair) AS seed",
		"NOT EXISTS (SELECT 1 FROM granted WHERE granted.repository_id = pair.repository_id)",
		"WHERE NOT walk.hidden",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe is missing %q, so it can no longer stop at the first ungranted row:\n%s", want, probe)
		}
	}
	if got, want := strings.Count(probe, "ORDER BY row.repository_id, row.scope_id"), 2; got != want {
		t.Fatalf("probe has %d ordered seeks, want %d (the walk's seed and its step)", got, want)
	}
	// The liveness test is what makes a step independent of how many
	// superseded generations the retention runner still keeps, and it only is
	// that if all four key columns are equalities against the pair the walk
	// just found. Losing any one of them leaves the generation a filter over
	// the pair's retained rows and the answer unchanged, so nothing else here
	// can see it.
	for _, want := range []string{
		"AND live_row.repository_id = pair.repository_id",
		"AND live_row.scope_id = pair.scope_id",
		"AND live_row.generation_id = scope.active_generation_id",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe is missing %q, so a step scans a pair's retained generations instead of seeking its active row:\n%s", want, probe)
		}
	}
	// A bound rendered per granted repository is what this shape replaced: it
	// cost one index probe per granted repository per producer entity, so a
	// broad grant scaled the read linearly. Nothing here may reintroduce one.
	for _, forbidden := range []string{"gap.lo", "gap.hi", "grant_bounds", "lag(repository_id)"} {
		if strings.Contains(probe, forbidden) {
			t.Fatalf("probe carries %q, a per-granted-repository bound; its cost then grows with the caller's grant:\n%s", forbidden, probe)
		}
	}
	if strings.Contains(probe, "row.confidence") || strings.Contains(probe, "row.evidence") {
		t.Fatalf("probe selects consumer evidence columns; it must answer whether, never which:\n%s", probe)
	}
	if got, want := len(recorder.args[1]), 4; got != want {
		t.Fatalf("len(args) = %d, want %d (producer repo, entity array, grant array, page size)", got, want)
	}
	if got, want := fmt.Sprintf("%v", recorder.args[1][3]), "2"; got != want {
		t.Fatalf("probe LIMIT argument = %v, want %v (one row per producer entity at most)", got, want)
	}
}

// TestCrossRepoDeadCodeProbeStatementIsSizeIndependent is why the probe binds
// arrays instead of rendering one placeholder per entity: the statement text is
// the same for every page and every grant, so a page of 250 candidates and a
// page of 2 plan as one statement rather than two.
func TestCrossRepoDeadCodeProbeStatementIsSizeIndependent(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id"}},
		{columns: crossRepoDeadCodeEvidenceColumns()},
		{columns: []string{"entity_id"}},
	})
	reader := NewContentReader(db)
	read := func(entityIDs []string, grant []string) {
		t.Helper()
		if _, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			entityIDs,
			crossRepoDeadCodeConsumerReads{PageRepositoryIDs: grant, SignalGrant: grant},
		); err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
	}
	read([]string{"entity-1"}, []string{codeGrantConsumerRepo})
	read(
		[]string{"entity-1", "entity-2", "entity-3"},
		[]string{codeGrantConsumerRepo, codeGrantGrantedRepo, codeGrantOtherRepo},
	)
	if recorder.queries[1] != recorder.queries[3] {
		t.Fatalf("probe text changed with the page and grant size:\nfirst:\n%s\nsecond:\n%s", recorder.queries[1], recorder.queries[3])
	}
}

// TestCrossRepoDeadCodeProbeRefusesAnEmptyGrant covers the one input that would
// make the probe lie. Its ranges are the complement of the grant, so an empty
// grant makes every range empty and the answer "nothing is hidden" -- for a
// caller who may see nothing, where everything is.
//
// The refusals that keep such a caller away from the probe are
// crossRepoDeadCodeConsumerReadPlan's, and they are pinned where that function
// is exercised: TestCrossRepoDeadCodeConsumerReadPlan's "scoped caller with no
// grant at all reads nothing" and "scoped request naming only ungranted
// consumers reads nothing" (code_dead_code_cross_repo_selector_test.go).
// Neither subtest here calls the plan. The second one below is the read's own
// refusal, one call further down, for a future caller that reaches it directly.
func TestCrossRepoDeadCodeProbeRefusesAnEmptyGrant(t *testing.T) {
	t.Parallel()

	// A request that named its consumers gets no signal read at all: the
	// handler's plan leaves SignalGrant empty, and the reader takes that as
	// "do not run it" rather than as "run it unbounded". One statement, no
	// hidden rows. This is the reader's half of the contract; the plan's half
	// -- deciding when SignalGrant is empty -- is TestCrossRepoDeadCodeConsumerReadPlan's.
	t.Run("named consumers skip the signal read", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: crossRepoDeadCodeEvidenceColumns()},
		})
		reader := NewContentReader(db)
		_, hidden, err := reader.CrossRepoDeadCodeConsumerEvidence(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			crossRepoDeadCodeConsumerReads{PageRepositoryIDs: []string{codeGrantConsumerRepo}},
		)
		if err != nil {
			t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
		}
		if len(hidden) != 0 {
			t.Fatalf("hidden = %#v, want empty", hidden)
		}
		if len(recorder.queries) != 1 {
			t.Fatalf("query count = %d, want 1; an empty SignalGrant must not reach the probe", len(recorder.queries))
		}
	})

	// The probe refuses an empty grant itself as well, so a caller that reaches
	// it directly gets the same refusal rather than a statement whose every
	// range is empty.
	t.Run("at the read itself", func(t *testing.T) {
		t.Parallel()

		db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
			{columns: []string{"entity_id"}, rows: [][]driver.Value{{"entity-1"}}},
		})
		reader := NewContentReader(db)
		hidden, err := reader.crossRepoDeadCodeUngrantedConsumers(
			context.Background(),
			codeGrantGrantedRepo,
			[]string{"entity-1"},
			nil,
		)
		if err != nil {
			t.Fatalf("crossRepoDeadCodeUngrantedConsumers() error = %v, want nil", err)
		}
		if len(hidden) != 0 {
			t.Fatalf("hidden = %#v, want empty; an empty grant hides everything, not nothing", hidden)
		}
		if len(recorder.queries) != 0 {
			t.Fatalf("query count = %d, want 0; the probe must not run without a grant", len(recorder.queries))
		}
	})
}

// TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector is the case the
// count exists to get right and the one it used to get wrong.
//
// The caller is granted the producer and one consumer, and asks about that
// consumer alone. An unrelated repository outside the grant also consumes the
// symbol. That consumer is not part of the question, so it must not be counted
// as hidden -- counting it buried the requested consumer's own strong evidence
// under permission_hidden_consumer and answered unknown_needs_evidence for a
// symbol the answer could prove live.
func TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector(t *testing.T) {
	t.Parallel()

	store := &crossRepoDeadCodeGrantStore{}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo, codeGrantConsumerRepo})
	req := newCodeGrantRouteRequest(t, "/api/v0/code/dead-code/cross-repo", map[string]any{
		"repo_id":           codeGrantGrantedRepo,
		"language":          "go",
		"consumer_repo_ids": []string{codeGrantConsumerRepo},
	}, &auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	answer := rec.Body.String()
	if !strings.Contains(answer, `"classification":"live_by_consumer"`) {
		t.Fatalf("the requested consumer's own strong evidence must answer live_by_consumer: %s", answer)
	}
	if strings.Contains(answer, "permission_hidden_consumer") {
		t.Fatalf("a consumer outside the requested set was counted as hidden: %s", answer)
	}
	if strings.Contains(answer, "hidden_consumer_evidence_count") {
		t.Fatalf("hidden count reported for a consumer the caller did not ask about: %s", answer)
	}
	if strings.Contains(answer, codeGrantOtherRepo) {
		t.Fatalf("response leaked the out-of-grant consumer %q: %s", codeGrantOtherRepo, answer)
	}
	// The selector is what empties the signal read's contribution, so the read
	// itself is skipped rather than run and discarded.
	if store.signalRead {
		t.Fatal("signal read ran for a request whose consumer selector drops every one of its rows")
	}
	if !slices.Equal(store.boundConsumerGrant, []string{codeGrantConsumerRepo}) {
		t.Fatalf("page read bound %#v, want only the requested consumer", store.boundConsumerGrant)
	}
}

// TestCrossRepoDeadCodeProbeLeavesNoEntityUnproven is what the probe bought.
//
// The read it replaced returned rows and stopped at a shared 1001-row sentinel,
// so one producer entity with a large fan-in spent the whole budget and every
// later entity on the page came back consumer_evidence_truncated -- unknown, for
// symbols whose own evidence was complete. The probe answers per entity and
// returns at most one row each, so the busy entity costs the others nothing:
// only the evidence page can still leave an entity unproven.
func TestCrossRepoDeadCodeProbeLeavesNoEntityUnproven(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	pageRows := [][]driver.Value{{
		"producer-late", codeGrantConsumerRepo, "checkout-api", "checkout-root",
		int64(1), "reachable", 0.95, codeprovenance.MethodImportBinding,
		[]byte(`["CALLS:checkout-root->producer-late"]`), []byte(`["go.main_function"]`),
		"gen-a", "active", observedAt, observedAt,
	}}
	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns(), rows: pageRows},
		{columns: []string{"entity_id"}, rows: [][]driver.Value{{"producer-early"}}},
	})
	reader := NewContentReader(db)

	evidence, hidden, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"producer-early", "producer-late"},
		crossRepoDeadCodeConsumerReads{
			PageRepositoryIDs: []string{codeGrantConsumerRepo},
			SignalGrant:       []string{codeGrantConsumerRepo},
		},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	for _, entityID := range []string{"producer-early", "producer-late"} {
		if crossRepoDeadCodeTruncationMarked(evidence[entityID]) {
			t.Fatalf("evidence[%s] = %#v, want no truncation marker: the page was complete and the probe covers every entity", entityID, evidence[entityID])
		}
	}
	if got, want := len(evidence["producer-late"]), 1; got != want {
		t.Fatalf("len(evidence[producer-late]) = %d, want %d (the granted page row, nothing added)", got, want)
	}
	if !hidden.has("producer-early") {
		t.Fatalf("hidden = %#v, want producer-early flagged", hidden)
	}
	if hidden.has("producer-late") {
		t.Fatalf("hidden = %#v, want producer-late unflagged; the probe reported only producer-early", hidden)
	}
}

// TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer pins the
// order this route shares with /dead-code and /dead-code/investigate: a
// consumer the caller may read settles the question, and one they may not read
// only decides it when nothing granted does.
//
// The route used to answer unknown_needs_evidence for a producer with both, so
// the same mixed shape read as "reachable" on one route and "cannot tell" on
// the other. It is one rule now. The hidden count stays on the row in every
// case, so a caller told the symbol is live is also told a consumer exists
// outside their grant.
func TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer(t *testing.T) {
	t.Parallel()

	weakConsumerRow := func(entityID string) crossRepoDeadCodeEvidence {
		row := crossRepoDeadCodeGrantConsumerRow(codeGrantConsumerRepo, entityID)
		row.Confidence = codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName)
		row.ConfidenceLabel = crossRepoDeadCodeConfidenceLabel(row.Confidence)
		row.ResolutionMethod = codeprovenance.MethodRepoUniqueName
		return row
	}
	entity := func(entityID string) EntityContent {
		return EntityContent{
			EntityID:     entityID,
			RepoID:       "repo-producer",
			RelativePath: "pkg/payments/" + entityID + ".go",
			EntityType:   "Function",
			EntityName:   "maybeLive",
			Language:     "go",
			SourceCache:  "func maybeLive() {}",
		}
	}
	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]EntityContent{
				"producer-strong-plus-hidden": entity("producer-strong-plus-hidden"),
				"producer-hidden-only":        entity("producer-hidden-only"),
				"producer-weak-plus-hidden":   entity("producer-weak-plus-hidden"),
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-strong-plus-hidden", "maybeLive", "go", "pkg/payments/strong.go", 8, 12),
			deadCodeInvestigationRow("producer-hidden-only", "maybeLive", "go", "pkg/payments/hidden.go", 8, 12),
			deadCodeInvestigationRow("producer-weak-plus-hidden", "maybeLive", "go", "pkg/payments/weak.go", 8, 12),
		},
		evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{
			"producer-strong-plus-hidden": {
				crossRepoDeadCodeGrantConsumerRow(codeGrantConsumerRepo, "producer-strong-plus-hidden"),
			},
			"producer-weak-plus-hidden": {weakConsumerRow("producer-weak-plus-hidden")},
		},
		hiddenConsumers: []string{
			"producer-strong-plus-hidden",
			"producer-hidden-only",
			"producer-weak-plus-hidden",
		},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Content: content, Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	buckets := decodeEnvelopeData(t, rec.Body.Bytes())["candidate_buckets"].(map[string]any)

	// Strong granted evidence beside a hidden consumer: live, and still counted.
	live := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "producer-strong-plus-hidden")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "unknown", "producer-strong-plus-hidden")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "dead", "producer-strong-plus-hidden")
	if got, want := live["hidden_consumer_evidence_count"], float64(1); got != want {
		t.Fatalf("hidden_consumer_evidence_count = %#v, want %#v; a live answer must still say a consumer is hidden", got, want)
	}

	// Nothing granted proves use, so the hidden consumer decides the answer.
	for _, entityID := range []string{"producer-hidden-only", "producer-weak-plus-hidden"} {
		unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", entityID)
		assertCrossRepoDeadCodeReason(t, unknown, "permission_hidden_consumer")
		assertCrossRepoDeadCodeBucketMissing(t, buckets, "live_by_consumer", entityID)
		assertCrossRepoDeadCodeBucketMissing(t, buckets, "dead", entityID)
		if got, want := unknown["hidden_consumer_evidence_count"], float64(1); got != want {
			t.Fatalf("%s hidden_consumer_evidence_count = %#v, want %#v", entityID, got, want)
		}
	}
}

// TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast is the exact
// boundary case: the read returns 1,000 rows for one entity and the 1,001st row
// -- the sentinel, which is dropped -- belongs to the next entity.
//
// The statement orders by entity id, so a sentinel carrying a different id is
// proof the read moved past the first entity and returned every row it has.
// Marking that entity consumer_evidence_truncated turns valid strong evidence
// into unknown_needs_evidence for a page that was never short.
func TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 29, 13, 0, 0, 0, time.UTC)
	row := func(entityID string) []driver.Value {
		return []driver.Value{
			entityID, codeGrantConsumerRepo, "checkout-api", "checkout-root",
			int64(1), "reachable", 0.95, codeprovenance.MethodImportBinding,
			[]byte(`["CALLS:checkout-root->` + entityID + `"]`), []byte(`["go.main_function"]`),
			"gen-a", "active", observedAt, observedAt,
		}
	}
	rows := make([][]driver.Value, 0, maxCrossRepoDeadCodeConsumerEvidenceRows+1)
	for i := 0; i < maxCrossRepoDeadCodeConsumerEvidenceRows; i++ {
		rows = append(rows, row("producer-complete"))
	}
	rows = append(rows, row("producer-next"))

	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: crossRepoDeadCodeEvidenceColumns(), rows: rows},
	})
	reader := NewContentReader(db)

	evidence, _, err := reader.CrossRepoDeadCodeConsumerEvidence(
		context.Background(),
		codeGrantGrantedRepo,
		[]string{"producer-complete", "producer-next"},
		crossRepoDeadCodeConsumerReads{},
	)
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerEvidence() error = %v, want nil", err)
	}
	if crossRepoDeadCodeTruncationMarked(evidence["producer-complete"]) {
		t.Fatalf("evidence[producer-complete] carries consumer_evidence_truncated, but the sentinel row belonged to the next entity, which proves this one was read in full")
	}
	if got, want := len(evidence["producer-complete"]), maxCrossRepoDeadCodeConsumerEvidenceRows; got != want {
		t.Fatalf("len(evidence[producer-complete]) = %d, want %d", got, want)
	}
	if !crossRepoDeadCodeTruncationMarked(evidence["producer-next"]) {
		t.Fatalf("evidence[producer-next] = %#v, want the marker: its rows start at the dropped sentinel", evidence["producer-next"])
	}
}

// TestHandleCrossRepoDeadCodeTruncatedSignalOutranksStrongEvidence is the same
// case at the route: a candidate carrying both a strong granted consumer and
// the truncation marker answers unknown_needs_evidence, never live_by_consumer.
func TestHandleCrossRepoDeadCodeTruncatedSignalOutranksStrongEvidence(t *testing.T) {
	t.Parallel()

	content := &crossRepoDeadCodeContentStore{
		fakeDeadCodeContentStore: fakeDeadCodeContentStore{
			fakePortContentStore: fakePortContentStore{
				repositories: []RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
			},
			entities: map[string]EntityContent{
				"producer-late": {
					EntityID:     "producer-late",
					RepoID:       "repo-producer",
					RelativePath: "pkg/payments/late.go",
					EntityType:   "Function",
					EntityName:   "maybeLive",
					Language:     "go",
					SourceCache:  "func maybeLive() {}",
				},
			},
		},
		rows: []map[string]any{
			deadCodeInvestigationRow("producer-late", "maybeLive", "go", "pkg/payments/late.go", 8, 12),
		},
		evidenceByEntity: map[string][]crossRepoDeadCodeEvidence{
			"producer-late": {
				crossRepoDeadCodeGrantConsumerRow(codeGrantConsumerRepo, "producer-late"),
				truncatedCrossRepoDeadCodeEvidence(),
			},
		},
	}
	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Content: content, Neo4j: fakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code/cross-repo",
		bytes.NewBufferString(`{"repo_id":"repo-producer","limit":10}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	buckets := decodeEnvelopeData(t, rec.Body.Bytes())["candidate_buckets"].(map[string]any)
	unknown := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", "producer-late")
	assertCrossRepoDeadCodeReason(t, unknown, "consumer_evidence_truncated")
	assertCrossRepoDeadCodeBucketMissing(t, buckets, "live_by_consumer", "producer-late")
}

func crossRepoDeadCodeTruncationMarked(rows []crossRepoDeadCodeEvidence) bool {
	for _, row := range rows {
		if row.NeedsEvidence && row.Reason == "consumer_evidence_truncated" {
			return true
		}
	}
	return false
}
