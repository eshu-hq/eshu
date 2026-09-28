// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Fixtures shared by the #7320 live proofs: a supersede must fold the old
// row's failure evidence into failure_details.prior_failure. The timestamps
// are fixed at process start so a row's seeded updated_at can be compared with
// the value the fold copied out of it.
var (
	pfCreatedAt     = time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Microsecond)
	pfLastAttemptAt = time.Now().UTC().Add(-95 * time.Minute).Truncate(time.Microsecond)
	pfUpdatedAt     = time.Now().UTC().Add(-90 * time.Minute).Truncate(time.Microsecond)
)

func pfStr(s string) *string { return &s }

// pfSeed is one old work row: its status, failure fields (nil is NULL) and
// lease. claimUntil is relative to now; nil leaves the row unleased.
type pfSeed struct {
	status                  string
	class, message, details *string
	attempts                int
	leaseOwner              string
	claimUntil              *time.Duration
}

// pfDeadLetter, pfFailed, pfRetrying, pfBlank, pfFailedNull and pfNeverFailed
// are the presence-rule cases: the first three carry evidence, pfFailedNull is
// a failed row with every field NULL (status alone earns the key), and the
// last two never failed and must not gain a prior_failure key.
func pfDeadLetter() pfSeed {
	return pfSeed{
		status: "dead_letter", attempts: 3,
		class: pfStr("graph_write_timeout"), message: pfStr("neo4j execute group timed out after 2s"),
		details: pfStr("phase=semantic label=Variable rows=500"),
	}
}

func pfFailed() pfSeed {
	return pfSeed{
		status: "failed", attempts: 3,
		class: pfStr("input_invalid"), message: pfStr("fact payload rejected"),
		details: pfStr(`{"fact_kind":"file","reason":"bad utf8"}`),
	}
}

func pfRetrying() pfSeed {
	return pfSeed{
		status: "retrying", attempts: 2,
		class: pfStr("projection_retryable"), message: pfStr("write canonical nodes: connection reset"),
		details: pfStr("dial tcp 10.0.0.4:7687: connection reset by peer"),
	}
}

func pfBlank() pfSeed {
	return pfSeed{status: "pending", attempts: 0, class: pfStr("  "), message: pfStr(""), details: nil}
}

func pfFailedNull() pfSeed { return pfSeed{status: "failed", attempts: 3} }

func pfNeverFailed() pfSeed { return pfSeed{status: "pending"} }

// pfWithLease returns s as a leased row: the owner and a lease relative to now.
func pfWithLease(s pfSeed, status, owner string, until time.Duration) pfSeed {
	s.status, s.leaseOwner, s.claimUntil = status, owner, &until
	return s
}

