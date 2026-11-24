// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// hasOutOfStampActiveSupplyChainImpactRowsQuery is the #10415 fallback
// trigger: one bounded existence probe for active finding supports of one
// generation whose CVE id the stamp does not name (#11197). It reads the
// per-support table through the (scope_id, generation_id) index. LIMIT 1 bounds
// the probe to one row: a quiet holder with no drift pays one index probe on
// its scoped path, and a full pass never runs it. Supports without a CVE id
// store the empty string, which the probe excludes: they are not CVE drift and
// the next genuine full pass converges them, while counting them would turn the
// fallback into every reopen for holders carrying them. No new index.
const hasOutOfStampActiveSupplyChainImpactRowsQuery = `
SELECT 1
FROM supply_chain_impact_finding_supports AS support
WHERE support.scope_id = $1
  AND support.generation_id = $2
  AND support.cve_id <> ''
  AND NOT (support.cve_id = ANY($3::text[]))
LIMIT 1
`

// HasOutOfStampActiveSupplyChainImpactRows reports whether one (scope,
// generation) holds an active finding row outside a stamped CVE set. An empty
// stamp reads nothing: the caller runs full without it. A retracted copy has
// no support, so it never triggers. The read refuses with
// supplychaincore.SupplyChainImpactSupportsNotReadyError while the supports table is not ready
// (#11197).
func (s FactStore) HasOutOfStampActiveSupplyChainImpactRows(
	ctx context.Context,
	scopeID string,
	generationID string,
	cveIDs []string,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("fact store database is required")
	}
	if len(cveIDs) == 0 {
		return false, nil
	}
	var has bool
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		if err := requireSupplyChainImpactSupportsReady(ctx, queryer, s.instruments, telemetry.SupplyChainImpactSupportsReaderOutOfStamp); err != nil {
			return err
		}
		rows, err := queryer.QueryContext(ctx, hasOutOfStampActiveSupplyChainImpactRowsQuery, scopeID, generationID, cveIDs)
		if err != nil {
			return fmt.Errorf("read out-of-stamp supply chain impact finding rows: %w", err)
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			has = true
		}
		return rows.Err()
	})
	if err != nil {
		return false, err
	}
	s.instruments.RecordSupplyChainImpactSupportsRead(ctx, telemetry.SupplyChainImpactSupportsReaderOutOfStamp, telemetry.SupplyChainImpactSupportsReadServed)
	return has, nil
}

// hasLinkedOutOfStampActiveSupplyChainImpactRowsQuery is the D2 exception
// probe (#10972): one bounded existence read for an active finding support of
// one generation whose CVE id the stamp does not name but whose deciding
// advisory id or advisory group members it does. Such a row is re-keyed by the
// stamp's own group change (a late alias merge, a split, a retired member,
// #10408), so its old key must be reaped by a full pass even under a fresh
// full-pass marker. It reads the same (scope_id, generation_id) index as the
// out-of-stamp probe, with the advisory and group-member arms the holder read
// already uses as residual filters, and LIMIT 1. A support with no CVE id
// counts here: a GHSA-only row is exactly what a late alias re-keys. No new
// index.
const hasLinkedOutOfStampActiveSupplyChainImpactRowsQuery = `
SELECT 1
FROM supply_chain_impact_finding_supports AS support
WHERE support.scope_id = $1
  AND support.generation_id = $2
  AND NOT (support.cve_id = ANY($3::text[]))
  AND (support.advisory_id = ANY($3::text[])
       OR support.payload #> '{provenance,advisory_group_members}' ?| $3::text[])
LIMIT 1
`

// HasLinkedOutOfStampActiveSupplyChainImpactRows reports whether one (scope,
// generation) holds an active finding row outside a stamp that the stamp still
// names through the row's advisory id or group members. An empty stamp reads
// nothing. The read refuses with
// supplychaincore.SupplyChainImpactSupportsNotReadyError while the supports
// table is not ready (#11197).
func (s FactStore) HasLinkedOutOfStampActiveSupplyChainImpactRows(
	ctx context.Context,
	scopeID string,
	generationID string,
	ids []string,
) (bool, error) {
	if s.database == nil {
		return false, fmt.Errorf("fact store database is required")
	}
	if len(ids) == 0 {
		return false, nil
	}
	var has bool
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		if err := requireSupplyChainImpactSupportsReady(ctx, queryer, s.instruments, telemetry.SupplyChainImpactSupportsReaderOutOfStamp); err != nil {
			return err
		}
		rows, err := queryer.QueryContext(ctx, hasLinkedOutOfStampActiveSupplyChainImpactRowsQuery, scopeID, generationID, ids)
		if err != nil {
			return fmt.Errorf("read linked out-of-stamp supply chain impact finding rows: %w", err)
		}
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			has = true
		}
		return rows.Err()
	})
	if err != nil {
		return false, err
	}
	s.instruments.RecordSupplyChainImpactSupportsRead(ctx, telemetry.SupplyChainImpactSupportsReaderOutOfStamp, telemetry.SupplyChainImpactSupportsReadServed)
	return has, nil
}
