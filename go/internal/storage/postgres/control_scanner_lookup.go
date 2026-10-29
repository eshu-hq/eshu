// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const scannerTargetForClaimQuery = `
SELECT target.scan_scope_id,target.authorization_namespace,target.tenant_id,
       target.workspace_id,target.subject_class,target.authorization_domain,
       target.manifest_digest,binding.canonical_observation_id,
       binding.source_collector_instance_id,binding.source_scope_id,
       binding.source_generation_id,binding.registry,binding.repository,
       binding.runtime_digest,binding.runtime_digest_kind,
       binding.runtime_image_reference,binding.manifest_verification_id,
       binding.policy_revision_hash,item.collector_instance_id,domain.value,
       binding.binding_id,item.generation_id
FROM scanner_image_target_attempts AS attempt
JOIN workflow_work_items AS item ON item.work_item_id=attempt.work_item_id
JOIN workflow_claims AS claim ON claim.work_item_id=item.work_item_id
JOIN scanner_image_targets AS target ON target.scan_scope_id=attempt.scan_scope_id
JOIN scanner_image_target_sources AS binding
  ON binding.binding_id=attempt.binding_id
 AND binding.scan_scope_id=target.scan_scope_id
JOIN collector_instances AS scanner
  ON scanner.instance_id=binding.scanner_collector_instance_id
 AND scanner.collector_kind='scanner_worker'
 AND scanner.enabled AND scanner.claims_enabled
 AND scanner.configuration->>'derive_from_running_images'='true'
CROSS JOIN LATERAL jsonb_array_elements(
  COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
WHERE item.work_item_id=$1 AND item.scope_id=target.scan_scope_id
  AND item.collector_kind='scanner_worker' AND item.status='claimed'
  AND item.current_claim_id=$2 AND item.current_fencing_token=$3
  AND item.current_owner_id=$4 AND item.lease_expires_at>clock_timestamp()
  AND claim.claim_id=$2 AND claim.fencing_token=$3
  AND claim.owner_id=$4 AND claim.status='active'
  AND claim.lease_expires_at>clock_timestamp()
  AND item.tenant_id=target.tenant_id
  AND item.workspace_id=target.workspace_id
  AND item.subject_class=target.subject_class
  AND item.policy_revision_hash=binding.policy_revision_hash
  AND item.collector_instance_id=binding.scanner_collector_instance_id
  AND item.authorization_scope_id=binding.source_scope_id
  AND binding.canonical_observation_id IS NOT NULL
  AND domain.value->>'authorization_domain'=target.authorization_domain
  AND domain.value->>'registry'=binding.registry
  AND encode(sha256(convert_to(domain.value::text,'UTF8')),'hex')=
      binding.registry_domain_config_sha256
  AND 1=(SELECT count(*) FROM jsonb_array_elements(COALESCE(
      scanner.configuration->'registry_domains','[]'::jsonb)) AS peer(value)
      WHERE peer.value->>'authorization_domain'=target.authorization_domain)
FOR SHARE OF item,claim,target,binding,attempt,scanner
`

// GetScannerImageTarget resolves only the canonical observation pinned to an
// active fenced claim. The protected helper rechecks source and grant epochs
// before the worker constructs network credentials or opens the registry.
func (s *WorkflowControlStore) GetScannerImageTarget(
	ctx context.Context, workItemID, claimID string, fencingToken int64, ownerID string,
) (workflow.ScannerImageTarget, error) {
	if s.beginner == nil {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup requires transactional store")
	}
	if workItemID == "" || claimID == "" || fencingToken <= 0 || ownerID == "" {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup requires active claim identity")
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	receipt, err := lockScannerAttemptAuthority(ctx, tx,
		workItemID, claimID, fencingToken, ownerID)
	if err != nil {
		return workflow.ScannerImageTarget{}, err
	}
	rows, err := tx.QueryContext(ctx, scannerTargetForClaimQuery,
		workItemID, claimID, fencingToken, ownerID)
	if err != nil {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		return workflow.ScannerImageTarget{}, ErrScannerTargetNotAuthorized
	}
	target := workflow.ScannerImageTarget{
		WorkItemID: workItemID,
		Sources:    make([]workflow.ScannerImageSourceBinding, 1),
	}
	source := &target.Sources[0]
	var verification sql.NullString
	var scannerInstanceID, bindingID, generationID string
	var domainConfig []byte
	err = rows.Scan(&target.ScanTargetID, &target.AuthorizationNamespace,
		&target.TenantID, &target.WorkspaceID, &target.SubjectClass,
		&target.AuthorizationDomain, &target.ManifestDigest,
		&source.SourceObservationID, &source.SourceCollectorInstanceID,
		&source.SourceScopeID, &source.SourceGenerationID,
		&source.Registry, &source.Repository, &source.RuntimeDigest,
		&source.RuntimeDigestKind, &source.RuntimeImageReference,
		&verification, &target.PolicyRevisionHash, &scannerInstanceID,
		&domainConfig, &bindingID, &generationID)
	closeErr := rows.Close()
	if err != nil {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup: %w", err)
	}
	if closeErr != nil {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup close: %w", closeErr)
	}
	source.ManifestVerificationID = verification.String
	target.RegistryDomainConfig = append(target.RegistryDomainConfig, domainConfig...)
	if scannerInstanceID == "" || bindingID != receipt.BindingID ||
		target.ScanTargetID != receipt.ScanScopeID ||
		generationID != receipt.ScanGenerationID {
		return workflow.ScannerImageTarget{}, ErrScannerTargetNotAuthorized
	}
	if err := target.Validate(); err != nil {
		return workflow.ScannerImageTarget{}, ErrScannerTargetNotAuthorized
	}
	if err := tx.Commit(); err != nil {
		return workflow.ScannerImageTarget{}, fmt.Errorf("scanner target lookup commit: %w", err)
	}
	committed = true
	return target, nil
}