// pfSeedScope inserts a scope with an older generation and a newer one. The
// newer generation is the scope's active pointer when its status is active.
func pfSeedScope(t *testing.T, database *sql.DB, scopeID, oldStatus, newStatus string) {
	t.Helper()
	seedClaimMaintenanceScopes(t, database, scopeID)
	for _, g := range []struct{ suffix, status, age string }{
		{"-gen-old", oldStatus, "2 hours"}, {"-gen-new", newStatus, "1 hour"},
	} {
		if _, err := database.Exec(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status,
                               activated_at, superseded_at)
VALUES ($1 || $2, $1, 'push', now() - $3::interval, now() - $3::interval, $4,
        CASE WHEN $4 IN ('active', 'superseded') THEN now() - $3::interval END,
        CASE WHEN $4 = 'superseded' THEN now() - interval '30 minutes' END)`,
			scopeID, g.suffix, g.age, g.status); err != nil {
			t.Fatalf("seed generation %s%s: %v", scopeID, g.suffix, err)
		}
	}
	if newStatus == "active" {
		if _, err := database.Exec(`UPDATE ingestion_scopes SET active_generation_id = $1 || '-gen-new' WHERE scope_id = $1`, scopeID); err != nil {
			t.Fatalf("seed active pointer %s: %v", scopeID, err)
		}
	}
}

// pfInsertWork inserts one work row on generationID with seed s.
func pfInsertWork(t *testing.T, database *sql.DB, workItemID, scopeID, generationID, stage string, s pfSeed) {
	t.Helper()
	var owner, until any
	if s.leaseOwner != "" {
		owner = s.leaseOwner
	}
	if s.claimUntil != nil {
		until = time.Now().UTC().Add(*s.claimUntil)
	}
	domain := "source_local"
	if stage == "reducer" {
		domain = "zz_probe_domain"
	}
	if _, err := database.Exec(`
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    lease_owner, claim_until, visible_at, last_attempt_at,
    failure_class, failure_message, failure_details, payload, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb, $15, $16)`,
		workItemID, scopeID, generationID, stage, domain, s.status, s.attempts,
		owner, until, pfCreatedAt, pfLastAttemptAt,
		s.class, s.message, s.details, pfCreatedAt, pfUpdatedAt); err != nil {
		t.Fatalf("seed work %s: %v", workItemID, err)
	}
}

// pfApplySeed rewrites an already inserted row to seed s, for fixtures that
// create the row through a shared helper.
func pfApplySeed(t *testing.T, database *sql.DB, workItemID string, s pfSeed) {
	t.Helper()
	var owner, until any
	if s.leaseOwner != "" {
		owner = s.leaseOwner
	}
	if s.claimUntil != nil {
		until = time.Now().UTC().Add(*s.claimUntil)
	}
	if _, err := database.Exec(`
UPDATE fact_work_items
SET status = $2, attempt_count = $3, lease_owner = $4, claim_until = $5, last_attempt_at = $6,
    failure_class = $7, failure_message = $8, failure_details = $9, created_at = $10, updated_at = $11
WHERE work_item_id = $1`, workItemID, s.status, s.attempts, owner, until, pfLastAttemptAt,
		s.class, s.message, s.details, pfCreatedAt, pfUpdatedAt); err != nil {
		t.Fatalf("apply seed to %s: %v", workItemID, err)
	}
}

// pfRow is a work row as the supersede left it.
type pfRow struct {
	status, raw                   string
	class, message                sql.NullString
	details                       map[string]any
	attempts                      int
	lastAttempt, created, updated time.Time
	leaseOwner, claimUntil        sql.NullString
}

func pfRead(t *testing.T, database *sql.DB, workItemID string) pfRow {
	t.Helper()
	var r pfRow
	var raw sql.NullString
	if err := database.QueryRow(`
SELECT status, failure_class, failure_message, failure_details, attempt_count,
       last_attempt_at, created_at, updated_at, lease_owner, claim_until::text
FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(
		&r.status, &r.class, &r.message, &raw, &r.attempts,
		&r.lastAttempt, &r.created, &r.updated, &r.leaseOwner, &r.claimUntil); err != nil {
		t.Fatalf("read work %s: %v", workItemID, err)
	}
	r.raw = raw.String
	if raw.Valid {
		if err := json.Unmarshal([]byte(raw.String), &r.details); err != nil {
			t.Fatalf("work %s failure_details %q is not a JSON object: %v", workItemID, raw.String, err)
		}
	}
	return r
}

// pfWant is what a supersede must leave: the site's marker, its own detail
// keys, and either the folded prior failure (prior set) or none.
type pfWant struct {
	class, message string
	detailKeys     map[string]any
	prior          *pfSeed
}

