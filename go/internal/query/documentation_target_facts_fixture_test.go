// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// targetFactsFixtureBase anchors every fixture observed_at so tied timestamps
// are exact.
var targetFactsFixtureBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// targetFactsFixtureScope names the two scopes the differential fixture uses.
const (
	targetFactsScopeA = "scope:target-facts-a"
	targetFactsScopeB = "scope:target-facts-b"
	targetFactsGenA   = "generation:target-facts-a"
	targetFactsGenB   = "generation:target-facts-b"
)

// targetFactRow is one fact_records row the fixture inserts.
type targetFactRow struct {
	ID        string
	Kind      string
	Scope     string
	Gen       string
	Offset    time.Duration // added to targetFactsFixtureBase; equal offsets tie
	Tombstone bool
	Payload   map[string]any
}

// refPayload is a payload whose candidate_refs, evidence_refs, or
// linked_entities (chosen by variant) name one target ref.
func refPayload(variant int, refKind, refID string) map[string]any {
	switch variant % 3 {
	case 0:
		return map[string]any{"candidate_refs": []map[string]string{{"kind": refKind, "id": refID}}}
	case 1:
		return map[string]any{"evidence_refs": []map[string]string{{"kind": refKind, "id": refID}}}
	default:
		return map[string]any{"linked_entities": []map[string]string{{"entity_type": refKind, "entity_id": refID}}}
	}
}

// seedTargetFactsScopes inserts the two ingestion scopes and generations the
// fixture facts point at.
func seedTargetFactsScopes(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, s := range [][2]string{{targetFactsScopeA, targetFactsGenA}, {targetFactsScopeB, targetFactsGenB}} {
		execProofStatements(t, ctx, db, []proofStatement{{`
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status
) VALUES ($1, 'repository', 'proof', $1, 'proof', 'proof', clock_timestamp(), clock_timestamp(), 'active')`, []any{s[0]}}, {`
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ($2, $1, 'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp())`, []any{s[0], s[1]}}})
	}
}

// insertTargetFacts inserts the rows one at a time; the differential fixture
// is small.
func insertTargetFacts(t *testing.T, ctx context.Context, db *sql.DB, rows []targetFactRow) {
	t.Helper()
	for _, row := range rows {
		payload, err := json.Marshal(row.Payload)
		if err != nil {
			t.Fatalf("marshal payload for %s: %v", row.ID, err)
		}
		observed := targetFactsFixtureBase.Add(row.Offset)
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
) VALUES ($1, $2, $3, $4, $1, 'proof', 'proof', $1, $5, $5, $6, $7::jsonb)
`, row.ID, row.Scope, row.Gen, row.Kind, observed, row.Tombstone, string(payload)); err != nil {
			t.Fatalf("insert fact %s: %v", row.ID, err)
		}
	}
}

// targetFactsCounts describes how many rows of each kind reference one target.
type targetFactsCounts struct {
	Mentions, Claims, Semantics int
}

// targetFactRows builds the matching rows for one repository target. Offsets
// cycle through four values so rows tie on observed_at both within and across
// kinds, which forces the fact_id tiebreak to decide the order.
func targetFactRows(target string, c targetFactsCounts) []targetFactRow {
	var rows []targetFactRow
	add := func(kind string, n int) {
		for i := 0; i < n; i++ {
			rows = append(rows, targetFactRow{
				ID:      fmt.Sprintf("fact:%s:%s:%02d", target, kind, i),
				Kind:    kind,
				Scope:   targetFactsScopeA,
				Gen:     targetFactsGenA,
				Offset:  time.Duration(i%4) * time.Second,
				Payload: refPayload(i, "repository", target),
			})
		}
	}
	add("documentation_entity_mention", c.Mentions)
	add("documentation_claim_candidate", c.Claims)
	add("semantic.documentation_observation", c.Semantics)
	return rows
}
