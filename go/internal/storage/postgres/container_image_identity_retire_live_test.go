// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Real-Postgres proofs for the #5847 container_image_identity retire.
//
// The retire is a DELETE whose only bound is its WHERE clause, and the
// in-process tests around it can only inspect the arguments handed to a fake
// execer — they never run the statement, so they cannot tell a bounded delete
// from an unbounded one, nor a fenced one from an unfenced one. These tests
// execute the real statement against real rows in the real bootstrap schema and
// count what is left, which is the only assertion that distinguishes those.
//
// They live in package postgres rather than package reducer because the fact
// row needs the real fact_records table with its ingestion_scopes and
// scope_generations foreign keys, its fencing_token column, and its indexes —
// which ApplyBootstrap owns. internal/storage/postgres already imports
// internal/reducer, so this direction is the one that compiles; a live test
// inside package reducer would have to hand-roll a four-column stand-in table
// that shares neither the columns the fence reads nor the constraints
// production rows are subject to.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ContainerImageIdentityRetire.*Live -count=1

const containerImageIdentityLiveFactKind = "reducer_container_image_identity"

// containerImageIdentityRetireTrigger is the fragment that identifies the retire
// statement inside an execer wrapper. It matches the statement's only DELETE,
// which is what the retire IS, rather than a preamble that a future rewrite
// could drop — an interleaving hook keyed on a vanished fragment never fires and
// silently proves nothing.
const containerImageIdentityRetireTrigger = "DELETE FROM fact_records"

// containerImageIdentityInterleavingExecer runs a hook the first time the
// wrapped writer issues a statement matching trigger, BEFORE that statement
// reaches the database.
//
// It exists to make a two-worker interleaving deterministic against real
// Postgres using the real production statements: the hook fires between worker
// B's INSERT and worker B's retire, which is exactly the window a concurrent
// stalled worker can land in — and, for the row-version proof, the only moment
// at which the freshly inserted row can be observed before the retire touches
// it.
type containerImageIdentityInterleavingExecer struct {
	db      *sql.DB
	trigger string
	hook    func()
	fired   bool
}

// ExecContext fires the hook once, before the first statement containing the
// trigger fragment reaches the database, then forwards every statement
// unchanged.
func (e *containerImageIdentityInterleavingExecer) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	if !e.fired && e.hook != nil && strings.Contains(query, e.trigger) {
		e.fired = true
		e.hook()
	}
	return e.db.ExecContext(ctx, query, args...)
}

// containerImageIdentityRetireLiveDB opens the DSN-gated database and applies
// the bootstrap schema, skipping the whole test when no DSN is configured.
//
// The gate is ESHU_POSTGRES_DSN alone, the same one every other live test in
// this package uses, so a developer or CI lane that already exports a DSN runs
// this proof without knowing a bespoke per-test flag exists.
//
// Schema setup and the proof itself get SEPARATE deadlines, and the returned
// context's 90s budget starts AFTER ApplyBootstrap returns. They were one budget
// before, which let one-time DDL spend the proof's time. On a fresh schema with
// this host saturated — a load average near 200, from CPU load generators an
// earlier session leaked and never reaped —
// TestContainerImageIdentityRetireCannotDeleteFresherEvidenceRowsLive
// failed at ~90.0s inside ApplyBootstrap, at a different bootstrap step each
// time (webhook_refresh_triggers, then service_evidence_snapshots), while warm
// re-runs against the same database passed.
//
// Cold DDL is not itself expensive, so the split is not about reclaiming time:
// against fresh containers holding 0 tables on that same host once the leaked
// load was reaped, ApplyBootstrap builds all 178 tables in ~0.85-1.0s, and the
// whole test under the old single budget passes cold in ~0.87s. The budget was
// never close to short — a first-time runner on an idle machine passes, and
// only a first-time runner on a heavily loaded one sees that red.
//
// The split is about budget COUPLING, not about naming the failing phase: that
// red was already labelled "apply bootstrap schema", by this same t.Fatalf,
// before any split existed. What one shared deadline hides is a bootstrap that
// is slow but FINITE — the old context was created BEFORE ApplyBootstrap ran, so
// setup spending 30s left the proof 60s, and a retire that then timed out would
// surface inside the retire with nothing in the retire being slow and no
// bootstrap string to correct the reading. Starting the proof's clock after
// ApplyBootstrap returns removes that coupling; 90s remains the budget for what
// this test actually measures, so a genuine hang in the retire path still trips
// it. ApplyBootstrap gets its own, larger allowance, amortized across every test
// in this file: 5 minutes is headroom, not a derived size — its duration under
// that near-200 load average was never measured.
func containerImageIdentityRetireLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()

	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres container_image_identity retire proof")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 5*time.Minute)
	err = ApplyBootstrap(schemaCtx, SQLDB{DB: sqlDB})
	cancelSchema()
	if err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return sqlDB, ctx
}

