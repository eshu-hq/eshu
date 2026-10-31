// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const scannerDiscoveryPageLimit = 256

const scannerDiscoveryCursorQuery = `
SELECT version,cycle_upper_observation_id,after_observation_id,
       after_authorization_domain
FROM scanner_image_discovery_cursors
WHERE scanner_collector_instance_id=$1 AND authorization_namespace=$2
  AND tenant_id=$3 AND workspace_id=$4 AND subject_class=$5
  AND policy_revision_hash=$6 AND discovery_policy_revision=$7`

const scannerDiscoveryUpperQuery = `
SELECT COALESCE(max(observation_id),0)
FROM scanner_running_image_observations`

// scannerDiscoveryAuthorizedSources holds every source, tenant, registry and
// cursor-window predicate both discovery reads share. The digest-class
// predicate differs per read, so it stays with each query; every other
// predicate lives here exactly once so skip counts cannot drift from the
// candidate filter.
const scannerDiscoveryAuthorizedSources = `
FROM scanner_running_image_observations AS observation
JOIN scanner_running_image_generations AS generation
  ON generation.producer_instance_id=observation.producer_instance_id
 AND generation.source_scope_id=observation.source_scope_id
 AND generation.source_generation_id=observation.source_generation_id
 AND generation.status='published'
JOIN scanner_running_image_active_scopes AS active_source
  ON active_source.producer_instance_id=observation.producer_instance_id
 AND active_source.source_scope_id=observation.source_scope_id
 AND active_source.published_generation_id=observation.source_generation_id
 AND active_source.active
JOIN scanner_running_image_producer_grants AS producer_grant
  ON producer_grant.principal_oid=generation.principal_oid
 AND producer_grant.producer_instance_id=observation.producer_instance_id
 AND producer_grant.source_scope_id=observation.source_scope_id
 AND producer_grant.producer_class=observation.producer_class
 AND producer_grant.observation_class=observation.observation_class
 AND producer_grant.grant_epoch=generation.producer_grant_epoch
 AND producer_grant.active
JOIN collector_instances AS producer
  ON producer.instance_id=observation.producer_instance_id
 AND producer.collector_kind=CASE WHEN observation.producer_class='kubernetes_pod'
                                THEN 'kubernetes_live' ELSE 'aws' END
 AND producer.enabled AND producer.claims_enabled
JOIN collector_instances AS scanner
  ON scanner.instance_id=$1 AND scanner.collector_kind='scanner_worker'
 AND scanner.enabled AND scanner.claims_enabled
 AND scanner.configuration->>'derive_from_running_images'='true'
JOIN eshu_security.role_policy_state AS role_policy
  ON role_policy.singleton AND role_policy.active
 AND role_policy.policy_revision=$11
 AND role_policy.database_identity=$12::uuid
CROSS JOIN LATERAL jsonb_array_elements(
  COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
LEFT JOIN LATERAL (
  SELECT min(verified.manifest_digest) AS manifest_digest,
         min(verified.verification_id) AS verification_id,
         count(*) AS match_count
  FROM scanner_running_image_manifest_verifications AS verified
  WHERE verified.observation_id=observation.observation_id
    AND verified.registry=observation.registry
    AND verified.repository=observation.repository
    AND verified.config_digest=observation.runtime_digest
) AS verification ON true
WHERE observation.observation_id<=$10
  AND (observation.observation_id>$8 OR
       (observation.observation_id=$8 AND
        domain.value->>'authorization_domain'>$9))
  AND domain.value->>'registry'=observation.registry
  AND domain.value->>'authorization_domain'<>''
  AND 1=(SELECT count(*) FROM jsonb_array_elements(
         COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS peer(value)
         WHERE peer.value->>'authorization_domain'=
               domain.value->>'authorization_domain')
  AND (($2='local' AND observation.source_scope_id=ANY($7::text[])) OR
       ($2='hosted' AND EXISTS (
         SELECT 1 FROM tenant_scope_grants AS scope_grant
         JOIN tenants AS tenant ON tenant.tenant_id=scope_grant.tenant_id
         JOIN workspaces AS workspace
           ON workspace.tenant_id=scope_grant.tenant_id
          AND workspace.workspace_id=scope_grant.workspace_id
         WHERE scope_grant.tenant_id=$3 AND scope_grant.workspace_id=$4
           AND scope_grant.scope_id=observation.source_scope_id
           AND scope_grant.subject_class=$5
           AND scope_grant.policy_revision_hash=$6
           AND scope_grant.tombstoned_at IS NULL
           AND scope_grant.effective_at<=clock_timestamp()
           AND (scope_grant.expires_at IS NULL OR
                scope_grant.expires_at>clock_timestamp())
           AND tenant.status='active' AND tenant.tombstoned_at IS NULL
           AND tenant.policy_revision_hash=$6
           AND workspace.status='active' AND workspace.tombstoned_at IS NULL
           AND workspace.policy_revision_hash=$6)))`

