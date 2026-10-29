// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const scannerResultFactLimit = 50_000

const scannerResultFactMembershipQuery = `
SELECT fact_id,fact_kind,fencing_token,source_system,is_tombstone
FROM fact_records
WHERE scope_id=$1 AND generation_id=$2 AND fact_id=ANY($3::text[])`

const scannerResultReceiptInsertQuery = `
INSERT INTO scanner_image_scan_results
(result_receipt_id,work_item_id,binding_id,scan_scope_id,scan_generation_id,
 claim_id,claim_fencing_token,claim_owner_id,completed_authority_vector,completed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)
ON CONFLICT DO NOTHING`

const scannerResultFactAssociationsInsertQuery = `
INSERT INTO scanner_image_scan_fact_associations
(fact_id,result_receipt_id,scan_scope_id,scan_generation_id,fact_kind,
 payload_sha256,fencing_token)
SELECT fact_id,$1,$2,$3,fact_kind,
       encode(sha256(convert_to(payload::text,'UTF8')),'hex'),fencing_token
FROM fact_records
WHERE scope_id=$2 AND generation_id=$3 AND fact_id=ANY($4::text[])
ON CONFLICT DO NOTHING`

// CommitDerivedImageResult commits a claimed image generation, its protected
// source receipt and exact fact membership, and claim completion atomically.
// The source authorization is locked before the workflow claim and retained
// through the transaction; a skipped generation is never accepted as success.
func (s IngestionStore) CommitDerivedImageResult(
	ctx context.Context, mutation workflow.ClaimMutation, scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration, sourceFacts []facts.Envelope,
) error {
	if err := validateClaimMutation(mutation); err != nil {
		return err
	}
	if !mutation.DerivedScannerImage || !strings.HasPrefix(scopeValue.ScopeID, "image://derived/") ||
		generation.ScopeID != scopeValue.ScopeID || len(sourceFacts) == 0 ||
		len(sourceFacts) > scannerResultFactLimit {
		return errors.New("derived image result boundary or fact count invalid")
	}
	expected, err := validateDerivedImageResultFacts(mutation, scopeValue.ScopeID, generation.GenerationID, sourceFacts)
	if err != nil {
		return err
	}
	// Attempt generations have their own work identity. Disable the ingestion
	// freshness skip so this path cannot return success before the receipt runs.
	generation.FreshnessHint = ""
	stream := make(chan facts.Envelope, factBatchSize)
	stopProducer := make(chan struct{})
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer close(stream)
		for _, fact := range sourceFacts {
			select {
			case stream <- fact:
			case <-stopProducer:
				return
			}
		}
	}()
	var firstReceipt scannerAuthorityReceipt
	beforeClaim := func(ctx context.Context, tx db.ExecQueryer) error {
		receipt, err := lockScannerAttemptAuthority(ctx, tx,
			mutation.WorkItemID, mutation.ClaimID, mutation.FencingToken, mutation.OwnerID)
		if err != nil {
			return err
		}
		if receipt.ScanScopeID != scopeValue.ScopeID || receipt.ScanGenerationID != generation.GenerationID ||
			receipt.BindingID == "" {
			return ErrScannerTargetNotAuthorized
		}
		firstReceipt = receipt
		return nil
	}
	stateWrote := false
	acceptedFacts := 0
	onAcceptedBatch := func(_ context.Context, _ db.ExecQueryer, batch []facts.Envelope) error {
		acceptedFacts += len(batch)
		return nil
	}
	stateWriter := func(ctx context.Context, executor collector.CommitExecutor) error {
		tx, ok := executor.(db.ExecQueryer)
		if !ok {
			return errors.New("derived image result transaction cannot read authority")
		}
		if firstReceipt.ScanScopeID != scopeValue.ScopeID ||
			firstReceipt.ScanGenerationID != generation.GenerationID ||
			firstReceipt.WorkItemID != mutation.WorkItemID ||
			firstReceipt.ClaimID != mutation.ClaimID ||
			firstReceipt.ClaimFencingToken != mutation.FencingToken ||
			firstReceipt.ClaimOwnerID != mutation.OwnerID ||
			firstReceipt.BindingID == "" || len(firstReceipt.CompletedAuthorityVector) == 0 {
			return ErrScannerTargetNotAuthorized
		}
		if acceptedFacts != len(expected) {
			return errors.New("derived image result did not persist every emitted fact")
		}
		if err := writeScannerImageResult(ctx, tx, mutation, firstReceipt, expected); err != nil {
			return err
		}
		if err := NewWorkflowControlStore(tx).CompleteClaim(ctx, mutation); err != nil {
			return fmt.Errorf("complete derived image claim: %w", err)
		}
		stateWrote = true
		return nil
	}
	err = s.commitScopeGeneration(ctx, mutation, true, scopeValue, generation,
		stream, nil, stateWriter, beforeClaim, onAcceptedBatch)
	close(stopProducer)
	<-producerDone
	if err != nil {
		return err
	}
	if !stateWrote {
		return errors.New("derived image result skipped without receipt or completion")
	}
	return nil
}

