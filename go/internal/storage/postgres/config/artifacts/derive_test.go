// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	contentartifacts "github.com/eshu-hq/eshu/go/internal/content/artifacts"
)

func supportedConfig() DeriverConfig {
	return DeriverConfig{
		ExtractorVersion: 3,
		Encoding:         Encoding{ServerEncoding: "UTF8", Supported: true},
		Fence:            Fence{Supported: true},
	}
}

// newTestDeriver builds a Deriver over fake with load in place of the
// production per-repository function.
func newTestDeriver(t *testing.T, fake *deriveFakeDB, config DeriverConfig, load loadFunc) *Deriver {
	t.Helper()
	deriver, err := NewDeriver(fake, config)
	if err != nil {
		t.Fatalf("NewDeriver() error = %v", err)
	}
	if load != nil {
		deriver.load = load
	}
	return deriver
}

// rowsLoad returns a load that builds the given rows without a statement.
func rowsLoad(rows ...map[string]any) loadFunc {
	return func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		return rows, contentartifacts.ReadStats{FilesListed: 7, CandidateCount: 4, CandidatesHydrated: 4}, nil
	}
}

func composeRow() map[string]any {
	return map[string]any{"path": "deploy/.env", "source_repo": "", "relative_path": "docker-compose.yml", "evidence_kind": "docker_compose_env_file"}
}

func ansibleRow() map[string]any {
	return map[string]any{"path": "inventory/hosts", "source_repo": "", "relative_path": "inventory/hosts", "evidence_kind": "ansible_inventory"}
}

// rowWriteStatements are the statements a failure path must never send.
var rowWriteStatements = []string{deleteRowsSQL, insertRowsSQL, upsertStateSQL}

func TestNewDeriverRejectsInvalidConfig(t *testing.T) {
	cases := map[string]struct {
		database Database
		config   DeriverConfig
	}{
		"nil database":       {nil, supportedConfig()},
		"zero extractor":     {&deriveFakeDB{}, DeriverConfig{}},
		"negative extractor": {&deriveFakeDB{}, DeriverConfig{ExtractorVersion: -1}},
		"negative timeout":   {&deriveFakeDB{}, DeriverConfig{ExtractorVersion: 1, Timeout: -time.Second}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDeriver(tc.database, tc.config); err == nil {
				t.Fatal("NewDeriver() error = nil, want an error")
			}
		})
	}
	deriver, err := NewDeriver(&deriveFakeDB{}, DeriverConfig{ExtractorVersion: 1})
	if err != nil {
		t.Fatalf("NewDeriver() error = %v", err)
	}
	if deriver.timeout != DefaultDeriveTimeout {
		t.Fatalf("default timeout = %v, want %v", deriver.timeout, DefaultDeriveTimeout)
	}
}

// TestDeriveStartupChecksShortCircuit: an unsupported encoding or fence skips
// the derive before any statement (decision record section 3.1, item 7).
func TestDeriveStartupChecksShortCircuit(t *testing.T) {
	cases := map[string]struct {
		encoding Encoding
		fence    Fence
		want     Outcome
	}{
		"encoding unsupported":  {Encoding{ServerEncoding: "LATIN1", Reason: ReasonEncodingUnsupported}, Fence{Supported: true}, OutcomeEncodingUnsupported},
		"encoding check failed": {Encoding{Reason: ReasonEncodingCheckFailed}, Fence{Supported: true}, OutcomeEncodingUnsupported},
		"fence missing":         {Encoding{Supported: true}, Fence{Reason: ReasonVersionFenceMissing}, OutcomeFenceMissing},
		"fence check failed":    {Encoding{Supported: true}, Fence{Reason: ReasonFenceCheckFailed}, OutcomeFenceMissing},
		"both unsupported":      {Encoding{Reason: ReasonEncodingUnsupported}, Fence{Reason: ReasonVersionFenceMissing}, OutcomeEncodingUnsupported},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &deriveFakeDB{}
			config := supportedConfig()
			config.Encoding, config.Fence = tc.encoding, tc.fence
			loaded := false
			deriver := newTestDeriver(t, fake, config, func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
				loaded = true
				return nil, contentartifacts.ReadStats{}, nil
			})
			result := deriver.DeriveRepository(context.Background(), Request{RepoID: "repo-a", Trigger: TriggerWrite})
			if result.Outcome != tc.want {
				t.Fatalf("Outcome = %q, want %q", result.Outcome, tc.want)
			}
			if got := fake.statements(); len(got) != 0 || fake.begins != 0 || loaded {
				t.Fatalf("statements = %v, begins = %d, loaded = %v; want no database work", got, fake.begins, loaded)
			}
			if result.Err != nil || result.RepoID != "repo-a" || result.Trigger != TriggerWrite {
				t.Fatalf("Result = %+v, want no error and the request echoed", result)
			}
		})
	}
}

