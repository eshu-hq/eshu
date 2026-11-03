// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"math"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Truncation causes reported by the evidence load (#7154). The set is closed
// and low cardinality on purpose: each value is a metric attribute.
const (
	// supplyChainImpactTruncationRounds means the active-evidence expansion
	// stopped at maxSupplyChainImpactActiveEvidenceLoads rounds, so evidence
	// reachable only through later rounds was never loaded.
	supplyChainImpactTruncationRounds = telemetry.SupplyChainImpactPartialCauseRounds
	// supplyChainImpactTruncationBudget means the per-intent evidence budget
	// (maxSupplyChainImpactEvidenceEnvelopesPerIntent) was spent, so later
	// expansion stages were skipped or stopped early.
	supplyChainImpactTruncationBudget = telemetry.SupplyChainImpactPartialCauseBudget
)

// supplyChainImpactTruncation records why one pass's bounded evidence load
// stopped short. The causes are kept apart because they do not all threaten
// the finding set (#7154).
//
// A finding's identity is built from its CVE, package, version, status,
// repository and digest. rounds and budget can leave evidence unloaded that
// changes those fields, so a pass truncated for either reason cannot tell a
// superseded finding row from one it merely did not reach: it must not
// retract. The suppression tail is different. A suppression is not part of a
// finding's identity, and core evidence for a call is always loaded in full
// before the tail is counted (see maxSupplyChainImpactActiveEvidenceRowsPerCall
// in internal/storage/postgres), so a tail truncation only makes the pass
// discard its suppression candidates and fail open.
type supplyChainImpactTruncation struct {
	rounds          bool
	budget          bool
	suppressionTail bool
}

// partial reports whether the finding set may be incomplete, which is the
// writer's PartialEvidence contract: upsert only, retract nothing.
func (t supplyChainImpactTruncation) partial() bool {
	return t.rounds || t.budget
}

// any reports whether any bounded load stopped short for any cause.
func (t supplyChainImpactTruncation) any() bool {
	return t.rounds || t.budget || t.suppressionTail
}

// causes lists the identity-affecting causes for the operator counter and log.
func (t supplyChainImpactTruncation) causes() []string {
	var causes []string
	if t.rounds {
		causes = append(causes, supplyChainImpactTruncationRounds)
	}
	if t.budget {
		causes = append(causes, supplyChainImpactTruncationBudget)
	}
	return causes
}

// maxSupplyChainImpactEvidenceEnvelopesPerIntent bounds the expansion evidence
// one intent may hold in memory: the envelopes the active-evidence,
// OS-package, scanner-analysis, resolved-digest and peer-identity stages add
// beyond the intent scope's own base load. Every per-key cap those stages used
// to carry is now paged to completion (#7154), so this budget is the one
// remaining valve against an unbounded read. It counts expansion evidence
// only: the base scope load was never capped, and a large vulnerability scope
// must not be marked partial for its own size.
//
// The default is a documented permanent constraint, not a tuning guess: the
// evidence note docs/internal/evidence/7154-capped-scope-convergence.md holds
// the measured bytes per envelope and the resulting memory bound. A var, not a
// const, so tests can lower it without seeding a hundred thousand rows.
var maxSupplyChainImpactEvidenceEnvelopesPerIntent = 100_000

// maxSupplyChainImpactEvidencePayloadBytesPerIntent bounds the raw JSON bytes
// of the envelopes one pass keeps decoded (#9781). The envelope count above
// cannot bound memory on its own: a parser file envelope is about 70 times a
// scanner or OS-package envelope. Decoded `map[string]any` costs about three to
// five times the raw JSON (QA file facts: about 3x; the synthetic shim: 4.65x),
// so this default holds a pass to roughly 0.8 to 1.3 GiB of decoded heap, and
// eight concurrent passes to about a third to a half of the 24 GiB GOMEMLIMIT.
// Files folded into a FileReachabilityFold are not retained and are not
// charged, so a pass over the whole JS/TS fleet stays far under it; a pass
// that crosses it is marked partial under the existing evidence_budget cause.
// A var, not a const, so tests can lower it.
var maxSupplyChainImpactEvidencePayloadBytesPerIntent int64 = 256 << 20

// supplyChainImpactEvidenceBudget tracks the expansion envelopes one pass has
// loaded against maxSupplyChainImpactEvidenceEnvelopesPerIntent, and the raw
// payload bytes it keeps against maxSupplyChainImpactEvidencePayloadBytesPerIntent.
// A nil budget is unlimited so stage-level tests can call a stage on its own.
type supplyChainImpactEvidenceBudget struct {
	limit int
	used  int
	// bytesLimit and bytesUsed are the retained-payload-bytes budget. A zero
	// bytesLimit is unlimited, which keeps stage-level tests that build a
	// budget directly on the count budget alone.
	bytesLimit int64
	bytesUsed  int64
	// activeEvidence sums the per-statement accounting of every
	// active-evidence load the pass makes (#8880), so the pass's success log
	// can name the statement that dominated it.
	activeEvidence supplyChainImpactActiveEvidenceTally
}

