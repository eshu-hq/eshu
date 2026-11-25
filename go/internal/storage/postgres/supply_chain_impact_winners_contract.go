// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The winners contract (#11117 A). A full resweep records, in the transaction
// that commits it, the contract version and hash of the statements it rebuilt
// with (migration 329). Every maintainer tick reads them back: no watermark
// row, a NULL or a lower version runs one guarded full across every instance;
// an equal version runs keyed ticks and the audit only; a higher version
// stands an older binary down. The persisted row replaces the per-process
// "full done" flag, so a reducer roll with an unchanged contract runs zero
// fulls.

// SupplyChainImpactWinnersContractVersion is this binary's winners contract
// version. Bump it when the winners a full or keyed resweep produces can
// change for the same facts: a change to the winner select, the rebuild, or a
// keyed build or apply statement, and a migration that rewrites impact facts or
// scope activation outside the capture owners (fact_upsert, fact_retract and
// the activation, operator and scanner triggers), since such a rewrite queues
// no dirty mark. Bump it also for a change to the capture owners, the drains
// or the staged audit that alters which keys get marked or visited: the pin
// test does not cover those statements. A bump makes every instance run one
// full after the roll. The
// pin test fails on any covered statement change until the version is bumped
// with its new hash pinned, or the hash is re-pinned under the same version
// with a differential proof that winner output is byte-identical (the
// supplyChainImpactWinnerDataColumns byte-parity rule).
const SupplyChainImpactWinnersContractVersion int64 = 2

// supplyChainImpactWinnersContractPins pins each contract version to the hash
// of the statements it names. A version maps to exactly one hash. Version 2
// was re-pinned without a bump when #11145 added the scope-remainder convert
// to the set and changed only the capture time a converted row carries: the
// stored winners are byte-identical for the same facts (differential in
// docs/internal/evidence/11145-scope-remainder-horizon.md, measured on the
// version 1 statements; version 2 rests on the scope-remainder live tests and
// on the convert writing only the dirty log). Version 1 keeps the hash of the
// statements it named.
var supplyChainImpactWinnersContractPins = map[int64]string{
	1: "c5731efdb688e71bf37154ff9401a973f588d686603b10e117ca5a8f0ea4dbff",
	// Version 2 (#10880 slice 1): the rebuild and keyed statements now
	// carry payload and source_confidence, so winners a resweep produces
	// change for the same facts. Every instance runs one full after
	// the roll; migration 332 backfills at migration time.
	// Re-pinned under the same version for #10839: the keyed active_facts
	// build precomputes the live generation set once per tick instead of
	// probing liveness per copy. Winner output is byte-identical for the
	// same facts: the hermetic fan-out parity test
	// (TestWinnersKeyedFanoutMatchesRecomputeLive) passes, and the QA
	// replica differential shows zero row differences both ways on two
	// pinned sets (13,260 and 24,936 rows); see
	// docs/internal/evidence/10839-live-gens-liveness-dedup.md. No full
	// resweep is triggered by this re-pin.
	// Re-pinned under the same version for the 2026-11-24 watermark-stall
	// fix: the five-tuple identifier probe persists to a narrow temp table
	// once per tick instead of running inside the active-facts build and
	// again for the scanned-copies count. Winner output is byte-identical
	// for the same facts by construction: the narrow table holds the probe
	// output verbatim (same text, same transaction and snapshot), the final
	// fetch joins the same live generations to the same copies, and the
	// winner differential is set-based (changed/deleted/unchanged counts),
	// so temp row order cannot move it. Hermetic proof:
	// TestKeyedTickScansIdentifierCopiesOnce (one probe pass, preserved
	// scanned signal) and TestWinnersKeyedFanoutMatchesRecomputeLive in the
	// coordinator's live slot. No full resweep is triggered by this re-pin.
	2: "25e4d8910be1951ac76980f97de64e5d5f9b483e9151a459236dc0761175747a",
}

// supplyChainImpactWinnersContractStatements returns, in a fixed order, the
// rendered statements whose text defines what a resweep writes: the full
// rebuild (which embeds the winner select), every keyed build and apply
// statement, and the scope-remainder convert (#11145), which decides which
// keys a later tick recomputes and the capture time the horizon reads.
func supplyChainImpactWinnersContractStatements() []string {
	stmts := []string{rebuildSupplyChainImpactWinnersSQL}
	stmts = append(stmts, winnersKeyedBuildIdentsStmts...)
	return append(stmts,
		selectWinnersKeyedChunkIdentsSQL,
		restrictWinnersKeyedIdentsToChunkSQL,
		buildWinnersKeyedNarrowSQL,
		analyzeWinnersKeyedNarrowSQL,
		buildWinnersKeyedActiveFactsSQL,
		addWinnersKeyedOperatorFactsSQL,
		applyWinnersKeyedSQL,
		convertWinnersKeyedScopeRemainderSQL,
	)
}

