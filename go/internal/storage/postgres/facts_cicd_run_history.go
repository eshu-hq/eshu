// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	maxCICDRunHistoricalFacts          = 10_000
	maxPreviousCICDRunCorrelationFacts = 1_000
)

var cicdRunHistoricalFactKinds = []string{
	facts.CICDRunFactKind,
	facts.CICDArtifactFactKind,
	facts.CICDEnvironmentObservationFactKind,
	facts.CICDTriggerEdgeFactKind,
	facts.CICDStepFactKind,
}

const listCICDRunFactsForRunKeysQuery = `
WITH target_generation AS MATERIALIZED (
    SELECT generation.ingested_at
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.generation_id = $2
),
requested_run_keys AS MATERIALIZED (
    SELECT
        BTRIM(run_key.provider) AS provider,
        BTRIM(run_key.run_id) AS run_id,
        COALESCE(NULLIF(BTRIM(run_key.run_attempt), ''), '1') AS run_attempt
    FROM UNNEST($3::text[], $4::text[], $5::text[])
        AS run_key(provider, run_id, run_attempt)
),
ranked_run_facts AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.scope_id,
        fact.generation_id,
        fact.fact_kind,
        fact.stable_fact_key,
        fact.schema_version,
        fact.collector_kind,
        fact.fencing_token,
        fact.source_confidence,
        fact.source_system,
        fact.source_fact_key,
        COALESCE(fact.source_uri, '') AS source_uri,
        COALESCE(fact.source_record_id, '') AS source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload,
        ROW_NUMBER() OVER (
            PARTITION BY fact.fact_kind, fact.stable_fact_key
            ORDER BY generation.ingested_at DESC,
                     generation.generation_id DESC,
                     fact.observed_at DESC,
                     fact.fact_id DESC
        ) AS fact_rank
    FROM fact_records AS fact
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    CROSS JOIN target_generation
    WHERE fact.scope_id = $1
      AND fact.fact_kind = ANY($6::text[])
      AND generation.status IN ('active', 'completed', 'superseded')
      AND (generation.ingested_at, generation.generation_id)
          < (target_generation.ingested_at, $2)
),
latest_run_facts AS MATERIALIZED (
    SELECT *
    FROM ranked_run_facts AS fact
    WHERE fact.fact_rank = 1
      AND fact.is_tombstone = FALSE
      AND EXISTS (
          SELECT 1
          FROM requested_run_keys AS requested
          WHERE BTRIM(fact.payload->>'provider') = requested.provider
            AND BTRIM(fact.payload->>'run_id') = requested.run_id
            AND COALESCE(NULLIF(BTRIM(fact.payload->>'run_attempt'), ''), '1') = requested.run_attempt
      )
),
selected_runs AS MATERIALIZED (
    SELECT DISTINCT
        BTRIM(fact.payload->>'commit_sha') AS commit_sha,
        COALESCE(BTRIM(fact.payload->>'repository_id'), '') AS repository_id
    FROM latest_run_facts AS fact
    WHERE fact.fact_kind = 'ci.run'
      AND BTRIM(fact.payload->>'commit_sha') <> ''
),
ranked_deployment_facts AS MATERIALIZED (
    SELECT
        fact.fact_id,
        fact.scope_id,
        fact.generation_id,
        fact.fact_kind,
        fact.stable_fact_key,
        fact.schema_version,
        fact.collector_kind,
        fact.fencing_token,
        fact.source_confidence,
        fact.source_system,
        fact.source_fact_key,
        COALESCE(fact.source_uri, '') AS source_uri,
        COALESCE(fact.source_record_id, '') AS source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload,
        ROW_NUMBER() OVER (
            PARTITION BY fact.fact_kind, fact.stable_fact_key
            ORDER BY generation.ingested_at DESC,
                     generation.generation_id DESC,
                     fact.observed_at DESC,
                     fact.fact_id DESC
        ) AS fact_rank
    FROM fact_records AS fact
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    CROSS JOIN target_generation
    WHERE fact.scope_id = $1
      AND fact.fact_kind = 'ci.deployment_event'
      AND generation.status IN ('active', 'completed', 'superseded')
      AND (generation.ingested_at, generation.generation_id)
          < (target_generation.ingested_at, $2)
),
latest_deployment_facts AS MATERIALIZED (
    SELECT *
    FROM ranked_deployment_facts AS fact
    WHERE fact.fact_rank = 1
      AND fact.is_tombstone = FALSE
      AND EXISTS (
          SELECT 1
          FROM selected_runs AS run
          WHERE BTRIM(fact.payload->>'sha') = run.commit_sha
            AND (
                run.repository_id = ''
                OR COALESCE(BTRIM(fact.payload->>'repository_id'), '') = ''
                OR COALESCE(BTRIM(fact.payload->>'repository_id'), '') = run.repository_id
            )
      )
),
selected_facts AS (
    SELECT * FROM latest_run_facts
    UNION ALL
    SELECT * FROM latest_deployment_facts
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    is_tombstone,
    payload
FROM selected_facts
ORDER BY observed_at ASC, fact_id ASC
LIMIT $7
`

