// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	reachabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
)

// coverageColumns is what both coverage statements return per gap: the
// repository, why it is a gap, and the generation being waited for.
var coverageColumns = []string{"repository_id", "state", "generation_id"}

// coverageRows builds one no_snapshot_yet gap per id, for tests that care which
// repositories are gaps and not why.
func coverageRows(ids ...string) [][]driver.Value {
	rows := make([][]driver.Value, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []driver.Value{id, code.CrossRepoDeadCodeCoverageStateNoSnapshotYet, "gen-" + id})
	}
	return rows
}

// The named-consumer statement is one statement per request, bound to the
// request's own list, and reports the incomplete repositories sorted.
func TestCrossRepoDeadCodeConsumerCoverageNamedRequest(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: coverageColumns, rows: coverageRows("repo-b", "repo-a")},
	})
	reader := NewContentReader(db)
	got, err := reader.CrossRepoDeadCodeConsumerCoverage(context.Background(), code.CrossRepoDeadCodeCoverageRequest{
		RepositoryIDs:      []string{"repo-a", "repo-b", "repo-c"},
		RequireActiveScope: true,
	})
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerCoverage() error = %v, want nil", err)
	}
	if want := []string{"repo-a", "repo-b"}; !slices.Equal(got.IncompleteRepositoryIDs(), want) || got.IncompleteTruncated {
		t.Fatalf("coverage = %#v, want sorted incomplete %v, not truncated", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d: coverage is one statement per request", got, want)
	}
	if recorder.queries[0] != deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery {
		t.Fatalf("statement is not the named-consumer coverage query:\n%s", recorder.queries[0])
	}
	args := recorder.args[0]
	if len(args) != 4 {
		t.Fatalf("args = %v, want ids, require-active-scope, epoch, limit", args)
	}
	if !strings.Contains(fmt.Sprint(args[0]), "repo-c") {
		t.Fatalf("ids argument = %v, want the encoded array carrying every requested repository", args[0])
	}
	if args[1] != true {
		t.Fatalf("require-active-scope argument = %v, want true", args[1])
	}
	// The epoch is the writer's own constant, never a copy: a literal here would
	// drift from the epoch the reachability writer stamps.
	if args[2] != int64(reachabilitystore.CodeReachabilityVerdictSchemaEpoch) {
		t.Fatalf("epoch argument = %v, want reachabilitystore.CodeReachabilityVerdictSchemaEpoch (%d)",
			args[2], reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
	}
	if args[3] != int64(deadcode.CrossRepoDeadCodeCoverageGapCap+1) {
		t.Fatalf("limit argument = %v, want the cap plus one sentinel row", args[3])
	}
}

// An unscoped, unnamed request checks every repository scope with one
// statement whose only parameter is the limit.
func TestCrossRepoDeadCodeConsumerCoverageAllRepositories(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: coverageColumns, rows: coverageRows("repo-z", "repo-z", "repo-a")},
	})
	reader := NewContentReader(db)
	got, err := reader.CrossRepoDeadCodeConsumerCoverage(context.Background(), code.CrossRepoDeadCodeCoverageRequest{
		AllRepositories: true,
	})
	if err != nil {
		t.Fatalf("CrossRepoDeadCodeConsumerCoverage() error = %v, want nil", err)
	}
	// A repository covered by two scopes can be reported twice; it is one gap.
	if want := []string{"repo-a", "repo-z"}; !slices.Equal(got.IncompleteRepositoryIDs(), want) {
		t.Fatalf("incomplete = %v, want %v", got.IncompleteRepositoryIDs(), want)
	}
	if recorder.queries[0] != deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery {
		t.Fatalf("statement is not the all-repositories coverage query:\n%s", recorder.queries[0])
	}
	if len(recorder.args[0]) != 2 {
		t.Fatalf("args = %v, want the epoch and the limit", recorder.args[0])
	}
	if recorder.args[0][0] != int64(reachabilitystore.CodeReachabilityVerdictSchemaEpoch) {
		t.Fatalf("epoch argument = %v, want reachabilitystore.CodeReachabilityVerdictSchemaEpoch (%d)",
			recorder.args[0][0], reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
	}
}

