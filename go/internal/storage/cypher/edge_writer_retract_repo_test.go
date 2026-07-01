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
	if got, want := len(executor.calls), 2; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if !strings.Contains(executor.calls[0].Cypher, "source_repo:Repository") {
		t.Fatalf("cypher missing Repository match: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "UNWIND $repo_ids AS repo_id") {
		t.Fatalf("cypher missing per-repo unwind anchor: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: repo_id})") {
		t.Fatalf("cypher missing indexed source Repository anchor: %s", executor.calls[0].Cypher)
	}
	if strings.Contains(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: repo_id})-[") {
		t.Fatalf("cypher expands relationships before completing indexed Repository anchor: %s", executor.calls[0].Cypher)
	}
	anchorIndex := strings.Index(executor.calls[0].Cypher, "MATCH (source_repo:Repository {id: repo_id})")
	expansionIndex := strings.Index(executor.calls[0].Cypher, "MATCH (source_repo)-[rel:")
	if anchorIndex < 0 || expansionIndex < 0 || anchorIndex >= expansionIndex {
		t.Fatalf("cypher must anchor Repository before relationship expansion: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[0].Cypher, "MATCH (repo:Repository {id: repo_id})") {
		t.Fatalf("cypher missing indexed RUNS_ON Repository anchor: %s", executor.calls[0].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "HAS_DEPLOYMENT_EVIDENCE") {
		t.Fatalf("artifact retract cypher missing evidence edge: %s", executor.calls[1].Cypher)
	}
	if strings.Contains(executor.calls[1].Cypher, "MATCH (source_repo:Repository {id: repo_id})-[") {
		t.Fatalf("artifact retract expands relationships before completing indexed Repository anchor: %s", executor.calls[1].Cypher)
	}
	if !strings.Contains(executor.calls[1].Cypher, "DETACH DELETE artifact") {
		t.Fatalf("artifact retract cypher missing DETACH DELETE: %s", executor.calls[1].Cypher)
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
	if got, want := len(entries), 2; got != want {
		t.Fatalf("log entries = %d, want %d\nlogs:\n%s", got, want, logs.String())
	}
	for i, call := range executor.calls {
		if _, ok := call.Parameters[StatementMetadataSummaryKey]; ok {
			t.Fatalf("executor call %d passed diagnostic statement summary to backend: %#v", i, call.Parameters)
		}
	}
	for i, wantRole := range []string{"repository_relationships", "evidence_artifacts"} {
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
		if wantRole == "repository_relationships" {
			summary, _ := entry["statement_summary"].(string)
			for _, wantRelationship := range []string{
				"DEPENDS_ON",
				"DEPLOYS_FROM",
				"DISCOVERS_CONFIG_IN",
				"PROVISIONS_DEPENDENCY_FOR",
				"USES_MODULE",
				"READS_CONFIG_FROM",
				"RUNS_ON",
			} {
				if !strings.Contains(summary, wantRelationship) {
					t.Fatalf("entry %d statement_summary = %q, missing %s", i, summary, wantRelationship)
				}
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
	if !ok || len(rawSummaries) != 2 {
		t.Fatalf("statement_summaries = %#v, want two statement roles", entry["statement_summaries"])
	}
	for _, want := range []string{
		"role=repository_relationships",
		"DEPLOYS_FROM",
		"DISCOVERS_CONFIG_IN",
		"PROVISIONS_DEPENDENCY_FOR",
		"USES_MODULE",
		"READS_CONFIG_FROM",
		"RUNS_ON",
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
