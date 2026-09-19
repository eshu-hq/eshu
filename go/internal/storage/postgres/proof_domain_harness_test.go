// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type recordingCanonicalWriter struct {
	calls []projector.CanonicalMaterialization
}

func (w *recordingCanonicalWriter) Write(_ context.Context, mat projector.CanonicalMaterialization) error {
	w.calls = append(w.calls, mat)
	return nil
}

type recordingContentWriter struct {
	calls []content.Materialization
}

func (w *recordingContentWriter) Write(_ context.Context, materialization content.Materialization) (content.Result, error) {
	w.calls = append(w.calls, materialization.Clone())
	return content.Result{RecordCount: len(materialization.Records)}, nil
}

type proofDomainDB struct {
	now           time.Time
	state         proofState
	reducerClaims int
	reducerAcked  int
}

type proofState struct {
	scopes            map[string]scope.IngestionScope
	scopeStatuses     map[string]string
	activeGenerations map[string]string
	generations       map[string]scope.ScopeGeneration
	facts             map[string]facts.Envelope
	evidenceFacts     map[string]evidenceRecord
	workItems         map[string]proofWorkItem
}

type proofWorkItem struct {
	workItemID   string
	stage        string
	domain       string
	status       string
	attemptCount int
	scopeID      string
	generationID string
	leaseOwner   string
	visibleAt    time.Time
	createdAt    time.Time
	updatedAt    time.Time
	claimUntil   time.Time
	payload      []byte
}

func newProofDomainDB(t *testing.T, now time.Time) *proofDomainDB {
	t.Helper()

	return &proofDomainDB{
		now: now.UTC(),
		state: proofState{
			scopes:            make(map[string]scope.IngestionScope),
			scopeStatuses:     make(map[string]string),
			activeGenerations: make(map[string]string),
			generations:       make(map[string]scope.ScopeGeneration),
			facts:             make(map[string]facts.Envelope),
			evidenceFacts:     make(map[string]evidenceRecord),
			workItems:         make(map[string]proofWorkItem),
		},
	}
}

func (database *proofDomainDB) Begin(context.Context) (db.Transaction, error) {
	return &proofDomainTx{
		database: database,
		state: proofState{
			scopes:            cloneScopes(database.state.scopes),
			scopeStatuses:     cloneStrings(database.state.scopeStatuses),
			activeGenerations: cloneStrings(database.state.activeGenerations),
			generations:       cloneGenerations(database.state.generations),
			facts:             cloneFacts(database.state.facts),
			evidenceFacts:     cloneEvidenceFacts(database.state.evidenceFacts),
			workItems:         cloneWorkItems(database.state.workItems),
		},
	}, nil
}

