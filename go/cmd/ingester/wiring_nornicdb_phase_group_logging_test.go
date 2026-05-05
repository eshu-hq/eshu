package main

import (
	"bytes"
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBPhaseGroupExecutorLogsEntityLabelSummaries(t *testing.T) {
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN function1",
			Parameters: map[string]any{
				"_eshu_phase":         "entities",
				"_eshu_entity_label":  "Function",
				"_eshu_scope_id":      "scope-a",
				"_eshu_generation_id": "gen-a",
				"rows": []map[string]any{
					{"entity_id": "f1"},
					{"entity_id": "f2"},
				},
			},
		},
		{
			Cypher: "RETURN function2",
			Parameters: map[string]any{
				"_eshu_phase":         "entities",
				"_eshu_entity_label":  "Function",
				"_eshu_scope_id":      "scope-a",
				"_eshu_generation_id": "gen-a",
				"rows": []map[string]any{
					{"entity_id": "f3"},
				},
			},
		},
		{
			Cypher: "RETURN variable-singleton",
			Parameters: map[string]any{
				"_eshu_phase":             "entities",
				"_eshu_entity_label":      "Variable",
				"_eshu_phase_group_mode":  "execute_only",
				"_eshu_statement_summary": "label=Variable rows=1 entity_id=v1 fallback=singleton_parameterized",
				"entity_id":               "v1",
				"props":                   map[string]any{"name": "v1"},
			},
		},
		{
			Cypher: "RETURN variable-grouped",
			Parameters: map[string]any{
				"_eshu_phase":        "entities",
				"_eshu_entity_label": "Variable",
				"rows": []map[string]any{
					{"entity_id": "v2"},
					{"entity_id": "v3"},
					{"entity_id": "v4"},
				},
			},
		},
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	functionLine := findEntityLabelSummaryLine(t, logs.String(), "Function")
	if !strings.Contains(functionLine, `"rows":3`) {
		t.Fatalf("Function summary line = %q, want rows=3", functionLine)
	}
	if !strings.Contains(functionLine, `"statements":2`) {
		t.Fatalf("Function summary line = %q, want statements=2", functionLine)
	}
	if !strings.Contains(functionLine, `"grouped_chunks":1`) {
		t.Fatalf("Function summary line = %q, want grouped_chunks=1", functionLine)
	}
	if !strings.Contains(functionLine, `"singleton_statements":0`) {
		t.Fatalf("Function summary line = %q, want singleton_statements=0", functionLine)
	}
	if !strings.Contains(functionLine, `"scope_id":"scope-a"`) {
		t.Fatalf("Function summary line = %q, want scope_id", functionLine)
	}
	if !strings.Contains(functionLine, `"generation_id":"gen-a"`) {
		t.Fatalf("Function summary line = %q, want generation_id", functionLine)
	}

	variableLine := findEntityLabelSummaryLine(t, logs.String(), "Variable")
	if !strings.Contains(variableLine, `"rows":4`) {
		t.Fatalf("Variable summary line = %q, want rows=4", variableLine)
	}
	if !strings.Contains(variableLine, `"statements":2`) {
		t.Fatalf("Variable summary line = %q, want statements=2", variableLine)
	}
	if !strings.Contains(variableLine, `"grouped_chunks":1`) {
		t.Fatalf("Variable summary line = %q, want grouped_chunks=1", variableLine)
	}
	if !strings.Contains(variableLine, `"singleton_statements":1`) {
		t.Fatalf("Variable summary line = %q, want singleton_statements=1", variableLine)
	}
}

func TestNornicDBPhaseGroupExecutorLogsEntityContainmentLabelSummaries(t *testing.T) {
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN containment1",
			Parameters: map[string]any{
				"_eshu_phase":        sourcecypher.CanonicalPhaseEntityContainment,
				"_eshu_entity_label": "Function",
				"rows": []map[string]any{
					{"entity_id": "f1", "file_path": "/repo/a.go"},
					{"entity_id": "f2", "file_path": "/repo/b.go"},
				},
			},
		},
		{
			Cypher: "RETURN containment2",
			Parameters: map[string]any{
				"_eshu_phase":        sourcecypher.CanonicalPhaseEntityContainment,
				"_eshu_entity_label": "Function",
				"rows": []map[string]any{
					{"entity_id": "f3", "file_path": "/repo/c.go"},
				},
			},
		},
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	line := findEntityLabelSummaryLineWithPhase(t, logs.String(), "Function", sourcecypher.CanonicalPhaseEntityContainment, true)
	if !strings.Contains(line, `"rows":3`) {
		t.Fatalf("Function containment summary line = %q, want rows=3", line)
	}
	if !strings.Contains(line, `"statements":2`) {
		t.Fatalf("Function containment summary line = %q, want statements=2", line)
	}
}

