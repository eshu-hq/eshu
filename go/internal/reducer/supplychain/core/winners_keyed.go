// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"time"

	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
)

// Keyed resweep of the #3389 canonical winners read model (#9805). The
// maintainer decides every tick from the persisted winners contract (#11117):
// a missing, NULL or lower stored version runs one guarded full across all
// instances, an equal version runs keyed ticks, and a higher version stands the
// binary down. A keyed tick consumes the winners dirty log and recomputes only
// the keys it names; a full-recompute audit runs beside the keyed ticks and
// queues any key the capture missed. See (#9805).

// WinnersDirtyIdentifier names one canonical key of the winners read model by
// the five columns every candidate fact of that key shares: the package triple
// (package id, repository id, subject digest), the key's first component C0
// (CVE id, else advisory id, else empty) and the advisory id. They are the
// fields the keyed probe of the keyed winners index matches.
type WinnersDirtyIdentifier struct {
	PackageID     string
	RepositoryID  string
	SubjectDigest string
	C0            string
	AdvisoryID    string
}

// SupplyChainImpactWinnersCapturer is the optional surface a write transaction
// offers for appending dirty-log identifiers in the transaction that writes the
// findings. postgres.SupplyChainImpactBeginner's transaction implements it. A
// transaction without it captures nothing, which is only valid in a test: the
// audit repairs a missed capture in production.
type SupplyChainImpactWinnersCapturer interface {
	// CaptureWinnersDirtyFacts appends one fact-kind dirty-log row per
	// identifier and returns the rows appended. The implementation also counts
	// them under source fact_upsert, once the transaction commits.
	CaptureWinnersDirtyFacts(ctx context.Context, identifiers []WinnersDirtyIdentifier) (int64, error)
	// RecordWinnersCapture counts rows another statement of this writer
	// appended (the retract statement's own log insert) under source, once the
	// transaction commits. A rollback discards the count.
	RecordWinnersCapture(ctx context.Context, source string, rows int64)
}

// SupplyChainImpactWinnersKeyedRebuilder recomputes only the keys the dirty log
// names and stamps the watermark. The result carries Path "keyed", or "full"
// when the cap sent it to the full rebuild. The tick stamp is sourced from
// the DB clock inside the tick transaction (#10706), never from the caller,
// so no stamp is passed in; the result reports the watermark the tick
// committed.
type SupplyChainImpactWinnersKeyedRebuilder interface {
	ResweepWinnersKeyed(ctx context.Context) (SupplyChainImpactWinnersResweep, error)
}

// SupplyChainImpactWinnersAudit is what one audit pass found.
type SupplyChainImpactWinnersAudit struct {
	// Repairs is the number of stale keys the audit queued for the next keyed
	// tick: keys whose winner row differs from a fresh recompute and were not
	// already waiting in the dirty log. It must be zero in steady state.
	Repairs int64
	// Skipped is true when the audit could not take the resweep lock for its
	// diff step; it did nothing and the next interval retries.
	Skipped bool
	// Deferred is true when the audit's candidate scan was refused by the
	// stale reader (replay budget exceeded or routing off the scan): the tick
	// ended with no writer contact and queued nothing. It is distinct from
	// Skipped (the lock was never attempted) and from error (nothing failed).
	// The next interval retries.
	Deferred bool
	// ReadDuration is the wall time of the audit's full recompute read, which
	// runs outside the resweep lock.
	ReadDuration time.Duration
	// Partial is true when the pass covered one range of a sweep that has not
	// completed (#11131): it is not a completed audit, so it records no audit
	// commit and does not reset the last-success age.
	Partial bool
	// More is true when the auditor has more ranges to run now: the
	// maintainer calls it again in the same tick while the tick budget and
	// the audit lease last.
	More bool
	// Outcome names a range pass's result for the range span and duration
	// histogram (advanced, completed, skipped, lock_busy, cursor_race,
	// deferred); empty for a whole-table audit.
	Outcome string
}

// SupplyChainImpactWinnersAuditor runs the full-recompute audit backstop.
type SupplyChainImpactWinnersAuditor interface {
	AuditWinners(ctx context.Context) (SupplyChainImpactWinnersAudit, error)
}

// SupplyChainImpactWinnersDirtyStats is the dirty log's depth and the age of
// its oldest row, sampled every tick. Depth unions the log and the staging
// queue (#10837 direction C); StagingDepth and StagingOldestAge carry the
// staging queue's own backlog for its gauges. StagingDepthUpperBound is true
// when StagingDepth, and so Depth, is an upper bound: the staging backlog
// passed the reader's exact-count cap and the reader reported the visible
// seq span instead of walking the whole queue (#11129). The ages stay exact.
type SupplyChainImpactWinnersDirtyStats struct {
	Depth                  int64
	OldestAge              time.Duration
	StagingDepth           int64
	StagingOldestAge       time.Duration
	StagingDepthUpperBound bool
}