func (database *proofDomainDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'succeeded'"):
		if len(args) != 4 {
			return nil, fmt.Errorf("projector ack args = %d, want 4", len(args))
		}
		return database.updateWorkItemStatus("projector", args[1].(string), args[2].(string), args[3].(string), "succeeded")
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'dead_letter'"):
		if len(args) != 7 {
			return nil, fmt.Errorf("projector fail args = %d, want 7", len(args))
		}
		return database.updateWorkItemStatus("projector", args[4].(string), args[5].(string), args[6].(string), "dead_letter")
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'retrying'"):
		if len(args) != 8 {
			return nil, fmt.Errorf("projector retry args = %d, want 8", len(args))
		}
		return database.retryProjectorWork(args[5].(string), args[6].(string), args[7].(string), args[4].(time.Time))
	case strings.Contains(query, "stage = 'reducer'") && strings.Contains(query, "SET status = 'succeeded'"):
		if len(args) != 4 {
			return nil, fmt.Errorf("reducer ack args = %d, want 4", len(args))
		}
		return database.updateWorkItemStatusByID(args[1].(string), args[2].(string), "succeeded")
	case strings.Contains(query, "stage = 'reducer'") && strings.Contains(query, "SET status = 'retrying'"):
		if len(args) != 8 {
			return nil, fmt.Errorf("reducer retry args = %d, want 8", len(args))
		}
		return database.retryReducerWork(args[5].(string), args[6].(string), args[4].(time.Time))
	case strings.Contains(query, "stage = 'reducer'") && strings.Contains(query, "SET status = 'dead_letter'"):
		if len(args) != 7 {
			return nil, fmt.Errorf("reducer fail args = %d, want 7", len(args))
		}
		return database.updateWorkItemStatusByID(args[4].(string), args[5].(string), "dead_letter")
	case strings.Contains(query, "INSERT INTO fact_work_items") && strings.Contains(query, "'reducer'"):
		workItemID := args[0].(string)
		if _, exists := database.state.workItems[workItemID]; exists {
			return proofResult{}, nil
		}
		payload, err := unmarshalPayload(args[7].([]byte))
		if err != nil {
			return nil, err
		}
		workItem := proofWorkItem{
			workItemID:   workItemID,
			stage:        "reducer",
			domain:       args[3].(string),
			status:       "pending",
			scopeID:      args[1].(string),
			generationID: args[2].(string),
			visibleAt:    args[6].(time.Time).UTC(),
			createdAt:    args[6].(time.Time).UTC(),
			updatedAt:    args[6].(time.Time).UTC(),
			payload:      args[7].([]byte),
		}
		if len(payload) > 0 {
			workItem.payload = args[7].([]byte)
		}
		database.state.workItems[workItem.workItemID] = workItem
		return proofResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (database *proofDomainDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "SELECT generation.generation_id, COALESCE(generation.freshness_hint, '')"):
		if len(args) != 1 {
			return nil, fmt.Errorf("active generation freshness args = %d, want 1", len(args))
		}
		scopeID := args[0].(string)
		activeGenerationID := database.state.activeGenerations[scopeID]
		if activeGenerationID == "" {
			return newProofRows(nil), nil
		}
		generation, ok := database.state.generations[activeGenerationID]
		if !ok {
			return newProofRows(nil), nil
		}
		return newProofRows([][]any{{
			generation.GenerationID,
			generation.FreshnessHint,
		}}), nil
	case strings.Contains(query, "FROM ingestion_scopes") && strings.Contains(query, "GROUP BY status"):
		return newProofRows(proofScopeCountRows(database.state.scopeStatuses)), nil
	case strings.Contains(query, "FROM scope_generations") && strings.Contains(query, "GROUP BY status"):
		return newProofRows(proofGenerationCountRows(database.state.generations)), nil
	case query == producerActivityQuery:
		return newProofRows([][]any{{false, nil}}), nil
	case strings.Contains(query, "JOIN ingestion_scopes") && strings.Contains(query, "current_active_generation_id"):
		return newProofRows(
			proofGenerationTransitionRows(database.state.generations, database.state.activeGenerations, database.now),
		), nil
	case query == activeWorkSummaryQuery:
		if len(args) != 1 {
			return nil, fmt.Errorf("active work summary args = %d, want 1", len(args))
		}
		rows, err := proofActiveWorkSummaryRows(database.state.workItems, args[0].(time.Time))
		if err != nil {
			return nil, err
		}
		return newProofRows(rows), nil
	case strings.Contains(query, "FROM fact_work_items") && strings.Contains(query, "GROUP BY stage, status"):
		return newProofRows(proofStageCountRows(database.state.workItems)), nil
	case strings.Contains(query, "GROUP BY domain") && strings.Contains(query, "oldest_outstanding_age_seconds"):
		if len(args) != 1 {
			return nil, fmt.Errorf("domain backlog args = %d, want 1", len(args))
		}
		return newProofRows(
			proofDomainBacklogRows(database.state.workItems, args[0].(time.Time)),
		), nil
	case strings.Contains(query, "AS total_count"):
		if len(args) != 1 {
			return nil, fmt.Errorf("queue snapshot args = %d, want 1", len(args))
		}
		return newProofRows([][]any{
			proofQueueSnapshotRow(database.state.workItems, args[0].(time.Time)),
		}), nil
	case strings.Contains(query, "oldest_blocked_age_seconds"):
		return newProofRows(nil), nil
	case strings.Contains(query, "COALESCE(failure_details, '') AS failure_details"):
		return newProofRows(nil), nil
	case isTerraformStateAdminQuery(query):
		// tfstate admin status queries are best-effort observability data and
		// not exercised by the proof harness; return empty rows so the wider
		// status snapshot can still resolve.
		return newProofRows(nil), nil
	case query == semanticExtractionObservabilityQuery:
		return newProofRows(nil), nil
	case query == awsFreshnessStatusCountsQuery:
		return newProofRows(nil), nil
	case query == awsFreshnessOldestQueuedAgeQuery:
		return newProofRows([][]any{{float64(0)}}), nil
	case query == vulnerabilitySourceStatusQuery:
		return newProofRows(nil), nil
	case query == registryMetadataTargetStatusQuery:
		return newProofRows(nil), nil
	case query == listActiveRepositoryFactsQuery:
		// Supersession-only active read: visible iff the fact's generation is
		// the scope's active generation and that generation is active. No
		// is_tombstone predicate, matching the concrete query.
		return newProofRows(
			proofActiveGenerationFactRows(database.state, false, proofActiveRepositoryFactKind),
		), nil
	case query == listActiveContainerImageIdentityFactsQuery:
		// Supersession plus is_tombstone = FALSE: an active-generation
		// tombstone is still excluded here, unlike the repository read.
		return newProofRows(
			proofActiveGenerationFactRows(database.state, true, nil),
		), nil
	case query == collectorFactEvidenceQuery:
		// Collector fact evidence is observability-only data the proof harness
		// does not exercise; return empty rows so the wider status snapshot can
		// still resolve. This case must precede the generic "FROM fact_records"
		// branch because the per-scope LATERAL aggregate (issue #3375) selects
		// FROM fact_records inside its subquery.
		return newProofRows(nil), nil
	case strings.Contains(query, "FROM fact_records") && strings.Contains(query, "fact_kind = 'repository'"):
		// The repository catalog now loads through the store's base connection
		// (issue #3481 shared cache) instead of the per-commit transaction, so
		// the outer harness connection must serve the catalog read too.
		return newProofRows(proofRepositoryCatalogRows(database.state.facts)), nil
	case strings.Contains(query, "FROM fact_records"):
		if len(args) != 2 {
			return nil, fmt.Errorf("list facts args = %d, want 2", len(args))
		}
		scopeID, _ := args[0].(string)
		generationID, _ := args[1].(string)
		return newProofRows(proofFactRows(database.state.facts, scopeID, generationID)), nil
	case strings.Contains(query, "stage = 'reducer'"):
		if len(args) != 7 {
			return nil, fmt.Errorf("reducer claim args = %d, want 7", len(args))
		}
		waitForProjectorDrain, _ := args[4].(bool)
		if waitForProjectorDrain && proofProjectorWorkOutstanding(database.state.workItems) {
			return newProofRows(nil), nil
		}
		return database.claimReducerWork(args[0].(time.Time), args[2].(string), args[3].(time.Time))
	case strings.Contains(query, "stage = 'projector'"):
		if len(args) != 4 {
			return nil, fmt.Errorf("projector claim args = %d, want 4", len(args))
		}
		sourceSystem, _ := args[3].(string)
		return database.claimProjectorWork(args[0].(time.Time), args[1].(string), args[2].(time.Time), sourceSystem)
	default:
		if isWorkflowCoordinatorStatusQuery(query) {
			return newProofRows(nil), nil
		}
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func proofProjectorWorkOutstanding(items map[string]proofWorkItem) bool {
	for _, item := range items {
		if item.stage != "projector" {
			continue
		}
		switch item.status {
		case "pending", "retrying", "claimed", "running":
			return true
		}
	}
	return false
}

func (database *proofDomainDB) claimProjectorWork(
	now time.Time,
	leaseOwner string,
	claimUntil time.Time,
	sourceSystem string,
) (db.Rows, error) {
	for key, item := range database.state.workItems {
		if item.stage != "projector" || (item.status != "pending" && item.status != "retrying") {
			continue
		}
		if !item.visibleAt.IsZero() && item.visibleAt.After(now) {
			continue
		}
		scopeRow, ok := database.state.scopes[item.scopeID]
		if !ok {
			return nil, fmt.Errorf("scope %q not found", item.scopeID)
		}
		if sourceSystem != "" && scopeRow.SourceSystem != sourceSystem {
			continue
		}
		item.status = "claimed"
		item.attemptCount++
		item.leaseOwner = leaseOwner
		item.claimUntil = claimUntil
		item.updatedAt = now
		database.state.workItems[key] = item

		generationRow, ok := database.state.generations[item.generationID]
		if !ok {
			return nil, fmt.Errorf("generation %q not found", item.generationID)
		}

		return newProofRows([][]any{{
			scopeRow.ScopeID,
			scopeRow.SourceSystem,
			string(scopeRow.ScopeKind),
			scopeRow.ParentScopeID,
			database.state.activeGenerations[scopeRow.ScopeID],
			proofPreviousGenerationExists(database.state.generations, scopeRow.ScopeID, generationRow.GenerationID),
			string(scopeRow.CollectorKind),
			scopeRow.PartitionKey,
			generationRow.GenerationID,
			item.attemptCount,
			generationRow.ObservedAt,
			generationRow.IngestedAt,
			string(generationRow.Status),
			string(generationRow.TriggerKind),
			generationRow.FreshnessHint,
			mustMarshalProofMetadata(scopeRow.Metadata),
		}}), nil
	}

	return newProofRows(nil), nil
}

func proofPreviousGenerationExists(
	generations map[string]scope.ScopeGeneration,
	scopeID string,
	currentGenerationID string,
) bool {
	for generationID, generation := range generations {
		if generationID != currentGenerationID && generation.ScopeID == scopeID {
			return true
		}
	}
	return false
}

func (database *proofDomainDB) claimReducerWork(now time.Time, leaseOwner string, claimUntil time.Time) (db.Rows, error) {
	for key, item := range database.state.workItems {
		if item.stage != "reducer" || (item.status != "pending" && item.status != "retrying") {
			continue
		}
		if !item.visibleAt.IsZero() && item.visibleAt.After(now) {
			continue
		}
		item.status = "claimed"
		item.attemptCount++
		item.leaseOwner = leaseOwner
		item.claimUntil = claimUntil
		item.updatedAt = now
		database.state.workItems[key] = item
		database.reducerClaims++
		return newProofRows([][]any{{
			item.workItemID,
			item.scopeID,
			item.generationID,
			item.domain,
			item.attemptCount,
			// container_image_identity_claim_epoch: this harness never
			// exercises the container_image_identity domain, so the epoch
			// stays at its zero opt-out value.
			int64(0),
			item.createdAt,
			item.visibleAt,
			// cycle_started_at: this harness never exercises reopen, so it
			// always equals created_at, matching COALESCE(reopened_at,
			// created_at) with reopened_at NULL.
			item.createdAt,
			item.payload,
		}}), nil
	}

	return newProofRows(nil), nil
}
