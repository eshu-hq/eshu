// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// PostgresCodeDriftedWriter persists admitted drifted pairs into the shared
// fact store as reducer_code_drifted_finding rows.
type PostgresCodeDriftedWriter struct {
	DB  factwrite.Execer
	Now func() time.Time
}

// WriteDriftedFindings stores one durable fact per admitted pair. Fact ids
// are stable by finding identity so reducer retries and replays upsert the
// same row instead of duplicating findings. A write with zero pairs still
// runs the generation-authoritative retire: an empty pass is the complete
// assessment of (ScopeID, GenerationID), so every stale finding under it
// goes.
func (w PostgresCodeDriftedWriter) WriteDriftedFindings(
	ctx context.Context,
	write DriftedWrite,
) (DriftedWriteResult, error) {
	if w.DB == nil {
		return DriftedWriteResult{}, fmt.Errorf("code drifted database is required")
	}
	now := factwrite.Now(w.Now)
	rows := make([]factwrite.VersionedRow, 0, len(write.Pairs))
	findingIDs := make([]string, 0, len(write.Pairs))
	for _, pair := range write.Pairs {
		row, factID, err := driftedPairFactRow(write, pair, now)
		if err != nil {
			return DriftedWriteResult{}, err
		}
		rows = append(rows, row)
		findingIDs = append(findingIDs, factID)
	}
	if err := factwrite.BatchInsertVersionedFacts(ctx, w.DB, rows); err != nil {
		return DriftedWriteResult{}, fmt.Errorf("write code drifted facts: %w", err)
	}
	// Generation-authoritative retire: this write is the complete,
	// mutually-exclusive assessment of (ScopeID, GenerationID) for this
	// pass, so any OTHER code drifted finding under the same
	// (scope_id, generation_id) is stale -- e.g. a prior pass's pair that
	// no longer verifies. Runs after the insert succeeds so the retire only
	// ever removes rows this pass's own fresh write has superseded, never a
	// still-in-flight batch's rows.
	if err := retireDriftedFindings(ctx, w.DB, write.ScopeID, write.GenerationID, findingIDs); err != nil {
		return DriftedWriteResult{}, err
	}
	return DriftedWriteResult{Written: len(rows)}, nil
}

// driftedPairFactRow builds the durable row for one admitted pair. The
// stable key binds (kind, scope, generation, pair identity): the same pair
// re-derived in a later generation keeps its finding id (continuity for the
// read surface) while the fact row is generation-scoped, so each
// generation's retire only touches its own rows.
func driftedPairFactRow(
	write DriftedWrite,
	admitted AdmittedPair,
	now time.Time,
) (factwrite.VersionedRow, string, error) {
	findingID := DriftedFindingID(write.RepoID, admitted.Pair.A, admitted.Pair.B)
	stableKey := strings.Join([]string{
		"code_drifted",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		findingID,
	}, ":")
	factID := facts.ReducerCodeDriftedFindingFactKind + ":" + facts.StableID(
		facts.ReducerCodeDriftedFindingFactKind,
		map[string]any{
			"scope_id":      strings.TrimSpace(write.ScopeID),
			"generation_id": strings.TrimSpace(write.GenerationID),
			"finding_id":    findingID,
		},
	)

	payload, err := factschema.EncodeReducerCodeDriftedFinding(reducerderivedv1.CodeDriftedFinding{
		ReducerDomain: string(reducercontract.DomainCodeDrifted),
		IntentID:      write.IntentID,
		ScopeID:       write.ScopeID,
		GenerationID:  write.GenerationID,
		SourceSystem:  write.SourceSystem,
		Cause:         write.Cause,
		FindingID:     findingID,
		RepoID:        write.RepoID,
		Similarity:    admitted.Similarity,
		Threshold:     DriftedSimilarityThreshold,
		SharedBands:   admitted.Pair.SharedBands,
		MemberA:       driftedMemberPayload(admitted.Pair.A),
		MemberB:       driftedMemberPayload(admitted.Pair.B),
		TruthLevel:    facts.SourceConfidenceDerived,
		Suppressions:  suppressionCountsPayload(write.Suppressions),
		Evidence:      payloadcore.NonNilMapSlice(driftEvidencePayload(admitted.Evidence)),
		SourceLayers:  []string{"source_declaration"},
	})
	if err != nil {
		return factwrite.VersionedRow{}, "", fmt.Errorf("encode code drifted payload: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return factwrite.VersionedRow{}, "", fmt.Errorf("marshal code drifted payload: %w", err)
	}

	return factwrite.VersionedRow{
		FactID:           factID,
		ScopeID:          write.ScopeID,
		GenerationID:     write.GenerationID,
		FactKind:         facts.ReducerCodeDriftedFindingFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
		CollectorKind:    factwrite.CollectorKind(write.SourceSystem),
		SourceConfidence: facts.SourceConfidenceDerived,
		SourceSystem:     write.SourceSystem,
		SourceFactKey:    write.IntentID,
		ObservedAt:       now,
		IngestedAt:       now,
		Payload:          string(payloadJSON),
	}, factID, nil
}

// driftedMemberPayload renders a member row into the fact payload shape.
func driftedMemberPayload(m MemberRow) reducerderivedv1.CodeDriftedMember {
	return reducerderivedv1.CodeDriftedMember{
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

// suppressionCountsPayload renders the generation suppression totals as the
// payload's JSON-number map. A nil map (no evaluations) stays nil so the
// omitempty key drops: the writer only stamps totals it actually computed.
func suppressionCountsPayload(counts map[string]int) map[string]any {
	if len(counts) == 0 {
		return nil
	}
	out := make(map[string]any, len(counts))
	for rule, count := range counts {
		out[rule] = count
	}
	return out
}

// driftEvidencePayload renders the drift evidence atoms into the payload's
// evidence array with fixed keys. A nil input encodes as an empty array
// (never null) to match the reducer convention.
func driftEvidencePayload(evidence []model.EvidenceAtom) []map[string]any {
	if len(evidence) == 0 {
		return []map[string]any{}
	}
	maps := make([]map[string]any, 0, len(evidence))
	for _, atom := range evidence {
		maps = append(maps, map[string]any{
			"id":            atom.ID,
			"source_system": atom.SourceSystem,
			"evidence_type": atom.EvidenceType,
			"scope_id":      atom.ScopeID,
			"key":           atom.Key,
			"value":         atom.Value,
			"confidence":    atom.Confidence,
		})
	}
	return maps
}
