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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// targetedDiffBase anchors every timestamp the #7584 differential seeds. The
// pre-pass runs at +1h and both arms run at +2h, so phase, memo and reopen
// timestamps are byte-identical across arms whenever the same row is written.
var targetedDiffBase = time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

// targetedDiffArmsAt is the clock both arms use.
var targetedDiffArmsAt = targetedDiffBase.Add(2 * time.Hour)

// targetedDiffReopenDomains are every reducer domain either reopen path
// touches: the two relationship domains and the correlation domains.
func targetedDiffReopenDomains() []string {
	return append([]string{"deployment_mapping", "code_import_repo_edge"}, CrossScopeCorrelationReopenDomains()...)
}

// targetedDiffPair is two isolated, fully bootstrapped schemas seeded
// identically: "whole" runs RunDeferredRelationshipMaintenance and "targeted"
// runs RunDeferredRelationshipMaintenanceForPartitions.
type targetedDiffPair struct {
	t        *testing.T
	ctx      context.Context
	whole    *sql.DB
	targeted *sql.DB
}

// targetedMaintenanceProofDSN returns the administrative DSN of the
// disposable PostgreSQL the #7584 targeted-maintenance proofs run on. It skips
// when ESHU_TARGETED_MAINTENANCE_PROOF_DSN is unset and fails closed when it is
// set without ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1.
func targetedMaintenanceProofDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_TARGETED_MAINTENANCE_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_TARGETED_MAINTENANCE_PROOF_DSN and ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1 to run the #7584 targeted-maintenance proofs")
	}
	if os.Getenv("ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE") != "1" {
		t.Fatal("ESHU_TARGETED_MAINTENANCE_PROOF_DSN is set without ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1")
	}
	return dsn
}

// newTargetedDiffPair opens both arms on the targeted-maintenance proof DSN.
func newTargetedDiffPair(t *testing.T) *targetedDiffPair {
	t.Helper()
	dsn := targetedMaintenanceProofDSN(t)
	pair := &targetedDiffPair{
		t:        t,
		whole:    openIsolatedBootstrapSchema(t, dsn, "tgt7584_whole"),
		targeted: openIsolatedBootstrapSchema(t, dsn, "tgt7584_scoped"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	pair.ctx = ctx
	return pair
}

// exec applies one statement to both arms. A nil arm is skipped, so a
// single-schema fixture (only whole set) reuses the same seeders.
func (p *targetedDiffPair) exec(query string, args ...any) {
	p.t.Helper()
	for _, database := range []*sql.DB{p.whole, p.targeted} {
		if database == nil {
			continue
		}
		if _, err := database.ExecContext(p.ctx, query, args...); err != nil {
			p.t.Fatalf("seed %q: %v", firstLine(query), err)
		}
	}
}

// scope seeds one ingestion scope with no active generation.
func (p *targetedDiffPair) scope(scopeID string) {
	p.t.Helper()
	p.exec(`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', NULL)`, scopeID, targetedDiffBase)
}

// generation seeds one generation at offset and, when activate, moves the
// scope's active pointer to it and supersedes the previous active generation.
func (p *targetedDiffPair) generation(scopeID, generationID string, offset time.Duration, activate bool) {
	p.t.Helper()
	status := "pending"
	if activate {
		status = "active"
		p.exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = $2
WHERE scope_id = $1 AND status = 'active'`, scopeID, targetedDiffBase.Add(offset))
	}
	p.exec(`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'poll', $3, $3, $4, CASE WHEN $4 = 'active' THEN $3::timestamptz END)`,
		generationID, scopeID, targetedDiffBase.Add(offset), status)
	if activate {
		p.exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
	}
}

// fact seeds one fact row of kind with a JSON payload.
func (p *targetedDiffPair) fact(factID, scopeID, generationID, kind, payload string) {
	p.t.Helper()
	p.exec(`INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, $5, $6::jsonb)`,
		factID, scopeID, generationID, kind, targetedDiffBase, payload)
}

// repo seeds a repository fact for repoID named name in the partition.
func (p *targetedDiffPair) repo(scopeID, generationID, repoID, name string) {
	p.t.Helper()
	p.fact("repo-"+generationID+"-"+repoID, scopeID, generationID, "repository",
		fmt.Sprintf(`{"repo_id":%q,"name":%q}`, repoID, name))
}

// terraformRef seeds a Terraform content fact in repoID whose content names
// targetAlias as an app_repo.
func (p *targetedDiffPair) terraformRef(factID, scopeID, generationID, repoID, path, targetAlias string) {
	p.t.Helper()
	p.fact(factID, scopeID, generationID, "content", fmt.Sprintf(
		`{"repo_id":%q,"artifact_type":"terraform","relative_path":%q,"content":"app_repo = \"%s\""}`,
		repoID, path, targetAlias))
}

// gitRepo seeds a git scope with one active generation holding one repository.
func (p *targetedDiffPair) gitRepo(scopeID, generationID, repoID, name string) {
	p.t.Helper()
	p.scope(scopeID)
	p.generation(scopeID, generationID, 0, true)
	p.repo(scopeID, generationID, repoID, name)
}

// workItems seeds one succeeded reducer work item per reopen domain in the
// partition, with ids "<generation>/<domain>".
func (p *targetedDiffPair) workItems(scopeID, generationID string) {
	p.t.Helper()
	for _, domain := range targetedDiffReopenDomains() {
		p.exec(`INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', 1, $5, $5)`,
			generationID+"/"+domain, scopeID, generationID, domain, targetedDiffBase)
	}
}

