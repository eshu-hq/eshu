// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestDeadCodeIncomingEntityIDsActiveRunBoundLive proves which completed
// incoming edges DeadCodeIncomingEntityIDs counts (#7249).
//
// The read binds to the repository's active acceptance run only when that run
// provably holds the complete edge set: the active generation is a full (not
// delta) generation with an acceptance row, both reducer materialization work
// items (code calls and inheritance) succeeded with none still queued, and no
// code_calls or inheritance_edges intent of the generation is pending. Bound,
// an edge kept only by a superseded generation no longer keeps its target
// alive -- the same key the reducer's reachability loader reads.
//
// Every other repository must get today's unbound answer unchanged: a delta
// generation carries only the changed files, and a run still projecting (or
// reset by a rebuild) carries only part of its edges, so binding there would
// call live symbols dead. The fallback expectation is computed by running the
// shipped deadCodeIncomingUnboundQuery, not a copy of it.
//
// Run with a disposable PostgreSQL 18 administrative database:
//
//	ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DSN=postgres://user:pass@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DISPOSABLE=1 \
//	go test ./internal/query -run TestDeadCodeIncomingEntityIDsActiveRunBoundLive -count=1
func TestDeadCodeIncomingEntityIDsActiveRunBoundLive(t *testing.T) {
	dsn := os.Getenv("ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DSN")
	optIn := os.Getenv("ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	t.Run("complete full active run binds: stale-only callee is not incoming", func(t *testing.T) {
		f := seedDeadCodeIncomingBoundRepo(ctx, t, db, deadCodeIncomingBoundFixture{name: "complete"})
		got, mode := readDeadCodeIncomingBoundLive(ctx, t, db, f)
		want := deadCodeIncomingSortedStrings([]string{f.entity("live"), f.entity("meta"), f.entity("parent")})
		if keys := deadCodeIncomingSortedKeys(got); !slices.Equal(keys, want) {
			t.Fatalf("incoming = %v, want %v (stale-only callee %s must not count)", keys, want, f.entity("stale"))
		}
		if mode != "active_run" {
			t.Fatalf("read_mode = %q, want active_run", mode)
		}
	})

	t.Run("complete run with no matching edge binds to an empty answer, not the fallback", func(t *testing.T) {
		f := seedDeadCodeIncomingBoundRepo(ctx, t, db, deadCodeIncomingBoundFixture{name: "emptyrun", activeEdgesUnrelated: true})
		got, mode := readDeadCodeIncomingBoundLive(ctx, t, db, f)
		if len(got) != 0 {
			t.Fatalf("incoming = %v, want empty: a provably complete run with no edge to a candidate is the answer",
				deadCodeIncomingSortedKeys(got))
		}
		if mode != "active_run" {
			t.Fatalf("read_mode = %q, want active_run", mode)
		}
	})

	t.Run("bound read counts only acceptance-paired (run, generation) rows, not a cross-pair", func(t *testing.T) {
		f := seedDeadCodeIncomingBoundRepo(ctx, t, db, deadCodeIncomingBoundFixture{name: "crosspair", secondScopeCrossPair: true})
		got, mode := readDeadCodeIncomingBoundLive(ctx, t, db, f)
		want := deadCodeIncomingSortedStrings([]string{f.entity("live"), f.entity("meta"), f.entity("parent")})
		if keys := deadCodeIncomingSortedKeys(got); !slices.Equal(keys, want) {
			t.Fatalf("incoming = %v, want %v (cross-pair callee %s is stamped with one scope's run and the "+
				"other scope's generation; no acceptance row pairs them)", keys, want, f.entity("cross"))
		}
		if mode != "active_run" {
			t.Fatalf("read_mode = %q, want active_run", mode)
		}
	})

	fallbacks := []deadCodeIncomingBoundFixture{
		{name: "delta", activeIsDelta: true, activeRunHasNoEdges: true},
		{name: "pending", activeLiveCallPending: true},
		{name: "noinherit", omitWorkItem: string(reducercontract.DomainInheritanceMaterialization)},
		{name: "nocodecall", omitWorkItem: string(reducercontract.DomainCodeCallMaterialization)},
		{name: "replayqueued", queuedReplay: true},
		{name: "rebuildreset", rebuildReset: true},
		{name: "noacceptance", noActiveAcceptance: true},
		// The gate holds over EVERY active (scope, generation) of the
		// repository: one complete scope cannot vouch for an incomplete one.
		{name: "twoscope", secondIncompleteScope: true},
	}
	for _, fixture := range fallbacks {
		t.Run("unprovable active run falls back to the unbound read: "+fixture.name, func(t *testing.T) {
			f := seedDeadCodeIncomingBoundRepo(ctx, t, db, fixture)
			got, mode := readDeadCodeIncomingBoundLive(ctx, t, db, f)
			if mode != "all_generations" {
				t.Fatalf("read_mode = %q, want all_generations", mode)
			}
			want := runShippedDeadCodeIncomingUnbound(ctx, t, db, f)
			if !maps.Equal(got, want) {
				t.Fatalf("incoming = %v, want the unbound answer %v", got, want)
			}
			if _, ok := got[f.entity("stale")]; !ok {
				t.Fatalf("incoming = %v, want stale-generation callee %s kept by the unbound fallback",
					deadCodeIncomingSortedKeys(got), f.entity("stale"))
			}
		})
	}
}