func TestNornicDBPhaseGroupExecutorLogsRollingEntityLabelSummaries(t *testing.T) {
	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:               inner,
		maxStatements:       5,
		entityMaxStatements: 1,
	}

	stmts := make([]sourcecypher.Statement, 0, defaultNornicDBEntityLabelSummaryExecutions+1)
	for i := 0; i < defaultNornicDBEntityLabelSummaryExecutions+1; i++ {
		stmts = append(stmts, sourcecypher.Statement{
			Cypher: "RETURN variable",
			Parameters: map[string]any{
				"_eshu_phase":        "entities",
				"_eshu_entity_label": "Variable",
				"rows": []map[string]any{
					{"entity_id": "v"},
				},
			},
		})
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	progressLine := findEntityLabelSummaryLineWithCompletion(t, logs.String(), "Variable", false)
	if !strings.Contains(progressLine, `"executions":10`) {
		t.Fatalf("Variable progress line = %q, want executions=10", progressLine)
	}
	if !strings.Contains(progressLine, `"rows":10`) {
		t.Fatalf("Variable progress line = %q, want rows=10", progressLine)
	}

	finalLine := findEntityLabelSummaryLineWithCompletion(t, logs.String(), "Variable", true)
	if !strings.Contains(finalLine, `"executions":11`) {
		t.Fatalf("Variable final line = %q, want executions=11", finalLine)
	}
	if !strings.Contains(finalLine, `"rows":11`) {
		t.Fatalf("Variable final line = %q, want rows=11", finalLine)
	}
}

func TestNornicDBPhaseGroupExecutorStripsDiagnosticStatementParamsBeforeDriver(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := nornicDBPhaseGroupExecutor{
		inner:         inner,
		maxStatements: 2,
	}

	stmts := []sourcecypher.Statement{
		{
			Cypher: "RETURN 1",
			Parameters: map[string]any{
				"rows":                    []map[string]any{{"entity_id": "one"}},
				"_eshu_phase":             "entities",
				"_eshu_statement_summary": "label=Function rows=1 entity_id=one",
			},
		},
	}

	if err := executor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}
	if got, want := len(inner.groupParams), 1; got != want {
		t.Fatalf("group params count = %d, want %d", got, want)
	}
	if _, ok := inner.groupParams[0]["_eshu_statement_summary"]; ok {
		t.Fatalf("group params include diagnostic summary: %#v", inner.groupParams[0])
	}
	if _, ok := inner.groupParams[0]["_eshu_phase"]; ok {
		t.Fatalf("group params include phase diagnostic: %#v", inner.groupParams[0])
	}
	if got, want := inner.groupParams[0]["rows"], stmts[0].Parameters["rows"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("group rows param = %#v, want %#v", got, want)
	}
}

func findEntityLabelSummaryLine(t *testing.T, logs string, label string) string {
	t.Helper()
	return findEntityLabelSummaryLineWithCompletion(t, logs, label, true)
}

func findEntityLabelSummaryLineWithCompletion(t *testing.T, logs string, label string, complete bool) string {
	t.Helper()
	return findEntityLabelSummaryLineWithPhase(t, logs, label, "", complete)
}

func findEntityLabelSummaryLineWithPhase(t *testing.T, logs string, label string, phase string, complete bool) string {
	t.Helper()

	completeToken := `"complete":false`
	if complete {
		completeToken = `"complete":true`
	}
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.Contains(line, `"msg":"nornicdb entity label summary"`) &&
			strings.Contains(line, `"label":"`+label+`"`) &&
			(phase == "" || strings.Contains(line, `"phase":"`+phase+`"`)) &&
			strings.Contains(line, completeToken) {
			return line
		}
	}
	t.Fatalf("entity label summary for %q phase=%q complete=%t not found in logs:\n%s", label, phase, complete, logs)
	return ""
}

func TestCanonicalExecutorForGraphBackendAllowsNornicDBGroupedWhenConformanceEnabled(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		true,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	ge, ok := executor.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatal("NornicDB canonical executor does not implement GroupExecutor when conformance grouped writes are enabled")
	}

	err := ge.ExecuteGroup(context.Background(), []sourcecypher.Statement{{Cypher: "RETURN 1"}})
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil", err)
	}
	if inner.groupCalls != 1 {
		t.Fatalf("inner ExecuteGroup calls = %d, want 1", inner.groupCalls)
	}
}