// prepass runs the real corpus-wide backfill (evidence, phase and memo) on
// both arms at +1h, establishing the steady state an obligation starts from.
func (p *targetedDiffPair) prepass() {
	p.t.Helper()
	for _, database := range []*sql.DB{p.whole, p.targeted} {
		if database == nil {
			continue
		}
		store := targetedDiffStore(database, targetedDiffBase.Add(time.Hour))
		if err := store.BackfillAllRelationshipEvidence(p.ctx, nil, nil); err != nil {
			p.t.Fatalf("pre-pass BackfillAllRelationshipEvidence() error = %v", err)
		}
	}
}

// targetedDiffStore builds a store on database with a fixed clock, one worker
// and the default batch size.
func targetedDiffStore(database *sql.DB, now time.Time) IngestionStore {
	store := NewIngestionStore(SQLDB{DB: database})
	store.Now = func() time.Time { return now }
	store.maintenanceWorkers = 1
	return store
}

// targetedDiffRow is one captured row: its table, primary key, owning
// partition, full comparable value, and committing transaction id.
type targetedDiffRow struct {
	table     string
	key       string
	partition scopeGenerationPartition
	value     string
	xmin      int64
}

// targetedDiffState is every compared row of one arm keyed by table|key.
type targetedDiffState map[string]targetedDiffRow

// capture reads every compared table of database. Evidence excludes only
// observed_at, which the shipped writer stamps from the wall clock. Phase,
// memo and work rows exclude the eshu:global genesis rows ApplyBootstrap
// stamps with now() (the global value-flow refresh singleton is not in either
// reopen domain list, so no maintenance pass reads or writes them).
func (p *targetedDiffPair) capture(database *sql.DB) targetedDiffState {
	p.t.Helper()
	state := targetedDiffState{}
	queries := map[string]string{
		"evidence": `SELECT evidence.evidence_id, generation.scope_id, evidence.generation_id,
  concat_ws('|', evidence.evidence_kind, evidence.relationship_type, COALESCE(evidence.source_repo_id, ''),
    COALESCE(evidence.target_repo_id, ''), COALESCE(evidence.source_entity_id, ''),
    COALESCE(evidence.target_entity_id, ''), evidence.confidence::text, evidence.rationale, evidence.details::text),
  evidence.xmin::text::bigint
FROM relationship_evidence_facts AS evidence
JOIN scope_generations AS generation ON generation.generation_id = evidence.generation_id`,
		"phase": `SELECT concat_ws('|', scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase),
  scope_id, generation_id, concat_ws('|', committed_at, updated_at), xmin::text::bigint
FROM graph_projection_phase_state
WHERE scope_id <> 'eshu:global'`,
		"memo": `SELECT scope_id || '|' || generation_id, scope_id, generation_id,
  concat_ws('|', catalog_fingerprint, committed_at), xmin::text::bigint
FROM deferred_backfill_partition_memo
WHERE scope_id <> 'eshu:global'`,
		"work": `SELECT work.work_item_id, work.scope_id, work.generation_id, row_to_json(work)::text, work.xmin::text::bigint
FROM fact_work_items AS work
WHERE work.scope_id <> 'eshu:global'`,
	}
	for table, query := range queries {
		rows, err := database.QueryContext(p.ctx, query)
		if err != nil {
			p.t.Fatalf("capture %s: %v", table, err)
		}
		for rows.Next() {
			row := targetedDiffRow{table: table}
			if err := rows.Scan(&row.key, &row.partition.ScopeID, &row.partition.GenerationID, &row.value, &row.xmin); err != nil {
				p.t.Fatalf("scan %s: %v", table, err)
			}
			state[table+"|"+row.key] = row
		}
		if err := rows.Close(); err != nil {
			p.t.Fatalf("close %s: %v", table, err)
		}
	}
	return state
}

// targetedDiffReport is the set comparison of one fixture.
type targetedDiffReport struct {
	// wholeOnlyInScope and targetedOnlyInScope are rows of compared partitions
	// that differ between arms; both must be empty.
	wholeOnlyInScope    []string
	targetedOnlyInScope []string
	// targetedChangedOutside are rows outside the compared partitions that the
	// targeted arm changed relative to the pre-arm state; must be empty.
	targetedChangedOutside []string
	// wholeChangedOutside are rows outside the compared partitions the whole
	// arm changed; reported, and asserted per fixture where it matters.
	wholeChangedOutside []string
}