// recordActiveEvidence folds one load's accounting into the pass tally. A nil
// budget (stage-level tests) records nothing.
func (b *supplyChainImpactEvidenceBudget) recordActiveEvidence(stats SupplyChainImpactActiveEvidenceLoadStats) {
	if b != nil {
		b.activeEvidence.add(stats)
		b.bytesUsed += stats.RetainedPayloadBytes
	}
}

// newSupplyChainImpactEvidenceBudget starts a pass's budget at limit, or at
// the package default when limit is not positive.
func newSupplyChainImpactEvidenceBudget(limit int) *supplyChainImpactEvidenceBudget {
	if limit <= 0 {
		limit = maxSupplyChainImpactEvidenceEnvelopesPerIntent
	}
	return &supplyChainImpactEvidenceBudget{limit: limit}
}

// charge records n newly loaded expansion envelopes and reports whether the
// pass may keep loading. The load that crosses the limit is kept: only later
// loads are skipped, so the budget never discards evidence it already paid for.
func (b *supplyChainImpactEvidenceBudget) charge(n int) bool {
	if b == nil {
		return true
	}
	b.used += n
	return b.used <= b.limit && !b.bytesExhausted()
}

// bytesExhausted reports whether the pass keeps more raw payload bytes than its
// byte budget allows. recordActiveEvidence adds the bytes each load retained,
// so every stage that already stops on charge stops on bytes too.
func (b *supplyChainImpactEvidenceBudget) bytesExhausted() bool {
	return b != nil && b.bytesLimit > 0 && b.bytesUsed > b.bytesLimit
}

// payloadBytesRemaining is the raw payload bytes the pass may still keep, for
// the loader's per-page stop. Zero means unlimited (nil budget or no byte
// limit); an already spent budget returns 1 so the loader stops after the first
// page instead of reading it as unlimited.
func (b *supplyChainImpactEvidenceBudget) payloadBytesRemaining() int64 {
	if b == nil || b.bytesLimit <= 0 {
		return 0
	}
	return max(b.bytesLimit-b.bytesUsed, 1)
}

// remaining is how many more expansion envelopes the pass may load before the
// budget is spent, never negative. A nil budget is unlimited.
func (b *supplyChainImpactEvidenceBudget) remaining() int {
	if b == nil {
		return math.MaxInt
	}
	return max(b.limit-b.used, 0)
}

// exhausted reports whether the pass has spent its budget.
func (b *supplyChainImpactEvidenceBudget) exhausted() bool {
	return b != nil && (b.used > b.limit || b.bytesExhausted())
}

// recordEvidenceHeadroom feeds the #9778 headroom gauges after the writer
// admitted a pass: the scope's last recorded partial state, so a complete pass
// releases it, and the envelopes the pass charged to its budget over that budget.
// Derived facts the protected-scan path adds after the static load are not
// charged, so they are not part of the ratio.
// A pass the writer rejected or that failed earlier never reaches here, so a
// superseded older pass cannot overwrite the newer pass's state. It reads the
// same partial() the writer's PartialEvidence uses, so the gauge follows the
// retraction contract and a suppression-tail truncation is not counted.
func (h SupplyChainImpactHandler) recordEvidenceHeadroom(
	intent reducercontract.Intent,
	loaded supplyChainImpactLoadedEvidence,
) {
	if h.Instruments == nil {
		return
	}
	h.Instruments.SupplyChainImpactHeadroom.RecordPass(
		intent.ScopeID,
		loaded.truncation.causes(),
		loaded.budgetEnvelopes,
		h.evidenceBudgetLimit(),
		// Wall clock, not evaluationNow: the gauge callback windows on time.Now,
		// and h.Now is a finding-evaluation clock that tests and replays pin.
		time.Now(),
	)
}

// evidenceBudgetLimit is the expansion envelope budget a pass runs under: the
// handler override, or the package default.
func (h SupplyChainImpactHandler) evidenceBudgetLimit() int {
	return newSupplyChainImpactEvidenceBudget(h.EvidenceBudget).limit
}

// evidencePayloadBytesLimit is the retained payload bytes budget a pass runs
// under: the handler override, or the package default.
func (h SupplyChainImpactHandler) evidencePayloadBytesLimit() int64 {
	if h.EvidencePayloadBytesLimit > 0 {
		return h.EvidencePayloadBytesLimit
	}
	return maxSupplyChainImpactEvidencePayloadBytesPerIntent
}

// addHeadroomSubSignals puts the #9778 success-line fields on subSignals: 1
// when the pass did not retract, and the envelopes the pass charged to its
// budget over that budget (budgetEnvelopes, never expansionEnvelopes: the
// protected-scan path adds uncharged derived facts to the latter). Before these the line carried only the two cause booleans, so a log
// search could not count partial passes or see how close one came.
func (h SupplyChainImpactHandler) addHeadroomSubSignals(
	subSignals map[string]float64,
	loaded supplyChainImpactLoadedEvidence,
) {
	subSignals["evidence_partial"] = boolSignal(loaded.truncation.partial())
	subSignals["evidence_budget_ratio"] = float64(loaded.budgetEnvelopes) / float64(h.evidenceBudgetLimit())
	subSignals["evidence_payload_bytes_ratio"] = float64(loaded.budgetPayloadBytes) / float64(h.evidencePayloadBytesLimit())
}