// A fully covered corpus returns no rows and is complete.
func TestCrossRepoDeadCodeConsumerCoverageCompleteWhenNoGaps(t *testing.T) {
	t.Parallel()

	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: coverageColumns},
	})
	got, err := NewContentReader(db).CrossRepoDeadCodeConsumerCoverage(context.Background(), code.CrossRepoDeadCodeCoverageRequest{
		AllRepositories: true,
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if !got.Complete() {
		t.Fatalf("coverage = %#v, want complete", got)
	}
}

// More gaps than the cap are cut and flagged, never silently dropped: the
// answer stays incomplete however many repositories are missing.
func TestCrossRepoDeadCodeConsumerCoverageCapsTheGapList(t *testing.T) {
	t.Parallel()

	ids := make([]string, 0, deadcode.CrossRepoDeadCodeCoverageGapCap+1)
	for i := 0; i <= deadcode.CrossRepoDeadCodeCoverageGapCap; i++ {
		ids = append(ids, fmt.Sprintf("repo-%03d", i))
	}
	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: coverageColumns, rows: coverageRows(ids...)},
	})
	got, err := NewContentReader(db).CrossRepoDeadCodeConsumerCoverage(context.Background(), code.CrossRepoDeadCodeCoverageRequest{
		AllRepositories: true,
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if len(got.Gaps) != deadcode.CrossRepoDeadCodeCoverageGapCap || !got.IncompleteTruncated || got.Complete() {
		t.Fatalf("coverage = %d ids truncated=%v, want the cap and truncated", len(got.Gaps), got.IncompleteTruncated)
	}
}

// A request that names nothing and is not "every repository" must not be read
// as covered.
func TestCrossRepoDeadCodeConsumerCoverageRefusesAnEmptyRequest(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, nil)
	_, err := NewContentReader(db).CrossRepoDeadCodeConsumerCoverage(context.Background(), code.CrossRepoDeadCodeCoverageRequest{})
	if err == nil {
		t.Fatalf("error = nil, want a refusal: an empty request would answer covered for nothing")
	}
	if len(recorder.queries) != 0 {
		t.Fatalf("query count = %d, want 0", len(recorder.queries))
	}
}

func TestCrossRepoDeadCodeConsumerCoveragePropagatesDatabaseErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	db, _ := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{{err: boom}})
	_, err := NewContentReader(db).CrossRepoDeadCodeConsumerCoverage(context.Background(), code.CrossRepoDeadCodeCoverageRequest{
		AllRepositories: true,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap the database error", err)
	}
}