// TestMayWriteConditions pins the three write conditions of protocol section
// 3, step 3, and the Proposed clause for a failure-only state row.
func TestMayWriteConditions(t *testing.T) {
	const ev = 3
	cases := []struct {
		name  string
		state *writeState
		v0    int64
		ev    int
		want  bool
	}{
		{"no state row", nil, 0, ev, true},
		{"failure-only row, older extractor", &writeState{extractorVersion: ev + 1}, 4, ev, true},
		{"failure-only row, NULL content_version only", &writeState{rowCount: intPtr(2), extractorVersion: ev}, 4, ev, true},
		{"newer version, same extractor", &writeState{contentVersion: int64Ptr(3), rowCount: intPtr(1), extractorVersion: ev}, 4, ev, true},
		{"newer version, newer derive extractor", &writeState{contentVersion: int64Ptr(3), rowCount: intPtr(1), extractorVersion: ev - 1}, 4, ev, true},
		{"newer version, older derive extractor", &writeState{contentVersion: int64Ptr(3), rowCount: intPtr(1), extractorVersion: ev + 1}, 4, ev, false},
		{"same version, same extractor", &writeState{contentVersion: int64Ptr(4), rowCount: intPtr(1), extractorVersion: ev}, 4, ev, false},
		{"same version, newer derive extractor", &writeState{contentVersion: int64Ptr(4), rowCount: intPtr(0), extractorVersion: ev - 1}, 4, ev, true},
		{"same version, older derive extractor", &writeState{contentVersion: int64Ptr(4), rowCount: intPtr(1), extractorVersion: ev + 1}, 4, ev, false},
		{"older version, newer derive extractor", &writeState{contentVersion: int64Ptr(5), rowCount: intPtr(1), extractorVersion: ev - 1}, 4, ev, false},
		{"older version, same extractor", &writeState{contentVersion: int64Ptr(5), rowCount: intPtr(1), extractorVersion: ev}, 4, ev, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mayWrite(tc.state, tc.v0, tc.ev); got != tc.want {
				t.Fatalf("mayWrite() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDeriveWritesInOneTransactionUnderTheLock: a writable derive reads v0
// outside any transaction, then locks, reads the state, deletes, inserts and
// upserts in one transaction, and commits.
func TestDeriveWritesInOneTransactionUnderTheLock(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(5)}
	deriver := newTestDeriver(t, fake, supportedConfig(), rowsLoad(composeRow(), ansibleRow()))
	result := deriver.DeriveRepository(context.Background(), Request{RepoID: "repo-a", ScopeID: "s", GenerationID: "g", Trigger: TriggerRepair})
	if result.Outcome != OutcomeOK || result.Err != nil {
		t.Fatalf("Result = %+v, want ok", result)
	}
	if result.ContentVersion != 5 || result.Rows != 2 || result.Stats.FilesListed != 7 {
		t.Fatalf("Result = %+v, want content version 5, 2 rows, 7 files listed", result)
	}
	want := []string{contentVersionSQL, repoLockSQL, readStateSQL, deleteRowsSQL, insertRowsSQL, upsertStateSQL}
	got := fake.statements()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("statements =\n%q\nwant\n%q", got, want)
	}
	for _, call := range fake.calls {
		if inTx := call.sql != contentVersionSQL; call.inTx != inTx {
			t.Fatalf("statement %.40q inTx = %v, want %v", call.sql, call.inTx, inTx)
		}
	}
	if fake.commits != 1 || fake.rollbacks != 0 {
		t.Fatalf("commits = %d, rollbacks = %d; want 1, 0", fake.commits, fake.rollbacks)
	}
	insert, _ := fake.callOf(insertRowsSQL)
	if !strings.Contains(strings.Join(argStrings(insert.args), " "), "deploy/.env") {
		t.Fatalf("insert args = %v, want the built paths", insert.args)
	}
	upsert, _ := fake.callOf(upsertStateSQL)
	if upsert.args[1] != int64(5) || upsert.args[2] != 3 || upsert.args[3] != 2 {
		t.Fatalf("state upsert args = %v, want content_version 5, extractor 3, row_count 2", upsert.args)
	}
}

func TestDeriveWithNoRowsSkipsTheInsert(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(1)}
	deriver := newTestDeriver(t, fake, supportedConfig(), rowsLoad())
	result := deriver.DeriveRepository(context.Background(), Request{RepoID: "repo-a", Trigger: TriggerWrite})
	if result.Outcome != OutcomeOK || fake.called(insertRowsSQL) || !fake.called(deleteRowsSQL) || !fake.called(upsertStateSQL) {
		t.Fatalf("Result = %+v, statements = %v; want ok with delete and state upsert but no insert", result, fake.statements())
	}
}

// TestDeriveSupersededRollsBack: a newer state row makes the derive roll back
// without touching rows or state.
func TestDeriveSupersededRollsBack(t *testing.T) {
	fake := &deriveFakeDB{
		version: int64Ptr(4),
		state:   &writeState{contentVersion: int64Ptr(6), rowCount: intPtr(9), extractorVersion: 3},
	}
	deriver := newTestDeriver(t, fake, supportedConfig(), rowsLoad(composeRow()))
	result := deriver.DeriveRepository(context.Background(), Request{RepoID: "repo-a", Trigger: TriggerWrite})
	if result.Outcome != OutcomeSuperseded || result.Err != nil {
		t.Fatalf("Result = %+v, want superseded", result)
	}
	for _, statement := range append(rowWriteStatements, recordFailureSQL) {
		if fake.called(statement) {
			t.Fatalf("superseded derive sent %.60q", statement)
		}
	}
	if fake.commits != 0 || fake.rollbacks != 1 {
		t.Fatalf("commits = %d, rollbacks = %d; want 0, 1", fake.commits, fake.rollbacks)
	}
}

// runFailure drives one failing derive and checks the failure-only update.
// rolledBack names row statements the derive may send inside its write
// transaction before that transaction fails and rolls back.
func runFailure(t *testing.T, fake *deriveFakeDB, config DeriverConfig, load loadFunc, rolledBack ...string) (Result, map[string]any) {
	t.Helper()
	var logs bytes.Buffer
	config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	deriver := newTestDeriver(t, fake, config, load)
	result := deriver.DeriveRepository(context.Background(), Request{RepoID: "repo-a", Trigger: TriggerBackfill})
	for _, statement := range rowWriteStatements {
		if fake.called(statement) && !slices.Contains(rolledBack, statement) {
			t.Fatalf("failed derive sent %.60q; a failure-only update never touches rows or content_version", statement)
		}
	}
	record, ok := fake.callOf(recordFailureSQL)
	if !ok || !record.inTx {
		t.Fatalf("statements = %v, want the failure-only update in a transaction", fake.statements())
	}
	if record.ctxErr != nil {
		t.Fatalf("failure update ran on a done context: %v", record.ctxErr)
	}
	if strings.Contains(recordFailureSQL, "content_version") {
		t.Fatal("recordFailureSQL names content_version; a failure-only update must not change it")
	}
	if !result.FailureRecorded {
		t.Fatalf("Result.FailureRecorded = false, want true (%+v)", result)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("log = %q, want one JSON record: %v", logs.String(), err)
	}
	if entry["level"] != "ERROR" || entry["event_name"] != "config_artifacts.derive.failed" || entry["repo_id"] != "repo-a" {
		t.Fatalf("log = %v, want ERROR config_artifacts.derive.failed with repo_id", entry)
	}
	if entry["trigger"] != "backfill" || entry["outcome"] != string(result.Outcome) || entry["error_class"] != result.ErrorClass {
		t.Fatalf("log = %v, want trigger, outcome and error_class of %+v", entry, result)
	}
	return result, entry
}

func TestDeriveLoadErrorRecordsFailure(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2)}
	result, _ := runFailure(t, fake, supportedConfig(), func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		return nil, contentartifacts.ReadStats{}, errFakeLoad
	})
	if result.Outcome != OutcomeError || result.ErrorClass != "error" || result.Err == nil || result.ContentVersion != 2 {
		t.Fatalf("Result = %+v, want error class error at content version 2", result)
	}
	record, _ := fake.callOf(recordFailureSQL)
	if record.args[0] != "repo-a" || record.args[1] != 3 || record.args[2] != "error" {
		t.Fatalf("failure args = %v, want repo-a, extractor 3, class error", record.args)
	}
}

