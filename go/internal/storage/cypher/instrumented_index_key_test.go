// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// oversizedIndexValue is above both Neo4j limits measured for #7058 (8164
// bytes for a single string key, 8151 for a (string, string, int) key).
var oversizedIndexValue = strings.Repeat("v", 9000)

func newIndexKeyTestInstruments(t *testing.T) (*telemetry.Instruments, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

func oversizedSkips(t *testing.T, reader *metric.ManualReader, label string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return metricCounterValue(t, rm, "eshu_dp_graph_oversized_index_keys_skipped_total", telemetry.MetricDimensionNodeLabel, label)
}

// statementsCarry reports whether any recorded statement parameter still
// holds needle.
func statementsCarry(t *testing.T, stmts []Statement, needle string) bool {
	t.Helper()
	for _, stmt := range stmts {
		encoded, err := json.Marshal(stmt.Parameters)
		if err != nil {
			t.Fatalf("marshal parameters: %v", err)
		}
		if strings.Contains(string(encoded), needle) {
			return true
		}
	}
	return false
}

func recordedStatements(r *recordingGroupExecutor) []Statement {
	all := append([]Statement(nil), r.executeCalls...)
	for _, group := range r.groupCalls {
		all = append(all, group...)
	}
	return all
}

func semanticIndexKeyWrite() semantic.EntityWrite {
	row := func(id, typ, name string) semantic.EntityRow {
		return semantic.EntityRow{
			RepoID: "repo-1", EntityID: id, EntityType: typ, EntityName: name,
			FilePath: "/repo/src/a.ts", RelativePath: "src/a.ts", Language: "typescript",
			StartLine: 1, EndLine: 2,
		}
	}
	return semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []semantic.EntityRow{
			row("fn-ok", "Function", "render"),
			row("fn-big", "Function", oversizedIndexValue),
			row("ann-big", "Annotation", oversizedIndexValue),
			row("var-big", "Variable", oversizedIndexValue),
		},
	}
}

// TestSemanticEntityWriterSkipsOversizedIndexKeysInEveryWriteMode is the F1
// regression: the reducer's semantic-entity writer (legacy rows on Neo4j,
// canonical-node rows on NornicDB) MERGEs Function, Annotation, Variable and
// other labels from facts, outside the canonical materialization guard. Every
// write mode must reach the backend without the oversized rows once it runs
// through InstrumentedExecutor, as it does in production.
func TestSemanticEntityWriterSkipsOversizedIndexKeysInEveryWriteMode(t *testing.T) {
	modes := map[string]func(Executor, int) *SemanticEntityWriter{
		"legacy_rows":         NewSemanticEntityWriter,
		"parameterized_rows":  NewSemanticEntityWriterWithParameterizedRows,
		"batched_properties":  NewSemanticEntityWriterWithBatchedProperties,
		"merge_first_rows":    NewSemanticEntityWriterWithMergeFirstRows,
		"canonical_node_rows": NewSemanticEntityWriterWithCanonicalNodeRows,
	}
	for name, newWriter := range modes {
		t.Run(name, func(t *testing.T) {
			instruments, reader := newIndexKeyTestInstruments(t)
			recorder := &recordingGroupExecutor{}
			writer := newWriter(&InstrumentedExecutor{Inner: recorder, Instruments: instruments}, 0)

			if _, err := writer.WriteSemanticEntities(context.Background(), semanticIndexKeyWrite()); err != nil {
				t.Fatalf("WriteSemanticEntities() error = %v", err)
			}
			stmts := recordedStatements(recorder)
			if statementsCarry(t, stmts, oversizedIndexValue) {
				t.Fatal("an oversized semantic entity value reached the backend")
			}
			if !statementsCarry(t, stmts, `"render"`) {
				t.Fatal("the normal Function row is missing from the write")
			}
			for _, label := range []string{"Function", "Annotation", "Variable"} {
				if got := oversizedSkips(t, reader, label); got != 1 {
					t.Errorf("skips{node_label=%s} = %d, want 1", label, got)
				}
			}
		})
	}
}

