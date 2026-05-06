package backendconformance

import (
	"context"
	"fmt"
	"strings"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const corpusTimeout = 30 * time.Second

// GraphQuery is the read-only graph port exercised by conformance runs.
type GraphQuery interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// ReadCase is one backend-neutral graph read shape.
type ReadCase struct {
	Name       string
	Capability CapabilityClass
	Cypher     string
	Parameters map[string]any
	MinRows    int
}

// WriteCase is one backend-neutral graph write shape.
type WriteCase struct {
	Name                  string
	Capability            CapabilityClass
	RequireAtomicGroup    bool
	TransactionVisibility string
	Statements            []sourcecypher.Statement
}

// Report captures the result of a conformance corpus run.
type Report struct {
	Results []CaseResult
}

// CaseResult captures one completed read or write case.
type CaseResult struct {
	Name       string
	Capability CapabilityClass
	Rows       int
	Statements int
}

// DefaultReadCorpus returns the deterministic read corpus used as the common
// graph-query adapter smoke for Chunk 5 backend conformance.
func DefaultReadCorpus() []ReadCase {
	return []ReadCase{
		{
			Name:       "direct repository read",
			Capability: CapabilityDirectGraphReads,
			Cypher: `MATCH (r:Repository {id: $repo_id})
RETURN r.id AS id, r.name AS name
LIMIT 1`,
			Parameters: map[string]any{"repo_id": "repo:backend-conformance"},
			MinRows:    1,
		},
		{
			Name:       "one-hop call traversal",
			Capability: CapabilityPathTraversal,
			Cypher: `MATCH (caller:Function {uid: $caller_uid})-[:CALLS]->(callee:Function {uid: $callee_uid})
RETURN caller.uid AS caller_uid, callee.uid AS callee_uid
LIMIT 1`,
			Parameters: map[string]any{
				"caller_uid": "function:backend-conformance:caller",
				"callee_uid": "function:backend-conformance:callee",
			},
			MinRows: 1,
		},
		{
			Name:       "dead-code candidate readiness",
			Capability: CapabilityDeadCodeReadiness,
			Cypher: `MATCH (f:Function {repo_id: $repo_id})
WHERE NOT EXISTS { MATCH (f)<-[:CALLS]-(:Function) }
RETURN f.uid AS uid, f.name AS name
ORDER BY f.name
LIMIT 25`,
			Parameters: map[string]any{"repo_id": "repo:backend-conformance"},
			MinRows:    1,
		},
		{
			Name:       "canonical file entity containment readback",
			Capability: CapabilityCanonicalWrites,
			Cypher: `MATCH (f:File {path: $file_path})-[rel:CONTAINS]->(n:Function {uid: $entity_uid})
WITH count(rel) AS contains_count, count(DISTINCT f) AS file_count, count(DISTINCT n) AS entity_count
WHERE contains_count = 1 AND file_count = 1 AND entity_count = 1
RETURN contains_count, file_count, entity_count`,
			Parameters: map[string]any{
				"file_path":  "backend-conformance/src/example.go",
				"entity_uid": "function:backend-conformance:file-entity",
			},
			MinRows: 1,
		},
	}
}

// DefaultWriteCorpus returns the deterministic write corpus used as the common
// Cypher executor smoke for Chunk 5 backend conformance.
func DefaultWriteCorpus() []WriteCase {
	return []WriteCase{
		{
			Name:       "canonical repository upsert",
			Capability: CapabilityCanonicalWrites,
			Statements: []sourcecypher.Statement{{
				Operation: sourcecypher.OperationCanonicalUpsert,
				Cypher: `MERGE (r:Repository {id: $repo_id})
SET r.name = $name`,
				Parameters: map[string]any{
					"repo_id": "repo:backend-conformance",
					"name":    "backend-conformance",
				},
			}},
		},
		{
			Name:                  "canonical call edge atomic visibility",
			Capability:            CapabilityCanonicalWrites,
			RequireAtomicGroup:    true,
			TransactionVisibility: "caller, callee, and CALLS edge must commit together",
			Statements: []sourcecypher.Statement{
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (caller:Function {uid: $caller_uid})
SET caller.repo_id = $repo_id,
    caller.name = $caller_name`,
					Parameters: map[string]any{
						"caller_uid":  "function:backend-conformance:caller",
						"caller_name": "ExampleCaller",
						"repo_id":     "repo:backend-conformance",
					},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (callee:Function {uid: $callee_uid})
SET callee.repo_id = $repo_id,
    callee.name = $callee_name`,
					Parameters: map[string]any{
						"callee_uid":  "function:backend-conformance:callee",
						"callee_name": "ExampleCallee",
						"repo_id":     "repo:backend-conformance",
					},
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (caller:Function {uid: $caller_uid})
MATCH (callee:Function {uid: $callee_uid})
MERGE (caller)-[:CALLS]->(callee)`,
					Parameters: map[string]any{
						"caller_uid": "function:backend-conformance:caller",
						"callee_uid": "function:backend-conformance:callee",
					},
				},
			},
		},
		{
			Name:                  "canonical file entity containment idempotency",
			Capability:            CapabilityCanonicalWrites,
			RequireAtomicGroup:    true,
			TransactionVisibility: "repository, directory, file, entity, and CONTAINS edge must commit together",
			Statements: []sourcecypher.Statement{
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (r:Repository {id: $repo_id})
SET r.name = $repo_name,
    r.path = $repo_path,
    r.scope_id = $scope_id,
    r.generation_id = $generation_id,
    r.evidence_source = 'projector/canonical'`,
					Parameters: backendConformanceContainmentParams(),
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (r:Repository {id: $repo_id})
MERGE (d:Directory {path: $dir_path})
SET d.name = $dir_name,
    d.repo_id = $repo_id,
    d.scope_id = $scope_id,
    d.generation_id = $generation_id
MERGE (r)-[rel:CONTAINS]->(d)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = $generation_id`,
					Parameters: backendConformanceContainmentParams(),
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (r:Repository {id: $repo_id})
MATCH (d:Directory {path: $dir_path})
MERGE (f:File {path: $file_path})
SET f.name = $file_name,
    f.relative_path = $relative_path,
    f.language = $language,
    f.lang = $language,
    f.repo_id = $repo_id,
    f.scope_id = $scope_id,
    f.generation_id = $generation_id,
    f.evidence_source = 'projector/canonical'
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = $generation_id
MERGE (d)-[dirRel:CONTAINS]->(f)
SET dirRel.evidence_source = 'projector/canonical',
    dirRel.generation_id = $generation_id`,
					Parameters: backendConformanceContainmentParams(),
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MERGE (n:Function {uid: $entity_uid})
SET n.id = $entity_uid,
    n.name = $entity_name,
    n.path = $file_path,
    n.relative_path = $relative_path,
    n.line_number = $line_number,
    n.start_line = $line_number,
    n.end_line = $line_number,
    n.repo_id = $repo_id,
    n.language = $language,
    n.lang = $language,
    n.scope_id = $scope_id,
    n.generation_id = $generation_id,
    n.evidence_source = 'projector/canonical'`,
					Parameters: backendConformanceContainmentParams(),
				},
				{
					Operation: sourcecypher.OperationCanonicalUpsert,
					Cypher: `MATCH (f:File {path: $file_path})
MATCH (n:Function {uid: $entity_uid})
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = $generation_id`,
					Parameters: backendConformanceContainmentParams(),
				},
			},
		},
	}
}

// backendConformanceContainmentParams returns the deterministic fixture values
// shared by the canonical file/entity containment write case.
func backendConformanceContainmentParams() map[string]any {
	return map[string]any{
		"repo_id":       "repo:backend-conformance",
		"repo_name":     "backend-conformance",
		"repo_path":     "backend-conformance",
		"dir_path":      "backend-conformance/src",
		"dir_name":      "src",
		"file_path":     "backend-conformance/src/example.go",
		"file_name":     "example.go",
		"relative_path": "src/example.go",
		"entity_uid":    "function:backend-conformance:file-entity",
		"entity_name":   "ExampleFileEntity",
		"language":      "go",
		"line_number":   int64(7),
		"scope_id":      "scope:backend-conformance",
		"generation_id": "generation:backend-conformance",
	}
}

// RunReadCorpus runs read cases against a GraphQuery implementation and returns
// a report with row counts per case.
func RunReadCorpus(ctx context.Context, graph GraphQuery, cases []ReadCase) (Report, error) {
	if graph == nil {
		return Report{}, fmt.Errorf("graph query is required")
	}
	if len(cases) == 0 {
		return Report{}, fmt.Errorf("read corpus must include at least one case")
	}

	report := Report{Results: make([]CaseResult, 0, len(cases))}
	for _, tc := range cases {
		if err := validateReadCase(tc); err != nil {
			return Report{}, err
		}
		caseCtx, cancel := context.WithTimeout(ctx, corpusTimeout)
		rows, err := graph.Run(caseCtx, tc.Cypher, tc.Parameters)
		cancel()
		if err != nil {
			return Report{}, fmt.Errorf("read case %q: %w", tc.Name, err)
		}
		if tc.MinRows > 0 && len(rows) < tc.MinRows {
			return Report{}, fmt.Errorf("read case %q returned %d rows, want at least %d", tc.Name, len(rows), tc.MinRows)
		}
		report.Results = append(report.Results, CaseResult{
			Name:       tc.Name,
			Capability: tc.Capability,
			Rows:       len(rows),
		})
	}
	return report, nil
}

// RunWriteCorpus runs write cases against a Cypher executor. Cases that require
// atomic visibility must run through sourcecypher.GroupExecutor.
func RunWriteCorpus(ctx context.Context, executor sourcecypher.Executor, cases []WriteCase) (Report, error) {
	if executor == nil {
		return Report{}, fmt.Errorf("cypher executor is required")
	}
	if len(cases) == 0 {
		return Report{}, fmt.Errorf("write corpus must include at least one case")
	}

	report := Report{Results: make([]CaseResult, 0, len(cases))}
	for _, tc := range cases {
		if err := validateWriteCase(tc); err != nil {
			return Report{}, err
		}
		if tc.RequireAtomicGroup {
			groupExecutor, ok := executor.(sourcecypher.GroupExecutor)
			if !ok {
				return Report{}, fmt.Errorf("write case %q requires grouped execution", tc.Name)
			}
			if err := groupExecutor.ExecuteGroup(ctx, tc.Statements); err != nil {
				return Report{}, fmt.Errorf("write case %q: %w", tc.Name, err)
			}
		} else {
			for _, stmt := range tc.Statements {
				if err := executor.Execute(ctx, stmt); err != nil {
					return Report{}, fmt.Errorf("write case %q: %w", tc.Name, err)
				}
			}
		}
		report.Results = append(report.Results, CaseResult{
			Name:       tc.Name,
			Capability: tc.Capability,
			Statements: len(tc.Statements),
		})
	}
	return report, nil
}

// RunPhaseWriteCorpus runs write cases against a phase-group executor. This is
// the default shape for NornicDB canonical writes, where Eshu commits bounded
// phase batches instead of assuming one transaction for a whole materialization.
func RunPhaseWriteCorpus(ctx context.Context, executor sourcecypher.PhaseGroupExecutor, cases []WriteCase) (Report, error) {
	if executor == nil {
		return Report{}, fmt.Errorf("phase-group cypher executor is required")
	}
	if len(cases) == 0 {
		return Report{}, fmt.Errorf("write corpus must include at least one case")
	}

	report := Report{Results: make([]CaseResult, 0, len(cases))}
	for _, tc := range cases {
		if err := validateWriteCase(tc); err != nil {
			return Report{}, err
		}
		if err := executor.ExecutePhaseGroup(ctx, tc.Statements); err != nil {
			return Report{}, fmt.Errorf("write case %q: %w", tc.Name, err)
		}
		report.Results = append(report.Results, CaseResult{
			Name:       tc.Name,
			Capability: tc.Capability,
			Statements: len(tc.Statements),
		})
	}
	return report, nil
}

// validateReadCase rejects incomplete read cases and obvious mutation shapes
// before they can reach a backend.
func validateReadCase(tc ReadCase) error {
	if strings.TrimSpace(tc.Name) == "" {
		return fmt.Errorf("read case name is required")
	}
	if tc.Capability == "" {
		return fmt.Errorf("read case %q capability is required", tc.Name)
	}
	if strings.TrimSpace(tc.Cypher) == "" {
		return fmt.Errorf("read case %q cypher is required", tc.Name)
	}
	if containsMutationKeyword(tc.Cypher) {
		return fmt.Errorf("read case %q contains a mutation keyword", tc.Name)
	}
	if tc.MinRows < 0 {
		return fmt.Errorf("read case %q minimum rows must be zero or positive", tc.Name)
	}
	return nil
}

// validateWriteCase rejects incomplete write cases and empty statement groups.
func validateWriteCase(tc WriteCase) error {
	if strings.TrimSpace(tc.Name) == "" {
		return fmt.Errorf("write case name is required")
	}
	if tc.Capability == "" {
		return fmt.Errorf("write case %q capability is required", tc.Name)
	}
	if len(tc.Statements) == 0 {
		return fmt.Errorf("write case %q statements are required", tc.Name)
	}
	if tc.RequireAtomicGroup && strings.TrimSpace(tc.TransactionVisibility) == "" {
		return fmt.Errorf("write case %q transaction visibility note is required", tc.Name)
	}
	for i, stmt := range tc.Statements {
		if strings.TrimSpace(stmt.Cypher) == "" {
			return fmt.Errorf("write case %q statement %d cypher is required", tc.Name, i)
		}
	}
	return nil
}

// containsMutationKeyword applies a conservative lexical guard to corpus read
// cases so the suite cannot accidentally mutate a live proof database.
func containsMutationKeyword(cypher string) bool {
	tokens := strings.FieldsFunc(cypher, isCypherTokenSeparator)
	for i, token := range tokens {
		upper := strings.ToUpper(token)
		if _, ok := readMutationKeywords[upper]; ok {
			return true
		}
		if upper == "LOAD" && i+1 < len(tokens) && strings.ToUpper(tokens[i+1]) == "CSV" {
			return true
		}
	}
	return false
}

var readMutationKeywords = map[string]struct{}{
	"CREATE":  {},
	"MERGE":   {},
	"DELETE":  {},
	"DETACH":  {},
	"SET":     {},
	"REMOVE":  {},
	"DROP":    {},
	"FOREACH": {},
}

// isCypherTokenSeparator splits on non-identifier runes so mutation checks use
// whole Cypher keywords instead of substring matching.
func isCypherTokenSeparator(r rune) bool {
	if r == '_' {
		return false
	}
	if r >= '0' && r <= '9' {
		return false
	}
	if r >= 'A' && r <= 'Z' {
		return false
	}
	if r >= 'a' && r <= 'z' {
		return false
	}
	return true
}
