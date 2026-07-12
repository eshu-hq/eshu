// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type proofDomainTx struct {
	db    *proofDomainDB
	state proofState
}

func (tx *proofDomainTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	_ = ctx
	switch {
	case strings.Contains(query, "pg_advisory_xact_lock"):
		return proofResult{}, nil
	case strings.Contains(query, "UPDATE scope_generations") && strings.Contains(query, "status = 'superseded'"):
		scopeID := args[1].(string)
		generationID := args[2].(string)
		for key, generation := range tx.state.generations {
			if generation.ScopeID != scopeID || generation.GenerationID == generationID {
				continue
			}
			if generation.Status != scope.GenerationStatusActive {
				continue
			}
			generation.Status = scope.GenerationStatusSuperseded
			tx.state.generations[key] = generation
		}
		return proofResult{}, nil
	case strings.Contains(query, "UPDATE scope_generations") && strings.Contains(query, "activated_at = COALESCE"):
		scopeID := args[1].(string)
		generationID := args[2].(string)
		for key, generation := range tx.state.generations {
			if generation.ScopeID != scopeID || generation.GenerationID != generationID {
				continue
			}
			generation.Status = scope.GenerationStatusActive
			tx.state.generations[key] = generation
		}
		return proofResult{}, nil
	case strings.Contains(query, "UPDATE ingestion_scopes") && strings.Contains(query, "active_generation_id = $3"):
		scopeID := args[1].(string)
		generationID := args[2].(string)
		tx.state.activeGenerations[scopeID] = generationID
		tx.state.scopeStatuses[scopeID] = string(scope.GenerationStatusActive)
		return proofResult{}, nil
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'succeeded'"):
		for key, item := range tx.state.workItems {
			if item.stage != "projector" || item.scopeID != args[1].(string) || item.generationID != args[2].(string) || item.leaseOwner != args[3].(string) {
				continue
			}
			item.status = "succeeded"
			item.updatedAt = tx.db.now
			item.leaseOwner = ""
			item.claimUntil = time.Time{}
			tx.state.workItems[key] = item
			return proofResult{}, nil
		}
		return nil, fmt.Errorf("projector work item not found for scope=%s generation=%s", args[1].(string), args[2].(string))
	case strings.Contains(query, "INSERT INTO ingestion_scopes"):
		metadata := map[string]string{}
		if payload, err := unmarshalPayload(args[11].([]byte)); err == nil {
			for key, value := range payload {
				if text, ok := value.(string); ok && text != "" {
					metadata[key] = text
				}
			}
		}
		scopeID := args[0].(string)
		incomingStatus := args[9].(string)
		incomingActiveGenerationID := stringFromAny(args[10])
		if existingActiveGenerationID := tx.state.activeGenerations[scopeID]; existingActiveGenerationID != "" && incomingActiveGenerationID == "" && incomingStatus == "pending" {
			incomingStatus = tx.state.scopeStatuses[scopeID]
			incomingActiveGenerationID = existingActiveGenerationID
		}
		scopeValue := scope.IngestionScope{
			ScopeID:       scopeID,
			ScopeKind:     scope.ScopeKind(args[1].(string)),
			SourceSystem:  args[2].(string),
			ParentScopeID: stringFromAny(args[4]),
			CollectorKind: scope.CollectorKind(args[5].(string)),
			PartitionKey:  args[6].(string),
			Metadata:      metadata,
		}
		tx.state.scopes[scopeValue.ScopeID] = scopeValue
		tx.state.scopeStatuses[scopeValue.ScopeID] = incomingStatus
		if incomingActiveGenerationID != "" {
			tx.state.activeGenerations[scopeValue.ScopeID] = incomingActiveGenerationID
		}
		return proofResult{}, nil
	case strings.Contains(query, "INSERT INTO scope_generations"):
		generation := scope.ScopeGeneration{
			GenerationID:    args[0].(string),
			ScopeID:         args[1].(string),
			TriggerKind:     scope.TriggerKind(args[2].(string)),
			FreshnessHint:   stringFromAny(args[3]),
			SourceCommitSHA: stringFromAny(args[4]),
			IsDelta:         args[5].(bool),
			ObservedAt:      args[6].(time.Time).UTC(),
			IngestedAt:      args[7].(time.Time).UTC(),
			Status:          scope.GenerationStatus(args[8].(string)),
		}
		if existing, ok := tx.state.generations[generation.GenerationID]; ok && existing.Status == scope.GenerationStatusActive && generation.Status == scope.GenerationStatusPending && existing.FreshnessHint == generation.FreshnessHint {
			generation.Status = existing.Status
			generation.ObservedAt = existing.ObservedAt
			generation.IngestedAt = existing.IngestedAt
		}
		tx.state.generations[generation.GenerationID] = generation
		return proofResult{}, nil
	case strings.Contains(query, "INSERT INTO fact_work_items") && strings.Contains(query, "'projector'"):
		workItemID := args[0].(string)
		if _, exists := tx.state.workItems[workItemID]; exists {
			return proofResult{}, nil
		}
		workItem := proofWorkItem{
			workItemID:   workItemID,
			stage:        "projector",
			domain:       args[3].(string),
			status:       "pending",
			attemptCount: 0,
			scopeID:      args[1].(string),
			generationID: args[2].(string),
			visibleAt:    args[4].(time.Time).UTC(),
			createdAt:    args[4].(time.Time).UTC(),
			updatedAt:    args[4].(time.Time).UTC(),
		}
		tx.state.workItems[workItem.workItemID] = workItem
		return proofResult{}, nil
	case strings.Contains(query, "DELETE FROM relationship_family_candidate_fact_ids"):
		return proofResult{}, nil
	case strings.Contains(query, "INSERT INTO relationship_family_candidate_fact_ids"):
		return proofResult{}, nil
	case strings.Contains(query, "DELETE FROM relationship_reference_candidate_keys"):
		return proofResult{}, nil
	case strings.Contains(query, "INSERT INTO relationship_reference_candidate_keys"):
		return proofResult{}, nil
	case strings.Contains(query, "INSERT INTO relationship_evidence_facts"):
		details := parseJSONBytes(args[10])
		tx.state.evidenceFacts[args[0].(string)] = evidenceRecord{
			generationID:   args[1].(string),
			evidenceKind:   args[2].(string),
			relType:        args[3].(string),
			sourceRepoID:   nullableToString(args[4]),
			targetRepoID:   nullableToString(args[5]),
			sourceEntityID: nullableToString(args[6]),
			targetEntityID: nullableToString(args[7]),
			confidence:     args[8].(float64),
			rationale:      args[9].(string),
			details:        details,
		}
		return proofResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected tx exec query: %s", query)
	}
}

