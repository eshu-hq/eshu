// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPilotRequiredEntryNeedsExecutableContract(t *testing.T) {
	manifest := Manifest{Version: 1, PilotRequiredIDs: []string{"pilot"}, Entries: []Entry{{ID: "pilot"}}}
	if err := ValidatePilotContracts(manifest); err == nil || !strings.Contains(err.Error(), "pilot") {
		t.Fatalf("missing pilot contract: %v", err)
	}
}

func pilotEvidenceFixture() (Manifest, PilotEvidenceArtifact) {
	query := "SELECT owner.uid FROM graph_node_owner AS owner WHERE owner.uid = $1 ORDER BY owner.uid LIMIT 1"
	queryHash := fmt.Sprintf("%x", sha256.Sum256([]byte(query)))
	contract := PilotContract{
		Version: 1, Result: "ordered identities", Authorization: "granted repository",
		CurrentHistory: "current generation only", Nulls: "unknown remains null",
		Duplicates: "one row per uid", Ordering: "uid ascending", Pagination: "keyset",
		Partial: "fail closed", Patterns: []string{"owner ledger"}, Rationale: "bounded page",
		Alternatives: []string{"graph scan"}, IndexCost: "one owner index",
		Workload:      PilotWorkload{Cardinality: "20000", Selectivity: "one UID among 20000 owners", Skew: "hot owner", Depth: "0", Fanout: "1", Payload: "uid", MaxResultPayloadBytes: 23, QueryCount: "1", Budgets: "2s"},
		Budget:        PilotBudget{MaxNormalMilliseconds: 2000, MaxQueryCount: 1, MaxWork: map[string]float64{"shared_blocks": 100}, NoiseTolerance: "fixture samples below ceiling"},
		Environment:   PilotEnvironment{Engine: "postgres", Version: "18", Schema: "schema-v1", Indexes: "owner-index", Fixture: "fixture-v1", Oracle: "fixture-oracle", Runner: "sql-runner"},
		RequiredCases: []PilotCase{{VariantID: "v1", CaseID: "normal", ScopeMode: "all_scopes", QuerySHA256: queryHash}},
	}
	manifest := Manifest{Version: 1, PilotRequiredIDs: []string{"pilot"}, Entries: []Entry{{
		ID: "pilot", QueryKind: queryKindSQLReadModel,
		Source: SourceRef{SourceSHA256: strings.Repeat("a", 64)}, Contract: &contract,
	}}}
	schema := []string{"CREATE TABLE graph_node_owner(uid text)"}
	indexes := []string{"CREATE INDEX owner_uid ON graph_node_owner(uid)"}
	build := PilotBuildIdentity{
		SchemaDDL: schema, Migrations: []string{}, IndexDDL: indexes,
		SchemaSHA256: PilotDefinitionsSHA256(schema), MigrationsSHA256: PilotDefinitionsSHA256([]string{}), IndexesSHA256: PilotDefinitionsSHA256(indexes),
	}
	base, candidate := build, build
	base.Commit, candidate.Commit = strings.Repeat("1", 64), strings.Repeat("2", 64)
	config := json.RawMessage(`{"max_connections":10}`)
	dataset := json.RawMessage(`{"version":"v1","seed":42,"distribution":{"owners":20},"topology":"local","storage":"fresh"}`)
	workload := json.RawMessage(`{"version":"v1","cases":1}`)
	fixtureDefinition := "generated fixture uid and owner"
	run := PilotCaseRun{
		Result: json.RawMessage(`[{"uid":"fixture-uid"}]`),
		Plan:   json.RawMessage(`{"Plan":{"Node Type":"Index Scan"}}`), Work: json.RawMessage(`{"rows":1,"shared_blocks":2,"query_count":1}`),
		ColdMilliseconds: []float64{2, 2.1}, WarmMilliseconds: []float64{1, 1.1},
		ColdPreparation: "cold_plan_warm_buffers", ColdProof: "new prepared plan, retained fixture buffers",
		ColdProofSHA256:     fmt.Sprintf("%x", sha256.Sum256([]byte("new prepared plan, retained fixture buffers"))),
		PlanCaptureSeparate: true,
	}
	evidence := PilotEvidenceArtifact{
		Version: 1,
		Environment: PilotRunEnvironment{
			Engine: "postgres", EngineVersion: "18", EngineImage: "postgres:18@sha256:fixture",
			Config: config, ConfigSHA256: PilotJSONSHA256(config),
			Dataset: dataset, DatasetSHA256: PilotJSONSHA256(dataset),
			FixtureDefinition: fixtureDefinition, FixtureSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(fixtureDefinition))),
			Workload: workload, WorkloadSHA256: PilotJSONSHA256(workload), HarnessSHA256: strings.Repeat("8", 64),
			BinarySHA256: strings.Repeat("9", 64), ColdReset: "cold_plan_warm_buffers",
		},
		Base: base, Candidate: candidate,
		Entries: []PilotEvidenceEntry{{
			EntryID: "pilot", ContractSHA256: PilotContractSHA256(contract),
			SourceSHA256: strings.Repeat("a", 64), Cases: []PilotCaseEvidence{{
				VariantID: "v1", CaseID: "normal", ScopeMode: "all_scopes", EmittedText: query, EmittedSHA256: queryHash,
				BaseEmittedText: query, BaseEmittedSHA256: queryHash,
				Parameters:     map[string]json.RawMessage{"uid": json.RawMessage(`"fixture-uid"`)},
				OracleProducer: "fixture-oracle", OracleArtifactSHA256: strings.Repeat("b", 64),
				OracleRecordedAt: "2026-01-01T00:00:00Z", MeasuredAt: "2026-01-01T00:01:00Z",
				Expected: json.RawMessage(`[{"uid":"fixture-uid"}]`), Actual: json.RawMessage(`[{"uid":"fixture-uid"}]`),
				Base: run, Candidate: run,
			}},
		}},
	}
	return manifest, evidence
}