// Every source, tenant and registry predicate is evaluated before LIMIT. The
// returned locator is a candidate only; admission locks and rechecks it.
//
// The page limit is formatted, not bound: the skip-counts query starts from
// the same argument slice as the candidates query, and the pgx stdlib driver
// cannot transmit an unreferenced parameter (SQLSTATE 42P18), so every bound
// argument must appear in the statement that carries it. The page-span bound
// therefore travels on a skip-only extension of the shared slice ($13/$14),
// never on the shared slice itself. request.Limit is validated to
// 1..scannerDiscoveryPageLimit by the caller, so the %d is always a small
// integer, never caller text.
const scannerDiscoveryCandidatesQueryFormat = `
SELECT observation.observation_id,observation.producer_instance_id,
       observation.source_scope_id,observation.source_generation_id,
       observation.object_id,observation.slot_id,observation.producer_class,
       observation.observation_class,observation.observed_revision,
       observation.registry,observation.repository,
       observation.runtime_image_reference,observation.runtime_digest,
       observation.runtime_digest_kind,
       CASE WHEN observation.runtime_digest_kind='manifest'
            THEN observation.runtime_digest ELSE verification.manifest_digest END,
       COALESCE(verification.verification_id,''),
       domain.value->>'authorization_domain'
` + scannerDiscoveryAuthorizedSources + `
  AND (observation.runtime_digest_kind='manifest' OR
       (observation.runtime_digest_kind='config' AND
        verification.match_count=1))
ORDER BY observation.observation_id,domain.value->>'authorization_domain'
LIMIT %d`

// scannerDiscoveryCandidatesQuery formats the candidates query for one
// validated page limit. The limit stays out of the bound arguments so the
// candidates statement and the skip-counts statement share one slice.
func scannerDiscoveryCandidatesQuery(limit int) string {
	return fmt.Sprintf(scannerDiscoveryCandidatesQueryFormat, limit)
}

// scannerDiscoverySkipCountsSelect counts authorized observations in the
// page span that the candidate filter drops for their digest class:
// tag-only references with no digest, and config digests with no single
// verified manifest. Rows failing any other shared predicate are in neither
// read; the two reads share every other predicate by construction above.
const scannerDiscoverySkipCountsSelect = `
SELECT CASE WHEN observation.runtime_digest_kind='unknown'
            THEN 'tag_only' ELSE 'unresolved_config' END,
       COUNT(*)
` + scannerDiscoveryAuthorizedSources + `
  AND (observation.runtime_digest_kind='unknown' OR
       (observation.runtime_digest_kind='config' AND
        verification.match_count IS DISTINCT FROM 1))`

// scannerDiscoverySkipCountsQuery counts the same skips over the whole
// cursor window that remains once the page under read is exhausted: no
// further candidate can follow, so the window remainder is the page span.
const scannerDiscoverySkipCountsQuery = scannerDiscoverySkipCountsSelect + `
GROUP BY 1`

// scannerDiscoverySkipCountsPageBound narrows the shared window to the page
// span actually read: skips at or below the page's last candidate in the
// candidate (observation, domain) order. It appends $13 and $14 to a
// skip-only extension of the shared 12-argument slice, so every bound
// parameter stays referenced by the statement that carries it: placing the
// page bound on the shared slice would trip the pgx stdlib unreferenced
// parameter refusal (SQLSTATE 42P18) on the candidates read.
const scannerDiscoverySkipCountsPageBound = `
  AND (observation.observation_id<$13 OR
       (observation.observation_id=$13 AND
        domain.value->>'authorization_domain'<=$14))`