func TestDeriveSQLStateErrorIsClassified(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2), failOn: map[string]error{"INSERT INTO config_artifact_paths": sqlStateError{code: "54000"}}}
	result, _ := runFailure(t, fake, supportedConfig(), rowsLoad(composeRow()), deleteRowsSQL, insertRowsSQL)
	if result.Outcome != OutcomeError || result.ErrorClass != "sqlstate_54000" {
		t.Fatalf("Result = %+v, want error class sqlstate_54000", result)
	}
	if fake.commits != 1 || fake.rollbacks < 1 {
		t.Fatalf("commits = %d, rollbacks = %d; want the write rolled back and the failure committed", fake.commits, fake.rollbacks)
	}
}

func TestDeriveRecoversPanic(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2)}
	result, entry := runFailure(t, fake, supportedConfig(), func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		panic("extractor bug")
	})
	if result.Outcome != OutcomePanic || result.ErrorClass != "panic" || !strings.Contains(result.Err.Error(), "extractor bug") {
		t.Fatalf("Result = %+v, want a recovered panic", result)
	}
	if stack, _ := entry["stack"].(string); !strings.Contains(stack, "derive") {
		t.Fatalf("log stack = %q, want the panic stack", stack)
	}
}

func TestDeriveUnknownEvidenceKindFails(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2)}
	row := map[string]any{"path": "x", "relative_path": "y", "evidence_kind": "not_a_kind"}
	result, _ := runFailure(t, fake, supportedConfig(), rowsLoad(row))
	if result.Outcome != OutcomeError || !strings.Contains(result.Err.Error(), "not_a_kind") {
		t.Fatalf("Result = %+v, want an error naming the unknown kind", result)
	}
}

