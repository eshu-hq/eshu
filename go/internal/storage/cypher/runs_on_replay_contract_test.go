// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func crossRepoRunsOnReplayGroup() []Statement {
	rows := []map[string]any{
		{"repo_id": "repo-a", "platform_id": "platform-a", "evidence_source": "resolver/cross-repo"},
		{"repo_id": "repo-b", "platform_id": "platform-b", "evidence_source": "resolver/cross-repo"},
	}
	statement := func(query string, row map[string]any) Statement {
		return Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     query,
			Parameters: map[string]any{"rows": []map[string]any{row}},
		}
	}
	return []Statement{
		statement(batchCanonicalRunsOnLegacyIdentityCleanupCypher, rows[0]),
		statement(batchCanonicalRunsOnLegacyIdentityCleanupCypher, rows[1]),
		statement(batchCanonicalRunsOnUpsertCypher, rows[0]),
		statement(batchCanonicalRunsOnUpsertCypher, rows[1]),
		{
			Operation: OperationCanonicalUpsert,
			Cypher:    batchCanonicalRepoDependencyUpsertCypher,
			Parameters: map[string]any{"rows": []map[string]any{{
				"repo_id": "repo-a", "target_repo_id": "repo-b",
				"evidence_source": "resolver/cross-repo",
			}}},
		},
	}
}

func TestCrossRepoRunsOnReplayGroupAcceptsMatchedChunksAndRepoDependency(t *testing.T) {
	if !isCanonicalRunsOnReplaySafeGroup(crossRepoRunsOnReplayGroup()) {
		t.Fatal("exact two-chunk RUNS_ON cleanup/upsert with repo dependency must replay")
	}
	if !isCanonicalRunsOnReplaySafeGroup([]Statement{
		crossRepoRunsOnReplayGroup()[0], crossRepoRunsOnReplayGroup()[2],
	}) {
		t.Fatal("exact single-chunk cross-repo RUNS_ON group must replay")
	}
}

func TestCrossRepoRunsOnReplayGroupDuplicatePairAcrossChunks(t *testing.T) {
	for _, test := range []struct {
		name       string
		sourceTool string
		want       bool
	}{
		{"identical duplicate", "argocd", true},
		{"conflicting duplicate", "terraform", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			group := crossRepoRunsOnReplayGroup()
			first := map[string]any{
				"repo_id": "repo-a", "platform_id": "platform-a",
				"evidence_source": "resolver/cross-repo", "source_tool": "argocd",
			}
			second := copyRunsOnReplayRow(first)
			second["source_tool"] = test.sourceTool
			group[0].Parameters = map[string]any{"rows": []map[string]any{first}}
			group[1].Parameters = map[string]any{"rows": []map[string]any{second}}
			group[2].Parameters = map[string]any{"rows": []map[string]any{first}}
			group[3].Parameters = map[string]any{"rows": []map[string]any{second}}
			if got := isCanonicalRunsOnReplaySafeGroup(group); got != test.want {
				t.Fatalf("duplicate-pair replay safety = %t, want %t", got, test.want)
			}
		})
	}
}

func copyRunsOnReplayRow(row map[string]any) map[string]any {
	copied := make(map[string]any, len(row))
	for key, value := range row {
		copied[key] = value
	}
	return copied
}

