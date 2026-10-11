// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPilotCypherRejectsUnsafeGrantExpressions(t *testing.T) {
	contract := PilotContract{AuthorizationAliases: []string{"repo"}, RequireOrder: true, RequireLimit: true}
	schema := []string{"CREATE INDEX repository_id FOR (r:Repository) ON (r.id)"}
	good := `MATCH (repo:Repository {id: $repo_id}) WHERE repo.id IN $allowed_repository_ids RETURN repo.id ORDER BY repo.id LIMIT $limit`
	if err := ValidatePilotQuery(queryKindCypher, good, contract, schema); err != nil {
		t.Fatalf("valid scoped query: %v", err)
	}
	for _, mutation := range []string{
		"repo.id > $allowed_repository_ids",
		"repo.id <> $allowed_repository_ids",
		"repo.name IN $allowed_repository_ids",
		"repo.id IS NOT NULL AND $allowed_repository_ids IS NOT NULL",
		"repo.id IN $allowed_repository_ids OR repo.id > $grant_ids",
		"repo.id IN $allowed_repository_ids XOR TRUE",
	} {
		query := strings.Replace(good, "repo.id IN $allowed_repository_ids", mutation, 1)
		if err := ValidatePilotQuery(queryKindCypher, query, contract, schema); err == nil {
			t.Errorf("accepted unsafe authorization %q", mutation)
		}
	}
	wrongLabel := strings.Replace(good, "repo:Repository", "repo:File", 1)
	if err := ValidatePilotQuery(queryKindCypher, wrongLabel, contract, schema); err == nil {
		t.Fatal("accepted grant alias bound to the wrong node label")
	}
	for _, tail := range []string{" nonsense", " RETURN repo.id", " SKIP 1", " ORDER BY repo.id"} {
		if err := ValidatePilotQuery(queryKindCypher, good+tail, contract, schema); err == nil {
			t.Errorf("accepted trailing syntax %q", tail)
		}
	}
	unknownFunction := strings.Replace(good, "repo.id IN $allowed_repository_ids", "repo.id IN $allowed_repository_ids AND mystery(repo.id)", 1)
	if err := ValidatePilotQuery(queryKindCypher, unknownFunction, contract, schema); err == nil {
		t.Fatal("accepted unsupported function")
	}
	sqlOnlyOperator := strings.Replace(good, "RETURN repo.id", "RETURN repo.row->>'id'", 1)
	if err := ValidatePilotQuery(queryKindCypher, sqlOnlyOperator, contract, schema); err == nil {
		t.Fatal("accepted PostgreSQL JSON operator in Cypher")
	}
	if err := ValidatePilotQuery(queryKindCypher, good+" AND TRUE", contract, schema); err == nil {
		t.Fatal("accepted boolean expression after page bound")
	}
}

func TestPilotCypherRequiresEachBoundGrantAlias(t *testing.T) {
	contract := PilotContract{AuthorizationAliases: []string{"source_repo", "target_repo"}, RequireOrder: true, RequireLimit: true}
	good := `MATCH (source_repo:Repository)-[:REPO_CONTAINS]->(source_file:File)-[:CALLS]->(target_repo:Repository) WHERE source_repo.id IN $allowed_repository_ids AND target_repo.id IN $allowed_scope_ids RETURN source_file.path ORDER BY source_file.path LIMIT $limit`
	if err := ValidatePilotQuery(queryKindCypher, good, contract, nil); err != nil {
		t.Fatalf("valid two-sided grant: %v", err)
	}
	for _, changed := range []string{
		strings.Replace(good, " AND target_repo.id IN $allowed_scope_ids", "", 1),
		strings.Replace(good, "target_repo.id IN $allowed_scope_ids", "target_repo.id > $allowed_scope_ids", 1),
		strings.Replace(good, "target_repo.id IN $allowed_scope_ids", "NOT target_repo.id IN $allowed_scope_ids", 1),
	} {
		if err := ValidatePilotQuery(queryKindCypher, changed, contract, nil); err == nil {
			t.Errorf("accepted missing or inverted target grant %q", changed)
		}
	}
}