func TestCanonicalExecutorForGraphBackendNornicDBGroupedFullStackReachesRawExecutor(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		true,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	if _, ok := executor.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("NornicDB grouped executor stack does not implement GroupExecutor")
	}

	writer := sourcecypher.NewCanonicalNodeWriter(executor, 0, nil)
	err := writer.Write(context.Background(), minimalCanonicalMaterialization())
	if err != nil {
		t.Fatalf("CanonicalNodeWriter.Write() error = %v, want nil", err)
	}
	if inner.groupCalls != 1 {
		t.Fatalf("raw ExecuteGroup calls = %d, want 1", inner.groupCalls)
	}
	if inner.executeCalls != 0 {
		t.Fatalf("raw Execute calls = %d, want 0 for grouped path", inner.executeCalls)
	}
}

func TestCanonicalExecutorForGraphBackendNornicDBDefaultFullStackUsesPhaseGroups(t *testing.T) {
	t.Parallel()

	inner := &groupCapableIngesterExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	if _, ok := executor.(sourcecypher.PhaseGroupExecutor); !ok {
		t.Fatal("NornicDB default executor stack does not implement PhaseGroupExecutor")
	}

	writer := sourcecypher.NewCanonicalNodeWriter(executor, 0, nil)
	err := writer.Write(context.Background(), minimalCanonicalMaterialization())
	if err != nil {
		t.Fatalf("CanonicalNodeWriter.Write() error = %v, want nil", err)
	}
	if inner.groupCalls == 0 {
		t.Fatal("raw ExecuteGroup calls = 0, want phase-group usage")
	}
	if inner.executeCalls == 0 {
		t.Fatal("raw Execute calls = 0, want sequential retract execution on the phase-group path")
	}
}

func TestNornicDBBatchedEntityContainmentFullStackUsesCrossFileBatchedEntityRows(t *testing.T) {
	t.Parallel()

	inner := &recordingGroupChunkExecutor{}
	executor := canonicalExecutorForGraphBackend(
		inner,
		runtimecfg.GraphBackendNornicDB,
		0,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		nil,
		nil,
	)
	writer := sourcecypher.NewCanonicalNodeWriter(executor, 0, nil).
		WithBatchedEntityContainmentInEntityUpsert()

	mat := minimalCanonicalMaterialization()
	mat.FirstGeneration = true
	mat.Files = []projector.FileRow{
		{Path: "/repos/my-repo/src/a.go", RelativePath: "src/a.go", Name: "a.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		{Path: "/repos/my-repo/src/b.go", RelativePath: "src/b.go", Name: "b.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
	}
	mat.Entities = []projector.EntityRow{
		{EntityID: "entity-1", Label: "Function", EntityName: "a", FilePath: "/repos/my-repo/src/a.go", RelativePath: "src/a.go", StartLine: 1, EndLine: 2, Language: "go", RepoID: "repo-1"},
		{EntityID: "entity-2", Label: "Function", EntityName: "b", FilePath: "/repos/my-repo/src/b.go", RelativePath: "src/b.go", StartLine: 3, EndLine: 4, Language: "go", RepoID: "repo-1"},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("CanonicalNodeWriter.Write() error = %v, want nil", err)
	}

	entityStatements := statementsContaining(inner.groupStatements, "MERGE (n:Function {uid: row.entity_id})")
	if got, want := len(entityStatements), 1; got != want {
		t.Fatalf("batched Function entity statements = %d, want %d", got, want)
	}
	stmt := entityStatements[0]
	for _, want := range []string{
		"MERGE (n:Function {uid: row.entity_id})",
		"SET n += row.props",
		"MATCH (f:File {path: row.file_path})",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("entity cypher = %q, want %q", stmt.Cypher, want)
		}
	}
	if strings.Contains(stmt.Cypher, "MATCH (f:File {path: $file_path})") {
		t.Fatalf("entity cypher = %q, want row-scoped file_path instead of statement file_path", stmt.Cypher)
	}
	if _, ok := stmt.Parameters["file_path"]; ok {
		t.Fatalf("entity statement unexpectedly carries statement-level file_path: %#v", stmt.Parameters)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows count = %d, want %d", got, want)
	}
	if got := rows[0]["file_path"]; got != "/repos/my-repo/src/a.go" {
		t.Fatalf("rows[0] file_path = %#v, want /repos/my-repo/src/a.go", got)
	}
	if got := rows[1]["file_path"]; got != "/repos/my-repo/src/b.go" {
		t.Fatalf("rows[1] file_path = %#v, want /repos/my-repo/src/b.go", got)
	}
	if got := len(statementsContaining(inner.executeStatements, "MERGE (n:Function {uid: row.entity_id})")); got != 0 {
		t.Fatalf("sequential Function entity statements = %d, want 0", got)
	}
}