// The coverage statements' shape is the performance contract: one indexed
// probe of the watermark primary key per active repository scope, never a read
// of code_reachability_rows (whose size is the corpus, not the repository
// count) and never a parameter that grows with the candidate page.
func TestCrossRepoDeadCodeConsumerCoverageStatementShape(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"named": deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery,
		"all":   deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, want := range []string{
				"code_reachability_repository_watermarks AS watermark",
				"watermark.scope_id = scope.scope_id",
				"watermark.generation_id = scope.active_generation_id",
				"watermark.repository_id = ",
				"scope.scope_kind = 'repository'",
				"generation.status = 'active'",
				"watermark.truncated",
			} {
				if !strings.Contains(query, want) {
					t.Errorf("%s coverage SQL is missing %q", name, want)
				}
			}
			if strings.Contains(query, "code_reachability_rows") {
				t.Errorf("%s coverage SQL reads code_reachability_rows; coverage is a watermark question", name)
			}
			if !regexp.MustCompile(`LIMIT \$\d`).MatchString(query) {
				t.Errorf("%s coverage SQL has no bound LIMIT", name)
			}
		})
	}
	// The probe joins the full primary key (scope, generation, repository), so
	// it is an index lookup rather than a scan of the watermark table.
	if !strings.Contains(deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery, "watermark.repository_id = scope.source_key") {
		t.Errorf("all-repositories coverage SQL does not probe the watermark's full primary key")
	}
	// The named statement finds the listed repositories' scopes in one pass with
	// a hashed ANY test. Joining the list to the scopes row by row planned as a
	// nested loop over list x scopes (520 ms for 3,000 ids on a 3,010-scope
	// fixture), which this shape must never return to.
	named := deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery
	for _, want := range []string{"scope.source_key = ANY($1::text[])", "AS MATERIALIZED", "NOT IN (SELECT source_key FROM matched)"} {
		if !strings.Contains(named, want) {
			t.Errorf("named coverage SQL is missing %q", want)
		}
	}
	if strings.Contains(named, "JOIN ingestion_scopes AS scope\n  ON scope.scope_kind = 'repository'\n AND scope.source_key = requested") ||
		strings.Contains(named, "= requested.") {
		t.Errorf("named coverage SQL joins the requested list to the scopes row by row")
	}
	// The placeholders are exactly the ones each statement binds.
	for name, want := range map[string][]string{
		"named": {"$1", "$2", "$3", "$4"},
		"all":   {"$1", "$2"},
	} {
		query := deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery
		if name == "all" {
			query = deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery
		}
		seen := map[string]bool{}
		for _, placeholder := range regexp.MustCompile(`\$\d+`).FindAllString(query, -1) {
			if !slices.Contains(want, placeholder) {
				t.Errorf("%s coverage SQL has placeholder %s, want only %v", name, placeholder, want)
			}
			seen[placeholder] = true
		}
		for _, placeholder := range want {
			if !seen[placeholder] {
				t.Errorf("%s coverage SQL never uses %s", name, placeholder)
			}
		}
	}
	// A watermark below the current verdict epoch is a gap like a missing or
	// truncated one (#7547): the writer bumps the epoch when verdict semantics
	// change, and a snapshot built earlier carries the old semantics. The test
	// is inside the same CASE, so the intent probe still gates it.
	for name, want := range map[string]string{
		"named": "watermark.verdict_schema_epoch < $3::integer",
		"all":   "watermark.verdict_schema_epoch < $1::integer",
	} {
		query := deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery
		if name == "all" {
			query = deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery
		}
		// The state CASE in the select list also starts "CASE WHEN"; the gating
		// CASE is the one right before the intent probe.
		then := strings.Index(query, "THEN (COALESCE((SELECT true")
		when := strings.LastIndex(query[:max(then, 0)], "CASE WHEN")
		if when < 0 || then < when || !strings.Contains(query[when:then], want) {
			t.Errorf("%s coverage SQL does not test %q inside the CASE that gates the intent probe", name, want)
		}
	}
}

// coverageIntentProbe returns the scalar intent probe a coverage statement
// carries, with the repository expression it binds replaced by <repo>, the
// generation's is_delta column replaced by <delta> (the named statement reads
// it through its matched CTE, the all-repositories statement from the
// generation join), and the whitespace collapsed, so the two statements'
// probes can be compared.
func coverageIntentProbe(t *testing.T, query, repoExpr, deltaExpr string) string {
	t.Helper()

	start := strings.Index(query, "(SELECT true")
	end := strings.Index(query, "LIMIT 1)")
	if start < 0 || end < start {
		t.Fatalf("coverage SQL has no scalar intent probe ending in LIMIT 1):\n%s", query)
	}
	probe := query[start : end+len("LIMIT 1)")]
	probe = strings.ReplaceAll(strings.ReplaceAll(probe, repoExpr, "<repo>"), deltaExpr, "<delta>")
	return strings.Join(strings.Fields(probe), " ")
}

// coverageWorkProbe returns the reducer-work probe a coverage statement ORs
// into the gating CASE (#7602), with the whitespace collapsed, so the two
// statements' probes can be compared. Both probes bind the same scope columns,
// so no expression needs normalizing.
func coverageWorkProbe(t *testing.T, query string) string {
	t.Helper()

	start := strings.Index(query, "FROM fact_work_items AS work")
	if start < 0 {
		t.Fatalf("coverage SQL has no reducer-work probe:\n%s", query)
	}
	end := strings.Index(query[start:], "LIMIT 1), false)")
	if end < 0 {
		t.Fatalf("coverage SQL reducer-work probe has no LIMIT 1) end:\n%s", query)
	}
	return strings.Join(strings.Fields(query[start:start+end+len("LIMIT 1), false)")]), " ")
}