func validateDerivedImageResultFacts(mutation workflow.ClaimMutation, scopeID, generationID string,
	sourceFacts []facts.Envelope,
) (map[string]string, error) {
	expected := make(map[string]string, len(sourceFacts))
	for _, fact := range sourceFacts {
		if fact.FactID == "" || fact.ScopeID != scopeID || fact.SourceRef.ScopeID != scopeID ||
			fact.GenerationID != generationID || fact.SourceRef.GenerationID != generationID ||
			fact.FencingToken != mutation.FencingToken || fact.IsTombstone {
			return nil, errors.New("derived image result fact boundary invalid")
		}
		producer := string(scope.CollectorScannerWorker)
		switch fact.FactKind {
		case facts.ScannerWorkerAnalysisFactKind, facts.ScannerWorkerWarningFactKind:
			if fact.SourceRef.SourceRecordID != scopeID {
				return nil, errors.New("derived image result source record invalid")
			}
		case chain.VulnerabilityOSPackageFactKind, chain.VulnerabilityWarningFactKind:
			producer = ospackagevulnerability.CollectorKind
		default:
			return nil, errors.New("derived image result fact kind invalid")
		}
		// Claim-plane identity is planner-owned (#9407): source_system is the
		// scanner_worker claim identity for every kind, while CollectorKind
		// stays analyzer-owned for package facts. Demanding the analyzer kind
		// in source_system would reject every genuine package fact.
		if fact.CollectorKind != producer ||
			fact.SourceRef.SourceSystem != string(scope.CollectorScannerWorker) {
			return nil, errors.New("derived image result fact producer invalid")
		}
		if _, duplicate := expected[fact.FactID]; duplicate {
			return nil, errors.New("derived image result duplicate fact ID")
		}
		expected[fact.FactID] = fact.FactKind
	}
	return expected, nil
}

func writeScannerImageResult(ctx context.Context, tx db.ExecQueryer, mutation workflow.ClaimMutation,
	receipt scannerAuthorityReceipt, expected map[string]string,
) error {
	factIDs := make([]string, 0, len(expected))
	for factID := range expected {
		factIDs = append(factIDs, factID)
	}
	rows, err := tx.QueryContext(ctx, scannerResultFactMembershipQuery,
		receipt.ScanScopeID, receipt.ScanGenerationID, factIDs)
	if err != nil {
		return fmt.Errorf("read derived image fact membership: %w", err)
	}
	seen := make(map[string]struct{}, len(expected))
	for rows.Next() {
		var factID, factKind, sourceSystem string
		var fence int64
		var tombstone bool
		if err := rows.Scan(&factID, &factKind, &fence, &sourceSystem, &tombstone); err != nil {
			_ = rows.Close()
			return fmt.Errorf("read derived image fact membership: %w", err)
		}
		// The persisted claim identity is planner-owned (#9407) for every
		// kind. The analyzer-owned CollectorKind was already checked on the
		// envelope before the write; this membership check pins kind,
		// fence, claim identity, and life, while the persisted rows keep
		// their collector kind for the authorized-impact read path.
		if expected[factID] != factKind || fence != mutation.FencingToken ||
			sourceSystem != string(scope.CollectorScannerWorker) || tombstone {
			_ = rows.Close()
			return errors.New("derived image persisted fact membership mismatch")
		}
		seen[factID] = struct{}{}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("read derived image fact membership: %w", errors.Join(readErr, closeErr))
	}
	if len(seen) != len(expected) {
		return errors.New("derived image persisted fact membership incomplete")
	}
	receiptID := facts.StableID("scanner-image-scan-result", map[string]any{
		"work_item_id": receipt.WorkItemID,
	})
	inserted, err := tx.ExecContext(ctx, scannerResultReceiptInsertQuery,
		receiptID, receipt.WorkItemID, receipt.BindingID, receipt.ScanScopeID,
		receipt.ScanGenerationID, receipt.ClaimID, receipt.ClaimFencingToken,
		receipt.ClaimOwnerID, string(receipt.CompletedAuthorityVector), mutation.ObservedAt.UTC())
	if err != nil {
		return fmt.Errorf("insert derived image scan receipt: %w", err)
	}
	if err := scannerResultRowsAffected(inserted, 1); err != nil {
		return err
	}
	associated, err := tx.ExecContext(ctx, scannerResultFactAssociationsInsertQuery,
		receiptID, receipt.ScanScopeID, receipt.ScanGenerationID, factIDs)
	if err != nil {
		return fmt.Errorf("associate derived image facts: %w", err)
	}
	return scannerResultRowsAffected(associated, int64(len(expected)))
}

func scannerResultRowsAffected(result interface{ RowsAffected() (int64, error) }, want int64) error {
	got, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read derived image result row count: %w", err)
	}
	if got != want {
		return fmt.Errorf("derived image result row count %d, want %d", got, want)
	}
	return nil
}
