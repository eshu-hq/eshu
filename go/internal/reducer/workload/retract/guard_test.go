// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// fakeReader answers the guard's two existing-edge reads from a fixed graph:
// defines and endpoints map a repository id to the target ids its
// $evidence_source edges point at.
type fakeReader struct {
	defines   map[string][]string
	endpoints map[string][]string
	extraRows []map[string]any
	err       error
	calls     []recordedCall
}

func (f *fakeReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.calls = append(f.calls, recordedCall{cypher: cypher, params: params})
	if f.err != nil {
		return nil, f.err
	}
	source := f.endpoints
	if strings.Contains(cypher, ":DEFINES]") {
		source = f.defines
	}
	var rows []map[string]any
	for _, repoID := range params["repo_ids"].([]string) {
		for _, target := range source[repoID] {
			rows = append(rows, map[string]any{"repo_id": repoID, "target_id": target})
		}
	}
	return append(rows, f.extraRows...), nil
}

// deleteRows returns the (repo_id, target_id) pairs one delete statement bound.
func deleteRows(t *testing.T, call recordedCall) [][2]string {
	t.Helper()
	var pairs [][2]string
	for _, row := range call.params["rows"].([]map[string]any) {
		repoID, _ := row["repo_id"].(string)
		targetID, ok := row["target_id"].(string)
		if !ok {
			t.Fatalf("delete row %#v has no target_id, want an id-scoped delete", row)
		}
		pairs = append(pairs, [2]string{repoID, targetID})
	}
	return pairs
}

func TestRepositoryEdgesGuardIssuesNoDeleteWhenNothingIsStale(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		defines:   map[string][]string{"repository:a": {"workload:api"}},
		endpoints: map[string][]string{"repository:a": {"endpoint:1"}},
	}
	exec := &countingExecutor{deleted: 5}
	keep := KeepLists([]string{"repository:a", "repository:b"},
		map[string][]string{"repository:a": {"workload:api"}},
		map[string][]string{"repository:a": {"endpoint:1"}})

	result, err := RepositoryEdges(context.Background(), exec, reader, 500, keep, "finalization/workloads")
	if err != nil {
		t.Fatalf("RepositoryEdges() error = %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("delete statements = %d (%v), want none: nothing is stale", len(exec.calls), exec.calls)
	}
	if result.Mode != ModeGuarded || !result.Counted || result.StaleDefines != 0 || result.StaleEndpointEdges != 0 {
		t.Fatalf("result = %+v, want guarded, counted, zero stale", result)
	}
	if len(reader.calls) != 2 {
		t.Fatalf("reads = %d, want one DEFINES and one EXPOSES_ENDPOINT read", len(reader.calls))
	}
	for i, call := range reader.calls {
		if !reflect.DeepEqual(call.params["repo_ids"], []string{"repository:a", "repository:b"}) ||
			call.params["evidence_source"] != "finalization/workloads" {
			t.Fatalf("read %d params = %#v", i, call.params)
		}
		if !strings.Contains(call.cypher, "MATCH (repo:Repository {id: requested_repo_id})") {
			t.Fatalf("read %d cypher = %q, want the repository-id anchor", i, call.cypher)
		}
	}
}

