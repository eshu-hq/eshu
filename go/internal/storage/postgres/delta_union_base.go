// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// deltaUnionBaseQuery is the one definition of the #7797 base rule, batched
// over (scope, union-base commit) pairs: for each pair, the newest projected
// full generation at that commit, non-delta, activated (activated_at set, the
// established projected predicate), of a terminal-or-active status. Active
// and completed generations sort before superseded ones: retention only ever
// prunes superseded generations, so an active or completed base is intact by
// construction, while a superseded base needs the retention-event probe
// below. Same-commit generations observe the same tree, so intactness
// outranks recency. A pair with no such generation returns no row. The
// projector and the successor guard bind one pair; a reader binds every
// delta-active scope it reached. Each pair runs the per-scope plan it ran
// before #10003 batched it: a bitmap scan on the scope_id prefix
// (scope_generations_scope_generation_idx on PostgreSQL 18.6) with the other
// predicates as a filter, under a nested loop over the pairs. It carries no
// dedicated index dependency. Performance Evidence:
// docs/internal/evidence/10003-delta-overlay-helper.md.
const deltaUnionBaseQuery = `
SELECT target.scope_id, base.generation_id, base.status
FROM unnest($1::text[], $2::text[]) AS target(scope_id, union_base_commit_sha)
CROSS JOIN LATERAL (
    SELECT generation_id, status
    FROM scope_generations
    WHERE scope_id = target.scope_id
      AND is_delta = false
      AND activated_at IS NOT NULL
      AND status IN ('active', 'completed', 'superseded')
      AND source_commit_sha = target.union_base_commit_sha
    ORDER BY (status IN ('active', 'completed')) DESC, ingested_at DESC, generation_id DESC
    LIMIT 1
) AS base
`

// deltaUnionBasePrunedQuery probes whether retention ever pruned each
// superseded base generation, by the same hashed identities the retention
// events carry (retentionHashID, computed in Go). It returns the scope hash
// of every pruned pair and is served by
// generation_retention_events_generation_idx. A probe failure is a refusal
// (#8181): the probe cannot confirm a superseded base survived retention
// intact, and overlaying the surviving rows would project a complete-looking
// tree from a possibly half-pruned base. The read fails closed with the
// existing refused outcome, which routes through the reindex watermark path
// so the next selection forces the full re-parse that restores the base. The
// empty-base check in listFactsUnion still catches a fully pruned base when
// the probe is down.
const deltaUnionBasePrunedQuery = `
SELECT target.scope_id_hash
FROM unnest($1::text[], $2::text[]) AS target(scope_id_hash, generation_id_hash)
WHERE EXISTS (
    SELECT 1
    FROM generation_retention_events AS event
    WHERE event.scope_id_hash = target.scope_id_hash
      AND event.generation_id_hash = target.generation_id_hash
)
`

// deltaBaseRequest is one delta whose base full generation resolves at its
// union-base commit.
type deltaBaseRequest struct {
	scopeID      string
	generationID string
	commits      deltaUnionCommits
}

// deltaBaseResolution is one request's outcome: the base generation id, or
// the fail-closed refusal when the base is missing, pruned, or unconfirmed.
type deltaBaseResolution struct {
	baseID  string
	refused *projector.DeltaBaseMissingError
}

// resolveDeltaBaseAtCommit returns the newest projected full generation at
// commits.UnionBase, the base a delta overlays at projection (#8780: the
// union-base commit, COALESCEd to the baseline for a legacy delta). The #8750
// destructive-successor guard sizes a delta against the same base through
// this helper, so the guard and the projector cannot disagree on it. A
// missing, pruned, or unconfirmed base fails closed with
// projector.DeltaBaseMissingError naming generationID and both commits. It is
// resolveDeltaBases for one request: one base lookup, plus one retention
// probe when the base is superseded.
func (s FactStore) resolveDeltaBaseAtCommit(ctx context.Context, scopeID, generationID string, commits deltaUnionCommits) (string, error) {
	resolved, err := s.resolveDeltaBases(ctx, []deltaBaseRequest{{scopeID: scopeID, generationID: generationID, commits: commits}})
	if err != nil {
		return "", err
	}
	resolution := resolved[scopeID]
	if resolution.refused != nil {
		return "", resolution.refused
	}
	return resolution.baseID, nil
}