// supplyChainImpactWinnersContractHashOf is the hex SHA-256 of statements,
// each length-prefixed so no two statement lists share a digest.
func supplyChainImpactWinnersContractHashOf(statements []string) string {
	h := sha256.New()
	for _, stmt := range statements {
		_, _ = fmt.Fprintf(h, "%d:%s\n", len(stmt), stmt)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// supplyChainImpactWinnersContractHash is this binary's contract hash.
var supplyChainImpactWinnersContractHash = supplyChainImpactWinnersContractHashOf(supplyChainImpactWinnersContractStatements())

// verifyWinnersContractPin returns an error unless statements hash to the pin
// of version.
func verifyWinnersContractPin(version int64, statements []string, pins map[int64]string) error {
	pinned, ok := pins[version]
	if !ok {
		return fmt.Errorf("winners contract version %d has no pinned hash", version)
	}
	if got := supplyChainImpactWinnersContractHashOf(statements); got != pinned {
		return fmt.Errorf("winners contract version %d pins hash %s, statements hash to %s: bump the version or re-pin with a differential proof", version, pinned, got)
	}
	return nil
}

// selectSupplyChainImpactWinnersContractSQL reads the stored contract: the
// maintainer's per-tick decision read, and the guarded full's re-read under
// the resweep lock.
const selectSupplyChainImpactWinnersContractSQL = `
SELECT contract_version, contract_hash FROM supply_chain_impact_winners_materialization WHERE singleton = 1
`

// stampSupplyChainImpactWinnersContractSQL records the contract a full
// rebuilt with. It runs in the full's transaction after the rebuild, which has
// already upserted the singleton row.
const stampSupplyChainImpactWinnersContractSQL = `
UPDATE supply_chain_impact_winners_materialization
SET contract_version = $1, contract_hash = $2
WHERE singleton = 1
`

// WinnersContract reads the stored winners contract and reports this binary's.
func (s SupplyChainImpactWinnersStore) WinnersContract(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersContract, error) {
	if s.DB == nil {
		return supplychaincore.SupplyChainImpactWinnersContract{}, fmt.Errorf("supply chain impact winners database is required")
	}
	return readWinnersContract(ctx, s.DB)
}

// readWinnersContract reads the stored contract through q.
func readWinnersContract(ctx context.Context, q db.Queryer) (supplychaincore.SupplyChainImpactWinnersContract, error) {
	contract := supplychaincore.SupplyChainImpactWinnersContract{
		BinaryVersion: SupplyChainImpactWinnersContractVersion,
		BinaryHash:    supplyChainImpactWinnersContractHash,
	}
	rows, err := q.QueryContext(ctx, selectSupplyChainImpactWinnersContractSQL)
	if err != nil {
		return contract, fmt.Errorf("read supply chain impact winners contract: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		var version sql.NullInt64
		var hash sql.NullString
		if err := rows.Scan(&version, &hash); err != nil {
			return contract, fmt.Errorf("scan supply chain impact winners contract: %w", err)
		}
		contract.Present = true
		contract.HasVersion = version.Valid
		contract.StoredVersion = version.Int64
		contract.StoredHash = hash.String
	}
	if err := rows.Err(); err != nil {
		return contract, fmt.Errorf("read supply chain impact winners contract: %w", err)
	}
	return contract, nil
}

// stampWinnersContract records this binary's contract in the full's
// transaction.
func stampWinnersContract(ctx context.Context, tx db.Transaction) error {
	if _, err := tx.ExecContext(ctx, stampSupplyChainImpactWinnersContractSQL,
		SupplyChainImpactWinnersContractVersion, supplyChainImpactWinnersContractHash); err != nil {
		return fmt.Errorf("stamp supply chain impact winners contract: %w", err)
	}
	return nil
}

// ResweepWinnersUnlessCurrent runs the full resweep of ResweepWinners unless
// the stored contract version, read in the same transaction after the resweep
// lock is held, is already at or above this binary's. Under READ COMMITTED
// that read takes a snapshot after the lock, so it sees a full another
// instance committed while this one waited for the lease: the maintainer's
// per-tick decision read can be stale, this read cannot. Then it writes
// nothing and returns Skipped with ContractCurrent.
func (s SupplyChainImpactWinnersStore) ResweepWinnersUnlessCurrent(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	return s.resweepFull(ctx, true)
}

var _ supplychaincore.SupplyChainImpactWinnersContractStore = SupplyChainImpactWinnersStore{}
