// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const workloadIdentityFactKind = "reducer_workload_identity"

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
		workloadIdentityFactKind,
		workloadIdentityStableFactKey(write),
		reducerFactCollectorKind(write.SourceSystem),
		facts.SourceConfidenceInferred,
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