// resolveDeltaBases resolves every request's base by the #7797 rule with at
// most two statements, whatever the request count: one base lookup, then one
// retention probe over the superseded bases. The result holds one resolution
// per request, keyed by scope id. A base lookup failure fails the whole call;
// a probe failure refuses every superseded base it covered (#8181). Each read
// closes its cursor before the next statement: the destructive-successor
// guard runs these reads on its commit transaction (#8750 window), one
// connection, where pgx refuses a statement while an earlier cursor is open
// ("conn busy"). Scope ids must be distinct.
func (s FactStore) resolveDeltaBases(ctx context.Context, requests []deltaBaseRequest) (map[string]deltaBaseResolution, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	resolved := make(map[string]deltaBaseResolution, len(requests))
	if len(requests) == 0 {
		return resolved, nil
	}
	scopeIDs := make([]string, 0, len(requests))
	commitSHAs := make([]string, 0, len(requests))
	for _, request := range requests {
		if _, dup := resolved[request.scopeID]; dup {
			return nil, fmt.Errorf("resolve delta union base: scope %q requested twice", request.scopeID)
		}
		resolved[request.scopeID] = deltaBaseResolution{}
		scopeIDs = append(scopeIDs, request.scopeID)
		commitSHAs = append(commitSHAs, request.commits.UnionBase)
	}
	bases, err := s.readDeltaUnionBases(ctx, scopeIDs, commitSHAs)
	if err != nil {
		return nil, err
	}
	var superseded []deltaBaseRequest
	for _, request := range requests {
		base := bases[request.scopeID]
		if base.id == "" {
			resolved[request.scopeID] = deltaBaseResolution{refused: deltaBaseMissing(request.scopeID, request.generationID, request.commits)}
			continue
		}
		resolved[request.scopeID] = deltaBaseResolution{baseID: base.id}
		if base.status == "superseded" {
			superseded = append(superseded, request)
		}
	}
	if len(superseded) == 0 {
		return resolved, nil
	}
	pruned, err := s.deltaUnionBasesPruned(ctx, superseded, resolved)
	for _, request := range superseded {
		if err == nil && !pruned[request.scopeID] {
			continue
		}
		if err != nil && s.logger != nil {
			// The probe cannot confirm the base is intact: fail closed with
			// the same refusal as a recorded prune event. The error stays
			// out of the outcome labels (no new cardinality); the refusal
			// routes through the reindex watermark path so the next
			// selection forces the full re-parse.
			s.logger.DebugContext(ctx, "delta union retention probe failed; refusing closed",
				log.ScopeID(request.scopeID),
				slog.String("generation_id", request.generationID),
				slog.Any("probe_error", err))
		}
		resolved[request.scopeID] = deltaBaseResolution{refused: deltaBaseMissing(request.scopeID, request.generationID, request.commits)}
	}
	return resolved, nil
}

// deltaUnionBaseRow is one base lookup row: the trimmed generation id and its
// status.
type deltaUnionBaseRow struct {
	id     string
	status string
}

// readDeltaUnionBases runs deltaUnionBaseQuery for the pairs and returns the
// base row by scope id; a scope with no base is absent. It closes its cursor
// before returning, so the retention probe can run on the same connection.
func (s FactStore) readDeltaUnionBases(ctx context.Context, scopeIDs, commitSHAs []string) (map[string]deltaUnionBaseRow, error) {
	rows, err := s.database.QueryContext(ctx, deltaUnionBaseQuery, scopeIDs, commitSHAs)
	if err != nil {
		return nil, fmt.Errorf("resolve delta union base: %w", err)
	}
	defer func() { _ = rows.Close() }()
	bases := make(map[string]deltaUnionBaseRow, len(scopeIDs))
	for rows.Next() {
		var scopeID, baseID, status string
		if scanErr := rows.Scan(&scopeID, &baseID, &status); scanErr != nil {
			return nil, fmt.Errorf("resolve delta union base scan: %w", scanErr)
		}
		bases[scopeID] = deltaUnionBaseRow{id: strings.TrimSpace(baseID), status: status}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve delta union base: %w", err)
	}
	return bases, nil
}

// deltaUnionBasesPruned reports, by scope id, whether retention recorded any
// prune event for each request's resolved base. Only superseded bases are
// probed: active and completed generations are never prune-eligible, so
// probing them would only add a query. An event means the base lost facts to
// retention (wholly or partially) and the union would project short of the
// full tree. A probe failure is returned as an error so the caller can fail
// closed: an unconfirmed base is indistinguishable from a half-pruned one.
func (s FactStore) deltaUnionBasesPruned(
	ctx context.Context,
	superseded []deltaBaseRequest,
	resolved map[string]deltaBaseResolution,
) (map[string]bool, error) {
	scopeHashes := make([]string, 0, len(superseded))
	generationHashes := make([]string, 0, len(superseded))
	scopeByHash := make(map[string]string, len(superseded))
	for _, request := range superseded {
		scopeHash := retentionHashID("scope", request.scopeID)
		scopeHashes = append(scopeHashes, scopeHash)
		generationHashes = append(generationHashes, retentionHashID("generation", resolved[request.scopeID].baseID))
		scopeByHash[scopeHash] = request.scopeID
	}
	rows, err := s.database.QueryContext(ctx, deltaUnionBasePrunedQuery, scopeHashes, generationHashes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	pruned := make(map[string]bool, len(superseded))
	for rows.Next() {
		var scopeHash string
		if err := rows.Scan(&scopeHash); err != nil {
			return nil, err
		}
		if scopeID, ok := scopeByHash[scopeHash]; ok {
			pruned[scopeID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pruned, nil
}

// deltaBaseMissing builds the fail-closed refusal for a delta with no usable
// base: never projected, fully pruned, or retention-pruned while superseded.
func deltaBaseMissing(scopeID, generationID string, commits deltaUnionCommits) *projector.DeltaBaseMissingError {
	return &projector.DeltaBaseMissingError{
		ScopeID:            scopeID,
		GenerationID:       generationID,
		BaselineCommitSHA:  commits.Baseline,
		UnionBaseCommitSHA: commits.UnionBase,
	}
}
