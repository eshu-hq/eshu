// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// scannerSourceProof is returned by the fixed-capability protected DB API.
// The admitted binding pins these non-reusable epochs; no generic fact row is
// accepted as source authority.
type scannerSourceProof struct {
	sourceInstanceID          string
	sourceScopeID             string
	sourceGenerationID        string
	registry                  string
	repository                string
	runtimeImageReference     string
	runtimeDigest             string
	runtimeDigestKind         string
	sourcePayloadHash         string
	domainConfigHash          string
	domainConfigRaw           []byte
	producerPrincipalOID      int64
	sourceActivationEpoch     int64
	producerGrantEpoch        int64
	sourceInstanceEpoch       int64
	tenantEpoch               int64
	workspaceEpoch            int64
	scopeGrantEpoch           int64
	registryEpoch             int64
	rolePolicyRevision        int64
	databaseIdentity          string
	manifestVerificationEpoch int64
}

const scannerCanonicalSourceProofQuery = `
SELECT source_instance_id,source_scope_id,source_generation_id,
       source_registry,source_repository,source_runtime_ref,
       source_runtime_digest,source_runtime_digest_kind,
       source_payload_sha256,domain_config_sha256,domain_config,
       producer_principal_oid,source_activation_epoch,producer_grant_epoch,
       source_instance_epoch,tenant_epoch,workspace_epoch,scope_grant_epoch,
       registry_epoch,role_policy_revision,database_identity,
       manifest_verification_epoch
FROM eshu_security.validate_scanner_source_admission_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::text[],$12,$13::uuid)`

func proveScannerSource(ctx context.Context, tx db.Queryer,
	item workflow.WorkItem, target workflow.ScannerImageTarget,
	source workflow.ScannerImageSourceBinding,
	request workflow.RunningImageDiscoveryRequest,
) (scannerSourceProof, error) {
	if source.SourceObservationID <= 0 || source.SourceFactID != "" ||
		request.ScannerCollectorInstanceID != item.CollectorInstanceID ||
		request.AuthorizationNamespace != target.AuthorizationNamespace ||
		request.TenantID != target.TenantID ||
		request.WorkspaceID != target.WorkspaceID ||
		request.SubjectClass != target.SubjectClass ||
		request.PolicyRevisionHash != target.PolicyRevisionHash {
		return scannerSourceProof{}, ErrScannerTargetNotAuthorized
	}
	rows, err := tx.QueryContext(ctx, scannerCanonicalSourceProofQuery,
		source.SourceObservationID, item.CollectorInstanceID,
		string(target.AuthorizationNamespace), target.TenantID,
		target.WorkspaceID, target.SubjectClass, target.PolicyRevisionHash,
		target.AuthorizationDomain, target.ManifestDigest,
		source.ManifestVerificationID, request.LocalSourceScopeIDs,
		request.ExpectedRolePolicyRevision, request.ExpectedDatabaseIdentity)
	if err != nil {
		return scannerSourceProof{}, fmt.Errorf("scanner canonical source admission: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return scannerSourceProof{}, fmt.Errorf("scanner canonical source admission: %w", err)
		}
		return scannerSourceProof{}, ErrScannerTargetNotAuthorized
	}
	var proof scannerSourceProof
	if err := rows.Scan(&proof.sourceInstanceID, &proof.sourceScopeID,
		&proof.sourceGenerationID, &proof.registry, &proof.repository,
		&proof.runtimeImageReference, &proof.runtimeDigest,
		&proof.runtimeDigestKind, &proof.sourcePayloadHash,
		&proof.domainConfigHash, &proof.domainConfigRaw,
		&proof.producerPrincipalOID, &proof.sourceActivationEpoch,
		&proof.producerGrantEpoch, &proof.sourceInstanceEpoch,
		&proof.tenantEpoch, &proof.workspaceEpoch,
		&proof.scopeGrantEpoch, &proof.registryEpoch,
		&proof.rolePolicyRevision, &proof.databaseIdentity,
		&proof.manifestVerificationEpoch); err != nil {
		return scannerSourceProof{}, fmt.Errorf("scan canonical source proof: %w", err)
	}
	if err := rows.Err(); err != nil {
		return scannerSourceProof{}, fmt.Errorf("scanner canonical source admission: %w", err)
	}
	if proof.sourceInstanceID != source.SourceCollectorInstanceID ||
		proof.sourceScopeID != source.SourceScopeID ||
		proof.sourceGenerationID != source.SourceGenerationID ||
		proof.registry != source.Registry ||
		proof.repository != source.Repository ||
		proof.runtimeImageReference != source.RuntimeImageReference ||
		proof.runtimeDigest != source.RuntimeDigest ||
		proof.runtimeDigestKind != string(source.RuntimeDigestKind) ||
		proof.sourcePayloadHash == "" || proof.domainConfigHash == "" ||
		proof.producerPrincipalOID <= 0 || proof.sourceActivationEpoch <= 0 ||
		proof.producerGrantEpoch <= 0 || proof.sourceInstanceEpoch <= 0 ||
		proof.registryEpoch <= 0 ||
		proof.rolePolicyRevision != request.ExpectedRolePolicyRevision ||
		proof.databaseIdentity != request.ExpectedDatabaseIdentity {
		return scannerSourceProof{}, ErrScannerTargetNotAuthorized
	}
	if source.RuntimeDigestKind == workflow.ScannerRuntimeDigestConfig &&
		(proof.manifestVerificationEpoch <= 0 || source.ManifestVerificationID == "") {
		return scannerSourceProof{}, ErrScannerTargetNotAuthorized
	}
	return proof, nil
}

func scannerBindingID(target workflow.ScannerImageTarget, item workflow.WorkItem,
	source workflow.ScannerImageSourceBinding, proof scannerSourceProof,
) string {
	// Binding identity covers the scan content and its generation-scoped
	// authority, not the volatile collector-instance epochs. Instance rows
	// flap on ordinary ops traffic (enable toggles, config edits) and their
	// epochs only move forward, so pinning them here would fork identity on
	// every flap and defeat completed-target suppression. The instance
	// epochs stay pinned in the binding columns and every use rechecks them
	// against current authority before the worker acts.
	var key strings.Builder
	for _, value := range [...]string{
		target.ScanTargetID, item.CollectorInstanceID,
		target.PolicyRevisionHash, fmt.Sprint(source.SourceObservationID),
		source.SourceScopeID, source.SourceGenerationID, source.Registry,
		source.Repository, source.RuntimeDigest, string(source.RuntimeDigestKind),
		source.RuntimeImageReference, source.ManifestVerificationID,
		proof.domainConfigHash, proof.sourcePayloadHash,
		fmt.Sprint(proof.sourceActivationEpoch),
		fmt.Sprint(proof.producerGrantEpoch),
		fmt.Sprint(proof.tenantEpoch), fmt.Sprint(proof.workspaceEpoch),
		fmt.Sprint(proof.scopeGrantEpoch),
		fmt.Sprint(proof.rolePolicyRevision), proof.databaseIdentity,
		fmt.Sprint(proof.manifestVerificationEpoch),
	} {
		fmt.Fprintf(&key, "%d:%s", len(value), value)
	}
	sum := sha256.Sum256([]byte(key.String()))
	return "scanner-binding:" + hex.EncodeToString(sum[:])
}
