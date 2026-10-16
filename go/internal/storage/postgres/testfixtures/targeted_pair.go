// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TargetedDiffBase anchors every timestamp the #7584 differential seeds. The
// pre-pass runs at +1h and both arms run at +2h, so phase, memo and reopen
// timestamps are byte-identical across arms whenever the same row is written.
var TargetedDiffBase = time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

// TargetedDiffArmsAt is the clock both arms use.
var TargetedDiffArmsAt = TargetedDiffBase.Add(2 * time.Hour)

// TargetedDiffPair is two isolated, fully bootstrapped schemas seeded
// identically: "whole" runs RunDeferredRelationshipMaintenance and "targeted"
// runs RunDeferredRelationshipMaintenanceForPartitions.
type TargetedDiffPair struct {
	Domains    []string
	RunPrepass func(context.Context, *sql.DB, time.Time) error
	T          *testing.T
	Ctx        context.Context
	Whole      *sql.DB
	Targeted   *sql.DB
}

// TargetedMaintenanceProofDSN returns the administrative DSN of the
// disposable PostgreSQL the #7584 targeted-maintenance proofs run on. It skips
// when ESHU_TARGETED_MAINTENANCE_PROOF_DSN is unset and fails closed when it is
// set without ESHU_TARGETED_MAINTENANCE_PROOF_DISPOSABLE=1.
func TargetedMaintenanceProofDSN(t *testing.T) string {
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

// NewTargetedDiffPair opens two isolated production schemas. Callers supply
// only their package-owned bootstrap and real whole-pass entrypoint.
func NewTargetedDiffPair(
	t *testing.T, dsn string,
	open func(*testing.T, string, string) *sql.DB,
	domains []string,
	runPrepass func(context.Context, *sql.DB, time.Time) error,
) *TargetedDiffPair {
	t.Helper()
	pair := &TargetedDiffPair{
		T:          t,
		Whole:      open(t, dsn, "tgt7584_whole"),
		Targeted:   open(t, dsn, "tgt7584_scoped"),
		Domains:    domains,
		RunPrepass: runPrepass,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	pair.Ctx = ctx
	return pair
}

// firstLine trims a SQL constant to its first non-blank line so the error names
// the query without pasting the whole statement. It mirrors the backfill
// package's helper of the same name: the two copies are deliberate, since
// neither test package can import the other's unexported helpers.
func firstLine(query string) string {
	for _, line := range strings.Split(query, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "(empty query)"
}

// exec applies one statement to both arms.
func (p *TargetedDiffPair) Exec(query string, args ...any) {
	p.T.Helper()
	for _, database := range []*sql.DB{p.Whole, p.Targeted} {
		if _, err := database.ExecContext(p.Ctx, query, args...); err != nil {
			p.T.Fatalf("seed %q: %v", firstLine(query), err)
		}
	}
}

// scope seeds one ingestion scope with no active generation.
func (p *TargetedDiffPair) Scope(scopeID string) {
	p.T.Helper()
	p.Exec(`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', NULL)`, scopeID, TargetedDiffBase)
}

// generation seeds one generation at offset and, when activate, moves the
// scope's active pointer to it and supersedes the previous active generation.
func (p *TargetedDiffPair) Generation(scopeID, generationID string, offset time.Duration, activate bool) {
	p.T.Helper()
	status := "pending"
	if activate {
		status = "active"
		p.Exec(`UPDATE scope_generations SET status = 'superseded', superseded_at = $2
WHERE scope_id = $1 AND status = 'active'`, scopeID, TargetedDiffBase.Add(offset))
	}
	p.Exec(`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'poll', $3, $3, $4, CASE WHEN $4 = 'active' THEN $3::timestamptz END)`,
		generationID, scopeID, TargetedDiffBase.Add(offset), status)
	if activate {
		p.Exec(`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, generationID)
	}
}

// fact seeds one fact row of kind with a JSON payload.
func (p *TargetedDiffPair) Fact(factID, scopeID, generationID, kind, payload string) {
	p.T.Helper()
	p.Exec(`INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, $4, $1, 'git', $1, $5, $5, $6::jsonb)`,
		factID, scopeID, generationID, kind, TargetedDiffBase, payload)
}

// repo seeds a repository fact for repoID named name in the partition.
func (p *TargetedDiffPair) Repo(scopeID, generationID, repoID, name string) {
	p.T.Helper()
	p.Fact("repo-"+generationID+"-"+repoID, scopeID, generationID, "repository",
		fmt.Sprintf(`{"repo_id":%q,"name":%q}`, repoID, name))
}

// terraformRef seeds a Terraform content fact in repoID whose content names
// targetAlias as an app_repo.
func (p *TargetedDiffPair) TerraformRef(factID, scopeID, generationID, repoID, path, targetAlias string) {
	p.T.Helper()
	p.Fact(factID, scopeID, generationID, "content", fmt.Sprintf(
		`{"repo_id":%q,"artifact_type":"terraform","relative_path":%q,"content":"app_repo = \"%s\""}`,
		repoID, path, targetAlias))
}

// gitRepo seeds a git scope with one active generation holding one repository.
func (p *TargetedDiffPair) GitRepo(scopeID, generationID, repoID, name string) {
	p.T.Helper()
	p.Scope(scopeID)
	p.Generation(scopeID, generationID, 0, true)
	p.Repo(scopeID, generationID, repoID, name)
}

// workItems seeds one succeeded reducer work item per reopen domain in the
// partition, with ids "<generation>/<domain>".
func (p *TargetedDiffPair) WorkItems(scopeID, generationID string) {
	p.T.Helper()
	for _, domain := range p.Domains {
		p.Exec(`INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', 1, $5, $5)`,
			generationID+"/"+domain, scopeID, generationID, domain, TargetedDiffBase)
	}
}

// Prepass runs the real whole-pass entrypoint on both arms at +1h.
func (p *TargetedDiffPair) Prepass() {
	p.T.Helper()
	for _, database := range []*sql.DB{p.Whole, p.Targeted} {
		if err := p.RunPrepass(p.Ctx, database, TargetedDiffBase.Add(time.Hour)); err != nil {
			p.T.Fatalf("pre-pass BackfillAllRelationshipEvidence() error = %v", err)
		}
	}
}

// TargetedDiffRow is one captured row: its table, primary key, owning
// partition, full comparable value, and committing transaction id.
type TargetedDiffRow struct {
	Table     string
	Key       string
	Partition backfill.ScopeGenerationPartition
	Value     string
	Xmin      int64
}

// TargetedDiffState is every compared row of one arm keyed by table|key.
type TargetedDiffState map[string]TargetedDiffRow

// capture reads every compared table of database. Evidence excludes only
// observed_at, which the shipped writer stamps from the wall clock. Phase,
// memo and work rows exclude the eshu:global genesis rows postgres.ApplyBootstrap
// stamps with now() (the global value-flow refresh singleton is not in either
// reopen domain list, so no maintenance pass reads or writes them).
func (p *TargetedDiffPair) Capture(database *sql.DB) TargetedDiffState {
	p.T.Helper()
	state := TargetedDiffState{}
	queries := map[string]string{
		"evidence": `SELECT evidence.evidence_id, generation.scope_id, evidence.generation_id,
  concat_ws('|', evidence.evidence_kind, evidence.relationship_type, COALESCE(evidence.source_repo_id, ''),
    COALESCE(evidence.target_repo_id, ''), COALESCE(evidence.source_entity_id, ''),
    COALESCE(evidence.target_entity_id, ''), evidence.confidence::text, evidence.rationale, evidence.details::text),
  evidence.Xmin::text::bigint
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
		"work": `SELECT work.work_item_id, work.scope_id, work.generation_id, row_to_json(work)::text, work.Xmin::text::bigint
FROM fact_work_items AS work
WHERE work.scope_id <> 'eshu:global'`,
	}
	for table, query := range queries {
		rows, err := database.QueryContext(p.Ctx, query)
		if err != nil {
			p.T.Fatalf("capture %s: %v", table, err)
		}
		for rows.Next() {
			row := TargetedDiffRow{Table: table}
			if err := rows.Scan(&row.Key, &row.Partition.ScopeID, &row.Partition.GenerationID, &row.Value, &row.Xmin); err != nil {
				p.T.Fatalf("scan %s: %v", table, err)
			}
			state[table+"|"+row.Key] = row
		}
		if err := rows.Close(); err != nil {
			p.T.Fatalf("close %s: %v", table, err)
		}
	}
	return state
}

// TargetedDiffReport is the set comparison of one fixture.
type TargetedDiffReport struct {
	// wholeOnlyInScope and targetedOnlyInScope are rows of compared partitions
	// that differ between arms; both must be empty.
	WholeOnlyInScope    []string
	TargetedOnlyInScope []string
	// targetedChangedOutside are rows outside the compared partitions that the
	// targeted arm changed relative to the pre-arm state; must be empty.
	TargetedChangedOutside []string
	// wholeChangedOutside are rows outside the compared partitions the whole
	// arm changed; reported, and asserted per fixture where it matters.
	WholeChangedOutside []string
}

// DiffStates compares before, whole and targeted over compared partitions.
// unchanged is the fixture's declared set of partitions the whole pass leaves
// untouched (#8082); on those alone a phase or memo row may differ between the
// arms in its timestamps (see UnchangedTimestampOnly).
func DiffStates(
	before, whole, targeted TargetedDiffState,
	compared, unchanged map[backfill.ScopeGenerationPartition]struct{},
) TargetedDiffReport {
	var report TargetedDiffReport
	inScope := func(row TargetedDiffRow) bool {
		_, ok := compared[row.Partition]
		return ok
	}
	for key, row := range whole {
		other, ok := targeted[key]
		switch {
		case inScope(row) && (!ok || other.Value != row.Value) &&
			!UnchangedTimestampOnly(key, before, whole, targeted, unchanged):
			report.WholeOnlyInScope = append(report.WholeOnlyInScope, key+" = "+row.Value)
		case !inScope(row):
			if prior, existed := before[key]; !existed || prior.Value != row.Value {
				report.WholeChangedOutside = append(report.WholeChangedOutside, key)
			}
		}
	}
	for key, row := range targeted {
		other, ok := whole[key]
		if inScope(row) && (!ok || other.Value != row.Value) &&
			!UnchangedTimestampOnly(key, before, whole, targeted, unchanged) {
			report.TargetedOnlyInScope = append(report.TargetedOnlyInScope, key+" = "+row.Value)
		}
		if !inScope(row) {
			if prior, existed := before[key]; !existed || prior.Value != row.Value {
				report.TargetedChangedOutside = append(report.TargetedChangedOutside, key)
			}
		}
	}
	for key, row := range before {
		if _, ok := targeted[key]; !ok && !inScope(row) {
			report.TargetedChangedOutside = append(report.TargetedChangedOutside, key+" (deleted)")
		}
	}
	for _, list := range [][]string{
		report.WholeOnlyInScope, report.TargetedOnlyInScope,
		report.TargetedChangedOutside, report.WholeChangedOutside,
	} {
		sort.Strings(list)
	}
	return report
}

// ChangedRows returns the keys of table rows in after that are new or changed
// relative to before, restricted to partition when it is non-empty.
func ChangedRows(before, after TargetedDiffState, table string) []string {
	var keys []string
	for key, row := range after {
		if row.Table != table {
			continue
		}
		if prior, ok := before[key]; !ok || prior.Value != row.Value {
			keys = append(keys, row.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

// AssertPhaseCommittedAfterEvidence pins commit order in one arm: every phase
// row the arm published for a partition was committed by a later transaction
// than every evidence row the arm newly wrote for that partition.
func AssertPhaseCommittedAfterEvidence(t *testing.T, arm string, before, after TargetedDiffState) {
	t.Helper()
	for key, phase := range after {
		if phase.Table != "phase" {
			continue
		}
		if prior, ok := before[key]; ok && prior.Value == phase.Value {
			continue
		}
		for evidenceKey, evidence := range after {
			if evidence.Table != "evidence" || evidence.Partition != phase.Partition {
				continue
			}
			if _, existed := before[evidenceKey]; existed {
				continue
			}
			if evidence.Xmin >= phase.Xmin {
				t.Fatalf("%s arm: phase %s committed in xid %d, not after evidence %s in xid %d",
					arm, phase.Key, phase.Xmin, evidence.Key, evidence.Xmin)
			}
		}
	}
}

// PartitionSet builds a partition set from scope/generation pairs.
func PartitionSet(pairs ...string) map[backfill.ScopeGenerationPartition]struct{} {
	set := make(map[backfill.ScopeGenerationPartition]struct{}, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		set[backfill.ScopeGenerationPartition{ScopeID: pairs[i], GenerationID: pairs[i+1]}] = struct{}{}
	}
	return set
}

// PartitionsOf builds a partition slice from scope/generation pairs, in the
// given order.
func PartitionsOf(pairs ...string) []backfill.ScopeGenerationPartition {
	partitions := make([]backfill.ScopeGenerationPartition, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		partitions = append(partitions, backfill.ScopeGenerationPartition{ScopeID: pairs[i], GenerationID: pairs[i+1]})
	}
	return partitions
}

// HookBeginner wraps a beginner so a fixture can act at an exact point of a
// pass: before the nth Begin, or on a statement inside a transaction.
type HookBeginner struct {
	Inner   db.Beginner
	Begins  int
	OnBegin func(n int) error
	OnQuery func(query string, args []any) error
	OnExec  func(query string, args []any) error
}

// Begin implements db.Beginner.
func (h *HookBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	h.Begins++
	if h.OnBegin != nil {
		if err := h.OnBegin(h.Begins); err != nil {
			return nil, err
		}
	}
	tx, err := h.Inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return HookTransaction{Transaction: tx, Hooks: h}, nil
}

// HookTransaction runs the HookBeginner's statement hooks.
type HookTransaction struct {
	db.Transaction
	Hooks *HookBeginner
}

// QueryContext implements db.Queryer.
func (h HookTransaction) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if h.Hooks.OnQuery != nil {
		if err := h.Hooks.OnQuery(query, args); err != nil {
			return nil, err
		}
	}
	return h.Transaction.QueryContext(ctx, query, args...)
}

// ExecContext implements db.Executor.
func (h HookTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if h.Hooks.OnExec != nil {
		if err := h.Hooks.OnExec(query, args); err != nil {
			return nil, err
		}
	}
	return h.Transaction.ExecContext(ctx, query, args...)
}

// UnchangedTimestampOnly reports whether the arms may disagree on the row at
// key because the whole arm left a declared-unchanged partition alone. All of
// these must hold: the row exists in before, whole and targeted; its partition
// is in unchanged; it is a phase or memo row; the whole row is identical to
// before; and the targeted row differs only in timestamps: a phase value must be exactly two non-empty timestamps. A phase row's key
// carries every non-timestamp column (scope, acceptance unit, source run,
// generation, keyspace, phase) and its value is committed_at|updated_at. A
// memo row's value is catalog_fingerprint|committed_at, so the fingerprint
// must match and the targeted value must carry exactly one non-empty timestamp.
func UnchangedTimestampOnly(
	key string,
	before, whole, targeted TargetedDiffState,
	unchanged map[backfill.ScopeGenerationPartition]struct{},
) bool {
	wholeRow, inWhole := whole[key]
	targetedRow, inTargeted := targeted[key]
	beforeRow, inBefore := before[key]
	if !inWhole || !inTargeted || !inBefore {
		return false
	}
	if _, ok := unchanged[wholeRow.Partition]; !ok {
		return false
	}
	if wholeRow.Value != beforeRow.Value {
		return false
	}
	switch wholeRow.Table {
	case "phase":
		targetedCommitted, targetedUpdated, okTargeted := strings.Cut(targetedRow.Value, "|")
		return okTargeted && targetedCommitted != "" && targetedUpdated != "" &&
			!strings.Contains(targetedUpdated, "|")
	case "memo":
		wholeFingerprint, _, okWhole := strings.Cut(wholeRow.Value, "|")
		targetedFingerprint, targetedCommitted, okTargeted := strings.Cut(targetedRow.Value, "|")
		return okWhole && okTargeted && wholeFingerprint == targetedFingerprint &&
			targetedCommitted != "" && !strings.Contains(targetedCommitted, "|")
	}
	return false
}

// SortedPartitions renders a fixture partition set in stable scope/generation order.
func SortedPartitions(set map[backfill.ScopeGenerationPartition]struct{}) []backfill.ScopeGenerationPartition {
	result := make([]backfill.ScopeGenerationPartition, 0, len(set))
	for partition := range set {
		result = append(result, partition)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ScopeID != result[j].ScopeID {
			return result[i].ScopeID < result[j].ScopeID
		}
		return result[i].GenerationID < result[j].GenerationID
	})
	return result
}

// WorkIDs expands one generation into the fixture's ordered reducer work IDs.
func WorkIDs(generationID string, domains []string) []string {
	ids := make([]string, 0, len(domains))
	for _, domain := range domains {
		ids = append(ids, generationID+"/"+domain)
	}
	return ids
}
