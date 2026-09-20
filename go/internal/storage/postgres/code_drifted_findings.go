// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// driftedFindingsQuery reads active reducer_code_drifted_finding facts for
// one repository. Active means the scope's active generation (the retire is
// per-generation, so older generations' rows stay) and no tombstones. $1 is
// the fact kind, $2 the payload repo_id; the optional $3 narrows hydration
// to the page window's finding ids. It never reads source_cache: every
// member field the read surface reports rides in the fact payload (#6835
// contract gate).
const driftedFindingsQuery = `
SELECT fact.schema_version, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = false
  AND fact.payload->>'repo_id' = $2
  AND (cardinality($3::text[]) = 0 OR fact.payload->>'finding_id' = ANY($3))
`

// PostgresCodeDriftedFindingStore serves admitted drifted pairs back to the
// code-divergence read surface from the shared fact store.
type PostgresCodeDriftedFindingStore struct {
	DB db.Queryer
}

// DriftedFindingStats returns one stat per active drifted finding in the
// repo: the writer finding id as fingerprint with the pair token max, so
// the merged cross-kind page ranks drifted pairs on the same members x
// tokens currency as the equality kinds.
func (s PostgresCodeDriftedFindingStore) DriftedFindingStats(
	ctx context.Context,
	repoID string,
) ([]codedivergence.GroupStat, error) {
	envelopes, err := s.queryDriftedEnvelopes(ctx, repoID, nil)
	if err != nil {
		return nil, err
	}
	stats := make([]codedivergence.GroupStat, 0, len(envelopes))
	for _, env := range envelopes {
		finding, err := decodeDriftedFindingPayload(env)
		if err != nil {
			return nil, err
		}
		tokens := finding.MemberA.TokenCount
		if finding.MemberB.TokenCount > tokens {
			tokens = finding.MemberB.TokenCount
		}
		stats = append(stats, codedivergence.GroupStat{
			Kind:        codedivergence.KindDrifted,
			Fingerprint: finding.FindingID,
			Members:     2,
			Tokens:      tokens,
		})
	}
	return stats, nil
}

// DriftedFindingRows hydrates exactly the given finding ids into drifted
// rows keyed by finding id. The caller passes only the page window, so a
// large active set never turns hydration into a full-repo fetch.
func (s PostgresCodeDriftedFindingStore) DriftedFindingRows(
	ctx context.Context,
	repoID string,
	findingIDs []string,
) (map[string]codedivergence.DriftedRow, error) {
	if len(findingIDs) == 0 {
		return map[string]codedivergence.DriftedRow{}, nil
	}
	envelopes, err := s.queryDriftedEnvelopes(ctx, repoID, findingIDs)
	if err != nil {
		return nil, err
	}
	rows := make(map[string]codedivergence.DriftedRow, len(envelopes))
	for _, env := range envelopes {
		finding, err := decodeDriftedFindingPayload(env)
		if err != nil {
			return nil, err
		}
		rows[finding.FindingID] = codedivergence.DriftedRow{
			FindingID:   finding.FindingID,
			Similarity:  finding.Similarity,
			Threshold:   finding.Threshold,
			SharedBands: finding.SharedBands,
			Members: []codedivergence.Member{
				driftedPayloadMember(finding.MemberA),
				driftedPayloadMember(finding.MemberB),
			},
		}
	}
	return rows, nil
}

func (s PostgresCodeDriftedFindingStore) queryDriftedEnvelopes(
	ctx context.Context,
	repoID string,
	findingIDs []string,
) ([]facts.Envelope, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("code drifted database is required")
	}
	// #nosec G201 -- the statement is a static const; only values bind.
	rows, err := s.DB.QueryContext(ctx, driftedFindingsQuery, facts.ReducerCodeDriftedFindingFactKind, repoID, pgarray.StringArray(findingIDs))
	if err != nil {
		return nil, fmt.Errorf("query code drifted findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	envelopes := make([]facts.Envelope, 0, 64)
	for rows.Next() {
		var schemaVersion, payload string
		if err := rows.Scan(&schemaVersion, &payload); err != nil {
			return nil, fmt.Errorf("scan code drifted finding payload: %w", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return nil, fmt.Errorf("decode code drifted finding payload: %w", err)
		}
		envelopes = append(envelopes, facts.Envelope{
			FactKind:      facts.ReducerCodeDriftedFindingFactKind,
			SchemaVersion: schemaVersion,
			Payload:       decoded,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return envelopes, nil
}

// decodeDriftedFindingPayload decodes one stored envelope through the typed
// factschema seam, so a malformed drifted fact fails the read instead of
// assembling into a finding. This call is also the kind's registered read
// consumer for the fact-kind consumer gate.
func decodeDriftedFindingPayload(env facts.Envelope) (reducerderivedv1.CodeDriftedFinding, error) {
	finding, err := factschema.DecodeReducerCodeDriftedFinding(postgresFactschemaEnvelope(env))
	if err != nil {
		return reducerderivedv1.CodeDriftedFinding{}, fmt.Errorf("decode code drifted finding payload: %w", err)
	}
	return finding, nil
}

func driftedPayloadMember(m reducerderivedv1.CodeDriftedMember) codedivergence.Member {
	return codedivergence.Member{
		EntityID:     m.EntityID,
		EntityName:   m.EntityName,
		EntityType:   m.EntityType,
		RelativePath: m.RelativePath,
		Language:     m.Language,
		StartLine:    m.StartLine,
		EndLine:      m.EndLine,
		TokenCount:   m.TokenCount,
	}
}
