// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/pgconn"
)

// scannerAuthorityReceipt is the exact protected source and claim witness
// locked through the transaction that commits scanner facts and completion.
type scannerAuthorityReceipt struct {
	ScanScopeID              string
	ScanGenerationID         string
	BindingID                string
	WorkItemID               string
	ClaimID                  string
	ClaimFencingToken        int64
	ClaimOwnerID             string
	CompletedAuthorityVector []byte
}

const scannerAttemptAuthorityQuery = `
SELECT scan_scope_id,scan_generation_id,binding_id,work_item_id,
       claim_id,claim_fencing_token,claim_owner_id,authority_vector
FROM eshu_security.authorize_scanner_attempt_v1($1,$2,$3,$4)`

// lockScannerAttemptAuthority validates and locks the original pinned binding,
// source publication and current grant epochs before the workflow claim lock.
// The caller must keep tx open through fact persistence and completion.
func lockScannerAttemptAuthority(ctx context.Context, tx db.ExecQueryer,
	workItemID, claimID string, fencingToken int64, ownerID string,
) (scannerAuthorityReceipt, error) {
	if tx == nil || workItemID == "" || claimID == "" ||
		fencingToken <= 0 || ownerID == "" {
		return scannerAuthorityReceipt{}, fmt.Errorf("scanner authority requires a fenced claim")
	}
	rows, err := tx.QueryContext(ctx, scannerAttemptAuthorityQuery,
		workItemID, claimID, fencingToken, ownerID)
	if err != nil {
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "28000" {
			return scannerAuthorityReceipt{}, ErrScannerTargetNotAuthorized
		}
		return scannerAuthorityReceipt{}, fmt.Errorf("scanner authority validation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return scannerAuthorityReceipt{}, fmt.Errorf("scanner authority validation: %w", err)
		}
		return scannerAuthorityReceipt{}, ErrScannerTargetNotAuthorized
	}
	var receipt scannerAuthorityReceipt
	if err := rows.Scan(&receipt.ScanScopeID, &receipt.ScanGenerationID,
		&receipt.BindingID, &receipt.WorkItemID, &receipt.ClaimID,
		&receipt.ClaimFencingToken, &receipt.ClaimOwnerID,
		&receipt.CompletedAuthorityVector); err != nil {
		return scannerAuthorityReceipt{}, fmt.Errorf("scanner authority receipt: %w", err)
	}
	if err := rows.Err(); err != nil {
		return scannerAuthorityReceipt{}, fmt.Errorf("scanner authority receipt: %w", err)
	}
	if receipt.WorkItemID != workItemID || receipt.ClaimID != claimID ||
		receipt.ClaimFencingToken != fencingToken ||
		receipt.ClaimOwnerID != ownerID || len(receipt.CompletedAuthorityVector) == 0 {
		return scannerAuthorityReceipt{}, ErrScannerTargetNotAuthorized
	}
	return receipt, nil
}