// seedContainerImageIdentityScopeGeneration creates the scope and generation a
// fact row's foreign keys require. Both are per-test unique, so concurrent runs
// of this package against one database do not collide.
func seedContainerImageIdentityScopeGeneration(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID, generationID string,
) {
	t.Helper()

	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text, '{}'::jsonb)
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now, generationID,
	); err != nil {
		t.Fatalf("seed ingestion_scopes %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		generationID, scopeID, now,
	); err != nil {
		t.Fatalf("seed scope_generations %s: %v", generationID, err)
	}
}

// seedContainerImageIdentityFactRow inserts one bare fact row directly,
// bypassing the writer, so a test can plant decoys the retire must not touch.
func seedContainerImageIdentityFactRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	factID, scopeID, generationID, factKind string,
	fencingToken int64,
) {
	t.Helper()

	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO fact_records
		  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
		   collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
		   observed_at, ingested_at, is_tombstone, payload)
		VALUES ($1, $2, $3, $4, $1, '1.0.0', 'git', $5, 'inferred', 'git', $1, $6, $6, FALSE, '{}'::jsonb)`,
		factID, scopeID, generationID, factKind, fencingToken, now,
	); err != nil {
		t.Fatalf("seed fact row %s: %v", factID, err)
	}
}

// containerImageIdentitySurvivingFactIDs returns the fact ids from the given set
// that are still present, sorted, so a failure message names exactly what went
// missing.
func containerImageIdentitySurvivingFactIDs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	candidates []string,
) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx,
		`SELECT fact_id FROM fact_records WHERE fact_id = ANY($1::text[])`, candidates)
	if err != nil {
		t.Fatalf("query surviving fact rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var survivors []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan surviving fact row: %v", err)
		}
		survivors = append(survivors, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate surviving fact rows: %v", err)
	}
	sort.Strings(survivors)
	return survivors
}

// containerImageIdentityLiveWrite builds a fenced one-decision write for the
// given outcome. exact_digest and tag_resolved are the only two outcomes that
// set CanonicalWrites=1, and they produce DIFFERENT fact ids for the same image
// reference because the identity embeds the outcome — which is what makes a
// re-classification a delete rather than an upsert.
func containerImageIdentityLiveWrite(
	scopeID, generationID, suffix string,
	evidenceAsOf time.Time,
	outcome reducer.ContainerImageIdentityOutcome,
) reducer.ContainerImageIdentityWrite {
	return reducer.ContainerImageIdentityWrite{
		IntentID:     "intent-" + suffix,
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "git",
		Cause:        "live retire proof",
		EvidenceAsOf: evidenceAsOf,
		Decisions: []reducer.ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           "sha256:" + "ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12cd34ef56ab12",
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          outcome,
				Reason:           "live proof decision",
				CanonicalWrites:  1,
				IdentityStrength: "tag_observation_with_digest",
			},
		},
	}
}

// TestContainerImageIdentityRetireBoundedToOwnPartitionLive is the proof the
// fake-execer tests cannot give.
//
// Three rows are planted that the retire must NOT delete — one differing only in
// fact_kind, one only in generation_id, one only in scope_id — plus one row that
// IS in the write's own partition and not in its keep-set, which must be the
// ONLY casualty. An unbounded delete (the `OR TRUE` mutant) takes all four and
// fails here immediately.
func TestContainerImageIdentityRetireBoundedToOwnPartitionLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityRetireLiveDB(t)

	suffix := fmt.Sprintf("5847-bound-%d", time.Now().UnixNano())
	targetScope := "scope-" + suffix
	targetGeneration := "gen-" + suffix
	siblingGeneration := "gen-sibling-" + suffix
	otherScope := "scope-other-" + suffix
	otherGeneration := "gen-other-" + suffix

	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, targetScope, targetGeneration)
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, otherScope, otherGeneration)
	// A second generation on the SAME scope: the generation_id predicate is the
	// one a "retire the whole scope" mistake would drop.
	if _, err := sqlDB.ExecContext(ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', now(), now(), 'superseded', now())
		ON CONFLICT (generation_id) DO NOTHING`,
		siblingGeneration, targetScope,
	); err != nil {
		t.Fatalf("seed sibling generation: %v", err)
	}

	var (
		otherKindRow       = "keep-other-kind-" + suffix
		otherGenerationRow = "keep-other-generation-" + suffix
		otherScopeRow      = "keep-other-scope-" + suffix
		samePartitionRow   = "retire-same-partition-" + suffix
	)
	seedContainerImageIdentityFactRow(t, ctx, sqlDB, otherKindRow, targetScope, targetGeneration, "oci.manifest", 0)
	seedContainerImageIdentityFactRow(
		t, ctx, sqlDB, otherGenerationRow, targetScope, siblingGeneration, containerImageIdentityLiveFactKind, 0)
	seedContainerImageIdentityFactRow(
		t, ctx, sqlDB, otherScopeRow, otherScope, otherGeneration, containerImageIdentityLiveFactKind, 0)
	seedContainerImageIdentityFactRow(
		t, ctx, sqlDB, samePartitionRow, targetScope, targetGeneration, containerImageIdentityLiveFactKind, 0)

	writer := reducer.PostgresContainerImageIdentityWriter{DB: sqlDB}
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, containerImageIdentityLiveWrite(
		targetScope, targetGeneration, suffix, time.Now().UTC(), reducer.ContainerImageIdentityExactDigest,
	))
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if result.CanonicalWrites != 1 {
		t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
	}
	if result.Retired != 1 {
		t.Fatalf("Retired = %d, want exactly 1 (only the same-partition non-keep row)", result.Retired)
	}

	planted := []string{otherKindRow, otherGenerationRow, otherScopeRow, samePartitionRow}
	survivors := containerImageIdentitySurvivingFactIDs(t, ctx, sqlDB, planted)
	want := []string{otherGenerationRow, otherKindRow, otherScopeRow}
	sort.Strings(want)
	if len(survivors) != len(want) {
		t.Fatalf(
			"surviving planted rows = %v, want %v (exactly one same-partition row must be deleted)",
			survivors, want,
		)
	}
	for i := range want {
		if survivors[i] != want[i] {
			t.Fatalf("surviving planted rows = %v, want %v", survivors, want)
		}
	}

	// The write's own row must still be there — a retire that deleted what it
	// just wrote would also produce "one row gone".
	var written int
	if err := sqlDB.QueryRowContext(ctx,
		`SELECT count(*) FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = $3`,
		targetScope, targetGeneration, containerImageIdentityLiveFactKind,
	).Scan(&written); err != nil {
		t.Fatalf("count written identity rows: %v", err)
	}
	if written != 1 {
		t.Fatalf("identity rows in the write's own partition = %d, want exactly 1 (its own)", written)
	}
}