// scannerDiscoverySkipCountsStatement returns the skip-counts statement for
// the page just read. A non-exhausted page counts only its own span, so
// pages partition the window without overlap; an exhausted page counts the
// window remainder, which is its span.
func scannerDiscoverySkipCountsStatement(exhausted bool) string {
	if exhausted {
		return scannerDiscoverySkipCountsQuery
	}
	return scannerDiscoverySkipCountsSelect + scannerDiscoverySkipCountsPageBound + `
GROUP BY 1`
}

// validateScannerDiscoveryRequest enforces the discovery page's closed input
// boundary: a pinned collector, policy, and role revision, a bounded limit,
// and exactly one of the local or hosted namespace shapes. Splitting this
// out of ListAuthorizedRunningImageObservationsPage keeps that function
// under the repo's funlen limit.
func validateScannerDiscoveryRequest(request workflow.RunningImageDiscoveryRequest) error {
	if request.ScannerCollectorInstanceID == "" || request.DiscoveryPolicyRevision <= 0 ||
		request.ExpectedRolePolicyRevision <= 0 || request.ExpectedDatabaseIdentity == "" ||
		request.Limit < 1 || request.Limit > scannerDiscoveryPageLimit {
		return errors.New("scanner discovery request invalid")
	}
	switch request.AuthorizationNamespace {
	case workflow.ScannerAuthorizationLocal:
		if request.TenantID != "" || request.WorkspaceID != "" ||
			request.SubjectClass != "" || request.PolicyRevisionHash != "" ||
			len(request.LocalSourceScopeIDs) == 0 || len(request.LocalSourceScopeIDs) > 1024 {
			return errors.New("scanner local discovery boundary invalid")
		}
	case workflow.ScannerAuthorizationHosted:
		if request.TenantID == "" || request.WorkspaceID == "" ||
			request.SubjectClass == "" || request.PolicyRevisionHash == "" ||
			len(request.LocalSourceScopeIDs) != 0 {
			return errors.New("scanner hosted discovery boundary invalid")
		}
	default:
		return errors.New("scanner discovery namespace invalid")
	}
	return nil
}