// deadCodeIncomingBoundFixture describes one repository: a superseded generation that
// carries the stale and live callers, and an active generation whose
// completeness the fields below weaken.
type deadCodeIncomingBoundFixture struct {
	name                  string
	activeIsDelta         bool
	activeRunHasNoEdges   bool
	activeLiveCallPending bool
	omitWorkItem          string
	queuedReplay          bool
	rebuildReset          bool
	noActiveAcceptance    bool
	activeEdgesUnrelated  bool
	secondIncompleteScope bool
	secondScopeCrossPair  bool
}

func (f deadCodeIncomingBoundFixture) repo() string  { return "repository:p7249-" + f.name }
func (f deadCodeIncomingBoundFixture) scope() string { return "scope:p7249-" + f.name }
func (f deadCodeIncomingBoundFixture) generation(g string) string {
	return "generation:p7249-" + f.name + "-" + g
}

func (f deadCodeIncomingBoundFixture) run(g string) string { return "run:p7249-" + f.name + "-" + g }

func (f deadCodeIncomingBoundFixture) entity(suffix string) string {
	return "content-entity:p7249-" + f.name + "-" + suffix
}

func (f deadCodeIncomingBoundFixture) candidates() []string {
	return []string{
		f.entity("live"), f.entity("stale"), f.entity("meta"), f.entity("parent"), f.entity("dead"), f.entity("cross"),
	}
}

