// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

const terraformConfigStateDriftFactKind = facts.ReducerTerraformConfigStateDriftFindingFactKind

// terraformConfigStateDriftOutcomeExact and terraformConfigStateDriftOutcome
// Ambiguous are the two outcome values this domain reaches today. See
// go/internal/correlation/drift/tfconfigstate/doc.go for why "derived",
// "stale", "unresolved", and "rejected" are not emitted.
const (
	terraformConfigStateDriftOutcomeExact     = "exact"
	terraformConfigStateDriftOutcomeAmbiguous = "ambiguous"
)

// TerraformConfigStateDriftFindingWriter persists admitted per-address drift
// findings and ambiguous-owner rejections as durable reducer facts. The
// writer must be idempotent by finding identity so reducer retries do not
// duplicate rows.
type TerraformConfigStateDriftFindingWriter interface {
	WriteTerraformConfigStateDriftFindings(
		ctx context.Context,
		write TerraformConfigStateDriftWrite,
	) (TerraformConfigStateDriftWriteResult, error)
}

// TerraformConfigStateDriftWrite is the durable publication request for one
// terraform_config_state_drift reducer intent. Exactly one of Candidates or
// AmbiguousOwners is populated per call:
//
//   - Candidates carries the admitted per-address candidates from
//     tfconfigstate.BuildCandidates (outcome "exact"); AmbiguousOwners is nil.
//   - AmbiguousOwners carries the competing config-repo candidate rows from
//     tfstatebackend.AmbiguousBackendOwnerError (outcome "ambiguous");
//     Candidates is nil because no single anchor was resolved to classify
//     against.
type TerraformConfigStateDriftWrite struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Cause           string
	BackendKind     string
	LocatorHash     string
	Candidates      []model.Candidate
	AmbiguousOwners []tfstatebackend.TerraformBackendRow
}

// TerraformConfigStateDriftWriteResult summarizes durable Terraform
// config-vs-state drift publication.
type TerraformConfigStateDriftWriteResult struct {
	CanonicalIDs    []string
	CanonicalWrites int
	EvidenceSummary string
}

// PostgresTerraformConfigStateDriftWriter persists admitted Terraform
// config-vs-state drift findings into the shared fact store.
type PostgresTerraformConfigStateDriftWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteTerraformConfigStateDriftFindings stores one durable fact per admitted
// per-address candidate, or exactly one durable "ambiguous" fact for the
// whole state scope when AmbiguousOwners is populated. Fact ids are stable by
// finding identity so reducer retries and replays upsert the same row instead
// of duplicating findings.
func (w PostgresTerraformConfigStateDriftWriter) WriteTerraformConfigStateDriftFindings(
	ctx context.Context,
	write TerraformConfigStateDriftWrite,
) (TerraformConfigStateDriftWriteResult, error) {
	if w.DB == nil {
		return TerraformConfigStateDriftWriteResult{}, fmt.Errorf("terraform config state drift database is required")
	}
	if len(write.Candidates) > 0 && len(write.AmbiguousOwners) > 0 {
		return TerraformConfigStateDriftWriteResult{}, fmt.Errorf(
			"terraform config state drift write must carry candidates or ambiguous owners, not both",
		)
	}

	now := reducerWriterNow(w.Now)
	var rows []reducerFactVersionedRow
	var canonicalIDs []string

	if len(write.AmbiguousOwners) > 0 {
		row, canonicalID, err := ambiguousOwnerFactRow(write, now)
		if err != nil {
			return TerraformConfigStateDriftWriteResult{}, err
		}
		rows = append(rows, row)
		canonicalIDs = append(canonicalIDs, canonicalID)
	} else {
		for _, candidate := range write.Candidates {
			row, canonicalID, err := exactFindingFactRow(write, candidate, now)
			if err != nil {
				return TerraformConfigStateDriftWriteResult{}, err
			}
			rows = append(rows, row)
			canonicalIDs = append(canonicalIDs, canonicalID)
		}
	}

	if err := reducerBatchInsertVersionedFacts(ctx, w.DB, rows); err != nil {
		return TerraformConfigStateDriftWriteResult{}, fmt.Errorf("write terraform config state drift fact: %w", err)
	}

	return TerraformConfigStateDriftWriteResult{
		CanonicalIDs:    canonicalIDs,
		CanonicalWrites: len(canonicalIDs),
		EvidenceSummary: fmt.Sprintf("wrote terraform config state drift findings %d", len(canonicalIDs)),
	}, nil
}