func TestRepositoryEdgesGuardDeletesExactlyTheStaleTargets(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{
		defines: map[string][]string{
			"repository:a": {"workload:worker", "workload:api", "workload:billing", "workload:billing"},
			"repository:b": {"workload:b-api"},
		},
		endpoints: map[string][]string{"repository:a": {"endpoint:1", "endpoint:2"}},
		// A row for a repository this pass does not own, and blank ids, are
		// never deleted even if an over-broad read returns them.
		extraRows: []map[string]any{
			{"repo_id": "repository:other", "target_id": "workload:other"},
			{"repo_id": "repository:a", "target_id": ""},
			{"repo_id": "", "target_id": "workload:x"},
		},
	}
	exec := &countingExecutor{deleted: 1}
	keep := KeepLists([]string{"repository:a", "repository:b"},
		map[string][]string{"repository:a": {"workload:api"}, "repository:b": {"workload:b-api"}},
		map[string][]string{"repository:a": {"endpoint:1"}})

	result, err := RepositoryEdges(context.Background(), exec, reader, 500, keep, "finalization/workloads")
	if err != nil {
		t.Fatalf("RepositoryEdges() error = %v", err)
	}
	if len(exec.calls) != 2 {
		t.Fatalf("delete statements = %d, want one DEFINES and one EXPOSES_ENDPOINT delete", len(exec.calls))
	}
	// Dropping the keep-list would put workload:api, workload:b-api or
	// endpoint:1 in these rows.
	wantDefines := [][2]string{{"repository:a", "workload:billing"}, {"repository:a", "workload:worker"}}
	if got := deleteRows(t, exec.calls[0]); !reflect.DeepEqual(got, wantDefines) {
		t.Fatalf("DEFINES delete rows = %v, want %v", got, wantDefines)
	}
	if got := deleteRows(t, exec.calls[1]); !reflect.DeepEqual(got, [][2]string{{"repository:a", "endpoint:2"}}) {
		t.Fatalf("EXPOSES_ENDPOINT delete rows = %v, want [[repository:a endpoint:2]]", got)
	}
	for i, shape := range []string{guardedDefinesCypher, guardedEndpointCypher} {
		if exec.calls[i].cypher != shape || exec.calls[i].params["evidence_source"] != "finalization/workloads" {
			t.Fatalf("delete %d = %q %#v, want the id-anchored guarded delete", i, exec.calls[i].cypher, exec.calls[i].params)
		}
	}
	if result.Mode != ModeGuarded || result.StaleDefines != 2 || result.StaleEndpointEdges != 1 ||
		result.DefinesDeleted != 1 || result.EndpointEdgesDeleted != 1 {
		t.Fatalf("result = %+v, want guarded, 2 + 1 stale, 1 + 1 deleted", result)
	}
}

func TestRepositoryEdgesGuardBatchesStaleDeletes(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{defines: map[string][]string{
		"repository:a": {"workload:1", "workload:2", "workload:3"},
	}}
	exec := &recordingExecutor{}
	keep := []KeepList{{RepoID: "repository:a"}}
	if _, err := RepositoryEdges(context.Background(), exec, reader, 2, keep, "src"); err != nil {
		t.Fatalf("RepositoryEdges() error = %v", err)
	}
	if len(exec.calls) != 2 || len(deleteRows(t, exec.calls[0])) != 2 || len(deleteRows(t, exec.calls[1])) != 1 {
		t.Fatalf("delete batches = %v, want 2 then 1 rows", exec.calls)
	}
}

func TestRepositoryEdgesGuardFailsTowardDeleting(t *testing.T) {
	t.Parallel()

	keep := KeepLists([]string{"repository:a"}, map[string][]string{"repository:a": {"workload:api"}}, nil)
	for name, tc := range map[string]struct {
		reader Reader
		mode   string
	}{
		"no reader":   {nil, ModeUnguardedNoReader},
		"read failed": {&fakeReader{err: errors.New("graph read timeout")}, ModeUnguardedReadFailed},
	} {
		exec := &recordingExecutor{}
		result, err := RepositoryEdges(context.Background(), exec, tc.reader, 500, keep, "src")
		if err != nil {
			t.Fatalf("%s: error = %v, want the keep-list delete to run", name, err)
		}
		if result.Mode != tc.mode || len(exec.calls) != 2 ||
			exec.calls[0].cypher != definesCypher || exec.calls[1].cypher != endpointCypher {
			t.Fatalf("%s: mode %q, %d statements; want %q and both keep-list deletes", name, result.Mode, len(exec.calls), tc.mode)
		}
		if tc.mode == ModeUnguardedReadFailed && (result.ReadErr == nil || !strings.Contains(result.ReadErr.Error(), "graph read timeout")) {
			t.Fatalf("%s: ReadErr = %v, want the read failure kept for the log", name, result.ReadErr)
		}
	}
}