func (tx *proofDomainTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "WITH latest_generations AS"):
		return newProofRows(proofLatestRelationshipFactRows(tx.state)), nil
	case strings.Contains(query, "FROM fact_records") && strings.Contains(query, "fact_kind = 'repository'"):
		return newProofRows(proofRepositoryCatalogRows(tx.state.facts)), nil
	case strings.Contains(query, "INSERT INTO fact_records") && strings.Contains(query, "RETURNING fact_id"):
		accepted, err := proofUpsertFactRecordsReturningAccepted(tx.state.facts, args)
		if err != nil {
			return nil, err
		}
		return newProofRows(accepted), nil
	default:
		return nil, errors.New("unexpected query in transaction")
	}
}

// proofUpsertFactRecordsReturningAccepted simulates the production
// upsertFactBatchSuffixReturningFactID upsert against the in-memory proof
// state: a fact_id is accepted (returned) when it is new or its incoming
// fencing_token is >= the currently stored token, mirroring
// "WHERE fact_records.fencing_token <= EXCLUDED.fencing_token" (issue #4444).
// A fenced-out fact_id is neither written to state.facts nor returned, so
// callers that filter afterBatch by the returned set see the same
// derived-evidence behavior the real guarded UPSERT produces.
func proofUpsertFactRecordsReturningAccepted(state map[string]facts.Envelope, args []any) ([][]any, error) {
	if len(args)%columnsPerFactRow != 0 {
		return nil, fmt.Errorf("fact batch args = %d, not a multiple of %d", len(args), columnsPerFactRow)
	}

	var accepted [][]any
	for off := 0; off < len(args); off += columnsPerFactRow {
		a := args[off : off+columnsPerFactRow]
		payload, err := unmarshalPayload(a[16].([]byte))
		if err != nil {
			return nil, err
		}
		envelope := facts.Envelope{
			FactID:           a[0].(string),
			ScopeID:          a[1].(string),
			GenerationID:     a[2].(string),
			FactKind:         a[3].(string),
			StableFactKey:    a[4].(string),
			SchemaVersion:    a[5].(string),
			CollectorKind:    a[6].(string),
			FencingToken:     a[7].(int64),
			SourceConfidence: a[8].(string),
			ObservedAt:       a[13].(time.Time).UTC(),
			IsTombstone:      a[15].(bool),
			Payload:          payload,
			SourceRef: facts.Ref{
				SourceSystem:   a[9].(string),
				ScopeID:        a[1].(string),
				GenerationID:   a[2].(string),
				FactKey:        a[10].(string),
				SourceURI:      stringFromAny(a[11]),
				SourceRecordID: stringFromAny(a[12]),
			},
		}

		if existing, ok := state[envelope.FactID]; ok && existing.FencingToken > envelope.FencingToken {
			continue // fenced out: stale fencing_token loses the WHERE guard
		}
		state[envelope.FactID] = envelope
		accepted = append(accepted, []any{envelope.FactID})
	}

	return accepted, nil
}

func (tx *proofDomainTx) Commit() error {
	tx.db.state = tx.state
	return nil
}

func (tx *proofDomainTx) Rollback() error { return nil }
