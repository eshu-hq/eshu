// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// workloadCandidatePreconditionQuery reports whether one scope generation
// holds the structural preconditions candidate extraction needs: a
// repository fact carrying graph identity joined to a file fact whose parsed
// payload holds at least one workload-signal key (#9697). It transfers a
// single boolean — no envelopes, no payloads — so the workload projection
// input loader can shed its bulk scope-local load for generations that
// cannot yield candidates, and shed the load-then-discard path for the rest
// when the resolution fence is closed.
//
// The predicate mirrors the structural gates of ExtractWorkloadCandidates,
// not its signal semantics: repository identity (graph_id and name),
// file-to-repo join on repo_id, and parsed_file_data key existence drawn
// from factload.WorkloadCandidateSignalKeys. A bare Jenkinsfile path with
// no parsed payload probes false and loads normally (missed saving, never
// a wrong deferral). Tombstones are intentionally NOT filtered: the
// scope-local kind loads hand tombstoned envelopes to extraction, so the
// probe sees the same input.
//
// Performance: two index-friendly equality partitions (scope_id,
// generation_id, fact_kind) with a key-existence operator on the inner
// side; the EXISTS stops at the first joined pair.
func workloadCandidatePreconditionQuery() string {
	quoted := make([]string, 0, len(factload.WorkloadCandidateSignalKeys))
	for _, key := range factload.WorkloadCandidateSignalKeys {
		quoted = append(quoted, "'"+key+"'")
	}
	return `SELECT EXISTS (
  SELECT 1
  FROM fact_records AS repo
  WHERE repo.scope_id = $1
    AND repo.generation_id = $2
    AND repo.fact_kind = 'repository'
    AND repo.payload->>'graph_id' <> ''
    AND repo.payload->>'name' <> ''
    AND EXISTS (
      SELECT 1
      FROM fact_records AS f
      WHERE f.scope_id = $1
        AND f.generation_id = $2
        AND f.fact_kind = 'file'
        AND f.payload->>'repo_id' = repo.payload->>'graph_id'
        AND f.payload->'parsed_file_data' ?| '{` + strings.Join(quoted, ",") + `}'
    )
)`
}

// ScopeMayHoldWorkloadCandidates implements
// factload.WorkloadCandidateProbe for the workload projection input
// loader's bulk-load pre-gate (#9697).
func (s FactStore) ScopeMayHoldWorkloadCandidates(
	ctx context.Context,
	scopeID string,
	generationID string,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("fact store database is required")
	}

	rows, err := s.database.QueryContext(ctx, workloadCandidatePreconditionQuery(), scopeID, generationID)
	if err != nil {
		return false, fmt.Errorf("probe workload candidate precondition: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, fmt.Errorf("probe workload candidate precondition: no rows returned")
	}
	var mayHold bool
	if err := rows.Scan(&mayHold); err != nil {
		return false, fmt.Errorf("probe workload candidate precondition scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("probe workload candidate precondition rows: %w", err)
	}
	return mayHold, nil
}
