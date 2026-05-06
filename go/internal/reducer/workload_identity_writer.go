package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const canonicalReducerFactInsertQuery = `
INSERT INTO fact_records (
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    source_system,
    source_fact_key,
    source_uri,
    source_record_id,
    observed_at,
    ingested_at,
    is_tombstone,
    payload
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb
)
ON CONFLICT (fact_id) DO UPDATE SET
    fact_kind = EXCLUDED.fact_kind,
    stable_fact_key = EXCLUDED.stable_fact_key,
    source_system = EXCLUDED.source_system,
    source_fact_key = EXCLUDED.source_fact_key,
    source_uri = EXCLUDED.source_uri,
    source_record_id = EXCLUDED.source_record_id,
    observed_at = EXCLUDED.observed_at,
    ingested_at = EXCLUDED.ingested_at,
    is_tombstone = EXCLUDED.is_tombstone,
    payload = EXCLUDED.payload
`

type workloadIdentityExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// PostgresWorkloadIdentityWriter persists one workload-identity reducer
// reconciliation into the shared fact store.
type PostgresWorkloadIdentityWriter struct {
	DB  workloadIdentityExecer
	Now func() time.Time
}

// WriteWorkloadIdentity stores one canonical workload-identity fact record.
func (w PostgresWorkloadIdentityWriter) WriteWorkloadIdentity(
	ctx context.Context,
	write WorkloadIdentityWrite,
) (WorkloadIdentityWriteResult, error) {
	if w.DB == nil {
		return WorkloadIdentityWriteResult{}, fmt.Errorf("workload identity database is required")
	}

	now := w.now()
	canonicalID := canonicalWorkloadIdentityID(write)
	payloadJSON, err := json.Marshal(workloadIdentityPayload(write, canonicalID))
	if err != nil {
		return WorkloadIdentityWriteResult{}, fmt.Errorf("marshal workload identity payload: %w", err)
	}

	if _, err := w.DB.ExecContext(
		ctx,
		canonicalReducerFactInsertQuery,
		write.IntentID,
		write.ScopeID,
		write.GenerationID,
		"reducer_workload_identity",
		workloadIdentityStableFactKey(write),
		write.SourceSystem,
		write.IntentID,
		nil,
		nil,
		now,
		now,
		false,
		payloadJSON,
	); err != nil {
		return WorkloadIdentityWriteResult{}, fmt.Errorf("write workload identity fact: %w", err)
	}

	return WorkloadIdentityWriteResult{
		CanonicalID:      canonicalID,
		CanonicalWrites:  1,
		ReconciledScopes: len(uniqueSortedStrings(write.RelatedScopeIDs)),
		EvidenceSummary: fmt.Sprintf(
			"wrote workload identity canonical fact %s",
			canonicalID,
		),
	}, nil
}

func (w PostgresWorkloadIdentityWriter) now() time.Time {
	return reducerWriterNow(w.Now)
}

func workloadIdentityStableFactKey(write WorkloadIdentityWrite) string {
	entityKeys := uniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := uniqueSortedStrings(write.RelatedScopeIDs)
	parts := []string{
		"workload_identity",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.Join(entityKeys, "|"),
		strings.Join(relatedScopeIDs, "|"),
	}

	return strings.Join(parts, ":")
}

func canonicalWorkloadIdentityID(write WorkloadIdentityWrite) string {
	entityKeys := uniqueSortedStrings(write.EntityKeys)
	relatedScopeIDs := uniqueSortedStrings(write.RelatedScopeIDs)
	parts := []string{
		"workload_identity",
		strings.TrimSpace(write.ScopeID),
		strings.TrimSpace(write.GenerationID),
		strings.TrimSpace(write.SourceSystem),
		strings.Join(entityKeys, "|"),
		strings.Join(relatedScopeIDs, "|"),
	}

	return "canonical:" + strings.Join(parts, ":")
}

func workloadIdentityPayload(write WorkloadIdentityWrite, canonicalID string) map[string]any {
	return map[string]any{
		"reducer_domain":    string(DomainWorkloadIdentity),
		"intent_id":         write.IntentID,
		"scope_id":          write.ScopeID,
		"generation_id":     write.GenerationID,
		"source_system":     write.SourceSystem,
		"cause":             write.Cause,
		"entity_keys":       uniqueSortedStrings(write.EntityKeys),
		"related_scope_ids": uniqueSortedStrings(write.RelatedScopeIDs),
		"canonical_id":      canonicalID,
	}
}

func reducerWriterNow(now func() time.Time) time.Time {
	if now != nil {
		return now().UTC()
	}

	return time.Now().UTC()
}