// ListAuthorizedRunningImageObservationsPage lists one bounded finite-cycle
// page. Authority remains advisory until the admission transaction rechecks it.
func (s *WorkflowControlStore) ListAuthorizedRunningImageObservationsPage(
	ctx context.Context, request workflow.RunningImageDiscoveryRequest,
) (workflow.RunningImageDiscoveryPage, error) {
	if s.database == nil {
		return workflow.RunningImageDiscoveryPage{}, errors.New("scanner discovery requires database")
	}
	if err := validateScannerDiscoveryRequest(request); err != nil {
		return workflow.RunningImageDiscoveryPage{}, err
	}
	args := []any{
		request.ScannerCollectorInstanceID,
		string(request.AuthorizationNamespace), request.TenantID, request.WorkspaceID,
		request.SubjectClass, request.PolicyRevisionHash,
		request.DiscoveryPolicyRevision,
	}
	page := workflow.RunningImageDiscoveryPage{}
	rows, err := s.database.QueryContext(ctx, scannerDiscoveryCursorQuery, args...)
	if err != nil {
		return page, fmt.Errorf("scanner discovery cursor: %w", err)
	}
	if rows.Next() {
		err = rows.Scan(&page.ExpectedCursor.Version,
			&page.ExpectedCursor.CycleUpperObservationID,
			&page.ExpectedCursor.AfterObservationID,
			&page.ExpectedCursor.AfterAuthorizationDomain)
	}
	closeErr := rows.Close()
	if err != nil {
		return page, fmt.Errorf("scanner discovery cursor: %w", err)
	}
	if closeErr != nil {
		return page, fmt.Errorf("scanner discovery cursor close: %w", closeErr)
	}
	upper := page.ExpectedCursor.CycleUpperObservationID
	if upper == 0 {
		upperRows, queryErr := s.database.QueryContext(ctx, scannerDiscoveryUpperQuery)
		if queryErr != nil {
			return page, fmt.Errorf("scanner discovery upper bound: %w", queryErr)
		}
		if !upperRows.Next() {
			_ = upperRows.Close()
			return page, errors.New("scanner discovery upper bound absent")
		}
		queryErr = upperRows.Scan(&upper)
		closeErr = upperRows.Close()
		if queryErr != nil {
			return page, fmt.Errorf("scanner discovery upper bound: %w", queryErr)
		}
		if closeErr != nil {
			return page, fmt.Errorf("scanner discovery upper bound close: %w", closeErr)
		}
	}
	candidateArgs := []any{
		request.ScannerCollectorInstanceID,
		string(request.AuthorizationNamespace), request.TenantID, request.WorkspaceID,
		request.SubjectClass, request.PolicyRevisionHash, request.LocalSourceScopeIDs,
		page.ExpectedCursor.AfterObservationID,
		page.ExpectedCursor.AfterAuthorizationDomain, upper,
		request.ExpectedRolePolicyRevision, request.ExpectedDatabaseIdentity,
	}
	rows, err = s.database.QueryContext(ctx, scannerDiscoveryCandidatesQuery(request.Limit+1), candidateArgs...)
	if err != nil {
		return page, fmt.Errorf("scanner discovery candidates: %w", err)
	}
	for rows.Next() {
		var candidate workflow.RunningImageObservationCandidate
		var producerClass, observationClass, digestKind string
		if err := rows.Scan(&candidate.ObservationID, &candidate.ProducerInstanceID,
			&candidate.SourceScopeID, &candidate.SourceGenerationID,
			&candidate.ObjectID, &candidate.SlotID, &producerClass, &observationClass,
			&candidate.ObservedRevision, &candidate.Registry, &candidate.Repository,
			&candidate.RuntimeImageReference, &candidate.RuntimeDigest, &digestKind,
			&candidate.ManifestDigest, &candidate.ManifestVerificationID,
			&candidate.AuthorizationDomain); err != nil {
			_ = rows.Close()
			return page, fmt.Errorf("scanner discovery candidate: %w", err)
		}
		candidate.ProducerClass = workflow.RunningImageProducerClass(producerClass)
		candidate.ObservationClass = workflow.RunningImageObservationClass(observationClass)
		candidate.RuntimeDigestKind = workflow.RunningImageDigestKind(digestKind)
		page.Candidates = append(page.Candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return page, fmt.Errorf("scanner discovery candidates: %w", err)
	}
	if err := rows.Close(); err != nil {
		return page, fmt.Errorf("scanner discovery candidates close: %w", err)
	}
	page.Exhausted = len(page.Candidates) <= request.Limit
	if len(page.Candidates) > request.Limit {
		page.Candidates = page.Candidates[:request.Limit]
	}
	// Scope the counts to the span just read. A non-exhausted read always
	// returns request.Limit (validated >= 1) candidates, so the span end
	// below always exists; an exhausted read keeps the window remainder.
	skipStatement := scannerDiscoverySkipCountsStatement(page.Exhausted)
	skipArgs := candidateArgs
	if !page.Exhausted {
		last := page.Candidates[len(page.Candidates)-1]
		skipArgs = append(append([]any{}, candidateArgs...), last.ObservationID, last.AuthorizationDomain)
	}
	skipRows, err := s.database.QueryContext(ctx, skipStatement, skipArgs...)
	if err != nil {
		return page, fmt.Errorf("scanner discovery skip counts: %w", err)
	}
	for skipRows.Next() {
		var class string
		var count int
		if err := skipRows.Scan(&class, &count); err != nil {
			_ = skipRows.Close()
			return page, fmt.Errorf("scanner discovery skip count: %w", err)
		}
		switch class {
		case "tag_only":
			page.SkippedTagOnly = count
		case "unresolved_config":
			page.SkippedUnresolvedConfig = count
		}
	}
	if err := skipRows.Err(); err != nil {
		_ = skipRows.Close()
		return page, fmt.Errorf("scanner discovery skip counts: %w", err)
	}
	if err := skipRows.Close(); err != nil {
		return page, fmt.Errorf("scanner discovery skip counts close: %w", err)
	}
	page.NextCursor = page.ExpectedCursor
	page.NextCursor.Version++
	if page.Exhausted {
		page.NextCursor.CycleUpperObservationID = 0
		page.NextCursor.AfterObservationID = 0
		page.NextCursor.AfterAuthorizationDomain = ""
	} else {
		last := page.Candidates[len(page.Candidates)-1]
		page.NextCursor.CycleUpperObservationID = upper
		page.NextCursor.AfterObservationID = last.ObservationID
		page.NextCursor.AfterAuthorizationDomain = last.AuthorizationDomain
	}
	return page, nil
}