func TestPilotQueryRejectsScopeSchemaAndTraversalMutations(t *testing.T) {
	contract := PilotContract{Version: 1, AuthorizationAliases: []string{"repo"}, RequiredSchema: []string{"repository_id"}, RequireOrder: true, RequireLimit: true}
	good := `MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS*1..2]->(f:File) WHERE repo.id IN $allowed_repo_ids RETURN f ORDER BY f.path LIMIT $limit`
	for _, query := range []string{
		`MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS*1..2]->(f:File) RETURN f ORDER BY f.path LIMIT $limit`,
		`MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS*]->(f:File) WHERE repo.id IN $allowed_repo_ids RETURN f ORDER BY f.path LIMIT $limit`,
		`MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS*1..2]->(f:File) WHERE repo.id IN $allowed_repo_ids OR f.name = 'public' RETURN f ORDER BY f.path LIMIT $limit`,
	} {
		if err := ValidatePilotQuery("cypher", query, contract, []string{"CREATE INDEX repository_id FOR (r:Repository) ON (r.id)"}); err == nil {
			t.Errorf("accepted unsafe query %q", query)
		}
	}
	if err := ValidatePilotQuery("cypher", good, contract, nil); err == nil {
		t.Error("accepted missing index")
	}
	if err := ValidatePilotQuery("cypher", good, contract, []string{"CREATE INDEX repository_id FOR (r:Repository) ON (r.id)"}); err != nil {
		t.Fatalf("safe query: %v", err)
	}
}

func TestPilotSQLCorrelatedAuthorizationMustPrecedePage(t *testing.T) {
	contract := PilotContract{
		RequiredSchema: []string{"owner_uid"}, RequireOrder: true, RequireLimit: true,
		CorrelationPredicates: []string{"fact.fact_id = owner.winning_row->>'source_fact_id'"},
		ScopePredicates: []PilotScopePredicate{{
			Alias:      "scope",
			Expression: "((scope.scope_kind = 'repository' AND scope.source_key = ANY($PARAM::text[])) OR fact.scope_id = ANY($PARAM::text[]))",
		}},
	}
	query := `SELECT owner.uid FROM graph_node_owner AS owner
	WHERE COALESCE((SELECT TRUE FROM fact_records AS fact
	JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id
	WHERE fact.fact_id = owner.winning_row->>'source_fact_id'
	AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY($1::text[]))
	OR fact.scope_id = ANY($2::text[])) LIMIT 1), FALSE)
	ORDER BY owner.uid LIMIT $3`
	schema := []string{"CREATE INDEX owner_uid ON graph_node_owner(uid)"}
	if err := ValidatePilotQuery(queryKindSQLReadModel, query, contract, schema); err != nil {
		t.Fatalf("correlated SQL: %v", err)
	}
	withoutGrant := strings.Replace(query, "AND ((scope.scope_kind = 'repository' AND scope.source_key = ANY($1::text[]))\n\tOR fact.scope_id = ANY($2::text[]))", "", 1)
	if err := ValidatePilotQuery(queryKindSQLReadModel, withoutGrant, contract, schema); err == nil {
		t.Fatal("accepted SQL with grant removed")
	}
	commentGrant := strings.Replace(withoutGrant, "LIMIT 1", "-- scope.scope_kind = 'repository' AND scope.source_key = ANY($1::text[])\n LIMIT 1", 1)
	if err := ValidatePilotQuery(queryKindSQLReadModel, commentGrant, contract, schema); err == nil {
		t.Fatal("accepted grant in SQL comment")
	}
	outerBypass := strings.Replace(query, "ORDER BY owner.uid", "OR TRUE ORDER BY owner.uid", 1)
	if err := ValidatePilotQuery(queryKindSQLReadModel, outerBypass, contract, schema); err == nil {
		t.Fatal("accepted outer OR bypass")
	}
}