const listPreviousCICDRunCorrelationFactsQuery = `
WITH target_generation AS MATERIALIZED (
    SELECT generation.ingested_at
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.generation_id = $2
),
previous_generation AS MATERIALIZED (
    SELECT generation.generation_id
    FROM scope_generations AS generation
    CROSS JOIN target_generation
    WHERE generation.scope_id = $1
      AND generation.status IN ('active', 'completed', 'superseded')
      AND (generation.ingested_at, generation.generation_id)
          < (target_generation.ingested_at, $2)
    ORDER BY generation.ingested_at DESC, generation.generation_id DESC
    LIMIT 1
)
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN previous_generation
  ON previous_generation.generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'reducer_ci_cd_run_correlation'
  AND fact.is_tombstone = FALSE
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// ListCICDRunFactsForRunKeys returns the latest retained run-scoped evidence
// for exact provider/run/attempt keys, plus deployment events matching the
// recovered runs' commits, from successful generations strictly older than
// targetGenerationID. Tombstones participate in latest-row ranking before they
// are filtered, so retracted evidence cannot be resurrected.
func (s FactStore) ListCICDRunFactsForRunKeys(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
	providers []string,
	runIDs []string,
	runAttempts []string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	keys, err := cleanCICDRunHistoryKeys(providers, runIDs, runAttempts)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(targetGenerationID) == "" || len(keys) == 0 {
		return nil, nil
	}

	providers, runIDs, runAttempts = splitCICDRunHistoryKeys(keys)
	rows, err := s.db.QueryContext(
		ctx,
		listCICDRunFactsForRunKeysQuery,
		strings.TrimSpace(scopeID),
		strings.TrimSpace(targetGenerationID),
		pq.Array(providers),
		pq.Array(runIDs),
		pq.Array(runAttempts),
		pq.Array(cicdRunHistoricalFactKinds),
		maxCICDRunHistoricalFacts+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list historical ci/cd run facts: %w", err)
	}
	loaded, err := scanCICDRunHistoryRows(rows, "list historical ci/cd run facts")
	if err != nil {
		return nil, err
	}
	if len(loaded) > maxCICDRunHistoricalFacts {
		return nil, fmt.Errorf(
			"list historical ci/cd run facts: result exceeds safety cap %d for %d run keys",
			maxCICDRunHistoricalFacts,
			len(keys),
		)
	}
	return loaded, nil
}

// ListPreviousCICDRunCorrelationFacts returns the correlation snapshot from
// the immediately preceding successful generation. It deliberately does not
// skip an empty predecessor, which would resurrect a stale older snapshot.
func (s FactStore) ListPreviousCICDRunCorrelationFacts(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(targetGenerationID) == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(
		ctx,
		listPreviousCICDRunCorrelationFactsQuery,
		strings.TrimSpace(scopeID),
		strings.TrimSpace(targetGenerationID),
		maxPreviousCICDRunCorrelationFacts+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list previous ci/cd run correlation facts: %w", err)
	}
	loaded, err := scanCICDRunHistoryRows(rows, "list previous ci/cd run correlation facts")
	if err != nil {
		return nil, err
	}
	if len(loaded) > maxPreviousCICDRunCorrelationFacts {
		return nil, fmt.Errorf(
			"list previous ci/cd run correlation facts: result exceeds safety cap %d",
			maxPreviousCICDRunCorrelationFacts,
		)
	}
	return loaded, nil
}

type cicdRunHistoryKey struct {
	provider   string
	runID      string
	runAttempt string
}

func cleanCICDRunHistoryKeys(providers, runIDs, runAttempts []string) ([]cicdRunHistoryKey, error) {
	if len(providers) != len(runIDs) || len(providers) != len(runAttempts) {
		return nil, fmt.Errorf("ci/cd run history key columns must have equal lengths")
	}
	unique := make(map[cicdRunHistoryKey]struct{}, len(providers))
	for index := range providers {
		key := cicdRunHistoryKey{
			provider:   strings.TrimSpace(providers[index]),
			runID:      strings.TrimSpace(runIDs[index]),
			runAttempt: strings.TrimSpace(runAttempts[index]),
		}
		if key.runAttempt == "" {
			key.runAttempt = "1"
		}
		if key.provider == "" || key.runID == "" {
			continue
		}
		unique[key] = struct{}{}
	}
	keys := make([]cicdRunHistoryKey, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := keys[i].provider + "\x00" + keys[i].runID + "\x00" + keys[i].runAttempt
		right := keys[j].provider + "\x00" + keys[j].runID + "\x00" + keys[j].runAttempt
		return left < right
	})
	return keys, nil
}

func splitCICDRunHistoryKeys(keys []cicdRunHistoryKey) ([]string, []string, []string) {
	providers := make([]string, 0, len(keys))
	runIDs := make([]string, 0, len(keys))
	runAttempts := make([]string, 0, len(keys))
	for _, key := range keys {
		providers = append(providers, key.provider)
		runIDs = append(runIDs, key.runID)
		runAttempts = append(runAttempts, key.runAttempt)
	}
	return providers, runIDs, runAttempts
}

func scanCICDRunHistoryRows(rows Rows, operation string) ([]facts.Envelope, error) {
	defer func() { _ = rows.Close() }()
	loaded := make([]facts.Envelope, 0)
	for rows.Next() {
		envelope, err := scanFactEnvelope(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", operation, err)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return loaded, nil
}