func TestCrossRepoRunsOnReplayGroupRejectsChangedContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]Statement) []Statement
	}{
		{"altered cleanup", func(s []Statement) []Statement { s[0].Cypher += " "; return s }},
		{"altered upsert", func(s []Statement) []Statement { s[2].Cypher += " "; return s }},
		{"reordered", func(s []Statement) []Statement { s[0], s[2] = s[2], s[0]; return s }},
		{"missing cleanup", func(s []Statement) []Statement { return append(s[:0:0], s[1:]...) }},
		{"missing upsert", func(s []Statement) []Statement { return append(s[:2:2], s[3:]...) }},
		{"extra cleanup", func(s []Statement) []Statement { return append(s, s[0]) }},
		{"mismatched chunk", func(s []Statement) []Statement {
			s[3].Parameters = map[string]any{"rows": []map[string]any{{
				"repo_id": "repo-b", "platform_id": "different", "evidence_source": "resolver/cross-repo",
			}}}
			return s
		}},
		{"empty repository id", func(s []Statement) []Statement {
			s[0].Parameters = map[string]any{"rows": []map[string]any{{
				"repo_id": "", "platform_id": "platform-a", "evidence_source": "resolver/cross-repo",
			}}}
			return s
		}},
		{"empty platform id", func(s []Statement) []Statement {
			s[2].Parameters = map[string]any{"rows": []map[string]any{{
				"repo_id": "repo-a", "platform_id": " ", "evidence_source": "resolver/cross-repo",
			}}}
			return s
		}},
		{"empty evidence source", func(s []Statement) []Statement {
			s[0].Parameters = map[string]any{"rows": []map[string]any{{
				"repo_id": "repo-a", "platform_id": "platform-a", "evidence_source": "",
			}}}
			return s
		}},
		{"wrong cleanup operation", func(s []Statement) []Statement { s[0].Operation = OperationCanonicalRetract; return s }},
		{"wrong upsert operation", func(s []Statement) []Statement { s[2].Operation = OperationCanonicalRetract; return s }},
		{"wrong unrelated operation", func(s []Statement) []Statement { s[4].Operation = OperationCanonicalRetract; return s }},
		{"arbitrary unrelated merge", func(s []Statement) []Statement {
			s[4].Cypher = "MERGE (p:ReplayProbe {id: $id}) SET p.name = $name"
			return s
		}},
		{"arbitrary unwind delete", func(s []Statement) []Statement {
			return append(s, Statement{
				Operation:  OperationCanonicalUpsert,
				Cypher:     "UNWIND $rows AS row MATCH (n {id: row.id}) DELETE n",
				Parameters: map[string]any{"rows": []map[string]any{{"id": "unrelated"}}},
			})
		}},
		{"accumulating merge set", func(s []Statement) []Statement {
			return append(s, Statement{
				Operation:  OperationCanonicalUpsert,
				Cypher:     "MERGE (n:ReplayProbe {id: $id}) SET n.count = coalesce(n.count, 0) + 1",
				Parameters: map[string]any{"id": "unrelated"},
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if isCanonicalRunsOnReplaySafeGroup(test.mutate(crossRepoRunsOnReplayGroup())) {
				t.Fatal("changed cross-repo RUNS_ON group accepted for commit-conflict replay")
			}
		})
	}
}

type workloadRunsOnGroupCapture struct {
	statements []reducer.CypherGroupStatement
}

func (*workloadRunsOnGroupCapture) ExecuteCypher(context.Context, string, map[string]any) error {
	return nil
}

func (c *workloadRunsOnGroupCapture) ExecuteCypherGroup(_ context.Context, group []reducer.CypherGroupStatement) error {
	c.statements = append([]reducer.CypherGroupStatement(nil), group...)
	return nil
}

func workloadRunsOnReplayGroup(t *testing.T) []Statement {
	t.Helper()
	capture := &workloadRunsOnGroupCapture{}
	projection := &reducer.ProjectionResult{RuntimePlatformRows: []reducer.RuntimePlatformRow{{
		Environment: "production", InstanceID: "instance-a", PlatformID: "platform-a",
		PlatformKind: "kubernetes", PlatformName: "production",
	}}}
	if _, err := reducer.NewWorkloadMaterializer(capture).Materialize(context.Background(), projection); err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}
	statements := make([]Statement, len(capture.statements))
	for index, statement := range capture.statements {
		statements[index] = Statement{
			Operation: OperationCanonicalUpsert,
			Cypher:    statement.Cypher, Parameters: statement.Parameters,
		}
	}
	return statements
}

func TestWorkloadRunsOnReplayGroupAcceptsExactMaterializerGroup(t *testing.T) {
	if !isCanonicalRunsOnReplaySafeGroup(workloadRunsOnReplayGroup(t)) {
		t.Fatal("exact workload RUNS_ON materializer group must replay")
	}
}

func TestWorkloadRunsOnReplayGroupDuplicatePair(t *testing.T) {
	for _, test := range []struct {
		name       string
		confidence float64
		want       bool
	}{
		{"identical duplicate", 0.9, true},
		{"conflicting duplicate", 0.8, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			group := workloadRunsOnReplayGroup(t)
			first := copyRunsOnReplayRow(group[0].Parameters["rows"].([]map[string]any)[0])
			first["platform_confidence"] = 0.9
			second := copyRunsOnReplayRow(first)
			second["platform_confidence"] = test.confidence
			params := map[string]any{"rows": []map[string]any{first, second}}
			for index := range group {
				group[index].Parameters = params
			}
			if got := isCanonicalRunsOnReplaySafeGroup(group); got != test.want {
				t.Fatalf("duplicate-pair replay safety = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWorkloadRunsOnReplayGroupRejectsChangedContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]Statement) []Statement
	}{
		{"altered cleanup", func(s []Statement) []Statement { s[0].Cypher += " "; return s }},
		{"altered identity", func(s []Statement) []Statement { s[1].Cypher += " "; return s }},
		{"altered tuple", func(s []Statement) []Statement { s[2].Cypher += " "; return s }},
		{"reordered", func(s []Statement) []Statement { s[0], s[1] = s[1], s[0]; return s }},
		{"missing statement", func(s []Statement) []Statement { return s[:2] }},
		{"extra statement", func(s []Statement) []Statement { return append(s, s[1]) }},
		{"mismatched rows", func(s []Statement) []Statement {
			s[2].Parameters = map[string]any{"rows": []map[string]any{{
				"instance_id": "instance-a", "platform_id": "different", "evidence_source": reducer.EvidenceSourceWorkloads,
			}}}
			return s
		}},
		{"empty instance id", func(s []Statement) []Statement {
			s[0].Parameters = map[string]any{"rows": []map[string]any{{
				"instance_id": "", "platform_id": "platform-a", "evidence_source": reducer.EvidenceSourceWorkloads,
			}}}
			return s
		}},
		{"wrong operation", func(s []Statement) []Statement { s[2].Operation = OperationCanonicalRetract; return s }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if isCanonicalRunsOnReplaySafeGroup(test.mutate(workloadRunsOnReplayGroup(t))) {
				t.Fatal("changed workload RUNS_ON group accepted for commit-conflict replay")
			}
		})
	}
}
