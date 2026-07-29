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

// terraformConfigStateDriftOutcomeExact, terraformConfigStateDriftOutcome
// Ambiguous, and terraformConfigStateDriftOutcomeUnresolved are the three
// outcome values this domain reaches today. See
// go/internal/correlation/drift/tfconfigstate/doc.go for why "derived" and
// "stale" are still not emitted, and for the issue #5594 decision that made
// "unresolved" durable (superseding the #5442-era "unresolved stays log-only"
// call recorded there).
const (
	terraformConfigStateDriftOutcomeExact      = "exact"
	terraformConfigStateDriftOutcomeAmbiguous  = "ambiguous"
	terraformConfigStateDriftOutcomeUnresolved = "unresolved"
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
// terraform_config_state_drift reducer intent. Exactly one of Candidates,
// AmbiguousOwners, or UnresolvedOwner is populated per call:
//
//   - Candidates carries the admitted per-address candidates from
//     tfconfigstate.BuildCandidates (outcome "exact"); the other two fields
//     are zero.
//   - AmbiguousOwners carries the competing config-repo candidate rows from
//     tfstatebackend.AmbiguousBackendOwnerError (outcome "ambiguous");
//     Candidates is nil and UnresolvedOwner is false because no single anchor
//     was resolved to classify against.
//   - UnresolvedOwner is true when tfstatebackend.ErrNoConfigRepoOwnsBackend
//     was returned: zero config repos claim this backend at all, not even an
//     ambiguous set (outcome "unresolved", issue #5594). Candidates and
//     AmbiguousOwners are both nil/empty; there is no competing evidence to
//     record, only the absence of any owner.
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
	UnresolvedOwner bool
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
// per-address candidate, exactly one durable "ambiguous" fact for the whole
// state scope when AmbiguousOwners is populated, or exactly one durable
// "unresolved" fact for the whole state scope when UnresolvedOwner is true.
// Fact ids are stable by finding identity so reducer retries and replays
// upsert the same row instead of duplicating findings.
func (w PostgresTerraformConfigStateDriftWriter) WriteTerraformConfigStateDriftFindings(
	ctx context.Context,
	write TerraformConfigStateDriftWrite,
) (TerraformConfigStateDriftWriteResult, error) {
	if w.DB == nil {
		return TerraformConfigStateDriftWriteResult{}, fmt.Errorf("terraform config state drift database is required")
	}
	writeModes := 0
	if len(write.Candidates) > 0 {
		writeModes++
	}
	if len(write.AmbiguousOwners) > 0 {
		writeModes++
	}
	if write.UnresolvedOwner {
		writeModes++
	}
	if writeModes > 1 {
		return TerraformConfigStateDriftWriteResult{}, fmt.Errorf(
			"terraform config state drift write must carry exactly one of candidates, ambiguous owners, or unresolved owner",
		)
	}

	now := reducerWriterNow(w.Now)
	var rows []reducerFactVersionedRow
	var canonicalIDs []string

	switch {
	case write.UnresolvedOwner:
		row, canonicalID, err := unresolvedOwnerFactRow(write, now)
		if err != nil {
			return TerraformConfigStateDriftWriteResult{}, err
		}
		rows = append(rows, row)
		canonicalIDs = append(canonicalIDs, canonicalID)
	case len(write.AmbiguousOwners) > 0:
		row, canonicalID, err := ambiguousOwnerFactRow(write, now)
		if err != nil {
			return TerraformConfigStateDriftWriteResult{}, err
		}
		rows = append(rows, row)
		canonicalIDs = append(canonicalIDs, canonicalID)
	default:
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

// unresolvedOwnerFactRow persists tfstatebackend.ErrNoConfigRepoOwnsBackend as
// exactly one durable "unresolved" row for the whole state-snapshot scope,
// mirroring ambiguousOwnerFactRow's shape (Address/DriftKind empty, one row
// per scope+generation, upserted by stable fact id across replays). Unlike
// the ambiguous case there is no competing evidence to record --
// AmbiguousOwnerCandidates stays nil -- because zero repos claim this
// backend, not two-or-more. Issue #5594: before this, ErrNoConfigRepoOwnsBackend
// was log-only, so a caller reading this scope's findings could not tell
// "evaluated, no drift" (an empty page) apart from "ownership never
// resolved" (also an empty page); this row makes the rejection visible at
// the read surface the same way the ambiguous row already does.
func unresolvedOwnerFactRow(
	write TerraformConfigStateDriftWrite,
	now time.Time,
) (reducerFactVersionedRow, string, error) {
	stableKey := strings.Join([]string{
		"terraform_config_state_drift",
		"unresolved_owner",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
	}, ":")
	canonicalID := "canonical:" + stableKey
	factID := terraformConfigStateDriftFactKind + ":" + facts.StableID(
		terraformConfigStateDriftFactKind,
		map[string]any{
			"scope_id":      strings.TrimSpace(write.ScopeID),
			"generation_id": strings.TrimSpace(write.GenerationID),
			"outcome":       terraformConfigStateDriftOutcomeUnresolved,
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
		CandidateID:   "unresolved_owner:" + write.ScopeID,
		CandidateKind: "terraform_config_state_drift_unresolved_owner",
		Outcome:       terraformConfigStateDriftOutcomeUnresolved,
		BackendKind:   write.BackendKind,
		LocatorHash:   write.LocatorHash,
		Confidence:    1.0,
		Evidence:      nonNilMapSlice(nil),
		SourceLayers: []string{
			"source_declaration",
		},
	})
	if err != nil {
		return reducerFactVersionedRow{}, "", fmt.Errorf("encode terraform config state drift unresolved payload: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return reducerFactVersionedRow{}, "", fmt.Errorf("marshal terraform config state drift unresolved payload: %w", err)
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