// TestDeriveTimeoutRecordsFailureOnALiveContext: a load that runs past the
// deadline ends as timeout, and the failure-only update still runs.
func TestDeriveTimeoutRecordsFailureOnALiveContext(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2)}
	config := supportedConfig()
	config.Timeout = 20 * time.Millisecond
	result, _ := runFailure(t, fake, config, func(ctx context.Context, _ contentartifacts.Reader, _ contentartifacts.Repository, _ []contentartifacts.File, _ bool, _ int) ([]map[string]any, contentartifacts.ReadStats, error) {
		<-ctx.Done()
		return nil, contentartifacts.ReadStats{}, ctx.Err()
	})
	if result.Outcome != OutcomeTimeout || result.ErrorClass != "timeout" {
		t.Fatalf("Result = %+v, want timeout", result)
	}
}

// TestDeriveCPUBoundOverrunIsATimeout: an extractor that ignores the context
// and returns after the deadline does not write; the derive is a timeout.
func TestDeriveCPUBoundOverrunIsATimeout(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2)}
	config := supportedConfig()
	config.Timeout = 10 * time.Millisecond
	result, _ := runFailure(t, fake, config, func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		time.Sleep(40 * time.Millisecond)
		return []map[string]any{composeRow()}, contentartifacts.ReadStats{}, nil
	})
	if result.Outcome != OutcomeTimeout || fake.begins != 1 {
		t.Fatalf("Result = %+v, begins = %d; want timeout and only the failure transaction", result, fake.begins)
	}
}