func seedDeadCodeIncomingBoundRepo(ctx context.Context, t *testing.T, db *sql.DB, f deadCodeIncomingBoundFixture) deadCodeIncomingBoundFixture {
	t.Helper()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v\n%s", f.name, err, query)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
		partition_key, observed_at, ingested_at, status, active_generation_id)
		VALUES ($1, 'repository', 'git', $2, 'git', $2, $3, $3, 'active', NULL)`, f.scope(), f.repo(), now)
	exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'snapshot', false, $3, $3, 'superseded', $3),
		       ($4, $2, 'snapshot', $5, $6, $6, 'active', $6)`,
		f.generation("old"), f.scope(), now.Add(-time.Hour), f.generation("new"), f.activeIsDelta, now)
	exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, f.generation("new"), f.scope())

	acceptance := `INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id,
		accepted_at, updated_at, generation_ingested_at) VALUES ($1, $2, $3, $4, $5, $5, $5)`
	exec(acceptance, f.scope(), f.repo(), f.run("old"), f.generation("old"), now.Add(-time.Hour))
	if !f.noActiveAcceptance {
		exec(acceptance, f.scope(), f.repo(), f.run("new"), f.generation("new"), now)
	}

	n := 0
	intent := func(gen, domain, payload string, completed bool) {
		t.Helper()
		n++
		var completedAt any
		if completed {
			completedAt = now
		}
		exec(`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
			acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
			VALUES ($1, $2, $1, $3, $4, $4, $5, $6, $7::jsonb, $8, $9)`,
			fmt.Sprintf("intent:p7249-%s-%d", f.name, n), domain, f.scope(), f.repo(), f.run(gen), f.generation(gen),
			payload, now, completedAt)
	}
	// The superseded generation called both live and stale; the active one
	// re-parsed the caller file and calls only live.
	intent("old", "code_calls", fmt.Sprintf(`{"callee_entity_id":%q,"resolution_method":"scip"}`, f.entity("live")), true)
	intent("old", "code_calls", fmt.Sprintf(`{"callee_entity_id":%q,"resolution_method":"scip"}`, f.entity("stale")), true)
	switch {
	case f.activeEdgesUnrelated:
		// The active run is complete but none of its edges reach a candidate.
		intent("new", "code_calls", fmt.Sprintf(`{"callee_entity_id":%q,"resolution_method":"scip"}`, f.entity("other")), true)
	case !f.activeRunHasNoEdges:
		activeComplete := !f.rebuildReset
		intent("new", "code_calls", fmt.Sprintf(`{"callee_entity_id":%q,"resolution_method":"scip"}`, f.entity("live")),
			activeComplete && !f.activeLiveCallPending)
		intent("new", "code_calls", fmt.Sprintf(
			`{"target_entity_id":%q,"relationship_type":"USES_METACLASS","resolution_method":"declared"}`,
			f.entity("meta")), activeComplete)
		intent("new", "inheritance_edges", fmt.Sprintf(`{"parent_entity_id":%q,"resolution_method":"declared"}`,
			f.entity("parent")), activeComplete)
	}

	// A rebuild reset deletes the succeeded reducer work items and reopens the
	// generation's intents, so it seeds neither.
	if !f.rebuildReset {
		for _, domain := range []string{
			string(reducercontract.DomainCodeCallMaterialization),
			string(reducercontract.DomainInheritanceMaterialization),
		} {
			if domain == f.omitWorkItem {
				continue
			}
			seedDeadCodeIncomingBoundWorkItem(ctx, t, db, f, domain, "succeeded", now)
		}
		if f.queuedReplay {
			seedDeadCodeIncomingBoundWorkItem(ctx, t, db, f, string(reducercontract.DomainCodeCallMaterialization), "pending",
				now.Add(time.Minute))
		}
	}
	if f.secondScopeCrossPair {
		// A second scope whose active full generation is complete and also
		// accepts this repository, plus one completed intent stamped with the
		// FIRST scope's active run id but the SECOND scope's generation. Both
		// ids are active, but no acceptance row pairs them.
		second := f
		second.name = f.name + "-b"
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
			partition_key, observed_at, ingested_at, status, active_generation_id)
			VALUES ($1, 'repository', 'git', $2, 'git', $1, $3, $3, 'active', NULL)`, second.scope(), f.repo(), now)
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'snapshot', false, $3, $3, 'active', $3)`, second.generation("new"), second.scope(), now)
		exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, second.generation("new"), second.scope())
		exec(acceptance, second.scope(), f.repo(), second.run("new"), second.generation("new"), now)
		for _, domain := range []string{
			string(reducercontract.DomainCodeCallMaterialization),
			string(reducercontract.DomainInheritanceMaterialization),
		} {
			seedDeadCodeIncomingBoundWorkItem(ctx, t, db, second, domain, "succeeded", now)
		}
		exec(`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
			acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
			VALUES ($1, 'code_calls', $1, $2, $3, $3, $4, $5, $6::jsonb, $7, $7)`,
			"intent:p7249-"+f.name+"-crosspair", second.scope(), f.repo(), f.run("new"), second.generation("new"),
			fmt.Sprintf(`{"callee_entity_id":%q,"resolution_method":"scip"}`, f.entity("cross")), now)
	}
	if f.secondIncompleteScope {
		// A second scope whose active full generation also accepts this
		// repository, but whose inheritance materialization never succeeded.
		second := f
		second.name = f.name + "-b"
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
			partition_key, observed_at, ingested_at, status, active_generation_id)
			VALUES ($1, 'repository', 'git', $2, 'git', $1, $3, $3, 'active', NULL)`, second.scope(), f.repo(), now)
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at, status, activated_at)
			VALUES ($1, $2, 'snapshot', false, $3, $3, 'active', $3)`, second.generation("new"), second.scope(), now)
		exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, second.generation("new"), second.scope())
		exec(acceptance, second.scope(), f.repo(), second.run("new"), second.generation("new"), now)
		seedDeadCodeIncomingBoundWorkItem(ctx, t, db, second, string(reducercontract.DomainCodeCallMaterialization), "succeeded", now)
	}
	return f
}

