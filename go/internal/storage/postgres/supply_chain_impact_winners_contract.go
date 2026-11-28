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
const SupplyChainImpactWinnersContractVersion int64 = 5

// supplyChainImpactWinnersContractPins pins each contract version to the hash
// of the statements it names. A version maps to exactly one hash. Version 2
// was re-pinned without a bump when #11145 added the scope-remainder convert
// to the set and changed only the capture time a converted row carries: the
// stored winners are byte-identical for the same facts (differential in
// #11145 (retired evidence record), measured on the
// version 1 statements; version 2 rests on the scope-remainder live tests and
// on the convert writing only the dirty log). Version 1 keeps the hash of the
// statements it named.
var supplyChainImpactWinnersContractPins = map[int64]string{
	1: "c5731efdb688e71bf37154ff9401a973f588d686603b10e117ca5a8f0ea4dbff",
	// Version 2 (#10880 slice 1): the rebuild and keyed statements now
	// carry payload and source_confidence, so winners a resweep produces
	// change for the same facts. Every instance runs one full after
	// the roll; migration 332 backfills at migration time.
	// Re-pinned under the same version for #11598: the winner select
	// projection strips a top-level selected_epss_score_date that real
	// payloads never carry (the date lives nested in provenance, which
	// the projection keeps). Winner output is byte-identical for the
	// same facts: the hermetic reversal (reverseWinnersEPSSDateStrip,
	// chained in TestWinnersContractRepinnedForSupportsInputs back to
	// the 25e4d891 pin) proves the shipped text differs from the legacy
	// text only by that strip, and the volatile live test rewrites every
	// DigestExcludedPayloadKeys entry top-level and proves no winner is
	// written. No full resweep is triggered by this re-pin.
	// Re-pinned under the same version for #10839: the keyed active_facts
	// build precomputes the live generation set once per tick instead of
	// probing liveness per copy. Winner output is byte-identical for the
	// same facts: the hermetic fan-out parity test
	// (TestWinnersKeyedFanoutMatchesRecomputeLive) passes, and the QA
	// replica differential shows zero row differences both ways on two
	// pinned sets (13,260 and 24,936 rows); see
	// #10839 (retired evidence record). No full
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
	//
	// Version 2 was re-pinned again, without a bump, when #10880 PR-C1 d moved the
	// maintainer's inputs (the full select, the keyed narrow probe, the keyed
	// final fetch and the operator statements) from fact_records to the supports
	// table.
	// TestWinnersContractRepinnedForSupportsInputs shows the shipped statements
	// reduce, by reversing the declared swaps alone, to the statements the previous
	// version-2 hash (25e4d891...) pinned; a live byte-identity differential of the
	// stored winners on a seed is owed with the live proofs.
	//
	// Re-pinned under the same version for #11120 single-detoast extraction:
	// the candidate CTEs extract every text field once through a guarded
	// LATERAL jsonb_to_record and the final projection reads the carried
	// columns. Winner output is byte-identical for the same facts: the
	// hermetic reversal (TestWinnersDetoastReversesToLegacy, chained in
	// TestWinnersContractRepinnedForSupportsInputs back to the 25e4d891 pin)
	// proves the shipped text differs from the legacy text only by the field
	// pairs, and TestSupplyChainImpactWinnersDetoastFieldEquivalenceLive proves every pair
	// equivalent on PG18 over edge-case payloads; see
	// #11120 (retired evidence record). No full resweep is
	// triggered by this re-pin.
	//
	// Re-pinned under the same version for #9805 C5 / #9975 C4 CTE-streaming
	// keyed body build: the stage-1 temp holds the copy identity plus the
	// once-extracted detoast fields without the payload, and the keyed body
	// streams through CTEs, fetching the full payload by fact_id only for
	// the emitted winner and override rows. Winner output is byte-identical
	// for the same facts: the hermetic reversal
	// (TestWinnersKeyedStreamBodyReversesToShipped) proves the streamed body
	// differs from the shipped body only by the declared transform, and the
	// live differential
	// (TestSupplyChainImpactWinnersKeyedStreamBodyMatchesLegacySelectLive,
	// EXCEPT ALL both directions over winner choice, source_count, evidence
	// ids, jsonb-null array semantics, every cvss bucket, operator facts, and
	// the assoc/class joins, plus
	// TestSupplyChainImpactWinnersKeyedStreamScalarRowParityLive for the
	// non-object payload row) proves the stored winners match. The
	// buffer-budget live test
	// (TestSupplyChainImpactWinnersKeyedStreamStaysWithinBufferBudgetLive)
	// pins the narrow temp at half the wide temp's bytes and the late fetch
	// at a fact_id point lookup. No full resweep is triggered by this
	// re-pin.
	2: "8f94f0f67e4eaa5f07f606192301dc7c5e2343230afbf1c1b829665386fb3bc1",
	// Version 3 (#9975 round-6 triple-guard probe): the keyed narrow probe
	// gains a triple-guarded plain-column twin
	// (buildWinnersKeyedNarrowKeyPresenceSQL) alongside the shipped
	// payload-expression build it falls back to before the key-presence fill
	// marker completes. Winner output is byte-identical for the same facts:
	// the live differential
	// (TestSupplyChainImpactWinnersNarrowTripleGuardProbeMatchesShippedLive, EXCEPT both
	// directions over every advisory-absent and triple edge class) proves the
	// twin returns the same copies, and the audit keeps the legacy statement
	// as its oracle. Every instance runs one full after the roll, per the
	// #11117 model, which also heals any winner a pre-marker tick wrote.
	//
	// Re-pinned under the same version for the ruled #9805 v3-full fix: the
	// single-statement supports rebuild becomes bounded keyed chunks over
	// fact_records. Winner output is byte-identical for the same facts, by
	// construction and hermetic proof, so no full resweep is triggered by
	// this re-pin:
	//   - the enumeration pages the shipped active-facts inner select, the
	//     exact legacy candidate set the audit runs;
	//   - the chunk select is the shipped body over the chunk temp
	//     (TestWinnersFullChunkSelectDerivesFromShippedBody), and the
	//     streamed body reverses byte-for-byte to the shipped body
	//     (TestWinnersFullChunkStreamBodyReversesToShipped), so rank and
	//     final projection match the legacy select row for row;
	//   - the chunk apply is the keyed apply with only the select swapped
	//     (the derivation panics on any other drift), keeping the same
	//     changed/upserted/deleted shape, and the chunk deletes tile the
	//     enumeration, so the union is the full delete;
	//   - the chunk narrow builds are narrow() of the shipped wide builds,
	//     the exact legacy base the pinned supports twins commute with
	//     (TestWinnersKeyedNarrowingCommutesWithSupportsSwaps).
	// The shipped-environment live differential of the chunked full against
	// the legacy select is owed with the live proofs.
	3: "2bff09ee3c0d3b9e667f9f096dca497ed95d1a75a2835e95323498f6828b56b2",
	// Version 4 (ruled #9805 stored-arm fix): the full enumeration unions the
	// live candidate set with the stored winners' identifiers
	// (winnersFullStoredIdentsSQL, derived from winnersStoredIdentSelectList,
	// which shares winnersStoredIdentC0 with the delete match). Winners a
	// resweep produces change for the same facts exactly where they were
	// wrong before: a stored-only key (tombstoned facts, a rotated canonical
	// key, a row left by a resweep to zero) is now visited and deleted, so
	// the stored table converges to the legacy select. Every instance runs
	// one full after the roll, which also heals any dead key an older binary
	// left behind. Differential justification, measured on the seeded
	// backend (TestSupplyChainImpactWinnersFullChunkedMatchesLegacyLive):
	// phases 1-2 agree with the legacy select EXCEPT ALL 0 in both directions
	// with equal row counts (live keys byte-identical: ranking, projection
	// and write shape untouched), phase 3 deletes exactly the tombstoned and
	// rotation-dropped keys (Deleted 2, Inserted 1) with EXCEPT ALL 0/0 and
	// stored == legacy == 8, and phase 4 deletes all 8 remaining winners on
	// a resweep to zero with the table and the legacy select both empty.
	// The UnlessCurrent healing proof (stored v3 runs the full, stored v4
	// skips) rides the same live run.
	4: "2e8903a9f694893f3caf8bdb1ec8bce8afd705c92974dc12e567cd371a2dfc40",
	// Version 5 (ruled #9805 round-5 row-source fix): every chunked-full
	// statement reads the supports table the way the shipped keyed path
	// does — the supports-swapped identifier enumeration, probe pair,
	// active-facts and operator builds, and the supports payload table
	// for the streamed winner and override fetches. Winners a resweep
	// produces change for the same facts exactly where they were wrong
	// before: on supports-only data the fact-sourced full rebuilt zero
	// winners and the stored arm emptied the table; the re-sourced full
	// ranks the verbatim supports payloads, so the stored table converges
	// to the supports oracle. Every instance runs one full after the roll,
	// which also heals any table a v4 binary emptied. Differential
	// justification, measured on seeded backends
	// (TestSupplyChainImpactWinnersFullChunkedMatchesLegacyLive phases 1-4
	// against the legacy and supports oracles, and
	// TestSupplyChainImpactWinnersFullChunkedMatchesSupportsWriterLive
	// seeded end to end through the production writer with no finding rows
	// in fact_records): phases agree with the supports oracle EXCEPT ALL 0
	// in both directions with equal row counts (payload-derived ranking,
	// projection and write shape untouched), and the writer-seeded phase
	// proves the legacy fact select reads nothing on that data. The
	// hermetic pins carry the row source per statement
	// (TestWinnersFullChunkedReadsPayloadDerivedRanking,
	// TestWinnersMaintainerStatementsReadNoFactRecords with the v3-full
	// exemption removed).
	5: "2a3dc4a2b1c250aca2efa17f4d5489478d7a108f66fcad57b287ec51f91ab736",
}