// pfAssertSuperseded compares a row with want by VALUE, so a fold that copies
// the wrong column, drops a field or renames a key fails.
func pfAssertSuperseded(t *testing.T, r pfRow, want pfWant, seed pfSeed) {
	t.Helper()
	if r.status != "superseded" || r.class.String != want.class || r.message.String != want.message {
		t.Fatalf("row = (%s, %q, %q), want (superseded, %q, %q)",
			r.status, r.class.String, r.message.String, want.class, want.message)
	}
	if r.leaseOwner.Valid || r.claimUntil.Valid {
		t.Fatalf("lease not cleared: owner=%v until=%v", r.leaseOwner, r.claimUntil)
	}
	if r.attempts != seed.attempts || !r.lastAttempt.Equal(pfLastAttemptAt) || !r.created.Equal(pfCreatedAt) {
		t.Fatalf("attempts/last_attempt/created = %d/%v/%v, want untouched %d/%v/%v",
			r.attempts, r.lastAttempt, r.created, seed.attempts, pfLastAttemptAt, pfCreatedAt)
	}
	own := map[string]any{}
	for k, v := range r.details {
		if k != "prior_failure" {
			own[k] = v
		}
	}
	if !reflect.DeepEqual(own, want.detailKeys) {
		t.Fatalf("supersede detail keys = %v, want %v", own, want.detailKeys)
	}
	prior, has := r.details["prior_failure"]
	if want.prior == nil {
		if has {
			t.Fatalf("prior_failure = %v, want the key absent for a row that never failed (details %q)", prior, r.raw)
		}
		return
	}
	got, ok := prior.(map[string]any)
	if !ok {
		t.Fatalf("prior_failure = %v (%T), want an object; details %q", prior, prior, r.raw)
	}
	nullable := func(p *string) any {
		if p == nil {
			return nil
		}
		return *p
	}
	wantPrior := map[string]any{
		"status":          want.prior.status,
		"failure_class":   nullable(want.prior.class),
		"failure_message": nullable(want.prior.message),
		"failure_details": nullable(want.prior.details),
	}
	gotUpdated, _ := got["updated_at"].(string)
	delete(got, "updated_at")
	if !reflect.DeepEqual(got, wantPrior) {
		t.Fatalf("prior_failure = %v, want %v", got, wantPrior)
	}
	parsed, err := time.Parse(time.RFC3339Nano, gotUpdated)
	if err != nil || !parsed.Equal(pfUpdatedAt) {
		t.Fatalf("prior_failure.updated_at = %q (%v), want %v", gotUpdated, err, pfUpdatedAt)
	}
}

// pfWork builds the projector work item a queue call takes for generationID.
func pfWork(scopeID, generationID string, attempts int) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{GenerationID: generationID, ScopeID: scopeID},
		AttemptCount: attempts,
	}
}

// ncStatement is one claim statement under the no-candidate proof: the
// cost harness statement plus the supersede CTEs whose UPDATE it must not run
// on a row when nothing is stale.
type ncStatement struct {
	costStatement
	cteNames []string
}

// ncStatements are the three claim statements the fold touches, with the
// supersede UPDATE CTEs each one carries.
func ncStatements() []ncStatement {
	var out []ncStatement
	for _, st := range costStatements() {
		names := []string{"superseded_stale_reducer_generations"}
		if st.name == "projector_claim" {
			names = []string{"superseded_stale_projector_generations", "superseded_stale_scope_generations"}
		}
		out = append(out, ncStatement{costStatement: st, cteNames: names})
	}
	return out
}

// ncSeed seeds n scopes whose only work row is a pending one on the current
// generation, so a claim sweep has no supersede candidate. staleRows of the
// scopes also get an older failed generation with a dead-lettered row carrying
// details: the seed the proof must reject.
func ncSeed(t *testing.T, database *sql.DB, projector bool, n, staleRows int) {
	t.Helper()
	costReset(t, database)
	scopeStatus, genStatus, stage, domain, oldGen := "active", "active", "reducer", "zz_probe_domain", "superseded"
	if projector {
		scopeStatus, genStatus, stage, domain, oldGen = "pending", "pending", "projector", "source_local", "failed"
	}
	stmts := []string{
		fmt.Sprintf(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'scope-'||i,'repository','git','scope-'||i,'git','scope-'||i, now(), now(), '%s' FROM generate_series(1,%d) i`, scopeStatus, n),
		fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g2','scope-'||i,'push', now()-interval '1 hour', now()-interval '1 hour','%s' FROM generate_series(1,%d) i`, genStatus, n),
		fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, visible_at, payload, created_at, updated_at)
SELECT '%s_scope-'||i||'_g2','scope-'||i,'scope-'||i||'-g2','%s','%s','pending',0, now()-interval '1 hour','{}'::jsonb, now()-interval '1 hour', now()-interval '1 hour' FROM generate_series(1,%d) i`, stage, stage, domain, n),
	}
	if !projector {
		stmts = append(stmts, `UPDATE ingestion_scopes SET active_generation_id = scope_id || '-g2'`)
	}
	if staleRows > 0 {
		stmts = append(stmts,
			fmt.Sprintf(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'scope-'||i||'-g1','scope-'||i,'push', now()-interval '2 hours', now()-interval '2 hours','%s' FROM generate_series(1,%d) i`, oldGen, staleRows),
			fmt.Sprintf(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, last_attempt_at, failure_class, failure_message, failure_details, payload, created_at, updated_at)
