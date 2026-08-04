// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	maxCICDRunHistoricalFacts = 12_000
)

var cicdRunHistoricalFactKinds = []string{
	facts.CICDRunFactKind,
	facts.CICDArtifactFactKind,
	facts.CICDWorkflowImageEvidenceFactKind,
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
requested_artifact_tombstones AS MATERIALIZED (
    SELECT BTRIM(stable_fact_key) AS stable_fact_key
    FROM UNNEST($6::text[]) AS tombstone(stable_fact_key)
    WHERE BTRIM(stable_fact_key) <> ''
),
retained_run_facts AS MATERIALIZED (
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
        generation.ingested_at AS generation_ingested_at
    FROM fact_records AS fact
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    CROSS JOIN target_generation
    WHERE fact.scope_id = $1
      AND fact.fact_kind = ANY($7::text[])
      AND generation.status IN ('active', 'completed', 'superseded')
      AND (generation.ingested_at, generation.generation_id)
          < (target_generation.ingested_at, $2)
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
        fact.source_uri,
        fact.source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload,
        fact.generation_ingested_at,
        ROW_NUMBER() OVER (
            PARTITION BY fact.fact_kind, fact.stable_fact_key
            ORDER BY fact.generation_ingested_at DESC,
                     fact.generation_id DESC,
                     fact.observed_at DESC,
                     fact.fact_id DESC
        ) AS fact_rank
    FROM retained_run_facts AS fact
),
ranked_tombstone_artifact_identities AS MATERIALIZED (
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
        fact.source_uri,
        fact.source_record_id,
        fact.observed_at,
        fact.is_tombstone,
        fact.payload,
        fact.generation_ingested_at,
        ROW_NUMBER() OVER (
            PARTITION BY fact.stable_fact_key
            ORDER BY fact.generation_ingested_at DESC,
                     fact.generation_id DESC,
                     fact.observed_at DESC,
                     fact.fact_id DESC
        ) AS fact_rank
    FROM retained_run_facts AS fact
    WHERE NOT $9::boolean
      AND fact.fact_kind = 'ci.artifact'
      AND fact.is_tombstone = FALSE
      AND BTRIM(fact.payload->>'provider') <> ''
      AND BTRIM(fact.payload->>'run_id') <> ''
      AND EXISTS (
          SELECT 1
          FROM requested_artifact_tombstones AS requested
          WHERE requested.stable_fact_key = fact.stable_fact_key
      )
),
latest_tombstone_artifact_identities AS MATERIALIZED (
    SELECT *
    FROM ranked_tombstone_artifact_identities AS fact
    WHERE fact.fact_rank = 1
),
latest_scope_run_generation AS MATERIALIZED (
    SELECT fact.generation_id, fact.generation_ingested_at
    FROM retained_run_facts AS fact
    WHERE $9::boolean
      AND fact.fact_kind = 'ci.run'
    ORDER BY fact.generation_ingested_at DESC, fact.generation_id DESC
    LIMIT 1
),
effective_run_keys AS MATERIALIZED (
    SELECT provider, run_id, run_attempt
    FROM requested_run_keys
    UNION
    SELECT
        BTRIM(fact.payload->>'provider'),
        BTRIM(fact.payload->>'run_id'),
        COALESCE(NULLIF(BTRIM(fact.payload->>'run_attempt'), ''), '1')
    FROM latest_tombstone_artifact_identities AS fact
    WHERE NOT $9::boolean
    UNION
    SELECT
        BTRIM(fact.payload->>'provider'),
        BTRIM(fact.payload->>'run_id'),
        COALESCE(NULLIF(BTRIM(fact.payload->>'run_attempt'), ''), '1')
    FROM ranked_run_facts AS fact
    JOIN latest_scope_run_generation AS snapshot
      ON snapshot.generation_id = fact.generation_id
    WHERE fact.fact_rank = 1
      AND fact.fact_kind = 'ci.run'
      AND fact.is_tombstone = FALSE
      AND BTRIM(fact.payload->>'provider') <> ''
      AND BTRIM(fact.payload->>'run_id') <> ''
),
latest_run_facts AS MATERIALIZED (
    SELECT *
    FROM ranked_run_facts AS fact
    WHERE fact.fact_rank = 1
      AND fact.is_tombstone = FALSE
      AND EXISTS (
          SELECT 1
          FROM effective_run_keys AS requested
          WHERE BTRIM(fact.payload->>'provider') = requested.provider
            AND BTRIM(fact.payload->>'run_id') = requested.run_id
            AND COALESCE(NULLIF(BTRIM(fact.payload->>'run_attempt'), ''), '1') = requested.run_attempt
      )
      AND (
          NOT $9::boolean
          OR fact.fact_kind = 'ci.run'
          OR EXISTS (
              SELECT 1
              FROM latest_scope_run_generation AS snapshot
              WHERE (fact.generation_ingested_at, fact.generation_id)
                    >= (snapshot.generation_ingested_at, snapshot.generation_id)
          )
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
latest_workflow_image_facts AS MATERIALIZED (
    SELECT *
    FROM ranked_run_facts AS fact
    WHERE fact.fact_rank = 1
      AND fact.fact_kind = 'ci.workflow_image_evidence'
      AND fact.is_tombstone = FALSE
      AND (
          NOT $9::boolean
          OR EXISTS (
              SELECT 1
              FROM latest_scope_run_generation AS snapshot
              WHERE (fact.generation_ingested_at, fact.generation_id)
                    >= (snapshot.generation_ingested_at, snapshot.generation_id)
          )
      )
      AND EXISTS (
          SELECT 1
          FROM selected_runs AS run
          WHERE run.repository_id <> ''
            AND BTRIM(fact.payload->>'repository_id') = run.repository_id
      )
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
        generation.ingested_at AS generation_ingested_at,
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
      AND (
          NOT $9::boolean
          OR EXISTS (
              SELECT 1
              FROM latest_scope_run_generation AS snapshot
              WHERE (fact.generation_ingested_at, fact.generation_id)
                    >= (snapshot.generation_ingested_at, snapshot.generation_id)
          )
      )
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
    UNION
    SELECT * FROM latest_tombstone_artifact_identities
    UNION ALL
    SELECT * FROM latest_deployment_facts
    UNION ALL
    SELECT * FROM latest_workflow_image_facts
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
LIMIT $8
`

// ListCICDRunFactsForRunKeys returns the latest retained run-scoped evidence
// for exact provider/run/attempt keys, repository-scoped workflow-image
// evidence, and deployment events matching the recovered runs' commits, from
// successful generations strictly older than targetGenerationID. Tombstones
// participate in latest-row ranking before they are filtered, so retracted
// evidence cannot be resurrected. Artifact tombstone keys recover their latest
// payload-bearing predecessor only as routing identity; callers must not
// classify that predecessor as live proof.
func (s FactStore) ListCICDRunFactsForRunKeys(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
	providers []string,
	runIDs []string,
	runAttempts []string,
	artifactTombstoneKeys []string,
) ([]facts.Envelope, error) {
	return s.listCICDRunFacts(
		ctx,
		scopeID,
		targetGenerationID,
		providers,
		runIDs,
		runAttempts,
		artifactTombstoneKeys,
		false,
	)
}

// ListCICDRunFactsForScopePatch returns the latest older normal run snapshot,
// later retained patch evidence, and ci.run anchors for exact live artifact
// keys. Evidence older than the normal snapshot stays excluded except for those
// run anchors, and artifact tombstones never seed omitted runs.
func (s FactStore) ListCICDRunFactsForScopePatch(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
	providers []string,
	runIDs []string,
	runAttempts []string,
) ([]facts.Envelope, error) {
	return s.listCICDRunFacts(
		ctx,
		scopeID,
		targetGenerationID,
		providers,
		runIDs,
		runAttempts,
		nil,
		true,
	)
}

func (s FactStore) listCICDRunFacts(
	ctx context.Context,
	scopeID string,
	targetGenerationID string,
	providers []string,
	runIDs []string,
	runAttempts []string,
	artifactTombstoneKeys []string,
	includeScopeSnapshot bool,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	keys, err := cleanCICDRunHistoryKeys(providers, runIDs, runAttempts)
	if err != nil {
		return nil, err
	}
	artifactTombstoneKeys = cleanCICDArtifactTombstoneKeys(artifactTombstoneKeys)
	runKeyCount := len(keys)
	artifactTombstoneKeyCount := len(artifactTombstoneKeys)
	if strings.TrimSpace(scopeID) == "" || strings.TrimSpace(targetGenerationID) == "" ||
		(len(keys) == 0 && len(artifactTombstoneKeys) == 0 && !includeScopeSnapshot) {
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
		pq.Array(artifactTombstoneKeys),
		pq.Array(cicdRunHistoricalFactKinds),
		maxCICDRunHistoricalFacts+1,
		includeScopeSnapshot,
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
			"list historical ci/cd run facts: result exceeds safety cap %d for %d run keys and %d artifact tombstone keys",
			maxCICDRunHistoricalFacts,
			runKeyCount,
			artifactTombstoneKeyCount,
		)
	}
	return loaded, nil
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
