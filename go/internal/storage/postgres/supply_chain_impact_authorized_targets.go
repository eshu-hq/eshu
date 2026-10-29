// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const (
	authorizedImpactTargetLimit   = 1000
	authorizedImpactScanFactLimit = 50000
)

const authorizedImpactLocalTargetsExistSQL = `SELECT EXISTS (
 SELECT 1 FROM scanner_image_targets WHERE authorization_namespace='local')`

const authorizedImpactScanFactCountSQL = `
SELECT count(*) FROM scanner_image_scan_fact_associations
WHERE result_receipt_id=$1 AND scan_scope_id=$2 AND scan_generation_id=$3`

const authorizedImpactScanFactsSQL = `
WITH authorized AS MATERIALIZED (
 SELECT fact.fact_id,fact.scope_id,fact.generation_id,fact.fact_kind,
        fact.stable_fact_key,fact.schema_version,fact.collector_kind,
        fact.fencing_token,fact.source_confidence,fact.source_system,
        fact.source_fact_key,COALESCE(fact.source_uri,''),
        COALESCE(fact.source_record_id,''),fact.observed_at,
        fact.is_tombstone,fact.payload
 FROM scanner_image_scan_fact_associations AS membership
 JOIN fact_records AS fact ON fact.fact_id=membership.fact_id
  AND fact.fencing_token=membership.fencing_token
  AND encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex')=membership.payload_sha256
 WHERE membership.result_receipt_id=$1
   AND membership.scan_scope_id=$2
   AND membership.scan_generation_id=$3
   AND fact.scope_id=membership.scan_scope_id
   AND fact.generation_id=membership.scan_generation_id
   AND fact.fact_kind=membership.fact_kind
   AND ((fact.fact_kind IN ('scanner_worker.analysis','scanner_worker.warning')
         AND fact.source_system='scanner_worker')
     OR (fact.fact_kind IN ('vulnerability.os_package','vulnerability.warning')
         AND fact.source_system='os_package_vulnerability'))
   AND NOT fact.is_tombstone
)
SELECT * FROM authorized ORDER BY fact_id LIMIT $4`

type authorizedImpactTargetReceipt struct {
	scanTargetID     string
	scanGenerationID string
	resultReceiptID  string
	bindingID        string
	workItemID       string
	claimID          string
	claimFence       int64
	claimOwnerID     string
	tenantID         string
	workspaceID      string
}

func loadCurrentAuthorizedImpactTargets(
	ctx context.Context, queryer db.Queryer, intent reducercontract.Intent,
	public authorizedImpactPublicAdvisorySet,
) ([]supplychaincore.AuthorizedScanImpactEvidence, []supplychaincore.ScanImpactPublicationWitness, string, error) {
	local, err := authorizedImpactLocalTargetsExist(ctx, queryer)
	if err != nil {
		return nil, nil, "", err
	}
	if local {
		// Local source allowlist membership is not yet persisted as a
		// protected current authority witness. Never infer it from facts.
		return nil, nil, "policy_unavailable", nil
	}
	currentTargets, reason, err := listAuthorizedImpactCurrentTargets(ctx, queryer)
	if err != nil || reason != "" {
		return nil, nil, reason, err
	}
	receipts, reason, err := listAuthorizedImpactTargetReceipts(ctx, queryer)
	if err != nil || reason != "" {
		return nil, nil, reason, err
	}
	if len(currentTargets) != len(receipts) {
		return nil, nil, "partial_snapshot", nil
	}
	for _, receipt := range receipts {
		if _, current := currentTargets[receipt.scanTargetID]; !current {
			return nil, nil, "partial_snapshot", nil
		}
	}
	if len(receipts) == 0 {
		return nil, nil, "", nil
	}
	targets := make([]supplychaincore.AuthorizedScanImpactEvidence, 0, len(receipts))
	publications := make([]supplychaincore.ScanImpactPublicationWitness, 0, len(receipts))
	for _, receipt := range receipts {
		target, witness, reason, err := loadAuthorizedImpactTarget(ctx, queryer, intent, receipt, public)
		if err != nil || reason != "" {
			return nil, nil, reason, err
		}
		targets = append(targets, target)
		publications = append(publications, witness)
	}
	return targets, publications, "", nil
}

