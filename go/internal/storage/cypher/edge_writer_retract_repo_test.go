// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterRetractEdgesRepoDependencyDispatch(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 3; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source_repo:Repository") {
		t.Fatalf("cypher missing Repository match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: $repo_id})") {
		t.Fatalf("cypher missing indexed source Repository anchor: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: $repo_id})-[") {
		t.Fatalf("cypher expands relationships before completing indexed Repository anchor: %s", executor.calls[0].Cypher)
	}
	anchorIndex := strings.Index(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: $repo_id})")
	expansionIndex := strings.Index(executor.calls[0].Cypher, "MATCH (source_repo)-[rel:")
	if anchorIndex < 0 || expansionIndex < 0 || anchorIndex >= expansionIndex {
		t.Fatalf("cypher must anchor Repository before relationship expansion: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "MATCH (repo:Repository {id: $repo_id})") {
		t.Fatalf("cypher missing indexed RUNS_ON Repository anchor: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[2].Cypher, "HAS_DEPLOYMENT_EVIDENCE") {
		t.Fatalf("artifact retract cypher missing evidence edge: %s", executor.calls[2].Cypher)
	}
	if strings.Contains(executor.calls[2].Cypher, "MATCH (source_repo:Repository {id: $repo_id})-[") {
		t.Fatalf("artifact retract expands relationships before completing indexed Repository anchor: %s", executor.calls[2].Cypher)
	}
	if !strings.Contains(executor.calls[2].Cypher, "DETACH DELETE artifact") {
		t.Fatalf("artifact retract cypher missing DETACH DELETE: %s", executor.calls[2].Cypher)
	}
}

func TestEdgeWriterRetractEdgesRepoDependencyLogsStatementDurations(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.Logger = logger

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
		{IntentID: "i2", RepositoryID: "repo-b", Payload: map[string]any{"repo_id": "repo-b"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	entries := decodeJSONLogEntries(t, logs.Bytes())
	if got, want := len(entries), 3; got != want {
		t.Fatalf("log entries = %d, want %d\nlogs:\n%s", got, want, logs.String())
	}
	for i, call := range executor.calls {
		if _, ok := call.Parameters[StatementMetadataSummaryKey]; ok {
			t.Fatalf("executor call %d passed diagnostic statement summary to backend: %#v", i, call.Parameters)
		}
	}
	for i, wantRole := range []string{"repository_relationship_edges", "runs_on_relationships", "evidence_artifacts"} {
		entry := entries[i]
		if got, want := entry["msg"], "shared edge retract statement completed"; got != want {
			t.Fatalf("entry %d msg = %v, want %v", i, got, want)
		}
		if got, want := entry["domain"], reducer.DomainRepoDependency; got != want {
			t.Fatalf("entry %d domain = %v, want %v", i, got, want)
		}
		if got, want := entry["evidence_source"], "finalization/workloads"; got != want {
			t.Fatalf("entry %d evidence_source = %v, want %v", i, got, want)
		}
		if got := entry["statement_role"]; got != wantRole {
			t.Fatalf("entry %d statement_role = %v, want %v", i, got, wantRole)
		}
		if got, want := entry["repo_count"], float64(2); got != want {
			t.Fatalf("entry %d repo_count = %v, want %v", i, got, want)
		}
		if _, ok := entry["duration_seconds"]; !ok {
			t.Fatalf("entry %d missing duration_seconds: %v", i, entry)
		}
		if wantRole == "repository_relationship_edges" {
			summary, _ := entry["statement_summary"].(string)
			for _, wantRelationship := range []string{
				"DEPENDS_ON",
				"DEPLOYS_FROM",
				"DISCOVERS_CONFIG_IN",
				"PROVISIONS_DEPENDENCY_FOR",
				"USES_MODULE",
				"READS_CONFIG_FROM",
			} {
				if !strings.Contains(summary, wantRelationship) {
					t.Fatalf("entry %d statement_summary = %q, missing %s", i, summary, wantRelationship)
				}
			}
			if strings.Contains(summary, "RUNS_ON") {
				t.Fatalf("entry %d statement_summary = %q, must not include RUNS_ON", i, summary)
			}
		}
	}
}

func TestEdgeWriterRetractEdgesRepoDependencyLogsGroupedStatementRoles(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.Logger = logger

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 1; got != want {
		t.Fatalf("group calls = %d, want %d", got, want)
	}
	for i, stmt := range executor.groupCalls[0] {
		if _, ok := stmt.Parameters[StatementMetadataSummaryKey]; ok {
			t.Fatalf("group statement %d passed diagnostic statement summary to backend: %#v", i, stmt.Parameters)
		}
	}

	entries := decodeJSONLogEntries(t, logs.Bytes())
	if got, want := len(entries), 1; got != want {
		t.Fatalf("log entries = %d, want %d\nlogs:\n%s", got, want, logs.String())
	}
	entry := entries[0]
	if got, want := entry["msg"], "shared edge retract group completed"; got != want {
		t.Fatalf("msg = %v, want %v", got, want)
	}
	if got, want := entry["execution_mode"], "group"; got != want {
		t.Fatalf("execution_mode = %v, want %v", got, want)
	}
	rawSummaries, ok := entry["statement_summaries"].([]any)
	if !ok || len(rawSummaries) != 3 {
		t.Fatalf("statement_summaries = %#v, want three statement roles", entry["statement_summaries"])
	}
	for _, want := range []string{
		"role=repository_relationship_edges",
		"DEPLOYS_FROM",
		"DISCOVERS_CONFIG_IN",
		"PROVISIONS_DEPENDENCY_FOR",
		"USES_MODULE",
		"READS_CONFIG_FROM",
		"role=runs_on_relationships",
		"role=evidence_artifacts",
	} {
		found := false
		for _, raw := range rawSummaries {
			summary, _ := raw.(string)
			if strings.Contains(summary, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("statement_summaries = %#v, missing %q", rawSummaries, want)
		}
	}
}

func TestEdgeWriterRetractEdgesRepoDependencySingleRepoGroupUsesBoundDeleteShape(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "projection/code-imports")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.groupCalls), 1; got != want {
		t.Fatalf("group calls = %d, want %d", got, want)
	}
	group := executor.groupCalls[0]
	if got, want := len(group), 3; got != want {
		t.Fatalf("grouped repo dependency statements = %d, want %d", got, want)
	}
	repoRelationships := group[0]
	if strings.Contains(repoRelationships.Cypher, "UNWIND") {
		t.Fatalf("single-repo relationship retract must avoid UNWIND to stay on bound-delete path: %s", repoRelationships.Cypher)
	}
	if !strings.Contains(repoRelationships.Cypher, "MATCH (source_repo:Repository {id: $repo_id})") {
		t.Fatalf("single-repo relationship retract must use direct repo_id parameter: %s", repoRelationships.Cypher)
	}
	if _, ok := repoRelationships.Parameters["repo_id"]; !ok {
		t.Fatalf("single-repo relationship retract missing repo_id parameter: %#v", repoRelationships.Parameters)
	}
	if _, ok := repoRelationships.Parameters["repo_ids"]; ok {
		t.Fatalf("single-repo relationship retract must not pass repo_ids: %#v", repoRelationships.Parameters)
	}
	if !strings.Contains(group[1].Cypher, "RUNS_ON") {
		t.Fatalf("second grouped statement should retract RUNS_ON edges: %s", group[1].Cypher)
	}
}

func TestEdgeWriterRetractEdgesRepoDependencyDiagnosticStatementTimingBypassesGroup(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	executor := &recordingRepoDependencyGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)
	writer.Logger = logger
	writer.RepoDependencyRetractStatementTiming = true

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: map[string]any{"repo_id": "repo-a"}},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainRepoDependency, rows, "finalization/workloads")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(executor.groupCalls); got != 0 {
		t.Fatalf("group calls = %d, want 0 diagnostic grouped calls", got)
	}
	if got, want := len(executor.calls), 3; got != want {
		t.Fatalf("executor calls = %d, want %d diagnostic statement calls", got, want)
	}

	entries := decodeJSONLogEntries(t, logs.Bytes())
	if got, want := len(entries), 3; got != want {
		t.Fatalf("log entries = %d, want %d\nlogs:\n%s", got, want, logs.String())
	}
	for i, wantRole := range []string{"repository_relationship_edges", "runs_on_relationships", "evidence_artifacts"} {
		entry := entries[i]
		if got, want := entry["msg"], "shared edge retract statement completed"; got != want {
			t.Fatalf("entry %d msg = %v, want %v", i, got, want)
		}
		if got := entry["statement_role"]; got != wantRole {
			t.Fatalf("entry %d statement_role = %v, want %v", i, got, wantRole)
		}
	}
	if !strings.Contains(executor.calls[0].Cypher, "DEPENDS_ON|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|USES_MODULE|READS_CONFIG_FROM") {
		t.Fatalf("first diagnostic call must retract repository relationship edges: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "RUNS_ON") {
		t.Fatalf("first diagnostic call must not include RUNS_ON cleanup: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "RUNS_ON") {
		t.Fatalf("second diagnostic call must retract RUNS_ON edges: %s", executor.calls[1].Cypher)
	}
	if strings.Contains(executor.calls[1].Cypher, "DEPENDS_ON|DEPLOYS_FROM") {
		t.Fatalf("second diagnostic call must not include repository relationship cleanup: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[2].Cypher, "DETACH DELETE artifact") {
		t.Fatalf("third diagnostic call must retract evidence artifacts: %s", executor.calls[2].Cypher)
	}
}

func TestRepoDependencyRetractSummariesShareRelationshipEdgeTypes(t *testing.T) {
	t.Parallel()

	grouped := buildRepoDependencyRetractStatements([]string{"repo-a"}, "projection/code-imports")
	diagnostic := buildRepoDependencyDiagnosticRetractStatements([]string{"repo-a"}, "projection/code-imports")

	wantGroupedSummary := repoDependencyRetractSummary("repository_relationship_edges", repoDependencyRelationshipEdgeTypes)
	if got := grouped[0].stmt.Parameters[StatementMetadataSummaryKey]; got != wantGroupedSummary {
		t.Fatalf("grouped relationship summary = %v, want %s", got, wantGroupedSummary)
	}
	if got := grouped[1].stmt.Parameters[StatementMetadataSummaryKey]; got != "role=runs_on_relationships relationships=RUNS_ON" {
		t.Fatalf("grouped RUNS_ON summary = %v, want RUNS_ON role", got)
	}

	wantDiagnosticSummary := repoDependencyRetractSummary(
		"repository_relationship_edges",
		repoDependencyRelationshipEdgeTypes,
	)
	if got := diagnostic[0].stmt.Parameters[StatementMetadataSummaryKey]; got != wantDiagnosticSummary {
		t.Fatalf("diagnostic relationship summary = %v, want %s", got, wantDiagnosticSummary)
	}
	if strings.Contains(wantDiagnosticSummary, "RUNS_ON") {
		t.Fatalf("diagnostic repository relationship summary must not include RUNS_ON: %s", wantDiagnosticSummary)
	}
}

type recordingRepoDependencyGroupExecutor struct {
	calls      []Statement
	groupCalls [][]Statement
}

func (r *recordingRepoDependencyGroupExecutor) Execute(_ context.Context, statement Statement) error {
	r.calls = append(r.calls, statement)
	return nil
}

func (r *recordingRepoDependencyGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	cloned := make([]Statement, len(stmts))
	copy(cloned, stmts)
	r.groupCalls = append(r.groupCalls, cloned)
	return nil
}

func decodeJSONLogEntries(t *testing.T, raw []byte) []map[string]any {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("json.Unmarshal(%s) error = %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func statementRepoAnchorIDs(stmt Statement) []string {
	if repoID, ok := stmt.Parameters["repo_id"].(string); ok {
		return []string{repoID}
	}
	if repoIDs, ok := stmt.Parameters["repo_ids"].([]string); ok {
		return repoIDs
	}
	return nil
}
