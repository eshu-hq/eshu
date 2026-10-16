// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// awsDriftStateARNBoundary marks the end of the shared prefix of the drift
// state join: everything before it (the requested_arns and
// aws_generation_arns CTEs, through the closing parenthesis) is common to the
// shipped statement and the #7666 reference statement.
const awsDriftStateARNBoundary = "),\nstate_arn_matches AS MATERIALIZED ("

// awsDriftStateARNReferenceTail is the pre-#7666 final SELECT: the direct
// join whose plan looped the active-state join per ARN. It is the semantic
// reference the fenced statement must reproduce row for row, in order.
const awsDriftStateARNReferenceTail = `)
SELECT
    fact.scope_id           AS state_scope_id,
    fact.generation_id      AS state_generation_id,
    fact.payload->>'address' AS address,
    fact.payload            AS payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN aws_generation_arns AS aws_arn
  ON aws_arn.arn = fact.payload->'attributes'->>'arn'
WHERE fact.fact_kind = 'terraform_state_resource'
  AND btrim(COALESCE(fact.payload->'attributes'->>'arn', '')) <> ''
ORDER BY aws_arn.arn ASC, fact.scope_id ASC, fact.payload->>'address' ASC, fact.fact_id ASC
`

// awsDriftStateARNSharedPrefix returns the shipped statement's text before the
// state_arn_matches CTE.
func awsDriftStateARNSharedPrefix(t *testing.T) string {
	t.Helper()
	if got := strings.Count(listActiveStateResourcesForAWSARNsQuery, awsDriftStateARNBoundary); got != 1 {
		t.Fatalf("shipped state join holds the %q boundary %d times, want exactly 1", awsDriftStateARNBoundary, got)
	}
	prefix, _, _ := strings.Cut(listActiveStateResourcesForAWSARNsQuery, awsDriftStateARNBoundary)
	return prefix
}

// awsDriftStateARNReferenceQuery derives the #7666 reference statement from
// the shipped constant: the shipped requested_arns and aws_generation_arns
// CTEs verbatim, then the pre-#7666 direct join. Deriving instead of copying
// keeps the reference's input set identical to production's.
func awsDriftStateARNReferenceQuery(t *testing.T) string {
	t.Helper()
	return awsDriftStateARNSharedPrefix(t) + awsDriftStateARNReferenceTail
}

// TestAWSCloudRuntimeDriftStateARNReferenceSharesShippedPrefix is the
// hermetic guard for the row differential: the reference must start with a
// byte prefix of the shipped statement that carries both input CTEs, so the
// differential compares only the state-side join.
func TestAWSCloudRuntimeDriftStateARNReferenceSharesShippedPrefix(t *testing.T) {
	prefix := awsDriftStateARNSharedPrefix(t)
	if !strings.HasPrefix(listActiveStateResourcesForAWSARNsQuery, prefix) {
		t.Fatal("derived prefix is not a byte prefix of the shipped state join")
	}
	for _, want := range []string{
		"requested_arns AS (",
		"jsonb_array_elements_text($3::jsonb)",
		"aws_generation_arns AS (",
		"fact.scope_id = $1",
		"fact.generation_id = $2",
	} {
		if !strings.Contains(prefix, want) {
			t.Fatalf("shared prefix lacks %q; the boundary moved:\n%s", want, prefix)
		}
	}
	if strings.Contains(prefix, "state_arn_matches") {
		t.Fatal("shared prefix reaches into the state_arn_matches CTE")
	}
	if !strings.HasPrefix(awsDriftStateARNReferenceQuery(t), prefix) {
		t.Fatal("reference statement does not start with the shipped prefix")
	}
}

// TestAWSCloudRuntimeDriftStateARNJoinKeepsBothFences pins the two fences
// the #7666 measurements showed are load-bearing: without MATERIALIZED the
// planner folds the CTE and can loop the active-scope join per ARN; without
// the OFFSET 0 LATERAL a stale fact_kind estimate walks the whole ARN index
// (278,044 buffers on the shim instead of 2,668).
func TestAWSCloudRuntimeDriftStateARNJoinKeepsBothFences(t *testing.T) {
	compact := strings.Join(strings.Fields(listActiveStateResourcesForAWSARNsQuery), " ")
	for _, want := range []string{
		"state_arn_matches AS MATERIALIZED (",
		"CROSS JOIN LATERAL (",
		"AND fact.payload->'attributes'->>'arn' = aws_arn.arn OFFSET 0 ) AS state",
		"FROM state_arn_matches AS state JOIN ingestion_scopes AS scope",
		"ORDER BY state.arn ASC, state.scope_id ASC, state.payload->>'address' ASC, state.fact_id ASC",
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("state join lacks %q:\n%s", want, listActiveStateResourcesForAWSARNsQuery)
		}
	}
}

// TestFactRecordsTerraformStateResourceARNIndexMatchesDriftProbe locks
// migration 180's key and predicate to the drift statement's LATERAL probe:
// a partial index is only usable when the query implies its predicate, and
// an expression index only when the query names the same expression.
func TestFactRecordsTerraformStateResourceARNIndexMatchesDriftProbe(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("migrations", "180_fact_records_terraform_state_resource_arn_idx.sql"))
	if err != nil {
		t.Fatalf("read migration 180: %v", err)
	}
	statement := regexp.MustCompile(`(?m)^--.*$`).ReplaceAllString(string(raw), "")
	compactMigration := strings.Join(strings.Fields(statement), " ")
	wantMigration := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + awsDriftStateARNIndexName +
		" ON fact_records ((payload->'attributes'->>'arn'))" +
		" WHERE fact_kind = 'terraform_state_resource'" +
		" AND btrim(COALESCE(payload->'attributes'->>'arn', '')) <> '';"
	if compactMigration != wantMigration {
		t.Fatalf("migration 180 statement =\n%s\nwant\n%s", compactMigration, wantMigration)
	}

	compactQuery := strings.Join(strings.Fields(listActiveStateResourcesForAWSARNsQuery), " ")
	wantProbe := "WHERE fact.fact_kind = 'terraform_state_resource'" +
		" AND btrim(COALESCE(fact.payload->'attributes'->>'arn', '')) <> ''" +
		" AND fact.payload->'attributes'->>'arn' = aws_arn.arn"
	if !strings.Contains(compactQuery, wantProbe) {
		t.Fatalf("drift state probe no longer matches the migration 180 key and predicate; want %q in:\n%s",
			wantProbe, listActiveStateResourcesForAWSARNsQuery)
	}
}
