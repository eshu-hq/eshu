package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestEdgeWriterWriteEdgesCodeCallsPersistsResolutionProvenance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		payload          map[string]any
		wantRelationship string
		wantMethod       codeprovenance.Method
		wantConfidence   float64
	}{
		{
			name: "call uses repo fallback confidence",
			payload: map[string]any{
				"caller_entity_id":   "content-entity:caller",
				"caller_entity_type": "Function",
				"callee_entity_id":   "content-entity:callee",
				"callee_entity_type": "Function",
				"resolution_method":  codeprovenance.MethodRepoUniqueName,
			},
			wantRelationship: "CALLS",
			wantMethod:       codeprovenance.MethodRepoUniqueName,
			wantConfidence:   0.50,
		},
		{
			name: "reference uses import binding confidence",
			payload: map[string]any{
				"caller_entity_id":   "content-entity:file",
				"caller_entity_type": "File",
				"callee_entity_id":   "content-entity:type",
				"callee_entity_type": "TypeAlias",
				"relationship_type":  "REFERENCES",
				"resolution_method":  codeprovenance.MethodImportBinding,
			},
			wantRelationship: "REFERENCES",
			wantMethod:       codeprovenance.MethodImportBinding,
			wantConfidence:   0.90,
		},
		{
			name: "metaclass uses declared confidence",
			payload: map[string]any{
				"source_entity_id":  "content-entity:class",
				"target_entity_id":  "content-entity:metaclass",
				"relationship_type": "USES_METACLASS",
				"resolution_method": codeprovenance.MethodDeclared,
			},
			wantRelationship: "USES_METACLASS",
			wantMethod:       codeprovenance.MethodDeclared,
			wantConfidence:   0.95,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingExecutor{}
			writer := NewEdgeWriter(executor, 0)

			rows := []reducer.SharedProjectionIntentRow{{
				IntentID:     "intent-1",
				RepositoryID: "repo-1",
				Payload:      tc.payload,
			}}
			if err := writer.WriteEdges(context.Background(), reducer.DomainCodeCalls, rows, "parser/code-calls"); err != nil {
				t.Fatalf("WriteEdges() error = %v", err)
			}
			if got, want := len(executor.calls), 1; got != want {
				t.Fatalf("executor calls = %d, want %d", got, want)
			}

			cypher := executor.calls[0].Cypher
			if !strings.Contains(cypher, "rel:"+tc.wantRelationship) {
				t.Fatalf("cypher missing %s relationship: %s", tc.wantRelationship, cypher)
			}
			if !strings.Contains(cypher, "rel.resolution_method = row.resolution_method") {
				t.Fatalf("cypher missing resolution_method write: %s", cypher)
			}
			if !strings.Contains(cypher, "rel.confidence = row.confidence") {
				t.Fatalf("cypher missing derived confidence write: %s", cypher)
			}
			if !strings.Contains(cypher, "rel.reason = row.reason") {
				t.Fatalf("cypher missing derived reason write: %s", cypher)
			}

			batchRows, ok := executor.calls[0].Parameters["rows"].([]map[string]any)
			if !ok || len(batchRows) != 1 {
				t.Fatalf("rows parameter = %#v, want one row", executor.calls[0].Parameters["rows"])
			}
			if got := batchRows[0]["resolution_method"]; got != tc.wantMethod {
				t.Fatalf("resolution_method = %v, want %v", got, tc.wantMethod)
			}
			if got := batchRows[0]["confidence"]; got != tc.wantConfidence {
				t.Fatalf("confidence = %v, want %v", got, tc.wantConfidence)
			}
			if got := batchRows[0]["reason"]; got != codeprovenance.Reason(tc.wantMethod) {
				t.Fatalf("reason = %v, want %v", got, codeprovenance.Reason(tc.wantMethod))
			}
		})
	}
}

func TestBuildCanonicalCodeCallUpsertPersistsResolutionProvenance(t *testing.T) {
	t.Parallel()

	stmt := BuildCanonicalCodeCallUpsert(CanonicalCodeCallParams{
		CallerEntityID:   "entity:function:caller",
		CalleeEntityID:   "entity:function:callee",
		CallKind:         "function_call",
		ResolutionMethod: codeprovenance.MethodSameFile,
	}, "parser/code-calls")

	if !strings.Contains(stmt.Cypher, "rel.resolution_method = $resolution_method") {
		t.Fatalf("Cypher missing resolution_method write: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.confidence = $confidence") {
		t.Fatalf("Cypher missing derived confidence write: %s", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.reason = $reason") {
		t.Fatalf("Cypher missing derived reason write: %s", stmt.Cypher)
	}
	if got := stmt.Parameters["resolution_method"]; got != codeprovenance.MethodSameFile {
		t.Fatalf("resolution_method = %v, want %v", got, codeprovenance.MethodSameFile)
	}
	if got := stmt.Parameters["confidence"]; got != 0.95 {
		t.Fatalf("confidence = %v, want 0.95", got)
	}
	if got := stmt.Parameters["reason"]; got != codeprovenance.Reason(codeprovenance.MethodSameFile) {
		t.Fatalf("reason = %v, want %v", got, codeprovenance.Reason(codeprovenance.MethodSameFile))
	}
}

func TestEdgeWriterWriteEdgesCodeCallsPersistsEveryConfidenceTier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method codeprovenance.Method
		want   float64
	}{
		{codeprovenance.MethodSCIP, 0.99},
		{codeprovenance.MethodDeclared, 0.95},
		{codeprovenance.MethodSameFile, 0.95},
		{codeprovenance.MethodImportBinding, 0.90},
		{codeprovenance.MethodTypeInferred, 0.80},
		{codeprovenance.MethodScopeUniqueName, 0.70},
		{codeprovenance.MethodRepoUniqueName, 0.50},
		{codeprovenance.MethodUnspecified, codeprovenance.LegacyConfidence},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			rowMap := map[string]any{}
			addCodeEdgeResolutionProperties(rowMap, tc.method)

			if got := rowMap["resolution_method"]; got != tc.method {
				t.Fatalf("resolution_method = %v, want %v", got, tc.method)
			}
			if got := rowMap["confidence"]; got != tc.want {
				t.Fatalf("confidence = %v, want %v", got, tc.want)
			}
			if got := rowMap["reason"]; got != codeprovenance.Reason(tc.method) {
				t.Fatalf("reason = %v, want %v", got, codeprovenance.Reason(tc.method))
			}
		})
	}
}

func TestEdgeWriterWriteEdgesCodeCallsInvalidMethodFallsBackToUnspecified(t *testing.T) {
	t.Parallel()

	rowMap := map[string]any{}
	addCodeEdgeResolutionProperties(rowMap, "not_a_method")

	if got := rowMap["resolution_method"]; got != codeprovenance.MethodUnspecified {
		t.Fatalf("resolution_method = %v, want %v", got, codeprovenance.MethodUnspecified)
	}
	if got := rowMap["confidence"]; got != codeprovenance.LegacyConfidence {
		t.Fatalf("confidence = %v, want %v", got, codeprovenance.LegacyConfidence)
	}
	if got := rowMap["reason"]; got != codeprovenance.Reason(codeprovenance.MethodUnspecified) {
		t.Fatalf("reason = %v, want %v", got, codeprovenance.Reason(codeprovenance.MethodUnspecified))
	}
}