func TestDeriveCanceledCallerIsAnError(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2)}
	ctx, cancel := context.WithCancel(context.Background())
	deriver := newTestDeriver(t, fake, supportedConfig(), func(context.Context, contentartifacts.Reader, contentartifacts.Repository, []contentartifacts.File, bool, int) ([]map[string]any, contentartifacts.ReadStats, error) {
		cancel()
		return nil, contentartifacts.ReadStats{}, context.Canceled
	})
	result := deriver.DeriveRepository(ctx, Request{RepoID: "repo-a", Trigger: TriggerWrite})
	if result.Outcome != OutcomeError || result.ErrorClass != "canceled" || !result.FailureRecorded {
		t.Fatalf("Result = %+v, want error class canceled with the failure recorded", result)
	}
}

func TestDeriveBlankRepoIDTouchesNothing(t *testing.T) {
	fake := &deriveFakeDB{}
	deriver := newTestDeriver(t, fake, supportedConfig(), rowsLoad())
	result := deriver.DeriveRepository(context.Background(), Request{RepoID: "  ", Trigger: TriggerWrite})
	if result.Outcome != OutcomeError || result.Err == nil || len(fake.statements()) != 0 {
		t.Fatalf("Result = %+v, statements = %v; want an error and no statement", result, fake.statements())
	}
}

func TestStoredRowsMapsNameScope(t *testing.T) {
	rows, err := storedRows([]map[string]any{composeRow(), ansibleRow()})
	if err != nil {
		t.Fatalf("storedRows() error = %v", err)
	}
	want := []Row{
		{RawPath: "deploy/.env", RelativePath: "docker-compose.yml", EvidenceKind: "docker_compose_env_file", NameScoped: true},
		{RawPath: "inventory/hosts", RelativePath: "inventory/hosts", EvidenceKind: "ansible_inventory", NameScoped: false},
	}
	if len(rows) != len(want) || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("storedRows() = %+v, want %+v", rows, want)
	}
	if _, err := storedRows([]map[string]any{{"path": "a", "relative_path": "b", "evidence_kind": "ansible_role", "source_repo": "named"}}); err == nil {
		t.Fatal("storedRows() accepted a named row; the stored build must be name-free")
	}
}

func argStrings(args []any) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if s, ok := arg.(interface{ String() string }); ok {
			out = append(out, s.String())
			continue
		}
		raw, _ := json.Marshal(arg)
		out = append(out, string(raw))
	}
	return out
}

// TestInsertRowsSQLDoesNotAbsorbAConflict: the row insert must raise a key
// conflict, not swallow it. A conflict on (repo_id, row_digest) means one
// derive holds a (raw_path, relative_path, evidence_kind) triple twice; with
// DO NOTHING the insert would store one row fewer than the row_count the
// state upsert records, a silent skew. The behaviour itself (SQLSTATE 23505,
// rollback, failure-only record) is proven against PostgreSQL by
// TestDeriveDuplicateRowDigestFailsLoudlyLive; this is the hermetic guard
// that the statement carries no conflict clause.
func TestInsertRowsSQLDoesNotAbsorbAConflict(t *testing.T) {
	if strings.Contains(strings.ToUpper(insertRowsSQL), "ON CONFLICT") {
		t.Fatalf("insertRowsSQL has a conflict clause; a duplicate row digest must fail the insert:\n%s", insertRowsSQL)
	}
}

// TestDeriveDuplicateRowDigestRecordsAFailureOnly: when the insert reports a
// duplicate key (SQLSTATE 23505) the step 3 transaction rolls back before the
// state upsert, and the derive records one failure of class sqlstate_23505.
func TestDeriveDuplicateRowDigestRecordsAFailureOnly(t *testing.T) {
	fake := &deriveFakeDB{version: int64Ptr(2), failOn: map[string]error{"INSERT INTO config_artifact_paths": sqlStateError{code: "23505"}}}
	result, _ := runFailure(t, fake, supportedConfig(), rowsLoad(composeRow(), composeRow()), deleteRowsSQL, insertRowsSQL)
	if result.Outcome != OutcomeError || result.ErrorClass != "sqlstate_23505" {
		t.Fatalf("Result = %+v, want error class sqlstate_23505", result)
	}
	if fake.called(upsertStateSQL) {
		t.Fatal("the state upsert ran after a failed insert")
	}
}
