// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// statementCaptureDB records every statement in order and answers the wake
// and satisfaction reads with empty results.
type statementCaptureDB struct {
	statements []capturedStatement
}

type capturedStatement struct {
	query string
	args  []any
}

func (d *statementCaptureDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	d.statements = append(d.statements, capturedStatement{query: query, args: args})
	return sharedIntentResult{}, nil
}

func (d *statementCaptureDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.statements = append(d.statements, capturedStatement{query: query, args: args})
	if strings.Contains(query, "reducer_waiting_items") {
		return &waitingWakeCountRows{}, nil
	}
	if query == exactPhaseWaitSatisfiedQuery {
		return &graphProjectionBoolRows{data: []bool{false}, idx: -1}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}

// ownGenerationPhaseRow is a generation's own phase row, the shape the
// cross-repo gate reads and the fan-in publishes.
func ownGenerationPhaseRow(scopeID, genID string, keyspace gpphase.Keyspace, phase gpphase.Phase) gpphase.PhaseState {
	now := time.Unix(0, 0).UTC()
	return gpphase.PhaseState{
		Key: gpphase.PhaseKey{
			ScopeID: scopeID, AcceptanceUnitID: scopeID, SourceRunID: genID,
			GenerationID: genID, Keyspace: keyspace,
		},
		Phase: phase, CommittedAt: now, UpdatedAt: now,
	}
}

// exactPhaseEnrollmentViolations reports every phase-exact deferral whose
// park key the phase Upsert would not wake: the publish of the
// generation's own (keyspace, phase) row must yield a wake key byte-equal to
// the key Fail parks on (waitingKeyFor), or the wait strands to the backstop.
func exactPhaseEnrollmentViolations(entries []WaitingDeferral, enrolled map[waitingPhaseRequirement]bool) []string {
	var out []string
	for _, entry := range entries {
		req := entry.phaseRequirement()
		if !req.exact() {
			continue
		}
		intent := reducercontract.Intent{ScopeID: "scope-guard", GenerationID: "gen-guard"}
		parkKey, ok := waitingKeyFor(intent, entry)
		if !ok {
			out = append(out, entry.FailureClass+": no park key")
			continue
		}
		row := ownGenerationPhaseRow("scope-guard", "gen-guard", entry.Keyspace, entry.Phase)
		keys := exactPhaseWakeKeys([]gpphase.PhaseState{row}, enrolled)
		if len(keys) != 1 || keys[0].keyJSON != parkKey {
			out = append(out, fmt.Sprintf("%s: park key %s, publish wakes %v", entry.FailureClass, parkKey, keys))
		}
	}
	return out
}

// TestExactPhaseDeferralsAreWokenByTheirPublish is the #11104 enrollment
// guard with its seeded RED/GREEN pair: GREEN on the shipped map; RED on a
// planted phase-exact deferral whose (keyspace, phase) the Upsert enrollment
// does not carry (a hand-kept list that forgot a new class).
func TestExactPhaseDeferralsAreWokenByTheirPublish(t *testing.T) {
	t.Parallel()

	if violations := exactPhaseEnrollmentViolations(WaitingDeferralMap(), exactPhaseEnrollment()); len(violations) != 0 {
		t.Fatalf("shipped map violations = %v, want none", violations)
	}

	planted := append([]WaitingDeferral{}, WaitingDeferralMap()...)
	planted = append(planted, WaitingDeferral{
		FailureClass: "planted_exact_phase_not_ready", WaitsForKind: WaitingKindPhaseCommitted,
		WakeSite: WaitingWakeSiteInTx, ProvidingSave: "planted",
		Keyspace: gpphase.KeyspaceServiceUID, Phase: gpphase.PhaseWorkloadMaterialization,
	})
	violations := exactPhaseEnrollmentViolations(planted, exactPhaseEnrollment())
	if len(violations) != 1 || !strings.Contains(violations[0], "planted_exact_phase_not_ready") {
		t.Fatalf("planted violations = %v, want exactly the planted class", violations)
	}
	if again := exactPhaseEnrollmentViolations(planted, exactPhaseEnrollmentFrom(planted)); len(again) != 0 {
		t.Fatalf("derived enrollment violations = %v, want none (the set derives from the map)", again)
	}
}

// TestBackwardEvidenceDeferralParksOnTheExactPhaseKey pins the registry row
// and the key: the cross-repo backward-evidence class parks phase-exact on the
// generation's own cross_repo_evidence/backward_evidence_committed key, which
// differs from the coarse key the generic phase Upsert wake matches.
func TestBackwardEvidenceDeferralParksOnTheExactPhaseKey(t *testing.T) {
	t.Parallel()

	entry, ok := WaitingDeferralByClass()[crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass]
	if !ok {
		t.Fatal("backward-evidence class has no deferral map row")
	}
	if entry.WaitsForKind != WaitingKindPhaseCommitted || entry.WakeSite != WaitingWakeSiteInTx {
		t.Fatalf("row kind/site = %s/%s, want phase_committed/in_tx", entry.WaitsForKind, entry.WakeSite)
	}
	key, ok := waitingKeyFor(reducercontract.Intent{ScopeID: "scope-x", GenerationID: "gen-x"}, entry)
	if !ok {
		t.Fatal("waitingKeyFor() ok = false")
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(key), &decoded); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	want := map[string]string{
		"scope_id": "scope-x", "generation_id": "gen-x",
		"keyspace": "cross_repo_evidence", "phase": "backward_evidence_committed",
	}
	if fmt.Sprint(decoded) != fmt.Sprint(want) {
		t.Fatalf("park key = %v, want %v", decoded, want)
	}
	if key == waitingScopeGenerationKey("scope-x", "gen-x") {
		t.Fatal("exact key equals the coarse key; the coarse wake would reach it")
	}
}

// TestPhaseUpsertStatementsForEnrolledAndOtherPhases pins the statement
// budget: a publish of non-enrolled phases sends exactly the shipped
// statements (one batch insert plus one coarse wake per scope generation),
// and only an enrolled own-generation row adds one exact wake statement.
func TestPhaseUpsertStatementsForEnrolledAndOtherPhases(t *testing.T) {
	t.Parallel()

	other := []gpphase.PhaseState{
		ownGenerationPhaseRow("scope-s", "gen-s", gpphase.KeyspaceCodeEntitiesUID, gpphase.PhaseCanonicalNodesCommitted),
		ownGenerationPhaseRow("scope-s", "gen-s", gpphase.KeyspaceCrossRepoEvidence, gpphase.PhaseDeploymentMapping),
	}
	capture := &statementCaptureDB{}
	if err := NewGraphProjectionPhaseStateStore(capture).Upsert(context.Background(), other); err != nil {
		t.Fatalf("Upsert(other) error = %v", err)
	}
	if len(capture.statements) != 2 {
		t.Fatalf("non-enrolled publish sent %d statements, want 2 (insert + coarse wake)", len(capture.statements))
	}
	if !strings.HasPrefix(strings.TrimSpace(capture.statements[0].query), "INSERT INTO graph_projection_phase_state") ||
		capture.statements[1].query != reopenWaitingRowsQuery ||
		capture.statements[1].args[2] != waitingScopeGenerationKey("scope-s", "gen-s") {
		t.Fatalf("non-enrolled statements = %v, want the shipped insert and coarse wake", capture.statements)
	}

	enrolled := append(other, ownGenerationPhaseRow("scope-s", "gen-s",
		gpphase.KeyspaceCrossRepoEvidence, gpphase.PhaseBackwardEvidenceCommitted))
	capture = &statementCaptureDB{}
	if err := NewGraphProjectionPhaseStateStore(capture).Upsert(context.Background(), enrolled); err != nil {
		t.Fatalf("Upsert(enrolled) error = %v", err)
	}
	if len(capture.statements) != 3 {
		t.Fatalf("enrolled publish sent %d statements, want 3 (insert + coarse wake + exact wake)", len(capture.statements))
	}
	exactKey, _ := waitingExactPhaseKey("scope-s", "gen-s", waitingPhaseRequirement{
		Keyspace: string(gpphase.KeyspaceCrossRepoEvidence), Phase: string(gpphase.PhaseBackwardEvidenceCommitted),
	})
	last := capture.statements[2]
	if last.query != reopenWaitingRowsQuery || last.args[1] != string(WaitingKindPhaseCommitted) || last.args[2] != exactKey {
		t.Fatalf("exact wake statement = %+v, want the reopen query on %s", last, exactKey)
	}

	// A foreign row of the enrolled phase (acceptance unit or source run not
	// the generation's own) names no parked key, so it sends nothing extra.
	foreign := ownGenerationPhaseRow("scope-s", "gen-s", gpphase.KeyspaceCrossRepoEvidence, gpphase.PhaseBackwardEvidenceCommitted)
	foreign.Key.SourceRunID = "run-other"
	if keys := exactPhaseWakeKeys([]gpphase.PhaseState{foreign}, exactPhaseEnrollment()); len(keys) != 0 {
		t.Fatalf("foreign row wake keys = %v, want none", keys)
	}
}

// TestWaitingSatisfiedRoutesExactPhaseToTheExactQuery pins the satisfaction
// routing: a phase-exact wait reads its own phase row with all six key
// columns, never the coarse any-phase branch of waitingSatisfiedQuery (which
// would satisfy at park time and spin).
func TestWaitingSatisfiedRoutesExactPhaseToTheExactQuery(t *testing.T) {
	t.Parallel()

	capture := &statementCaptureDB{}
	req := waitingPhaseRequirement{Keyspace: "cross_repo_evidence", Phase: "backward_evidence_committed"}
	if _, err := waitingSatisfied(context.Background(), capture, WaitingKindPhaseCommitted,
		"scope-r", "gen-r", "", "", "waiter-r", nil, nil, req, time.Now()); err != nil {
		t.Fatalf("waitingSatisfied() error = %v", err)
	}
	if len(capture.statements) != 1 || capture.statements[0].query != exactPhaseWaitSatisfiedQuery {
		t.Fatalf("statements = %v, want only the exact phase query", capture.statements)
	}
	if got := fmt.Sprint(capture.statements[0].args); got != "[scope-r gen-r cross_repo_evidence backward_evidence_committed]" {
		t.Fatalf("exact query args = %s", got)
	}
	for _, column := range []string{"acceptance_unit_id = $1", "source_run_id = $2", "keyspace = $3", "phase = $4"} {
		if !strings.Contains(exactPhaseWaitSatisfiedQuery, column) {
			t.Fatalf("exact query missing %q", column)
		}
	}
}
