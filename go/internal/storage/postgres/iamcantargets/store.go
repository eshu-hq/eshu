// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcantargets

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/iamcan"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// candidateScopesQuery samples the readiness of every AWS
// scope that could hold a requested CAN_PERFORM target (#6785): same account
// ($1), a requested (service_kind, region) pair ($2/$3; an empty region means
// any region, for S3), excluding the permission's own scope ($4). Per scope it
// returns the active generation, whether that generation is in status
// 'active', whether aws_resource_materialization committed CloudResource nodes
// for it, and whether any generation is still 'pending'.
//
// Measured before landing (docs/internal/design/6785-cross-scope-can-perform-and-uses-readiness.md
// §3.2): 105 000 scopes (85 000 AWS across 50 accounts x 17 regions x 100
// services, plus 20 000 git) and 525 000 generations, Postgres 16, three runs
// at 10.9-13.1 ms. The ingestion_scopes filter is a parallel seq scan
// (3 530 shared hits) because the default collation cannot serve a LIKE
// prefix; the per-candidate phase and pending probes are index-only scans on
// graph_projection_phase_state_lookup_idx and scope_generations_scope_idx. It
// runs once per CAN_PERFORM evaluation that names an exact cross-scope target.
const candidateScopesQuery = `
WITH wanted AS MATERIALIZED (
    SELECT DISTINCT w.service_kind, w.region
    FROM unnest($2::text[], $3::text[]) AS w(service_kind, region)
), candidates AS MATERIALIZED (
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    JOIN wanted
      ON split_part(scope.scope_id, ':', 4) = wanted.service_kind
     AND (wanted.region = '' OR split_part(scope.scope_id, ':', 3) = wanted.region)
    WHERE scope.scope_id LIKE 'aws:' || $1 || ':%'
      AND split_part(scope.scope_id, ':', 2) = $1
      AND scope.scope_id <> $4
)
SELECT candidate.scope_id,
       COALESCE(candidate.active_generation_id, '') AS active_generation_id,
       COALESCE(active.status = 'active', FALSE) AS generation_active,
       EXISTS (
           SELECT 1 FROM graph_projection_phase_state AS phase
           WHERE phase.scope_id = candidate.scope_id
             AND phase.acceptance_unit_id = 'aws_resource_materialization:' || candidate.scope_id
             AND phase.source_run_id = candidate.active_generation_id
             AND phase.generation_id = candidate.active_generation_id
             AND phase.keyspace = 'cloud_resource_uid'
             AND phase.phase = 'canonical_nodes_committed'
       ) AS nodes_committed,
       EXISTS (
           SELECT 1 FROM scope_generations AS pending
           WHERE pending.scope_id = candidate.scope_id
             AND pending.status = 'pending'
       ) AS generation_pending
FROM candidates AS candidate
LEFT JOIN scope_generations AS active
  ON active.scope_id = candidate.scope_id
 AND active.generation_id = candidate.active_generation_id
ORDER BY candidate.scope_id
`

// FactLister is the postgres.FactStore read the store pins to each candidate
// scope's active generation.
type FactLister interface {
	ListFactsByKindAndPayloadValue(
		ctx context.Context,
		scopeID string,
		generationID string,
		factKind string,
		payloadKey string,
		payloadValues []string,
	) ([]facts.Envelope, error)
}

// Store implements iamcan.CrossScopeTargetLoader
// over the shared fact store. It samples candidate-scope readiness first, then
// reads only the requested ARNs from each scope's active generation as that
// sample saw it, so a generation that activates mid-call is never judged
// against the committed flag of a different generation.
type Store struct {
	DB    db.Queryer
	Facts FactLister
}

// LoadCrossScopeTargets answers one bounded cross-scope request. Reads are
// bounded by account, requested service/region, and exact ARN; an empty
// request issues no query.
func (s Store) LoadCrossScopeTargets(
	ctx context.Context,
	request iamcan.CrossScopeTargetRequest,
) (iamcan.CrossScopeTargetSnapshot, error) {
	if len(request.Targets) == 0 {
		return iamcan.CrossScopeTargetSnapshot{}, nil
	}
	if s.DB == nil || s.Facts == nil {
		return iamcan.CrossScopeTargetSnapshot{}, fmt.Errorf("iam can_perform cross-scope target store requires a database and fact store")
	}

	serviceKinds := make([]string, 0, len(request.Targets))
	regions := make([]string, 0, len(request.Targets))
	arnsByService := make(map[string][]string)
	seenPair := make(map[[2]string]struct{})
	for _, target := range request.Targets {
		arnsByService[target.ServiceKind] = append(arnsByService[target.ServiceKind], target.ARN)
		pair := [2]string{target.ServiceKind, target.Region}
		if _, seen := seenPair[pair]; seen {
			continue
		}
		seenPair[pair] = struct{}{}
		serviceKinds = append(serviceKinds, target.ServiceKind)
		regions = append(regions, target.Region)
	}

	scopes, err := s.listCandidateScopes(ctx, request, serviceKinds, regions)
	if err != nil {
		return iamcan.CrossScopeTargetSnapshot{}, err
	}
	snapshot := iamcan.CrossScopeTargetSnapshot{Scopes: scopes, Resources: make(map[string][]facts.Envelope)}
	for _, scope := range scopes {
		if !scope.GenerationActive {
			continue
		}
		arns := arnsByService[crossScopeServiceKind(scope.ScopeID)]
		if len(arns) == 0 {
			continue
		}
		envelopes, err := s.Facts.ListFactsByKindAndPayloadValue(
			ctx, scope.ScopeID, scope.ActiveGenerationID, facts.AWSResourceFactKind, "arn", arns,
		)
		if err != nil {
			return iamcan.CrossScopeTargetSnapshot{}, fmt.Errorf("load cross-scope aws_resource targets for scope %s: %w", scope.ScopeID, err)
		}
		if len(envelopes) > 0 {
			snapshot.Resources[scope.ScopeID] = envelopes
		}
	}
	return snapshot, nil
}

func (s Store) listCandidateScopes(
	ctx context.Context,
	request iamcan.CrossScopeTargetRequest,
	serviceKinds []string,
	regions []string,
) ([]iamcan.CrossScopeTargetScope, error) {
	rows, err := s.DB.QueryContext(
		ctx,
		candidateScopesQuery,
		request.AccountID,
		pgarray.StringArray(serviceKinds),
		pgarray.StringArray(regions),
		request.ExcludeScopeID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cross-scope iam can_perform target scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var scopes []iamcan.CrossScopeTargetScope
	for rows.Next() {
		var scope iamcan.CrossScopeTargetScope
		if err := rows.Scan(
			&scope.ScopeID,
			&scope.ActiveGenerationID,
			&scope.GenerationActive,
			&scope.NodesCommitted,
			&scope.GenerationPending,
		); err != nil {
			return nil, fmt.Errorf("scan cross-scope iam can_perform target scope: %w", err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cross-scope iam can_perform target scopes: %w", err)
	}
	return scopes, nil
}

// crossScopeServiceKind returns the service segment of an
// aws:<account>:<region>:<service> scope id.
func crossScopeServiceKind(scopeID string) string {
	parts := strings.Split(scopeID, ":")
	if len(parts) != 4 {
		return ""
	}
	return parts[3]
}