func TestRepositoryEdgesGuardSurfacesDeleteFailure(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{endpoints: map[string][]string{"repository:a": {"endpoint:gone"}}}
	exec := &recordingExecutor{errOnCall: 1, err: errors.New("boom")}
	_, err := RepositoryEdges(context.Background(), exec, reader, 500, []KeepList{{RepoID: "repository:a"}}, "src")
	if err == nil || !strings.Contains(err.Error(), "retract stale repository endpoint edges: boom") {
		t.Fatalf("error = %v, want the endpoint delete failure", err)
	}
}

// TestObserveLogsGuardModeAndReadFailure pins the operator log: the retract
// mode and stale counts on every run, and a warning when the guard fell back
// to the unguarded deletes: with the read error after a failed read, and
// without one when no reader was wired, which in production means the
// composition root dropped the guard. It swaps the default logger, so it must
// not run in parallel.
func TestObserveLogsGuardModeAndReadFailure(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	keep := []KeepList{{RepoID: "repository:a"}}
	Observe(context.Background(), nil, Run{ScopeID: "scope", GenerationID: "gen"}, keep,
		Result{Repositories: 1, Mode: ModeGuarded, StaleDefines: 2, StaleEndpointEdges: 1, Counted: true}, 0)
	Observe(context.Background(), nil, Run{ScopeID: "scope", GenerationID: "gen"}, keep,
		Result{Repositories: 1, Mode: ModeUnguardedReadFailed, ReadErr: errors.New("graph read timeout"), Counted: true}, 0)
	Observe(context.Background(), nil, Run{ScopeID: "scope", GenerationID: "gen"}, keep,
		Result{Repositories: 1, Mode: ModeUnguardedNoReader, Counted: true}, 0)

	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	if len(records) != 3 {
		t.Fatalf("log lines = %d, want 3", len(records))
	}
	guarded, fallback, noReader := records[0], records[1], records[2]
	if guarded["level"] != "INFO" || guarded["retract_mode"] != ModeGuarded ||
		guarded["stale_defines"] != float64(2) || guarded["stale_repository_endpoint_edges"] != float64(1) {
		t.Fatalf("guarded log = %v, want INFO, guarded mode, 2 + 1 stale", guarded)
	}
	if fallback["level"] != "WARN" || fallback["retract_mode"] != ModeUnguardedReadFailed ||
		fallback["read_error"] != "graph read timeout" {
		t.Fatalf("fallback log = %v, want WARN with the read error", fallback)
	}
	if noReader["level"] != "WARN" || noReader["retract_mode"] != ModeUnguardedNoReader {
		t.Fatalf("no-reader log = %v, want WARN: a missing guard in production is a wiring defect", noReader)
	}
}

// TestObserveLogsIntentAndSkippedMode pins the #7285 retract-scope log
// contract: every line names its intent and entity keys, so an operator can
// match a retract to the intent that ran it, and a retract skipped for lack
// of scope truth warns, because its stale edges stay until a later run. It
// swaps the default logger, so it must not run in parallel.
func TestObserveLogsIntentAndSkippedMode(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	run := Run{ScopeID: "scope", GenerationID: "gen", IntentID: "intent-1", EntityKeys: []string{"repo:repository:b"}}
	Observe(context.Background(), nil, run, nil, Result{Repositories: 2, Mode: ModeSkippedNoScopeTruth}, 0)

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("decode log line %q: %v", buf.Bytes(), err)
	}
	keys, _ := record["entity_keys"].([]any)
	if record["level"] != "WARN" || record["retract_mode"] != ModeSkippedNoScopeTruth ||
		record["intent_id"] != "intent-1" || len(keys) != 1 || keys[0] != "repo:repository:b" ||
		record["repository_count"] != float64(2) {
		t.Fatalf("skipped log = %v, want WARN, skipped mode, the intent id and its entity keys", record)
	}
}
