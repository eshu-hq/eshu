// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package postgres (this file): status transitions for DB-backed identity
// provider configs (#4966, epic #4962) — EnableProviderConfig,
// DisableProviderConfig, and the shared row-locked setProviderConfigStatus.
// Split out of identity_provider_config_writes.go to keep both files under the
// 500-line cap; Create/Update/Revert remain in that file.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// EnableProviderConfig transitions a provider config from draft to active.
// The caller must have already confirmed a passing test-connection result for
// enable.ExpectedActiveRevisionID. This method row-locks the provider config
// (the same lockProviderConfig Update/Revert use) and compares the CURRENT
// active_revision_id against ExpectedActiveRevisionID inside that lock,
// failing closed with ErrProviderConfigRevisionChanged on a mismatch — a
// concurrent Update or Revert cannot slip an untested revision into "active"
// between the caller's test-connection call and this call
// (concurrency-deadlock-rigor: same conflict domain, same lock order as
// Update/Revert — lock the provider config row, then touch it — so this
// cannot deadlock against them).
func (s *IdentitySubjectStore) EnableProviderConfig(
	ctx context.Context,
	enable ProviderConfigEnable,
) (ProviderConfigWriteResult, error) {
	if s.db == nil {
		return ProviderConfigWriteResult{}, errors.New("identity subject store database is required")
	}
	providerConfigID := strings.TrimSpace(enable.ProviderConfigID)
	tenantID := strings.TrimSpace(enable.TenantID)
	expectedRevisionID := strings.TrimSpace(enable.ExpectedActiveRevisionID)
	if providerConfigID == "" || tenantID == "" {
		return ProviderConfigWriteResult{}, errors.New("provider_config_id and tenant_id are required")
	}
	if expectedRevisionID == "" {
		return ProviderConfigWriteResult{}, errors.New("expected_active_revision_id is required to enable a provider config")
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, found, err := lockProviderConfig(ctx, tx, providerConfigID, tenantID)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	if !found {
		return ProviderConfigWriteResult{Found: false}, nil
	}
	if current.activeRevisionID != expectedRevisionID {
		return ProviderConfigWriteResult{}, ErrProviderConfigRevisionChanged
	}

	rows, err := tx.QueryContext(ctx, setProviderConfigStatusQuery, providerConfigID, tenantID, "active", enable.Now.UTC())
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("set provider config status: %w", err)
	}
	var status string
	scanned := rows.Next()
	if scanned {
		if err := rows.Scan(&status); err != nil {
			_ = rows.Close()
			return ProviderConfigWriteResult{}, fmt.Errorf("scan provider config status: %w", err)
		}
	}
	rowsErr := rows.Err()
	if err := rows.Close(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("close provider config status rows: %w", err)
	}
	if rowsErr != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("set provider config status: %w", rowsErr)
	}
	if !scanned {
		// The row-locked read above found it; a concurrent tombstone between
		// the lock and this UPDATE is the only way this branch is reached.
		return ProviderConfigWriteResult{Found: false}, nil
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit enable provider config: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: providerConfigID,
		Status:           status,
		Found:            true,
		Changed:          status == "active",
	}, nil
}

// DisableProviderConfig transitions a provider config from active back to
// draft. Idempotent.
func (s *IdentitySubjectStore) DisableProviderConfig(
	ctx context.Context,
	disable ProviderConfigDisable,
) (ProviderConfigWriteResult, error) {
	return s.setProviderConfigStatus(ctx, disable.ProviderConfigID, disable.TenantID, "draft", disable.Now)
}

// setProviderConfigStatus flips a provider config between draft and active
// (currently only DisableProviderConfig calls this; EnableProviderConfig has
// its own variant below because it also validates
// ExpectedActiveRevisionID). It runs the same row-lock-then-write sequence as
// Update/Revert/Enable (lockProviderConfig via selectProviderConfigForUpdateQuery
// ... FOR UPDATE, inside one local transaction), so a Disable racing an
// Enable/Update/Revert against the same provider_config_id serializes behind
// whichever acquires the row lock first, rather than running as a bare
// autocommit UPDATE outside that shared conflict domain
// (concurrency-deadlock-rigor: same conflict domain, same lock order as its
// siblings — lock the provider config row, then touch it).
func (s *IdentitySubjectStore) setProviderConfigStatus(
	ctx context.Context,
	providerConfigID, tenantID, targetStatus string,
	now time.Time,
) (ProviderConfigWriteResult, error) {
	if s.db == nil {
		return ProviderConfigWriteResult{}, errors.New("identity subject store database is required")
	}
	providerConfigID = strings.TrimSpace(providerConfigID)
	tenantID = strings.TrimSpace(tenantID)
	if providerConfigID == "" || tenantID == "" {
		return ProviderConfigWriteResult{}, errors.New("provider_config_id and tenant_id are required")
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	_, found, err := lockProviderConfig(ctx, tx, providerConfigID, tenantID)
	if err != nil {
		return ProviderConfigWriteResult{}, err
	}
	if !found {
		return ProviderConfigWriteResult{Found: false}, nil
	}

	rows, err := tx.QueryContext(ctx, setProviderConfigStatusQuery, providerConfigID, tenantID, targetStatus, now.UTC())
	if err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("set provider config status: %w", err)
	}
	var status string
	scanned := rows.Next()
	if scanned {
		if err := rows.Scan(&status); err != nil {
			_ = rows.Close()
			return ProviderConfigWriteResult{}, fmt.Errorf("scan provider config status: %w", err)
		}
	}
	rowsErr := rows.Err()
	if err := rows.Close(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("close provider config status rows: %w", err)
	}
	if rowsErr != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("set provider config status: %w", rowsErr)
	}
	if !scanned {
		// The row-locked read above found it; a concurrent tombstone between
		// the lock and this UPDATE is the only way this branch is reached.
		return ProviderConfigWriteResult{Found: false}, nil
	}

	if err := tx.Commit(); err != nil {
		return ProviderConfigWriteResult{}, fmt.Errorf("commit set provider config status: %w", err)
	}
	committed = true
	return ProviderConfigWriteResult{
		ProviderConfigID: providerConfigID,
		Status:           status,
		Found:            true,
		Changed:          status == targetStatus,
	}, nil
}
