// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
)

// semanticNonRetractCalls returns the recorded statements that are not
// retracts, in dispatch order. Writer tests assert upsert shapes through
// it so they do not depend on how many statements the retract dispatch
// emits (#8359 split the single DETACH DELETE into edge retracts plus a
// node reap).
func semanticNonRetractCalls(calls []Statement) []Statement {
	out := make([]Statement, 0, len(calls))
	for _, call := range calls {
		if call.Operation != OperationCanonicalRetract {
			out = append(out, call)
		}
	}
	return out
}

// #8359: semantic_entity_containment gets a dedicated [rel:CONTAINS] edge
// retract. The old retract DETACH DELETEd shared semantic nodes, taking
// foreign edges (canonical CONTAINS, CALLS, documentation edges) with them.
// The dedicated retract deletes only the CONTAINS edges this writer stamped,
// scoped by endpoint labels and evidence_source, and reaps a node only once
// it carries no edge at all.

func TestSemanticContainmentEdgeRetractShape(t *testing.T) {
	t.Parallel()

	for name, cypher := range map[string]string{
		"full":  RetractSemanticEntityContainmentEdgesCypher,
		"delta": RetractSemanticEntityContainmentDeltaEdgesCypher,
	} {
		if !strings.Contains(cypher, "[rel:CONTAINS]") {
			t.Errorf("%s retract does not name [rel:CONTAINS]:\n%s", name, cypher)
		}
		if !strings.Contains(cypher, "DELETE rel") {
			t.Errorf("%s retract does not DELETE rel:\n%s", name, cypher)
		}
		if strings.Contains(cypher, "DETACH DELETE") {
			t.Errorf("%s retract still DETACH DELETEs shared nodes:\n%s", name, cypher)
		}
		if !strings.Contains(cypher, "rel.evidence_source = $evidence_source") {
			t.Errorf("%s retract is not fenced by the edge evidence_source stamp:\n%s", name, cypher)
		}
		// Endpoint-label scope: sources are File or ImplBlock (Rust
		// ownership), targets are the semantic entity labels.
		for _, label := range []string{"File", "ImplBlock", "Function", "Variable", "Module"} {
			if !strings.Contains(cypher, label) {
				t.Errorf("%s retract does not scope endpoint label %q:\n%s", name, label, cypher)
			}
		}
	}
	if !strings.Contains(RetractSemanticEntityContainmentEdgesCypher, "n.repo_id IN $repo_ids") {
		t.Errorf("full retract lost the repo scope:\n%s", RetractSemanticEntityContainmentEdgesCypher)
	}
	if !strings.Contains(RetractSemanticEntityContainmentDeltaEdgesCypher, "n.path IN $file_paths") {
		t.Errorf("delta retract lost the file-path scope:\n%s", RetractSemanticEntityContainmentDeltaEdgesCypher)
	}
}

// The broad (non-canonical) retract dispatch must delete edges first and
// reap nodes only when edgeless — never DETACH DELETE.
func TestSemanticBroadRetractDeletesEdgesBeforeReapingNodes(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriter(executor, 0)
	if _, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	if len(executor.calls) != 3 {
		t.Fatalf("retract statements = %d, want 3 (stamped edge retract, node-scoped edge retract, node reap)", len(executor.calls))
	}
	edge, reap := executor.calls[0].Cypher, executor.calls[2].Cypher
	if edge != RetractSemanticEntityContainmentEdgesCypher {
		t.Errorf("first retract statement is not the dedicated edge retract:\n%s", edge)
	}
	for _, stmt := range executor.calls {
		if strings.Contains(stmt.Cypher, "DETACH DELETE") {
			t.Errorf("retract still DETACH DELETEs:\n%s", stmt.Cypher)
		}
		if stmt.Operation != OperationCanonicalRetract {
			t.Errorf("retract operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
		}
	}
	if !strings.Contains(reap, "DELETE n") || !strings.Contains(reap, "NOT EXISTS") {
		t.Errorf("node reap does not guard on edgelessness:\n%s", reap)
	}
	if !strings.Contains(reap, "n.evidence_source = $evidence_source") {
		t.Errorf("node reap is not fenced to semantic-owned nodes:\n%s", reap)
	}
}

// Canonical-node mode: the canonical writer owns File CONTAINS for the
// canonical-owned labels (stamped projector/canonical), so the semantic
// retract keeps clearing only semantic properties there, but the dedicated
// edge retract still runs for the edges this writer owns in that mode
// (Module/Variable containment, Rust ImplBlock ownership) — fenced by the
// same evidence_source stamp, so canonical edges survive.
func TestSemanticCanonicalRetractRunsDedicatedEdgeRetractWithoutDetach(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSemanticEntityWriterWithCanonicalNodeRows(executor, 0).WithLabelScopedRetract()
	if _, err := writer.WriteSemanticEntities(context.Background(), semantic.EntityWrite{
		RepoIDs: []string{"repo-1"},
	}); err != nil {
		t.Fatalf("WriteSemanticEntities() error = %v", err)
	}
	sawEdgeRetract, sawClear := false, false
	for _, stmt := range executor.calls {
		if strings.Contains(stmt.Cypher, "DETACH DELETE") {
			t.Errorf("canonical retract still DETACH DELETEs:\n%s", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "[rel:CONTAINS]") && strings.Contains(stmt.Cypher, "DELETE rel") {
			sawEdgeRetract = true
		}
		if strings.Contains(stmt.Cypher, "REMOVE ") {
			sawClear = true
		}
	}
	if !sawEdgeRetract {
		t.Error("canonical mode emitted no dedicated [rel:CONTAINS] edge retract")
	}
	if !sawClear {
		t.Error("canonical mode lost the semantic property clear for canonical-owned labels")
	}
}

// Every containment write template must stamp the edge it MERGEs, or the
// dedicated retract cannot fence canonical-writer edges out.
func TestSemanticContainmentWritesStampEdgeEvidenceSource(t *testing.T) {
	t.Parallel()

	for _, plan := range semanticEntityPlans() {
		if !strings.Contains(plan.cypher, "rel.evidence_source = row.evidence_source") {
			t.Errorf("%s upsert does not stamp rel.evidence_source:\n%s", plan.label, plan.cypher)
		}
	}
	if !strings.Contains(semanticRustImplBlockOwnershipCypher, "rel.evidence_source") {
		t.Errorf("Rust ownership upsert does not stamp rel.evidence_source:\n%s", semanticRustImplBlockOwnershipCypher)
	}
}

// The registry in edge/materialized derives this family's identity from the
// held write template; it must MERGE on endpoints alone (no property map).
func TestSemanticContainmentIdentityTemplateMergesOnEndpointsAlone(t *testing.T) {
	t.Parallel()

	if !strings.Contains(SemanticEntityContainmentUpsertCypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
		t.Errorf("identity template does not carry the endpoint-only CONTAINS MERGE:\n%s", SemanticEntityContainmentUpsertCypher)
	}
	if strings.Contains(SemanticEntityContainmentUpsertCypher, "[rel:CONTAINS {") {
		t.Errorf("identity template folds properties into the MERGE key:\n%s", SemanticEntityContainmentUpsertCypher)
	}
}