// A missing or truncated watermark is a gap only for a repository that CAN be a
// consumer: its active generation has a code_calls or inheritance_edges intent,
// completed or pending (#7547). Both statements apply the same predicate, as a
// correlated scalar probe behind the watermark test -- written as EXISTS the
// planner hoists it into a hashed subplan over every such intent in the
// database. Run against PostgreSQL the predicate answers: a repository with no
// intent is complete, a zero-root repository with intents and a truncated
// watermark is a gap, and a pending-only repository with no watermark is a gap
// (TestCrossRepoDeadCodeConsumerCoverageLive). A refresh intent counts only on a
// delta generation (TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive).
// Between activation and the first per-edge intent the intent probe reads the
// repository as complete, so a second correlated probe ORs in the reducer work
// still outstanding for the active generation: any non-succeeded reducer item
// in the code_call_materialization or inheritance_materialization domain keeps
// the gap, including a dead-lettered one (#7602).
func TestCrossRepoDeadCodeConsumerCoverageUniversePredicate(t *testing.T) {
	t.Parallel()

	named := coverageIntentProbe(t, deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery, "scope.source_key", "scope.is_delta")
	all := coverageIntentProbe(t, deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery, "scope.source_key", "generation.is_delta")
	if named != all {
		t.Fatalf("the two statements disagree about which repositories can be consumers:\nnamed: %s\nall:   %s", named, all)
	}
	namedWork := coverageWorkProbe(t, deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery)
	allWork := coverageWorkProbe(t, deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery)
	if namedWork != allWork {
		t.Fatalf("the two statements disagree about which reducer work keeps a gap:\nnamed: %s\nall:   %s", namedWork, allWork)
	}
	for _, want := range []string{
		"work.scope_id = scope.scope_id",
		"work.generation_id = scope.active_generation_id",
		"work.stage = 'reducer'",
		"work.domain IN ('code_call_materialization', 'inheritance_materialization')",
		"work.status <> 'succeeded'",
	} {
		if !strings.Contains(namedWork, want) {
			t.Errorf("work probe is missing %q:\n%s", want, namedWork)
		}
	}
	for _, want := range []string{
		"FROM shared_projection_acceptance AS acceptance",
		"JOIN shared_projection_intents AS intent",
		"intent.projection_domain IN ('code_calls', 'inheritance_edges')",
		"intent.generation_id = acceptance.generation_id",
		"acceptance.generation_id = scope.active_generation_id",
		"acceptance.acceptance_unit_id = <repo>",
		// A refresh intent has no edge and the loader never reads it, so on a
		// full generation it cannot make a repository a consumer; on a delta
		// generation no watermark is ever written, so any intent keeps the gap
		// (#7591). A bare "NOT intent.is_refresh_intent" would hide that gap.
		"AND (<delta> OR NOT intent.is_refresh_intent)",
	} {
		if !strings.Contains(named, want) {
			t.Errorf("intent probe is missing %q:\n%s", want, named)
		}
	}
	// The predicate must not filter on completion: a pending-only repository
	// with no watermark is a gap, because its edges are not drained yet.
	if strings.Contains(named, "completed_at") {
		t.Errorf("intent probe filters on completed_at; a pending-only repository must still count:\n%s", named)
	}
	for name, query := range map[string]string{
		"named": deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery,
		"all":   deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery,
	} {
		if strings.Contains(query, "EXISTS") {
			t.Errorf("%s coverage SQL uses EXISTS, which the planner hoists out of the per-scope probe", name)
		}
		if !strings.Contains(query, "COALESCE((SELECT true") || !strings.Contains(query, "ELSE false END") {
			t.Errorf("%s coverage SQL does not gate the intent probe behind the watermark test", name)
		}
	}
}