// TestCanonicalNodeWriterSkipsOversizedTerraformStateAndKindKeys is the F2
// regression: TerraformStateResource.address, TerraformModule (name, path)
// and the kind slot of the K8sResource composite key ride the same atomic
// canonical write but were outside the canonical guard.
func TestCanonicalNodeWriterSkipsOversizedTerraformStateAndKindKeys(t *testing.T) {
	instruments, reader := newIndexKeyTestInstruments(t)
	recorder := &recordingGroupExecutor{}
	writer := NewCanonicalNodeWriter(&InstrumentedExecutor{Inner: recorder, Instruments: instruments}, 500, instruments)

	bigKind := strings.Repeat("k", 300) // name+path fit the canonical guard; kind pushes the 4-slot key over.
	longPath := "/repo/" + strings.Repeat("p", 7800) + ".yaml"
	err := writer.Write(context.Background(), canonical.CanonicalMaterialization{
		ScopeID: "scope-1", GenerationID: "gen-1", RepoID: "repo-1", RepoPath: "/repo", FirstGeneration: true,
		Repository: &canonical.RepositoryRow{RepoID: "repo-1", Name: "repo", Path: "/repo"},
		Files: []canonical.FileRow{
			{Path: longPath, RelativePath: "x.yaml", Name: "x.yaml", Language: "yaml", RepoID: "repo-1"},
		},
		Entities: []canonical.EntityRow{
			{EntityID: "k8s-ok", Label: "K8sResource", EntityName: "svc-ok", FilePath: longPath, RepoID: "repo-1", Metadata: map[string]any{"kind": "Service"}},
			{EntityID: "k8s-big", Label: "K8sResource", EntityName: "svc-big", FilePath: longPath, RepoID: "repo-1", Metadata: map[string]any{"kind": bigKind}},
		},
		TerraformStateResources: []canonical.TerraformStateResourceRow{
			{UID: "tf-ok", Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket", Name: "logs", StatePath: "tfstate://s3/h"},
			{UID: "tf-big", Address: oversizedIndexValue, ResourceType: "aws_s3_bucket", Name: "big", StatePath: "tfstate://s3/h"},
		},
		TerraformStateModules: []canonical.TerraformStateModuleRow{
			{UID: "mod-ok", ModuleAddress: "module.app", StatePath: "tfstate://s3/h"},
			{UID: "mod-big", ModuleAddress: oversizedIndexValue, StatePath: "tfstate://s3/h"},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	stmts := recordedStatements(recorder)
	// The skipped uids may still appear in MATCH ... WHERE r.uid IN $uids
	// attribute-cleanup statements, which match nothing for a node that was
	// never written; only the oversized values themselves must be gone.
	for _, needle := range []string{oversizedIndexValue, bigKind} {
		if statementsCarry(t, stmts, needle) {
			t.Fatalf("statement parameters still carry %.20q", needle)
		}
	}
	for _, needle := range []string{`"tf-ok"`, `"mod-ok"`, `"k8s-ok"`} {
		if !statementsCarry(t, stmts, needle) {
			t.Fatalf("normal row %s missing from the write", needle)
		}
	}
	for label, want := range map[string]int64{"TerraformStateResource": 1, "TerraformModule": 1, "K8sResource": 1} {
		if got := oversizedSkips(t, reader, label); got != want {
			t.Errorf("skips{node_label=%s} = %d, want %d", label, got, want)
		}
	}
}

func TestInstrumentedExecutorSkipsScalarStatementAndGroupMember(t *testing.T) {
	recorder := &recordingGroupExecutor{}
	exec := &InstrumentedExecutor{Inner: recorder}
	scalar := Statement{
		Operation:  OperationCanonicalUpsert,
		Cypher:     "MERGE (n:Function {uid: $entity_id})\nSET n += $props",
		Parameters: map[string]any{"entity_id": "f", "props": map[string]any{"name": oversizedIndexValue, "path": "a.go"}},
	}
	if err := exec.Execute(context.Background(), scalar); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(recorder.executeCalls) != 0 {
		t.Fatalf("oversized scalar statement executed: %d calls", len(recorder.executeCalls))
	}

	keep := Statement{Cypher: "MERGE (n:Function {uid: $entity_id})\nSET n += $props", Parameters: map[string]any{"entity_id": "g", "props": map[string]any{"name": "ok"}}}
	if err := exec.ExecuteGroup(context.Background(), []Statement{scalar, keep}); err != nil {
		t.Fatalf("ExecuteGroup() error = %v", err)
	}
	if len(recorder.groupCalls) != 1 || len(recorder.groupCalls[0]) != 1 || recorder.groupCalls[0][0].Parameters["entity_id"] != "g" {
		t.Fatalf("group calls = %+v, want only the in-bound statement", recorder.groupCalls)
	}
}