func listAuthorizedImpactCurrentTargets(ctx context.Context, queryer db.Queryer) (map[string]struct{}, string, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactCurrentTargetsSQL, authorizedImpactTargetLimit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list current authorized scan targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	targets := make(map[string]struct{})
	for rows.Next() {
		var targetID string
		if err := rows.Scan(&targetID); err != nil {
			return nil, "", fmt.Errorf("read current authorized scan target: %w", err)
		}
		targets[targetID] = struct{}{}
		if len(targets) > authorizedImpactTargetLimit {
			return nil, "cardinality_limit", nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate current authorized scan targets: %w", err)
	}
	return targets, "", nil
}

func authorizedImpactLocalTargetsExist(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactLocalTargetsExistSQL)
	if err != nil {
		return false, fmt.Errorf("check local scan target authority: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, errors.Join(fmt.Errorf("local scan target authority query returned no row"), rows.Err())
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("read local scan target authority: %w", err)
	}
	return exists, rows.Err()
}

func listAuthorizedImpactTargetReceipts(ctx context.Context, queryer db.Queryer) ([]authorizedImpactTargetReceipt, string, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactTargetReceiptsSQL, authorizedImpactTargetLimit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list current protected scan receipts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	receipts := make([]authorizedImpactTargetReceipt, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var receipt authorizedImpactTargetReceipt
		if err := rows.Scan(&receipt.scanTargetID, &receipt.scanGenerationID,
			&receipt.resultReceiptID, &receipt.bindingID, &receipt.workItemID,
			&receipt.claimID, &receipt.claimFence, &receipt.claimOwnerID,
			&receipt.tenantID, &receipt.workspaceID); err != nil {
			return nil, "", fmt.Errorf("read current protected scan receipt: %w", err)
		}
		if _, duplicate := seen[receipt.scanTargetID]; duplicate {
			return nil, "partial_snapshot", nil
		}
		seen[receipt.scanTargetID] = struct{}{}
		receipts = append(receipts, receipt)
		if len(receipts) > authorizedImpactTargetLimit {
			return nil, "cardinality_limit", nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate current protected scan receipts: %w", err)
	}
	return receipts, "", nil
}

func loadAuthorizedImpactTarget(
	ctx context.Context, queryer db.Queryer, _ reducercontract.Intent,
	receipt authorizedImpactTargetReceipt, public authorizedImpactPublicAdvisorySet,
) (supplychaincore.AuthorizedScanImpactEvidence, supplychaincore.ScanImpactPublicationWitness, string, error) {
	expectedFacts, err := countAuthorizedImpactScanFacts(ctx, queryer, receipt)
	if err != nil {
		return supplychaincore.AuthorizedScanImpactEvidence{}, supplychaincore.ScanImpactPublicationWitness{}, "", err
	}
	if expectedFacts > authorizedImpactScanFactLimit {
		return supplychaincore.AuthorizedScanImpactEvidence{}, supplychaincore.ScanImpactPublicationWitness{}, "cardinality_limit", nil
	}
	rows, err := queryer.QueryContext(ctx, authorizedImpactScanFactsSQL,
		receipt.resultReceiptID, receipt.scanTargetID, receipt.scanGenerationID, authorizedImpactScanFactLimit+1)
	if err != nil {
		return supplychaincore.AuthorizedScanImpactEvidence{}, supplychaincore.ScanImpactPublicationWitness{}, "", fmt.Errorf("load protected scan facts: %w", err)
	}
	target := supplychaincore.AuthorizedScanImpactEvidence{
		ScanTargetID: receipt.scanTargetID, ScanGenerationID: receipt.scanGenerationID,
	}
	witness := supplychaincore.ScanImpactPublicationWitness{
		ScanTargetID: receipt.scanTargetID, ScanGenerationID: receipt.scanGenerationID,
		BindingID: receipt.bindingID, WorkItemID: receipt.workItemID,
		ClaimID: receipt.claimID, ClaimFencingToken: receipt.claimFence,
		ClaimOwnerID: receipt.claimOwnerID, TenantID: receipt.tenantID,
		WorkspaceID: receipt.workspaceID,
	}
	for rows.Next() {
		envelope, err := scanSupplyChainImpactFact(rows)
		if err != nil {
			_ = rows.Close()
			return target, witness, "", fmt.Errorf("decode protected scan fact: %w", err)
		}
		switch envelope.FactKind {
		case chain.VulnerabilityOSPackageFactKind:
			target.OSPackages = append(target.OSPackages, envelope)
			witness.IntrinsicEvidenceFactIDs = append(witness.IntrinsicEvidenceFactIDs, envelope.FactID)
		case facts.ScannerWorkerAnalysisFactKind:
			analysis, err := schemadecode.DecodeScannerWorkerAnalysis(envelope)
			if err != nil || analysis.AnalysisStatus != "completed" || analysis.CoverageStatus != "scanned" {
				_ = rows.Close()
				return target, witness, "partial_snapshot", nil
			}
			if witness.AnalysisVersion != "" && witness.AnalysisVersion != envelope.SchemaVersion {
				_ = rows.Close()
				return target, witness, "partial_snapshot", nil
			}
			witness.AnalysisVersion = envelope.SchemaVersion
			target.Analyses = append(target.Analyses, envelope)
			witness.IntrinsicEvidenceFactIDs = append(witness.IntrinsicEvidenceFactIDs, envelope.FactID)
		case facts.ScannerWorkerWarningFactKind, chain.VulnerabilityWarningFactKind:
			_ = rows.Close()
			return target, witness, "partial_snapshot", nil
		default:
			_ = rows.Close()
			return target, witness, "partial_snapshot", nil
		}
		if len(target.OSPackages)+len(target.Analyses) > authorizedImpactScanFactLimit {
			_ = rows.Close()
			return target, witness, "cardinality_limit", nil
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return target, witness, "", errors.Join(readErr, closeErr)
	}
	if len(target.OSPackages)+len(target.Analyses) != expectedFacts {
		return target, witness, "partial_snapshot", nil
	}
	if len(target.Analyses) != 1 || witness.AnalysisVersion == "" {
		return target, witness, "partial_snapshot", nil
	}
	return loadAuthorizedImpactTargetAdvisories(ctx, queryer, receipt, target, witness, public)
}

func countAuthorizedImpactScanFacts(ctx context.Context, queryer db.Queryer, receipt authorizedImpactTargetReceipt) (int, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactScanFactCountSQL,
		receipt.resultReceiptID, receipt.scanTargetID, receipt.scanGenerationID)
	if err != nil {
		return 0, fmt.Errorf("count protected scan fact members: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.Join(fmt.Errorf("protected scan fact count returned no row"), rows.Err())
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("read protected scan fact count: %w", err)
	}
	return count, rows.Err()
}
