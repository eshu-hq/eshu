package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

func (db *proofDomainDB) Begin(context.Context) (Transaction, error) {
	return &proofDomainTx{
		db: db,
		state: proofState{
			scopes:            cloneScopes(db.state.scopes),
			scopeStatuses:     cloneStrings(db.state.scopeStatuses),
			activeGenerations: cloneStrings(db.state.activeGenerations),
			generations:       cloneGenerations(db.state.generations),
			facts:             cloneFacts(db.state.facts),
			evidenceFacts:     cloneEvidenceFacts(db.state.evidenceFacts),
			workItems:         cloneWorkItems(db.state.workItems),
		},
	}, nil
}

func (db *proofDomainDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'succeeded'"):
		if len(args) != 4 {
			return nil, fmt.Errorf("projector ack args = %d, want 4", len(args))
		}
		return db.updateWorkItemStatus("projector", args[1].(string), args[2].(string), args[3].(string), "succeeded")
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'dead_letter'"):
		if len(args) != 7 {
			return nil, fmt.Errorf("projector fail args = %d, want 7", len(args))
		}
		return db.updateWorkItemStatus("projector", args[4].(string), args[5].(string), args[6].(string), "dead_letter")
	case strings.Contains(query, "WHERE stage = 'projector'") && strings.Contains(query, "status = 'retrying'"):
		if len(args) != 8 {
			return nil, fmt.Errorf("projector retry args = %d, want 8", len(args))
		}
		return db.retryProjectorWork(args[5].(string), args[6].(string), args[7].(string), args[4].(time.Time))
	case strings.Contains(query, "stage = 'reducer'") && strings.Contains(query, "SET status = 'succeeded'"):
		if len(args) != 3 {
			return nil, fmt.Errorf("reducer ack args = %d, want 3", len(args))
		}
		return db.updateWorkItemStatusByID(args[1].(string), args[2].(string), "succeeded")
	case strings.Contains(query, "stage = 'reducer'") && strings.Contains(query, "SET status = 'retrying'"):
		if len(args) != 7 {
			return nil, fmt.Errorf("reducer retry args = %d, want 7", len(args))
		}
		return db.retryReducerWork(args[5].(string), args[6].(string), args[4].(time.Time))
	case strings.Contains(query, "stage = 'reducer'") && strings.Contains(query, "SET status = 'dead_letter'"):
		if len(args) != 6 {
			return nil, fmt.Errorf("reducer fail args = %d, want 6", len(args))
		}
		return db.updateWorkItemStatusByID(args[4].(string), args[5].(string), "dead_letter")
	case strings.Contains(query, "INSERT INTO fact_work_items") && strings.Contains(query, "'reducer'"):
		workItemID := args[0].(string)
		if _, exists := db.state.workItems[workItemID]; exists {
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
		db.state.workItems[workItem.workItemID] = workItem
		return proofResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *proofDomainDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	switch {
	case strings.Contains(query, "SELECT generation.generation_id, COALESCE(generation.freshness_hint, '')"):
		if len(args) != 1 {
			return nil, fmt.Errorf("active generation freshness args = %d, want 1", len(args))
		}
		scopeID := args[0].(string)
		activeGenerationID := db.state.activeGenerations[scopeID]
		if activeGenerationID == "" {
			return newProofRows(nil), nil
		}
		generation, ok := db.state.generations[activeGenerationID]
		if !ok {
			return newProofRows(nil), nil
		}
		return newProofRows([][]any{{
			generation.GenerationID,
			generation.FreshnessHint,
		}}), nil
	case strings.Contains(query, "FROM ingestion_scopes") && strings.Contains(query, "GROUP BY status"):
		return newProofRows(proofScopeCountRows(db.state.scopeStatuses)), nil
	case strings.Contains(query, "FROM scope_generations") && strings.Contains(query, "GROUP BY status"):
		return newProofRows(proofGenerationCountRows(db.state.generations)), nil
	case strings.Contains(query, "JOIN ingestion_scopes") && strings.Contains(query, "current_active_generation_id"):
		return newProofRows(
			proofGenerationTransitionRows(db.state.generations, db.state.activeGenerations, db.now),
		), nil
	case strings.Contains(query, "FROM fact_work_items") && strings.Contains(query, "GROUP BY stage, status"):
		return newProofRows(proofStageCountRows(db.state.workItems)), nil
	case strings.Contains(query, "GROUP BY domain") && strings.Contains(query, "oldest_outstanding_age_seconds"):
		if len(args) != 1 {
			return nil, fmt.Errorf("domain backlog args = %d, want 1", len(args))
		}
		return newProofRows(
			proofDomainBacklogRows(db.state.workItems, args[0].(time.Time)),
		), nil
	case strings.Contains(query, "SELECT COUNT(*) AS total_count"):
		if len(args) != 1 {
			return nil, fmt.Errorf("queue snapshot args = %d, want 1", len(args))
		}
		return newProofRows([][]any{
			proofQueueSnapshotRow(db.state.workItems, args[0].(time.Time)),
		}), nil
	case strings.Contains(query, "oldest_blocked_age_seconds"):
		return newProofRows(nil), nil
	case strings.Contains(query, "COALESCE(failure_details, '') AS failure_details"):
		return newProofRows(nil), nil
	case strings.Contains(query, "FROM fact_records"):
		if len(args) != 2 {
			return nil, fmt.Errorf("list facts args = %d, want 2", len(args))
		}
		scopeID, _ := args[0].(string)
		generationID, _ := args[1].(string)
		return newProofRows(proofFactRows(db.state.facts, scopeID, generationID)), nil
	case strings.Contains(query, "stage = 'reducer'"):
		if len(args) != 7 {
			return nil, fmt.Errorf("reducer claim args = %d, want 7", len(args))
		}
		waitForProjectorDrain, _ := args[4].(bool)
		if waitForProjectorDrain && proofProjectorWorkOutstanding(db.state.workItems) {
			return newProofRows(nil), nil
		}
		return db.claimReducerWork(args[0].(time.Time), args[2].(string), args[3].(time.Time))
	case strings.Contains(query, "stage = 'projector'"):
		if len(args) != 3 {
			return nil, fmt.Errorf("projector claim args = %d, want 3", len(args))
		}
		return db.claimProjectorWork(args[0].(time.Time), args[1].(string), args[2].(time.Time))
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

func (db *proofDomainDB) claimProjectorWork(now time.Time, leaseOwner string, claimUntil time.Time) (Rows, error) {
	for key, item := range db.state.workItems {
		if item.stage != "projector" || (item.status != "pending" && item.status != "retrying") {
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
		db.state.workItems[key] = item

		scopeRow, ok := db.state.scopes[item.scopeID]
		if !ok {
			return nil, fmt.Errorf("scope %q not found", item.scopeID)
		}
		generationRow, ok := db.state.generations[item.generationID]
		if !ok {
			return nil, fmt.Errorf("generation %q not found", item.generationID)
		}

		return newProofRows([][]any{{
			scopeRow.ScopeID,
			scopeRow.SourceSystem,
			string(scopeRow.ScopeKind),
			scopeRow.ParentScopeID,
			db.state.activeGenerations[scopeRow.ScopeID],
			proofPreviousGenerationExists(db.state.generations, scopeRow.ScopeID, generationRow.GenerationID),
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

func (db *proofDomainDB) claimReducerWork(now time.Time, leaseOwner string, claimUntil time.Time) (Rows, error) {
	for key, item := range db.state.workItems {
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
		db.state.workItems[key] = item
		db.reducerClaims++
		return newProofRows([][]any{{
			item.workItemID,
			item.scopeID,
			item.generationID,
			item.domain,
			item.attemptCount,
			item.createdAt,
			item.visibleAt,
			item.payload,
		}}), nil
	}

	return newProofRows(nil), nil
}

func (tx *proofDomainTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	_ = ctx
	switch {
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
			GenerationID:  args[0].(string),
			ScopeID:       args[1].(string),
			TriggerKind:   scope.TriggerKind(args[2].(string)),
			FreshnessHint: stringFromAny(args[3]),
			ObservedAt:    args[4].(time.Time).UTC(),
			IngestedAt:    args[5].(time.Time).UTC(),
			Status:        scope.GenerationStatus(args[6].(string)),
		}
		if existing, ok := tx.state.generations[generation.GenerationID]; ok && existing.Status == scope.GenerationStatusActive && generation.Status == scope.GenerationStatusPending && existing.FreshnessHint == generation.FreshnessHint {
			generation.Status = existing.Status
			generation.ObservedAt = existing.ObservedAt
			generation.IngestedAt = existing.IngestedAt
		}
		tx.state.generations[generation.GenerationID] = generation
		return proofResult{}, nil
	case strings.Contains(query, "INSERT INTO fact_records"):
		// Batch INSERT: args contains N * columnsPerFactRow parameters.
		if len(args)%columnsPerFactRow != 0 {
			return nil, fmt.Errorf("fact batch args = %d, not a multiple of %d", len(args), columnsPerFactRow)
		}
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
			tx.state.facts[envelope.FactID] = envelope
		}
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
	default:
		return nil, errors.New("unexpected query in transaction")
	}
}

func (tx *proofDomainTx) Commit() error {
	tx.db.state = tx.state
	return nil
}

func (tx *proofDomainTx) Rollback() error { return nil }

type proofDomainTx struct {
	db    *proofDomainDB
	state proofState
}
