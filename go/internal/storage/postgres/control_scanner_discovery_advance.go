// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrScannerDiscoveryCursorAdvanced means another coordinator committed the
// same bounded page first. The caller must read a new page before retrying.
var ErrScannerDiscoveryCursorAdvanced = errors.New("scanner discovery cursor advanced")

type scannerDiscoveryAdvance struct {
	request  workflow.RunningImageDiscoveryRequest
	expected workflow.RunningImageDiscoveryCursor
	next     workflow.RunningImageDiscoveryCursor
}

const scannerDiscoveryAdvanceQuery = `
SELECT eshu_security.advance_scanner_discovery_cursor_v1(
    $1::jsonb,$2::jsonb,$3::jsonb)`

const scannerDiscoveryAuthorizeQuery = `
SELECT eshu_security.authorize_scanner_discovery_v1($1,$2::uuid)`

func authorizeScannerDiscovery(ctx context.Context, tx db.Queryer,
	request workflow.RunningImageDiscoveryRequest,
) error {
	rows, err := tx.QueryContext(ctx, scannerDiscoveryAuthorizeQuery,
		request.ExpectedRolePolicyRevision, request.ExpectedDatabaseIdentity)
	if err != nil {
		return fmt.Errorf("authorize scanner discovery: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return errors.New("scanner discovery authorization absent")
	}
	var authorized bool
	if err := rows.Scan(&authorized); err != nil {
		return fmt.Errorf("scanner discovery authorization: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scanner discovery authorization: %w", err)
	}
	if !authorized {
		return errors.New("scanner discovery authorization denied")
	}
	return nil
}

func advanceScannerDiscoveryCursor(ctx context.Context, tx db.Queryer,
	advance *scannerDiscoveryAdvance,
) error {
	if advance == nil {
		return nil
	}
	requestJSON, err := json.Marshal(advance.request)
	if err != nil {
		return fmt.Errorf("encode scanner discovery request: %w", err)
	}
	expectedJSON, err := json.Marshal(advance.expected)
	if err != nil {
		return fmt.Errorf("encode scanner discovery expected cursor: %w", err)
	}
	nextJSON, err := json.Marshal(advance.next)
	if err != nil {
		return fmt.Errorf("encode scanner discovery next cursor: %w", err)
	}
	rows, err := tx.QueryContext(ctx, scannerDiscoveryAdvanceQuery,
		string(requestJSON), string(expectedJSON), string(nextJSON))
	if err != nil {
		return fmt.Errorf("advance scanner discovery cursor: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return fmt.Errorf("advance scanner discovery cursor: no result")
	}
	var advanced bool
	if err := rows.Scan(&advanced); err != nil {
		return fmt.Errorf("advance scanner discovery cursor: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("advance scanner discovery cursor: %w", err)
	}
	if !advanced {
		return ErrScannerDiscoveryCursorAdvanced
	}
	return nil
}