// SupplyChainImpactWinnersDirtyLogReader reads the dirty log's depth and age.
type SupplyChainImpactWinnersDirtyLogReader interface {
	DirtyLogStats(ctx context.Context) (SupplyChainImpactWinnersDirtyStats, error)
}

// SupplyChainImpactWinnersContract is the winners contract a committed full
// resweep recorded with the watermark, beside the contract this binary runs
// (#11117). The version names the rebuild and keyed statements' semantics; the
// hash is the digest of their rendered text, pinned to the version by a test.
type SupplyChainImpactWinnersContract struct {
	// Present is false when the watermark row does not exist: no resweep has
	// ever committed.
	Present bool
	// HasVersion is false when the row exists but no full recorded a contract
	// (a NULL version: a pre-#11117 binary, or a rollback runbook reset).
	HasVersion bool
	// StoredVersion and StoredHash are what the last committed full recorded.
	StoredVersion int64
	StoredHash    string
	// BinaryVersion and BinaryHash are this binary's contract.
	BinaryVersion int64
	BinaryHash    string
}

// SupplyChainImpactWinnersContractStore reads the persisted winners contract
// and runs the full resweep only while that contract is behind this binary's.
// Implemented by postgres.SupplyChainImpactWinnersStore.
type SupplyChainImpactWinnersContractStore interface {
	// WinnersContract reads the stored contract and reports this binary's.
	WinnersContract(ctx context.Context) (SupplyChainImpactWinnersContract, error)
	// ResweepWinnersUnlessCurrent runs the full resweep, recording this
	// binary's contract in its transaction, unless the stored version, read
	// under the resweep lock, is already at or above this binary's. Then it
	// writes nothing and returns Skipped with ContractCurrent set.
	ResweepWinnersUnlessCurrent(ctx context.Context) (SupplyChainImpactWinnersResweep, error)
	// ResweepWinnersUnlessNewer runs the full resweep, recording this binary's
	// contract in its transaction, unless the stored version, read under the
	// resweep lock, is above this binary's. Then it writes nothing and returns
	// Skipped with StoodDown set. A current contract still runs: the
	// supports-ready full (#10880 PR-C1) rebuilds the same contract from the
	// supports table.
	ResweepWinnersUnlessNewer(ctx context.Context) (SupplyChainImpactWinnersResweep, error)
}

// SupplyChainImpactWinnersAuditDomain names the audit's single-owner lease
// partition, separate from the resweep's so a long audit read never blocks a
// keyed tick's lease.
const SupplyChainImpactWinnersAuditDomain = "supply_chain_impact_winners_audit"

// DigestExcludedPayloadKeys lists the payload keys the finding content digest
// leaves out: the top-level volatile keys, plus the nested provenance entry
// carrying the selected EPSS score date (a rescan re-dates an unchanged score,
// so the date is digest-excluded while the score stays covered). The
// provenance keys that carry the digests themselves are the other nested
// exclusions and are handled by contentDigestOf.
//
// The #9805 capture filter skips a rewrite whose digest equals the stored one.
// That is sound only while no payload key the winners select reads is in this
// list; a guard in the postgres package reads the select and fails if one is.
// Since #10880 the stored winner payload carries evidence_fact_ids, the one
// top-level served key in this list: the select strips the volatile keys
// before storing. Under the #11598 Option C arbiter amendment to #10880
// Decision 4, neither evidence_fact_ids nor the nested provenance score date is
// secondary-compared at capture: both accept staleness on canonical_winners
// across content-identical rescans until the next semantic change while
// supports.payload stays fresh for Explain and AuditWinners masks both fields
// so steady-state Audits keep Repairs=0.
func DigestExcludedPayloadKeys() []string {
	return []string{"intent_id", "cause", "generation_id", "evidence_fact_ids", selectedEPSSScoreDateProvenanceKey}
}

// dirtyIdentifierOf derives the dirty-log identifier of a finding from the
// typed payload the writer persists, using the same empty-string and first-key
// component rules as the winners select.
func dirtyIdentifierOf(payload reducerderivedv1.SupplyChainImpactFinding) WinnersDirtyIdentifier {
	c0 := payload.CVEID
	if c0 == "" {
		c0 = payload.AdvisoryID
	}
	return WinnersDirtyIdentifier{
		PackageID:     payload.PackageID,
		RepositoryID:  payload.RepositoryID,
		SubjectDigest: payload.SubjectDigest,
		C0:            c0,
		AdvisoryID:    payload.AdvisoryID,
	}
}

