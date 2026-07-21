// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const (
	relationshipFamilyIndexProofDSNEnv = "ESHU_RELATIONSHIP_FAMILY_INDEX_PROOF_DSN"
	relationshipFamilyProofDatabase    = "ifa_relationship_family_proof"
	relationshipFamilyProofIndexName   = "fact_records_relationship_family_scope_generation_idx_v2"
)

type relationshipFamilyProofDB struct {
	admin  *sql.DB
	scoped *sql.DB
	schema string
}

type relationshipFamilyPlan struct {
	ExecutionMS float64
	PlanningMS  float64
	Root        relationshipFamilyPlanNode
}

type relationshipFamilyPlanNode struct {
	NodeType         string                       `json:"Node Type"`
	IndexName        string                       `json:"Index Name"`
	RelationName     string                       `json:"Relation Name"`
	ActualRows       float64                      `json:"Actual Rows"`
	ActualTotalTime  float64                      `json:"Actual Total Time"`
	SharedHitBlocks  int64                        `json:"Shared Hit Blocks"`
	SharedReadBlocks int64                        `json:"Shared Read Blocks"`
	Plans            []relationshipFamilyPlanNode `json:"Plans"`
}

func openRelationshipFamilyProofDB(t *testing.T) relationshipFamilyProofDB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(relationshipFamilyIndexProofDSNEnv))
	if dsn == "" {
		t.Skip("set ESHU_RELATIONSHIP_FAMILY_INDEX_PROOF_DSN to run the local relationship-family index Odù proof")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open proof postgres: %v", err)
	}
	var database string
	if err := admin.QueryRowContext(ctx, "SELECT current_database()").Scan(&database); err != nil {
		_ = admin.Close()
		t.Fatalf("read proof database name: %v", err)
	}
	if database != relationshipFamilyProofDatabase {
		_ = admin.Close()
		t.Fatalf("refusing relationship-family proof against database %q; want dedicated %q", database, relationshipFamilyProofDatabase)
	}
	schema := fmt.Sprintf("relationship_family_odu_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		_ = admin.Close()
		t.Fatalf("create proof schema: %v", err)
	}
	scopedDSN := relationshipFamilyDSNWithSearchPath(t, dsn, schema)
	scoped, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteSQLIdentifier(schema)+" CASCADE")
		_ = admin.Close()
		t.Fatalf("open scoped proof postgres: %v", err)
	}
	scoped.SetMaxOpenConns(20)
	scoped.SetMaxIdleConns(20)
	if _, err := scoped.ExecContext(ctx, deferredPartitionProofSchemaSQL); err != nil {
		_ = scoped.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+quoteSQLIdentifier(schema)+" CASCADE")
		_ = admin.Close()
		t.Fatalf("create relationship-family proof tables: %v", err)
	}
	proof := relationshipFamilyProofDB{admin: admin, scoped: scoped, schema: schema}
	t.Cleanup(func() {
		_ = proof.scoped.Close()
		_, _ = proof.admin.ExecContext(context.Background(), "DROP SCHEMA "+quoteSQLIdentifier(proof.schema)+" CASCADE")
		_ = proof.admin.Close()
	})
	return proof
}

func relationshipFamilyDSNWithSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse proof DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func seedRelationshipFamilyProofOdu(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) (ifa.Odu, []relationships.CatalogEntry, []scopeGenerationPartition) {
	t.Helper()
	odu := ifa.RepoDependencyBackfillProofOdu()
	type coordinate struct {
		scopeID      string
		generationID string
		observedAt   time.Time
	}
	coordinates := make(map[string]coordinate)
	for _, fact := range odu.Facts {
		coordinates[fact.ScopeID] = coordinate{
			scopeID:      fact.ScopeID,
			generationID: fact.GenerationID,
			observedAt:   fact.ObservedAt,
		}
	}
	keys := make([]string, 0, len(coordinates))
	for scopeID := range coordinates {
		keys = append(keys, scopeID)
	}
	sort.Strings(keys)
	for _, scopeID := range keys {
		item := coordinates[scopeID]
		if _, err := db.ExecContext(ctx,
			"INSERT INTO ingestion_scopes (scope_id, active_generation_id) VALUES ($1, $2)",
			item.scopeID, item.generationID); err != nil {
			t.Fatalf("seed scope %q: %v", item.scopeID, err)
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO scope_generations (generation_id, scope_id, ingested_at) VALUES ($1, $2, $3)",
			item.generationID, item.scopeID, item.observedAt); err != nil {
			t.Fatalf("seed generation %q: %v", item.generationID, err)
		}
	}
	store := NewFactStore(SQLDB{DB: db})
	if err := store.UpsertFacts(ctx, odu.Facts); err != nil {
		t.Fatalf("seed proof Odù facts: %v", err)
	}
	if err := refreshRelationshipReferenceCandidateKeys(ctx, SQLDB{DB: db}, odu.Facts); err != nil {
		t.Fatalf("seed proof relationship reference keys: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records; ANALYZE relationship_reference_candidate_keys"); err != nil {
		t.Fatalf("analyze proof facts: %v", err)
	}

	catalog := ifa.RepositoryCatalog(odu.Facts)
	partitions := relationshipFamilySourcePartitions(odu.Facts)
	if len(partitions) != 8 {
		t.Fatalf("proof source partitions = %d, want 8", len(partitions))
	}
	return odu, catalog, partitions
}

func relationshipFamilySourcePartitions(input []facts.Envelope) []scopeGenerationPartition {
	byKey := make(map[string]scopeGenerationPartition)
	for _, fact := range input {
		repoID, _ := fact.Payload["repo_id"].(string)
		if !strings.HasPrefix(repoID, "repository:source-") {
			continue
		}
		key := fact.ScopeID + "\x00" + fact.GenerationID
		byKey[key] = scopeGenerationPartition{ScopeID: fact.ScopeID, GenerationID: fact.GenerationID}
	}
	partitions := make([]scopeGenerationPartition, 0, len(byKey))
	for _, partition := range byKey {
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(i, j int) bool { return partitions[i].ScopeID < partitions[j].ScopeID })
	return partitions
}

func replaceRelationshipFamilyQueryFragment(
	t *testing.T,
	query string,
	oldFragment string,
	newFragment string,
	label string,
) string {
	t.Helper()
	if count := strings.Count(query, oldFragment); count != 1 {
		t.Fatalf("relationship-family query %s fragment count = %d, want exactly 1", label, count)
	}
	return strings.Replace(query, oldFragment, newFragment, 1)
}

func relationshipFamilyOldQuery(t *testing.T) string {
	t.Helper()
	query := listDeferredScopedRelationshipFactRecordsQuery
	sourcePredicate := "AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')\n      AND " +
		deferredRelationshipFamilyCandidatePredicateSQL
	query = replaceRelationshipFamilyQueryFragment(
		t,
		query,
		sourcePredicate,
		"AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')",
		"source predicate",
	)
	candidateReferenceCTE := `candidate_reference_keys AS MATERIALIZED (
    SELECT ref.fact_id, ref.source_repo_id, ref.reference_key
    FROM relationship_family_payload_facts AS fact
    JOIN relationship_reference_candidate_keys AS ref
      ON ref.fact_id = fact.fact_id
     AND ref.scope_id = $3
     AND ref.generation_id = $4
),
`
	query = replaceRelationshipFamilyQueryFragment(t, query, candidateReferenceCTE, "", "candidate reference CTE")
	newReferenceArm := `        SELECT ref.fact_id
        FROM candidate_reference_keys AS ref
        JOIN unnest($2::text[], $7::text[]) AS catalog_repo_id(value, reference_key)
          ON catalog_repo_id.value <> ref.source_repo_id
         AND position('|' || catalog_repo_id.reference_key || '|' in ref.reference_key) > 0`
	oldReferenceArm := `        SELECT fact.fact_id
        FROM relationship_family_payload_facts AS fact
        WHERE EXISTS (
          SELECT 1
          FROM relationship_reference_candidate_keys AS ref
          JOIN unnest($2::text[], $7::text[]) AS catalog_repo_id(value, reference_key)
            ON catalog_repo_id.value <> ref.source_repo_id
           AND position('|' || catalog_repo_id.reference_key || '|' in ref.reference_key) > 0
          WHERE ref.fact_id = fact.fact_id
            AND ref.scope_id = $3
            AND ref.generation_id = $4
        )`
	return replaceRelationshipFamilyQueryFragment(t, query, newReferenceArm, oldReferenceArm, "reference arm")
}

func relationshipFamilyCandidateQuery(t *testing.T) string {
	t.Helper()
	query := relationshipFamilyOldQuery(t)
	needle := "AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')"
	replacement := needle + "\n      AND " + deferredRelationshipFamilyCandidatePredicateSQL
	return replaceRelationshipFamilyQueryFragment(t, query, needle, replacement, "candidate source predicate")
}

func relationshipFamilyNarrowReferenceQuery() string {
	return listDeferredScopedRelationshipFactRecordsQuery
}

func relationshipFamilyAliasArmQuery() string {
	return latestGenerationCTE + `,
source_facts AS MATERIALIZED (
    SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind, fact.payload
    FROM fact_records AS fact
    WHERE fact.scope_id = $2
      AND fact.generation_id = $3
      AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
),
relationship_family_payload_facts AS MATERIALIZED (
    SELECT fact.fact_id, lower(fact.payload::text) AS payload_lower
    FROM source_facts AS fact
    ` + deferredRelationshipFamilyPayloadFactsFilterSQL + `
)
SELECT fact.fact_id
FROM relationship_family_payload_facts AS fact
JOIN latest_generations AS latest
  ON latest.scope_id = $2
 AND latest.generation_id = $3
WHERE fact.payload_lower LIKE ANY($1)
ORDER BY fact.fact_id
`
}

func createRelationshipFamilyProofIndex(t *testing.T, ctx context.Context, db *sql.DB) time.Duration {
	t.Helper()
	started := time.Now()
	if _, err := db.ExecContext(ctx, MigrationSQL("relationship_family_candidate_index")); err != nil {
		t.Fatalf("create relationship-family proof index: %v", err)
	}
	duration := time.Since(started)
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze indexed proof facts: %v", err)
	}
	return duration
}

func explainRelationshipFamilyQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args ...any,
) relationshipFamilyPlan {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain relationship-family query: %v", err)
	}
	var documents []struct {
		Plan          relationshipFamilyPlanNode `json:"Plan"`
		PlanningTime  float64                    `json:"Planning Time"`
		ExecutionTime float64                    `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &documents); err != nil || len(documents) != 1 {
		t.Fatalf("decode relationship-family plan: err=%v raw=%s", err, raw)
	}
	return relationshipFamilyPlan{
		ExecutionMS: documents[0].ExecutionTime,
		PlanningMS:  documents[0].PlanningTime,
		Root:        documents[0].Plan,
	}
}

func relationshipFamilyPlanUsesIndex(node relationshipFamilyPlanNode, indexName string) bool {
	if node.IndexName == indexName {
		return true
	}
	for _, child := range node.Plans {
		if relationshipFamilyPlanUsesIndex(child, indexName) {
			return true
		}
	}
	return false
}

func relationshipFamilyPlanBuffers(node relationshipFamilyPlanNode) (int64, int64) {
	// PostgreSQL's root plan node already reports the inclusive statement
	// totals. Summing descendants would count the same block accesses again.
	return node.SharedHitBlocks, node.SharedReadBlocks
}

func relationshipFamilyPlanSummary(node relationshipFamilyPlanNode) string {
	var lines []string
	var walk func(relationshipFamilyPlanNode, int)
	walk = func(current relationshipFamilyPlanNode, depth int) {
		lines = append(lines, fmt.Sprintf(
			"%s%s relation=%s index=%s rows=%.0f total_ms=%.3f",
			strings.Repeat("  ", depth),
			current.NodeType,
			current.RelationName,
			current.IndexName,
			current.ActualRows,
			current.ActualTotalTime,
		))
		for _, child := range current.Plans {
			walk(child, depth+1)
		}
	}
	walk(node, 0)
	return strings.Join(lines, "\n")
}