func exactFindingFactRow(
	write TerraformConfigStateDriftWrite,
	candidate model.Candidate,
	now time.Time,
) (reducerFactVersionedRow, string, error) {
	driftKind := readDriftKindAtom(candidate)
	stableKey := strings.Join([]string{
		"terraform_config_state_drift",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.TrimSpace(candidate.CorrelationKey),
		strings.TrimSpace(driftKind),
	}, ":")
	canonicalID := "canonical:" + stableKey
	factID := terraformConfigStateDriftFactKind + ":" + facts.StableID(
		terraformConfigStateDriftFactKind,
		map[string]any{
			"scope_id":      strings.TrimSpace(write.ScopeID),
			"generation_id": strings.TrimSpace(write.GenerationID),
			"address":       strings.TrimSpace(candidate.CorrelationKey),
			"drift_kind":    strings.TrimSpace(driftKind),
		},
	)

	payload, err := factschema.EncodeReducerTerraformConfigStateDriftFinding(reducerderivedv1.TerraformConfigStateDriftFinding{
		ReducerDomain: string(DomainConfigStateDrift),
		IntentID:      write.IntentID,
		ScopeID:       write.ScopeID,
		GenerationID:  write.GenerationID,
		SourceSystem:  write.SourceSystem,
		Cause:         write.Cause,
		CanonicalID:   canonicalID,
		CandidateID:   candidate.ID,
		CandidateKind: candidate.Kind,
		Outcome:       terraformConfigStateDriftOutcomeExact,
		Address:       candidate.CorrelationKey,
		DriftKind:     driftKind,
		BackendKind:   write.BackendKind,
		LocatorHash:   write.LocatorHash,
		Confidence:    candidate.Confidence,
		Evidence:      nonNilMapSlice(driftEvidencePayload(candidate.Evidence)),
		SourceLayers: []string{
			"source_declaration",
			"observed_resource",
		},
	})
	if err != nil {
		return reducerFactVersionedRow{}, "", fmt.Errorf("encode terraform config state drift payload: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return reducerFactVersionedRow{}, "", fmt.Errorf("marshal terraform config state drift payload: %w", err)
	}

	return reducerFactVersionedRow{
		FactID:           factID,
		ScopeID:          write.ScopeID,
		GenerationID:     write.GenerationID,
		FactKind:         terraformConfigStateDriftFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
		CollectorKind:    reducerFactCollectorKind(write.SourceSystem),
		SourceConfidence: facts.SourceConfidenceInferred,
		SourceSystem:     write.SourceSystem,
		SourceFactKey:    write.IntentID,
		ObservedAt:       now,
		IngestedAt:       now,
		Payload:          string(payloadJSON),
	}, canonicalID, nil
}

func ambiguousOwnerFactRow(
	write TerraformConfigStateDriftWrite,
	now time.Time,
) (reducerFactVersionedRow, string, error) {
	stableKey := strings.Join([]string{
		"terraform_config_state_drift",
		"ambiguous_owner",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
	}, ":")
	canonicalID := "canonical:" + stableKey
	factID := terraformConfigStateDriftFactKind + ":" + facts.StableID(
		terraformConfigStateDriftFactKind,
		map[string]any{
			"scope_id":      strings.TrimSpace(write.ScopeID),
			"generation_id": strings.TrimSpace(write.GenerationID),
			"outcome":       terraformConfigStateDriftOutcomeAmbiguous,
		},
	)

	payload, err := factschema.EncodeReducerTerraformConfigStateDriftFinding(reducerderivedv1.TerraformConfigStateDriftFinding{
		ReducerDomain:            string(DomainConfigStateDrift),
		IntentID:                 write.IntentID,
		ScopeID:                  write.ScopeID,
		GenerationID:             write.GenerationID,
		SourceSystem:             write.SourceSystem,
		Cause:                    write.Cause,
		CanonicalID:              canonicalID,
		CandidateID:              "ambiguous_owner:" + write.ScopeID,
		CandidateKind:            "terraform_config_state_drift_ambiguous_owner",
		Outcome:                  terraformConfigStateDriftOutcomeAmbiguous,
		BackendKind:              write.BackendKind,
		LocatorHash:              write.LocatorHash,
		Confidence:               1.0,
		AmbiguousOwnerCandidates: ambiguousOwnerCandidatesPayload(write.AmbiguousOwners),
		Evidence:                 nonNilMapSlice(nil),
		SourceLayers: []string{
			"source_declaration",
		},
	})
	if err != nil {
		return reducerFactVersionedRow{}, "", fmt.Errorf("encode terraform config state drift ambiguous payload: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return reducerFactVersionedRow{}, "", fmt.Errorf("marshal terraform config state drift ambiguous payload: %w", err)
	}

	return reducerFactVersionedRow{
		FactID:           factID,
		ScopeID:          write.ScopeID,
		GenerationID:     write.GenerationID,
		FactKind:         terraformConfigStateDriftFactKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.ReducerDerivedSchemaVersionV1,
		CollectorKind:    reducerFactCollectorKind(write.SourceSystem),
		SourceConfidence: facts.SourceConfidenceInferred,
		SourceSystem:     write.SourceSystem,
		SourceFactKey:    write.IntentID,
		ObservedAt:       now,
		IngestedAt:       now,
		Payload:          string(payloadJSON),
	}, canonicalID, nil
}

// ambiguousOwnerCandidatesPayload converts the resolver's competing rows into
// the provenance-only evidence shape the payload carries. No candidate is
// picked or ranked; the order mirrors the query port's return order.
func ambiguousOwnerCandidatesPayload(rows []tfstatebackend.TerraformBackendRow) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"repo_id":            row.RepoID,
			"scope_id":           row.ScopeID,
			"commit_id":          row.CommitID,
			"commit_observed_at": row.CommitObservedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func driftEvidencePayload(evidence []model.EvidenceAtom) []map[string]any {
	if len(evidence) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(evidence))
	for _, atom := range evidence {
		out = append(out, map[string]any{
			"id":            atom.ID,
			"source_system": atom.SourceSystem,
			"evidence_type": atom.EvidenceType,
			"scope_id":      atom.ScopeID,
			"key":           atom.Key,
			"value":         atom.Value,
			"confidence":    atom.Confidence,
		})
	}
	return out
}