SELECT '%s_scope-'||i||'_g1','scope-'||i,'scope-'||i||'-g1','%s','%s','dead_letter',3, now()-interval '90 minutes','graph_write_timeout','timed out', %s,'{}'::jsonb, now()-interval '2 hours', now()-interval '90 minutes' FROM generate_series(1,%d) i`, stage, stage, domain, costDetailExpr(800), staleRows))
	}
	costExec(t, database, append(stmts, `VACUUM ANALYZE fact_work_items`, `ANALYZE scope_generations`, `ANALYZE ingestion_scopes`)...)
}

// ncPlanNode is the part of an EXPLAIN (FORMAT JSON) node the proof reads.
type ncPlanNode struct {
	NodeType      string       `json:"Node Type"`
	Operation     string       `json:"Operation"`
	Relation      string       `json:"Relation Name"`
	Index         string       `json:"Index Name"`
	Subplan       string       `json:"Subplan Name"`
	ActualRows    float64      `json:"Actual Rows"`
	ActualLoops   float64      `json:"Actual Loops"`
	SharedHit     int64        `json:"Shared Hit Blocks"`
	SharedRead    int64        `json:"Shared Read Blocks"`
	SharedDirtied int64        `json:"Shared Dirtied Blocks"`
	SharedWritten int64        `json:"Shared Written Blocks"`
	Children      []ncPlanNode `json:"Plans"`
}

// ncPlan is one EXPLAIN (ANALYZE, BUFFERS, VERBOSE) result.
type ncPlan struct {
	root     ncPlanNode
	planning float64
}

// ncExplain runs EXPLAIN (ANALYZE, BUFFERS, VERBOSE, FORMAT JSON) of a claim
// statement in a transaction it rolls back.
func ncExplain(t *testing.T, database *sql.DB, statement string, args []any) ncPlan {
	t.Helper()
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var raw string
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, VERBOSE, FORMAT JSON) "+statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	var doc []struct {
		Plan     ncPlanNode `json:"Plan"`
		Planning float64    `json:"Planning Time"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || len(doc) != 1 {
		t.Fatalf("decode explain (err=%v, %d documents): %s", err, len(doc), raw)
	}
	return ncPlan{root: doc[0].Plan, planning: doc[0].Planning}
}

// shape lists the node type, operation, relation, index and subplan of every
// node in pre-order: two texts with the same shape ran the same plan.
func (p ncPlan) shape() []string {
	var out []string
	var walk func(ncPlanNode)
	walk = func(n ncPlanNode) {
		out = append(out, strings.Join([]string{n.NodeType, n.Operation, n.Relation, n.Index, n.Subplan}, "|"))
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(p.root)
	return out
}

// rowsWritten returns the rows the UPDATE in the named CTE wrote and whether
// the CTE was found. An UPDATE without RETURNING reports no rows itself, so the
// rows it wrote are the rows its input node produced.
func (p ncPlan) rowsWritten(cte string) (float64, bool) {
	var rows float64
	var found bool
	var walk func(ncPlanNode)
	walk = func(n ncPlanNode) {
		if n.NodeType == "ModifyTable" && n.Operation == "Update" && n.Subplan == "CTE "+cte && len(n.Children) > 0 {
			found = true
			rows = n.Children[0].ActualRows * n.Children[0].ActualLoops
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(p.root)
	return rows, found
}