// TestContainerImageIdentityRetireEmptyKeepSetClearsDemotedGenerationLive
// covers the case an identity-keyed upsert cannot reach at all.
//
// When every image reference in a generation falls out of the two canonical
// outcomes, no row is written over the stale ones — only an empty keep-set
// clears them. This proves the empty-array form (`fact_id <> ALL('{}')`) really
// does match every row in the partition against real Postgres, which the
// fake-execer test could only assert as an argument shape, and that it still
// stops at the partition boundary.
func TestContainerImageIdentityRetireEmptyKeepSetClearsDemotedGenerationLive(t *testing.T) {
	sqlDB, ctx := containerImageIdentityRetireLiveDB(t)

	suffix := fmt.Sprintf("5847-demoted-%d", time.Now().UnixNano())
	targetScope := "scope-" + suffix
	targetGeneration := "gen-" + suffix
	otherScope := "scope-other-" + suffix
	otherGeneration := "gen-other-" + suffix

	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, targetScope, targetGeneration)
	seedContainerImageIdentityScopeGeneration(t, ctx, sqlDB, otherScope, otherGeneration)

	stale := []string{"stale-a-" + suffix, "stale-b-" + suffix}
	for _, id := range stale {
		seedContainerImageIdentityFactRow(t, ctx, sqlDB, id, targetScope, targetGeneration, containerImageIdentityLiveFactKind, 0)
	}
	bystander := "bystander-" + suffix
	seedContainerImageIdentityFactRow(t, ctx, sqlDB, bystander, otherScope, otherGeneration, containerImageIdentityLiveFactKind, 0)

	writer := reducer.PostgresContainerImageIdentityWriter{DB: sqlDB}
	result, err := writer.WriteContainerImageIdentityDecisions(ctx, reducer.ContainerImageIdentityWrite{
		IntentID:     "intent-" + suffix,
		ScopeID:      targetScope,
		GenerationID: targetGeneration,
		SourceSystem: "git",
		Cause:        "live demotion proof",
		EvidenceAsOf: time.Now().UTC(),
		Decisions:    nil,
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if result.Retired != len(stale) {
		t.Fatalf("Retired = %d, want %d", result.Retired, len(stale))
	}
	if !result.RetiredWithoutCanonicalWrites {
		t.Fatal("RetiredWithoutCanonicalWrites = false; a zero-canonical write that cleared a " +
			"non-empty prior set must be flagged so an evidence-visibility gap is visible to an operator")
	}

	survivors := containerImageIdentitySurvivingFactIDs(
		t, ctx, sqlDB, append(append([]string{}, stale...), bystander))
	if len(survivors) != 1 || survivors[0] != bystander {
		t.Fatalf(
			"surviving rows = %v, want exactly [%s]: a demoted generation retires its own stale rows and nothing else",
			survivors, bystander,
		)
	}
}
