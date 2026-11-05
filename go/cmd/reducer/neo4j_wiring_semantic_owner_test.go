// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSemanticEntityWriterForGraphBackendKeepsCanonicalOwnershipForNeo4j pins
// the #10110 ownership split on Neo4j: canonical-owned labels (Function here)
// are enriched by uid without re-stamping evidence_source or re-MERGEing the
// canonical File CONTAINS edge, while semantic-owned labels (Variable, Module)
// keep the per-label MATCH-File-first template, so a row whose File is absent
// creates no node (the #6968 pin in backendconformance).
func TestSemanticEntityWriterForGraphBackendKeepsCanonicalOwnershipForNeo4j(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNeo4j, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}
	result, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
		Rows: []semantic.EntityRow{
			semanticFunctionRow("function-go-1"),
			semanticVariableRow("variable-go-1"),
			semanticModuleRow("module-ts-1"),
		},
	})
	if err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}

	upserts := map[string]string{}
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalUpsert {
			continue
		}
		label, _ := call.Parameters[sourcecypher.StatementMetadataEntityLabelKey].(string)
		upserts[label] = call.Cypher
	}

	function := upserts["Function"]
	if !strings.Contains(function, "MATCH (n:Function {uid: row.entity_id})") {
		t.Fatalf("Function upsert = %q, want enrichment of the canonical node by uid", function)
	}
	for _, forbidden := range []string{
		"MERGE (n:Function",
		"MATCH (f:File",
		"CONTAINS",
		"n.evidence_source = row.evidence_source",
	} {
		if strings.Contains(function, forbidden) {
			t.Fatalf("Function upsert = %q, must not contain %q: the canonical projector owns the node and its File edge", function, forbidden)
		}
	}

	for _, label := range []string{"Variable", "Module"} {
		want := "UNWIND $rows AS row\nMATCH (f:File {path: row.file_path})\nMERGE (n:" + label + " {uid: row.entity_id})"
		if !strings.HasPrefix(upserts[label], want) {
			t.Fatalf("%s upsert = %q, want the MATCH-File-first semantic template prefix %q", label, upserts[label], want)
		}
		if !strings.Contains(upserts[label], "MERGE (f)-[rel:CONTAINS]->(n)") ||
			!strings.Contains(upserts[label], "n.evidence_source = row.evidence_source") {
			t.Fatalf("%s upsert = %q, want the semantic writer to own the node and its CONTAINS edge", label, upserts[label])
		}
	}

	// MATCH-owned rows are reconciled against the projector's uid
	// derivation (#8359): a Function row whose uid is not the canonical
	// derivation would no-op, so it must be reported, not hidden.
	if len(result.UnmatchedEntityIDs) != 1 || result.UnmatchedEntityIDs[0] != "function-go-1" {
		t.Fatalf("UnmatchedEntityIDs = %v, want [function-go-1] (non-derived Function uid)", result.UnmatchedEntityIDs)
	}
}

// TestSemanticEntityWriterForGraphBackendNeo4jDeltaRetractKeepsBroadShape pins
// the Neo4j delta retract: the broad pipe-label edge retracts and reap scoped
// to the touched file paths, plus one per-label REMOVE that clears semantic
// properties from canonical-owned nodes on those paths. No statement deletes
// a canonical-owned node.
func TestSemanticEntityWriterForGraphBackendNeo4jDeltaRetractKeepsBroadShape(t *testing.T) {
	t.Parallel()

	executor := &recordingReducerStatementExecutor{}
	writer, err := semanticEntityWriterForGraphBackend(executor, 100, runtimecfg.GraphBackendNeo4j, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
	}
	if _, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs:         []string{"repo-1"},
		Rows:            []semantic.EntityRow{semanticFunctionRow("function-go-1")},
		DeltaProjection: true,
		DeltaFilePaths:  []string{"/repo/main.go"},
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}

	var broad, clears int
	for _, call := range executor.calls {
		if call.Operation != sourcecypher.OperationCanonicalRetract {
			continue
		}
		if !strings.Contains(call.Cypher, "n.path IN $file_paths") {
			t.Fatalf("delta retract = %q, want file-path scope", call.Cypher)
		}
		if strings.Contains(call.Cypher, "DETACH DELETE") {
			t.Fatalf("delta retract = %q, want no DETACH DELETE on shared nodes", call.Cypher)
		}
		switch {
		case strings.Contains(call.Cypher, ":Annotation|Typedef|TypeAlias"):
			broad++
		case strings.Contains(call.Cypher, "REMOVE n."):
			clears++
		default:
			t.Fatalf("delta retract = %q, want a broad edge retract, the broad reap, or a canonical-node clear", call.Cypher)
		}
	}
	if broad != 3 || clears != 9 {
		t.Fatalf("delta retracts: broad=%d clears=%d, want broad=3 clears=9", broad, clears)
	}
}