func seedDeadCodeIncomingBoundWorkItem(
	ctx context.Context, t *testing.T, db *sql.DB, f deadCodeIncomingBoundFixture, domain, status string, at time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
		status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		fmt.Sprintf("work:p7249-%s-%s-%s", f.name, domain, status), f.scope(), f.generation("new"),
		string(recovery.StageReducer), domain, status, at,
	); err != nil {
		t.Fatalf("seed %s work item %s/%s: %v", f.name, domain, status, err)
	}
}

// readDeadCodeIncomingBoundLive runs the production read and returns its answer plus the
// dead_code_incoming.read_mode the span recorded.
func readDeadCodeIncomingBoundLive(
	ctx context.Context, t *testing.T, db *sql.DB, f deadCodeIncomingBoundFixture,
) (map[string]code.DeadCodeIncomingEdge, string) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := NewContentReader(db)
	reader.tracer = provider.Tracer("dead-code-incoming-bound-live-test")
	got, err := reader.DeadCodeIncomingEntityIDs(ctx, f.repo(), f.candidates())
	if err != nil {
		t.Fatalf("DeadCodeIncomingEntityIDs(%s) error = %v", f.name, err)
	}
	mode := ""
	for _, span := range recorder.Ended() {
		for _, attr := range span.Attributes() {
			if attr.Key == "dead_code_incoming.read_mode" {
				mode = attr.Value.AsString()
			}
		}
	}
	return got, mode
}

// runShippedDeadCodeIncomingUnbound executes the shipped unbound statement directly so
// the fallback expectation cannot drift from what production runs.
func runShippedDeadCodeIncomingUnbound(
	ctx context.Context, t *testing.T, db *sql.DB, f deadCodeIncomingBoundFixture,
) map[string]code.DeadCodeIncomingEdge {
	t.Helper()
	candidates := f.candidates()
	args := []any{f.repo()}
	placeholders := make([]string, 0, len(candidates))
	for i, id := range candidates {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+2))
	}
	rows, err := db.QueryContext(ctx, deadCodeIncomingUnboundQuery(strings.Join(placeholders, ", ")), args...)
	if err != nil {
		t.Fatalf("run shipped unbound statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	want := map[string]code.DeadCodeIncomingEdge{}
	for rows.Next() {
		var id string
		var method sql.NullString
		if err := rows.Scan(&id, &method); err != nil {
			t.Fatalf("scan unbound row: %v", err)
		}
		mergeDeadCodeIncomingEdge(want, id, method.String)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("unbound rows: %v", err)
	}
	return want
}

func deadCodeIncomingSortedKeys(m map[string]code.DeadCodeIncomingEdge) []string {
	return slices.Sorted(maps.Keys(m))
}

func deadCodeIncomingSortedStrings(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	return out
}