// dirtyIdentifiersToCapture returns the distinct identifiers of the rows a pass
// is about to write that are new or changed, in row order. A rerun of a
// generation that already has rows of its own is judged against them, matched
// by fact id: a row is skipped when the pass's own generation already holds
// that fact id with an equal, non-empty content digest. A rewrite of identical
// content leaves every semantic input of the winners select unchanged (fact id,
// scope, generation and tombstone state are fixed by the fact id; every
// semantic payload field is inside the digest). Under the #11598 Option C
// arbiter amendment to #10880 Decision 4, evidence_fact_ids is no longer
// secondary-compared here: daily enrichment rescans mint new fact_ids for
// unchanged scores, and staging winner recaptures on evidence_fact_ids alone
// amplified re-marks across duplicate copies. supports.payload stays fresh for
// Explain while winners.payload accepts staleness on evidence_fact_ids and
// provenance.selected_epss_score_date until the next semantic change;
// AuditWinners masks those two provenance fields so steady-state Audits keep
// Repairs=0. The first pass of a generation has no rows of its own, so it is
// judged against the scope's previous generation, matched by stable key
// (#11414): identical content re-proves an already-computed winner and stages
// nothing, instead of re-staging the whole window on every re-match. The
// baseline switch mirrors changedCVEIDs: once the generation has rows of its
// own they are the baseline and the previous generation is not consulted.
func dirtyIdentifiersToCapture(rows []changedRow, stored []storedFinding, generationID string) []WinnersDirtyIdentifier {
	own := make(map[string]storedFinding, len(stored))
	prior := make(map[string]storedFinding, len(stored))
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		} else {
			prior[row.stableKey] = row
		}
	}
	rerun := len(own) > 0
	seen := make(map[WinnersDirtyIdentifier]struct{}, len(rows))
	var out []WinnersDirtyIdentifier
	for _, row := range rows {
		if rerun {
			if base, ok := own[row.factID]; ok && base.digest != "" && base.digest == row.digest {
				continue
			}
		} else if base, ok := prior[row.stableKey]; ok && base.digest != "" && base.digest == row.digest {
			continue
		}
		if _, dup := seen[row.ident]; dup {
			continue
		}
		seen[row.ident] = struct{}{}
		out = append(out, row.ident)
	}
	return out
}

// excludeRetiredWinnersDirtyIdentifiers drops the identifiers this pass's own
// retraction stages (#11411): one pass stages one dirty row per key. The
// retired set mirrors the retraction's tombstone predicate exactly
// (retractSupersededSupplyChainImpactFindingsQuery and its scoped twin: same
// generation, fact id outside the pass's keep set, stored fencing token at or
// below the pass's, and, on a scoped pass, a cve id inside the stamp), so a
// key the capture drops is always staged by the retraction in the same
// transaction. A partial-evidence pass retracts nothing, so it excludes
// nothing: the flag must match the retraction gate in
// commitSupplyChainImpactRows.
func excludeRetiredWinnersDirtyIdentifiers(idents []WinnersDirtyIdentifier, stored []storedFinding, keep []string, generationID string, fencingToken int64, partialEvidence bool, scopedCVEIDs []string) []WinnersDirtyIdentifier {
	if len(idents) == 0 || partialEvidence {
		return idents
	}
	kept := make(map[string]struct{}, len(keep))
	for _, id := range keep {
		kept[id] = struct{}{}
	}
	var scoped map[string]struct{}
	if len(scopedCVEIDs) > 0 {
		scoped = make(map[string]struct{}, len(scopedCVEIDs))
		for _, id := range scopedCVEIDs {
			scoped[id] = struct{}{}
		}
	}
	retired := make(map[WinnersDirtyIdentifier]struct{})
	for _, row := range stored {
		if row.generationID != generationID {
			continue
		}
		if _, ok := kept[row.factID]; ok {
			continue
		}
		if row.fencingToken > fencingToken {
			continue
		}
		if scoped != nil {
			if _, ok := scoped[row.cveID]; !ok {
				continue
			}
		}
		retired[storedWinnersDirtyIdentifier(row)] = struct{}{}
	}
	if len(retired) == 0 {
		return idents
	}
	out := make([]WinnersDirtyIdentifier, 0, len(idents))
	for _, ident := range idents {
		if _, ok := retired[ident]; ok {
			continue
		}
		out = append(out, ident)
	}
	return out
}

// storedWinnersDirtyIdentifier names the canonical winners key of a stored
// finding row with the same empty-string and first-component rules as
// dirtyIdentifierOf, so the upsert capture and the retraction stage the same
// key for the same row (#11411).
func storedWinnersDirtyIdentifier(row storedFinding) WinnersDirtyIdentifier {
	c0 := row.cveID
	if c0 == "" {
		c0 = row.advisoryID
	}
	return WinnersDirtyIdentifier{
		PackageID:     row.packageID,
		RepositoryID:  row.repositoryID,
		SubjectDigest: row.subjectDigest,
		C0:            c0,
		AdvisoryID:    row.advisoryID,
	}
}
