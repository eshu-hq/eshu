// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"time"

	reducerderivedv1 "github.com/eshu-hq/eshu-sdk/factschema/reducerderived/v1"
)

// Keyed resweep of the #3389 canonical winners read model (#9805). The
// maintainer still runs one full resweep at startup. Every later tick consumes
// the winners dirty log and recomputes only the keys it names; a full-recompute
// audit runs beside the keyed ticks and queues any key the capture missed. See
// docs/internal/evidence/9805-winners-keyed-resweep.md.

// WinnersDirtyIdentifier names one canonical key of the winners read model by
// the five columns every candidate fact of that key shares: the package triple
// (package id, repository id, subject digest), the key's first component C0
// (CVE id, else advisory id, else empty) and the advisory id. They are the
// fields the keyed probe of the existing package-lookup index matches.
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
	// them under source fact_upsert at capture time.
	CaptureWinnersDirtyFacts(ctx context.Context, identifiers []WinnersDirtyIdentifier) (int64, error)
	// RecordWinnersCapture counts rows another statement of this writer
	// appended (the retract statement's own log insert) under source.
	RecordWinnersCapture(ctx context.Context, source string, rows int64)
}

// SupplyChainImpactWinnersKeyedRebuilder recomputes only the keys the dirty log
// names and stamps the watermark. The result carries Path "keyed", or "full"
// when the cap sent it to the full rebuild.
type SupplyChainImpactWinnersKeyedRebuilder interface {
	ResweepWinnersKeyed(ctx context.Context, materializedAt any) (SupplyChainImpactWinnersResweep, error)
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
	// ReadDuration is the wall time of the audit's full recompute read, which
	// runs outside the resweep lock.
	ReadDuration time.Duration
}

// SupplyChainImpactWinnersAuditor runs the full-recompute audit backstop.
type SupplyChainImpactWinnersAuditor interface {
	AuditWinners(ctx context.Context) (SupplyChainImpactWinnersAudit, error)
}

// SupplyChainImpactWinnersDirtyStats is the dirty log's depth and the age of
// its oldest row, sampled every tick.
type SupplyChainImpactWinnersDirtyStats struct {
	Depth     int64
	OldestAge time.Duration
}

// SupplyChainImpactWinnersDirtyLogReader reads the dirty log's depth and age.
type SupplyChainImpactWinnersDirtyLogReader interface {
	DirtyLogStats(ctx context.Context) (SupplyChainImpactWinnersDirtyStats, error)
}

// SupplyChainImpactWinnersAuditDomain names the audit's single-owner lease
// partition, separate from the resweep's so a long audit read never blocks a
// keyed tick's lease.
const SupplyChainImpactWinnersAuditDomain = "supply_chain_impact_winners_audit"

// DigestExcludedPayloadKeys lists the top-level payload keys the finding content
// digest leaves out, plus nothing else: the provenance key that carries the
// digest itself is the only nested exclusion and is handled by contentDigestOf.
//
// The #9805 capture filter skips a rewrite whose digest equals the stored one.
// That is sound only while no payload key the winners select reads is in this
// list; a guard in the postgres package reads the select and fails if one is.
func DigestExcludedPayloadKeys() []string {
	return []string{"intent_id", "cause", "generation_id", "evidence_fact_ids"}
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
// is about to write that are new or changed, in row order. A row is skipped only
// when the pass's own generation already holds that fact id with an equal,
// non-empty content digest: a rewrite of identical content leaves every input of
// the winners select unchanged (fact id, scope, generation and tombstone state
// are fixed by the fact id; every payload field the select reads is inside the
// digest). The prior generation is never consulted: a new generation mints new
// fact ids, so an unchanged finding is a changed winner row.
func dirtyIdentifiersToCapture(rows []changedRow, stored []storedFinding, generationID string) []WinnersDirtyIdentifier {
	own := make(map[string]storedFinding, len(stored))
	for _, row := range stored {
		if row.generationID == generationID {
			own[row.factID] = row
		}
	}
	seen := make(map[WinnersDirtyIdentifier]struct{}, len(rows))
	var out []WinnersDirtyIdentifier
	for _, row := range rows {
		if base, ok := own[row.factID]; ok && base.digest != "" && base.digest == row.digest {
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