func TestPilotCapturedProductionGrammar(t *testing.T) {
	postgresPath, graphPath := os.Getenv("ESHU_PILOT_SQL_CAPTURE"), os.Getenv("ESHU_PILOT_CYPHER_CAPTURE")
	if postgresPath == "" || graphPath == "" {
		t.Skip("set both ESHU_PILOT_SQL_CAPTURE and ESHU_PILOT_CYPHER_CAPTURE")
	}
	manifest, err := LoadManifestFile("testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, suite := range []struct {
		path string
		key  string
		want int
	}{{postgresPath, "Evidence", 96}, {graphPath, "statements", 280}} {
		body, err := os.ReadFile(suite.path)
		if err != nil {
			t.Fatal(err)
		}
		var report map[string]json.RawMessage
		if err := json.Unmarshal(body, &report); err != nil {
			t.Fatal(err)
		}
		var cases []struct {
			EntryID     string `json:"entry_id"`
			VariantID   string `json:"variant_id"`
			CaseID      string `json:"case_id"`
			EmittedText string `json:"emitted_text"`
		}
		if err := json.Unmarshal(report[suite.key], &cases); err != nil {
			t.Fatal(err)
		}
		if len(cases) != suite.want {
			t.Fatalf("%s: %d cases, want %d", suite.key, len(cases), suite.want)
		}
		for _, captured := range cases {
			id := captured.EntryID
			if id == "" {
				id = "QP-CLOUD-RESOURCE-IDENTITY-PAGE"
			}
			entry, ok := pilotEntry(manifest, id)
			if !ok || entry.Contract == nil {
				t.Fatalf("unknown pilot entry %s", id)
			}
			var required *PilotCase
			for i := range entry.Contract.RequiredCases {
				candidate := &entry.Contract.RequiredCases[i]
				if candidate.VariantID == captured.VariantID && candidate.CaseID == captured.CaseID {
					required = candidate
					break
				}
			}
			if required == nil {
				t.Fatalf("%s/%s: case not registered", captured.VariantID, captured.CaseID)
			}
			schema := make([]string, 0, len(entry.Contract.RequiredSchema))
			for _, name := range entry.Contract.RequiredSchema {
				schema = append(schema, "CREATE INDEX "+name+" FOR (r:Repository) ON (r.id)")
			}
			if err := ValidatePilotCaseQuery(entry.QueryKind, captured.EmittedText, *entry.Contract, *required, schema); err != nil {
				t.Errorf("%s/%s: %v", captured.VariantID, captured.CaseID, err)
			}
		}
	}
}

func TestPilotSQLRejectsChangedGrantLiteralAndCorrelationBypass(t *testing.T) {
	contract := PilotContract{
		ScopePredicates: []PilotScopePredicate{
			{Alias: "scope", Expression: "scope.scope_kind = 'repository'"},
			{Alias: "scope", Expression: "scope.source_key = ANY($PARAM::text[])"},
		},
		CorrelationPredicates: []string{"fact.fact_id = owner.winning_row->>'source_fact_id'"},
		RequireOrder:          true, RequireLimit: true,
	}
	good := `SELECT owner.uid FROM graph_node_owner AS owner WHERE COALESCE((SELECT TRUE FROM fact_records AS fact JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id WHERE fact.fact_id = owner.winning_row->>'source_fact_id' AND scope.scope_kind = 'repository' AND scope.source_key = ANY($1::text[]) LIMIT 1), FALSE) ORDER BY owner.uid LIMIT $2`
	if err := ValidatePilotQuery(queryKindSQLReadModel, good, contract, nil); err != nil {
		t.Fatalf("valid correlated grant: %v", err)
	}
	for _, mutation := range []string{
		strings.Replace(good, "scope.scope_kind = 'repository'", "scope.scope_kind = 'public'", 1),
		strings.Replace(good, "fact.fact_id = owner.winning_row->>'source_fact_id'", "fact.fact_id = owner.winning_row->>'other_fact_id'", 1),
		strings.Replace(good, "fact.fact_id = owner.winning_row->>'source_fact_id'", "(fact.fact_id = owner.winning_row->>'source_fact_id' OR TRUE)", 1),
		strings.Replace(good, "scope.source_key = ANY($1::text[])", "scope.source_key <> ANY($1::text[])", 1),
		strings.Replace(good, "ON scope.scope_id = fact.scope_id", "ON TRUE", 1),
		strings.Replace(good, "ON scope.scope_id = fact.scope_id", "ON scope.scope_id = fact.scope_id OR TRUE", 1),
		strings.Replace(good, "JOIN ingestion_scopes AS scope ON", "JOIN graph_node_owner AS owner ON owner.uid = fact.fact_id JOIN ingestion_scopes AS scope ON", 1),
		`SELECT owner.uid FROM graph_node_owner AS owner WHERE COALESCE((SELECT TRUE FROM fact_records AS fact JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id WHERE fact.fact_id = owner.winning_row->>'source_fact_id' LIMIT 1), (SELECT TRUE FROM fact_records AS fact JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id WHERE scope.scope_kind = 'repository' AND scope.source_key = ANY($1::text[]) LIMIT 1), FALSE) ORDER BY owner.uid LIMIT $2`,
		good + " nonsense",
	} {
		if err := ValidatePilotQuery(queryKindSQLReadModel, mutation, contract, nil); err == nil {
			t.Errorf("accepted unsafe SQL %q", mutation)
		}
	}
}

func TestPilotEvidenceAcceptsGitCommitAndRejectsAmbiguousWorkMetric(t *testing.T) {
	manifest, evidence := pilotEvidenceFixture()
	evidence.Base.Commit = strings.Repeat("a", 40)
	evidence.Candidate.Commit = strings.Repeat("b", 40)
	if err := ValidatePilotEvidence(manifest, &evidence); err != nil {
		t.Fatalf("valid Git object IDs: %v", err)
	}
	work := json.RawMessage(`{"query_count":1,"shared_blocks":2,"child":{"shared_blocks":101}}`)
	if _, ok := pilotWorkNumber(work, "shared_blocks"); ok {
		t.Fatal("ambiguous nested work metric was accepted")
	}
	nonnumericDuplicate := json.RawMessage(`{"shared_blocks":2,"child":{"shared_blocks":"unknown"}}`)
	if _, ok := pilotWorkNumber(nonnumericDuplicate, "shared_blocks"); ok {
		t.Fatal("nonnumeric duplicate work metric was ignored")
	}
}