// supplyChainImpactWinnersContractStatements returns, in a fixed order, the
// rendered statements whose text defines what a resweep writes to the
// winners tables: the full-chunk enumeration, probe, rank and apply
// statements (which embed the winner select), every keyed build and apply
// statement, and the scope-remainder convert (#11145), which decides which
// keys a later tick recomputes and the capture time the horizon reads. The
// enrichment first-carry stamps (#11466) are intentionally outside this pin:
// they land in a side table, not in winner content, and the live carry tests
// cover them instead.
func supplyChainImpactWinnersContractStatements() []string {
	stmts := winnersFullChunkContractStatements()
	stmts = append(stmts, winnersKeyedBuildIdentsStmts...)
	return append(stmts,
		selectWinnersKeyedChunkIdentsSQL,
		restrictWinnersKeyedIdentsToChunkSQL,
		buildWinnersKeyedNarrowFromSupportsSQL,
		buildWinnersKeyedNarrowKeyPresenceSQL,
		analyzeWinnersKeyedNarrowSQL,
		buildWinnersKeyedActiveFactsNarrowFromSupportsSQL,
		addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
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
	return s.resweepFull(ctx, winnersFullUnlessCurrent)
}

// ResweepWinnersUnlessNewer runs the full resweep of ResweepWinners unless the
// stored contract version, read in the same transaction after the resweep lock
// is held, is above this binary's. Then it writes nothing and returns Skipped
// with StoodDown, as the keyed tick does. A current contract still runs: this is
// the supports-ready full (#10880 PR-C1), which rebuilds the current contract
// from the supports table, and it must not stamp this binary's older contract
// over a newer binary's that committed after the maintainer's decision read.
func (s SupplyChainImpactWinnersStore) ResweepWinnersUnlessNewer(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersResweep, error) {
	return s.resweepFull(ctx, winnersFullUnlessNewer)
}

// winnersFullGuard is what the full's contract re-read under the resweep lock
// stops on.
type winnersFullGuard int

const (
	// winnersFullUnguarded reads no contract (ResweepWinners).
	winnersFullUnguarded winnersFullGuard = iota
	// winnersFullUnlessCurrent stops on a stored version at or above the
	// binary's (ResweepWinnersUnlessCurrent, #11117).
	winnersFullUnlessCurrent
	// winnersFullUnlessNewer stops on a stored version above the binary's
	// (ResweepWinnersUnlessNewer, #10880 PR-C1).
	winnersFullUnlessNewer
)

// stopsOn reports the result the full returns without writing, and true, when
// the stored contract stops it under this guard.
func (g winnersFullGuard) stopsOn(stored supplychaincore.SupplyChainImpactWinnersContract) (supplychaincore.SupplyChainImpactWinnersResweep, bool) {
	if !stored.HasVersion {
		return supplychaincore.SupplyChainImpactWinnersResweep{}, false
	}
	switch {
	case g == winnersFullUnlessCurrent && stored.StoredVersion >= stored.BinaryVersion:
		return supplychaincore.SupplyChainImpactWinnersResweep{Skipped: true, ContractCurrent: true}, true
	case g == winnersFullUnlessNewer && stored.StoredVersion > stored.BinaryVersion:
		return supplychaincore.SupplyChainImpactWinnersResweep{Skipped: true, StoodDown: true}, true
	}
	return supplychaincore.SupplyChainImpactWinnersResweep{}, false
}

var _ supplychaincore.SupplyChainImpactWinnersContractStore = SupplyChainImpactWinnersStore{}
