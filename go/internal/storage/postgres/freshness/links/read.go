// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import "strings"

// LedgerReadEnabledEnv names the switch of the PR-3c changed-since ledger
// read (#7127). The API and the MCP server read it to answer get_changed_since
// from the link ledger; off, the read path issues no ledger SQL and serves
// the legacy two-generation diff exactly as before.
const LedgerReadEnabledEnv = "ESHU_CHANGED_SINCE_LEDGER_READ_ENABLED"

// ReadEnabled reports whether getenv turns the ledger-read switch on. It
// parses exactly like Enabled, so the writer and the reader agree on any
// value: 1, true, t, yes, y and on (any case, surrounding blanks trimmed)
// are on; anything else, an unparsable value included, is off.
func ReadEnabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv(LedgerReadEnabledEnv))) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	}
	return false
}

// ChainDeltaRowBudget bounds the stored delta rows one ledger read composes
// (2026-10-16 theory proof, provisional 200k). A chain whose links hold
// more rows falls back to the legacy statement: the bound keeps a read
// O(changes) instead of unbounded, and the fallback keeps it exact.
const ChainDeltaRowBudget = 200000

// ChainMaxLinks bounds the link-chain walk of one ledger read. It mirrors
// the backfill's 64-step recursion cap, well above the retained generations
// of a scope; a longer walk falls back instead of scanning unbounded ledger
// history.
const ChainMaxLinks = 64

// ReadFallbackReason names why a ledger read fell back to the legacy
// two-generation diff. Every fallback is counted; the wire contract is
// unchanged, so a fallback still answers exact rows.
type ReadFallbackReason string

const (
	// ReadFallbackCurrentUnlinked means the current generation has no link
	// row yet: the writer has not linked it (or nothing was ever linked).
	ReadFallbackCurrentUnlinked ReadFallbackReason = "current_unlinked"
	// ReadFallbackPriorOffChain means the current generation is linked but
	// no link chain connects the requested prior to it: a gap, a branch,
	// or a break (delta, poison, rebase root) sits between them.
	ReadFallbackPriorOffChain ReadFallbackReason = "prior_off_chain"
	// ReadFallbackCursorMoved means the scope cursor moved while the read
	// ran (or already points past the requested current generation): the
	// chain the read walked may no longer be the linked state.
	ReadFallbackCursorMoved ReadFallbackReason = "cursor_moved"
	// ReadFallbackDigestMismatch means the cursor or a link on the chain
	// carries another digest version: its state digests are not comparable
	// with this binary's PayloadDigestInput.
	ReadFallbackDigestMismatch ReadFallbackReason = "digest_mismatch"
	// ReadFallbackSameGeneration means the prior reference resolved to the
	// current active generation: there is no window to diff from the
	// ledger, so the legacy statement answers it.
	ReadFallbackSameGeneration ReadFallbackReason = "same_generation"
	// ReadFallbackChainTooLarge means the chain's stored delta rows exceed
	// ChainDeltaRowBudget: composing it would no longer be O(changes).
	ReadFallbackChainTooLarge ReadFallbackReason = "chain_too_large"
	// ReadFallbackLedgerError means a ledger statement failed or returned
	// an incomplete chain (a retention prune or a link commit landing
	// mid-read): the read falls back instead of answering partial rows.
	ReadFallbackLedgerError ReadFallbackReason = "ledger_error"
)

// ReadFallbackReasons is the closed, ordered fallback set. The reader and
// its telemetry iterate it so a new reason cannot ship without a
// deterministic position.
func ReadFallbackReasons() []ReadFallbackReason {
	return []ReadFallbackReason{
		ReadFallbackCurrentUnlinked,
		ReadFallbackPriorOffChain,
		ReadFallbackCursorMoved,
		ReadFallbackDigestMismatch,
		ReadFallbackSameGeneration,
		ReadFallbackChainTooLarge,
		ReadFallbackLedgerError,
	}
}

// ReadOutcome describes one ledger-read attempt: Served with Links composed
// links, or a Fallback with the reason the legacy statement answered.
type ReadOutcome struct {
	Served bool
	// Links is the number of composed links when Served.
	Links int
	// Fallback is set when the legacy statement answered.
	Fallback ReadFallbackReason
}

// Outcome returns the telemetry outcome label of an attempt: ledger_single
// for one composed link, ledger_chain for several, or fallback_<reason>.
func (o ReadOutcome) Outcome() string {
	if o.Served {
		if o.Links == 1 {
			return "ledger_single"
		}
		return "ledger_chain"
	}
	return "fallback_" + string(o.Fallback)
}