// diffStates compares before, whole and targeted over compared partitions.
func diffStates(
	before, whole, targeted targetedDiffState,
	compared map[scopeGenerationPartition]struct{},
) targetedDiffReport {
	var report targetedDiffReport
	inScope := func(row targetedDiffRow) bool {
		_, ok := compared[row.partition]
		return ok
	}
	for key, row := range whole {
		other, ok := targeted[key]
		switch {
		case inScope(row) && (!ok || other.value != row.value):
			report.wholeOnlyInScope = append(report.wholeOnlyInScope, key+" = "+row.value)
		case !inScope(row):
			if prior, existed := before[key]; !existed || prior.value != row.value {
				report.wholeChangedOutside = append(report.wholeChangedOutside, key)
			}
		}
	}
	for key, row := range targeted {
		other, ok := whole[key]
		if inScope(row) && (!ok || other.value != row.value) {
			report.targetedOnlyInScope = append(report.targetedOnlyInScope, key+" = "+row.value)
		}
		if !inScope(row) {
			if prior, existed := before[key]; !existed || prior.value != row.value {
				report.targetedChangedOutside = append(report.targetedChangedOutside, key)
			}
		}
	}
	for key, row := range before {
		if _, ok := targeted[key]; !ok && !inScope(row) {
			report.targetedChangedOutside = append(report.targetedChangedOutside, key+" (deleted)")
		}
	}
	for _, list := range [][]string{
		report.wholeOnlyInScope, report.targetedOnlyInScope,
		report.targetedChangedOutside, report.wholeChangedOutside,
	} {
		sort.Strings(list)
	}
	return report
}

// changedRows returns the keys of table rows in after that are new or changed
// relative to before, restricted to partition when it is non-empty.
func changedRows(before, after targetedDiffState, table string) []string {
	var keys []string
	for key, row := range after {
		if row.table != table {
			continue
		}
		if prior, ok := before[key]; !ok || prior.value != row.value {
			keys = append(keys, row.key)
		}
	}
	sort.Strings(keys)
	return keys
}

// assertPhaseCommittedAfterEvidence pins commit order in one arm: every phase
// row the arm published for a partition was committed by a later transaction
// than every evidence row the arm newly wrote for that partition.
func assertPhaseCommittedAfterEvidence(t *testing.T, arm string, before, after targetedDiffState) {
	t.Helper()
	for key, phase := range after {
		if phase.table != "phase" {
			continue
		}
		if prior, ok := before[key]; ok && prior.value == phase.value {
			continue
		}
		for evidenceKey, evidence := range after {
			if evidence.table != "evidence" || evidence.partition != phase.partition {
				continue
			}
			if _, existed := before[evidenceKey]; existed {
				continue
			}
			if evidence.xmin >= phase.xmin {
				t.Fatalf("%s arm: phase %s committed in xid %d, not after evidence %s in xid %d",
					arm, phase.key, phase.xmin, evidence.key, evidence.xmin)
			}
		}
	}
}

// partitionSet builds a partition set from scope/generation pairs.
func partitionSet(pairs ...string) map[scopeGenerationPartition]struct{} {
	set := make(map[scopeGenerationPartition]struct{}, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		set[scopeGenerationPartition{ScopeID: pairs[i], GenerationID: pairs[i+1]}] = struct{}{}
	}
	return set
}

// partitionsOf builds a partition slice from scope/generation pairs, in the
// given order.
func partitionsOf(pairs ...string) []scopeGenerationPartition {
	partitions := make([]scopeGenerationPartition, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		partitions = append(partitions, scopeGenerationPartition{ScopeID: pairs[i], GenerationID: pairs[i+1]})
	}
	return partitions
}

// hookBeginner wraps a beginner so a fixture can act at an exact point of a
// pass: before the nth Begin, or on a statement inside a transaction.
type hookBeginner struct {
	inner   db.Beginner
	begins  int
	onBegin func(n int) error
	onQuery func(query string, args []any) error
	onExec  func(query string, args []any) error
}

// Begin implements db.Beginner.
func (h *hookBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	h.begins++
	if h.onBegin != nil {
		if err := h.onBegin(h.begins); err != nil {
			return nil, err
		}
	}
	tx, err := h.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return hookTransaction{Transaction: tx, hooks: h}, nil
}

// hookTransaction runs the hookBeginner's statement hooks.
type hookTransaction struct {
	db.Transaction
	hooks *hookBeginner
}

// QueryContext implements db.Queryer.
func (h hookTransaction) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if h.hooks.onQuery != nil {
		if err := h.hooks.onQuery(query, args); err != nil {
			return nil, err
		}
	}
	return h.Transaction.QueryContext(ctx, query, args...)
}

// ExecContext implements db.Executor.
func (h hookTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if h.hooks.onExec != nil {
		if err := h.hooks.onExec(query, args); err != nil {
			return nil, err
		}
	}
	return h.Transaction.ExecContext(ctx, query, args...)
}