func TestPilotCaseScopeModeIsImmutable(t *testing.T) {
	contract := PilotContract{AuthorizationAliases: []string{"repo"}, RequireOrder: true, RequireLimit: true}
	query := "MATCH (repo:Repository {id: $repo_id}) RETURN repo ORDER BY repo.id LIMIT $limit"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(query)))
	caseSpec := PilotCase{VariantID: "all", CaseID: "page", ScopeMode: "all_scopes", QuerySHA256: digest}
	if err := ValidatePilotCaseQuery(queryKindCypher, query, contract, caseSpec, nil); err != nil {
		t.Fatalf("all-scopes case: %v", err)
	}
	caseSpec.ScopeMode = "scoped"
	if err := ValidatePilotCaseQuery(queryKindCypher, query, contract, caseSpec, nil); err == nil {
		t.Fatal("scoped case accepted unscoped text")
	}
	caseSpec.ScopeMode = "other"
	if err := ValidatePilotCaseQuery(queryKindCypher, query, contract, caseSpec, nil); err == nil {
		t.Fatal("accepted unknown scope mode")
	}
}

func TestPilotEvidenceRejectsMissingStaleAndUnexercised(t *testing.T) {
	manifest, evidence := pilotEvidenceFixture()
	if err := ValidatePilotEvidence(manifest, nil); err == nil {
		t.Fatal("accepted missing artifact")
	}
	if err := ValidatePilotEvidence(manifest, &evidence); err != nil {
		t.Fatalf("valid evidence: %v", err)
	}
	stale := evidence
	stale.Entries = append([]PilotEvidenceEntry(nil), evidence.Entries...)
	stale.Entries[0].SourceSHA256 = strings.Repeat("b", 64)
	if err := ValidatePilotEvidence(manifest, &stale); err == nil {
		t.Fatal("accepted stale source")
	}
	missing := evidence
	missing.Entries = nil
	if err := ValidatePilotEvidence(manifest, &missing); err == nil {
		t.Fatal("accepted unexercised required variant")
	}
	tooSlow := evidence
	tooSlow.Entries = append([]PilotEvidenceEntry(nil), evidence.Entries...)
	tooSlow.Entries[0].Cases = append([]PilotCaseEvidence(nil), evidence.Entries[0].Cases...)
	tooSlow.Entries[0].Cases[0].Candidate.ColdMilliseconds = []float64{2500, 2600}
	if err := ValidatePilotEvidence(manifest, &tooSlow); err == nil || !strings.Contains(err.Error(), "timing") {
		t.Fatalf("accepted normal timing over budget: %v", err)
	}
	tooMuchWork := evidence
	tooMuchWork.Entries = append([]PilotEvidenceEntry(nil), evidence.Entries...)
	tooMuchWork.Entries[0].Cases = append([]PilotCaseEvidence(nil), evidence.Entries[0].Cases...)
	tooMuchWork.Entries[0].Cases[0].Candidate.Work = json.RawMessage(`{"rows":1,"shared_blocks":101,"query_count":1}`)
	if err := ValidatePilotEvidence(manifest, &tooMuchWork); err == nil || !strings.Contains(err.Error(), "work metric") {
		t.Fatalf("accepted work over budget: %v", err)
	}
}
