// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The supports contract (#10880 PR-B1). It is separate from the winners
// contract: supports feed no winners until PR-C, and a winners bump would run a
// full resweep on every instance for no change in winner output.

// SupplyChainImpactSupportsContractVersion is this binary's supports contract
// version, and the suffix of the backfill marker. Bump it only when the support
// rows for the same facts change: a change to the columns the upsert derives, or
// to the statements that decide which supports exist (the retraction delete, the
// backfill chunk). A bump creates a new marker, so the backfill runs again from
// the start, and every read that needs the supports table answers 503 until it
// finishes. The pin test fails on any covered statement change until the version
// is bumped with its new hash pinned.
const SupplyChainImpactSupportsContractVersion = 1

// supplyChainImpactSupportsWriterSetting is the transaction setting the
// maintainer's writer carries. The fence trigger on fact_records (migration 336)
// skips rows written under it; every other writer of finding facts marks its
// scope dirty.
const (
	supplyChainImpactSupportsWriterSettingName  = "eshu.supply_chain_impact_supports_writer"
	supplyChainImpactSupportsWriterSettingValue = "supports_v1"
)

// setSupplyChainImpactSupportsWriterSQL sets the writer setting for the current
// transaction (is_local = true), the same pattern as migration 139.
const setSupplyChainImpactSupportsWriterSQL = `SELECT set_config('` + supplyChainImpactSupportsWriterSettingName + `', '` + supplyChainImpactSupportsWriterSettingValue + `', true)`

// supplyChainImpactSupportsContractPins pins each supports contract version to
// the hash of the statements it names. A version maps to exactly one hash.
var supplyChainImpactSupportsContractPins = map[int]string{
	1: "3f720ba20fc6f6ef850cd00b72d36992bd7357291b1f4c6b77cfa442865a6184",
}

// supplyChainImpactSupportsContractStatements returns, in a fixed order, the
// statements whose text decides which supports exist and what they hold: the
// writer's upsert (which also writes operator-scope rows, since the operator
// pass runs through the same writer), the retraction delete, the backfill
// chunk read, the marker completion with its fence recheck, the readiness
// predicate, and the writer setting the fence reads.
func supplyChainImpactSupportsContractStatements() []string {
	return []string{
		upsertSupplyChainImpactSupportsSQL,
		deleteRetractedSupplyChainImpactSupportsSQL,
		supplyChainImpactSupportsBackfillChunkSQL,
		supplyChainImpactSupportsBackfillCompleteSQL,
		supplyChainImpactSupportsReadySQL,
		setSupplyChainImpactSupportsWriterSQL,
	}
}

// supplyChainImpactSupportsContractHashOf is the hex SHA-256 of statements, each
// length-prefixed so no two statement lists share a digest.
func supplyChainImpactSupportsContractHashOf(statements []string) string {
	h := sha256.New()
	for _, stmt := range statements {
		_, _ = fmt.Fprintf(h, "%d:%s\n", len(stmt), stmt)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// verifySupportsContractPin returns an error unless statements hash to the pin
// of version.
func verifySupportsContractPin(version int, statements []string, pins map[int]string) error {
	pinned, ok := pins[version]
	if !ok {
		return fmt.Errorf("supports contract version %d has no pinned hash", version)
	}
	if got := supplyChainImpactSupportsContractHashOf(statements); got != pinned {
		return fmt.Errorf("supports contract version %d pins hash %s, statements hash to %s: bump the version or re-pin with a differential proof", version, pinned, got)
	}
	return nil
}

// BeginSupplyChainImpactTx opens a transaction wrapped in the reducer's
// supply-chain impact writer surface. It first sets the fence setting for the
// transaction, so the fact rows the writer writes never mark a scope dirty. The
// method lives beside the supports contract it serves, keeping the winners store
// file under its line cap.
func (b SupplyChainImpactBeginner) BeginSupplyChainImpactTx(
	ctx context.Context,
) (supplychaincore.SupplyChainImpactTx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, setSupplyChainImpactSupportsWriterSQL); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set supply chain impact supports writer setting: %w", err)
	}
	return &supplyChainImpactTx{tx: tx, instruments: b.Instruments}, nil
}
