// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file bounds one call's active-row working set across every repository
// (and every stream) it drains (#11436, follow-up to #11428). PR #11431 capped
// ONE drain at supplyChainImpactSupportsDrainMaxActiveRows active envelopes
// with fail-fast truncation, but GetSupplyChainAdvisoriesForRepos folds every
// under-cap page into byRepo across ALL requested repositories with no
// call-level budget: 200 repositories at 90,000 actives each pass every
// per-drain cap and still accumulate 18,000,000 envelopes. The reconciliation
// fact stream (drainActiveSecurityAlertReconciliationFacts) accumulates pages
// with no cap at all. Both now share one call budget: past it the call fails
// with the existing truncation error shape and the resume cursor, the same
// semantics as the per-drain cap (never serve a prefix as complete, never
// silently truncate).

// supplyChainImpactSupportsCallBudgetEnv tunes the per-call active-row budget.
// A positive value wins; anything else (unset, blank, malformed,
// non-positive) keeps the package default.
const supplyChainImpactSupportsCallBudgetEnv = "ESHU_SUPPLY_CHAIN_IMPACT_SUPPORTS_CALL_MAX_ACTIVE_ROWS"

// supplyChainImpactSupportsCallBudgetMultiple sizes the default call budget as
// a multiple of the per-drain cap: a call survives a handful of fully-capped
// drains plus ordinary siblings, while a call-wide hot set still fails fast.
// A 152,000 finding repository measured about 365 MB through the supports
// path, so five fully-capped drains bound the worst case far below the 7.6 GB
// heap that OOMKilled the engine.
const supplyChainImpactSupportsCallBudgetMultiple = 5

// supplyChainImpactSupportsCallDefaultMaxActiveRows is the default per-call
// active-row budget. Inactive raw rows carry no payload and never count, the
// same unit as the per-drain cap.
const supplyChainImpactSupportsCallDefaultMaxActiveRows = supplyChainImpactSupportsCallBudgetMultiple * supplyChainImpactSupportsDrainDefaultMaxActiveRows

// supplyChainImpactSupportsCallMaxActiveRows is the live per-call budget, a
// package var (not a const) so tests can model a multi-repository call with a
// few pages instead of hundreds of thousands of rows; the shipped binary
// keeps the default.
var supplyChainImpactSupportsCallMaxActiveRows = supplyChainImpactSupportsCallDefaultMaxActiveRows

// supplyChainImpactSupportsCallBudget resolves the per-call active-row budget
// for this call: the env override when it parses positive, else the package
// value (or the default when that is not positive). The env knob is floored
// at the per-drain default: one repository alone may legitimately deliver up
// to the per-drain cap, so an operator-set call budget below it would fail
// single-repository calls the per-drain cap allows. The package var is
// honored verbatim so tests can model a tripping call with a few pages;
// the shipped binary keeps the default.
func supplyChainImpactSupportsCallBudget() int {
	if raw := strings.TrimSpace(os.Getenv(supplyChainImpactSupportsCallBudgetEnv)); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n < supplyChainImpactSupportsDrainDefaultMaxActiveRows {
				return supplyChainImpactSupportsDrainDefaultMaxActiveRows
			}
			return n
		}
	}
	if supplyChainImpactSupportsCallMaxActiveRows < 1 {
		return supplyChainImpactSupportsCallDefaultMaxActiveRows
	}
	return supplyChainImpactSupportsCallMaxActiveRows
}

// errSupplyChainImpactSupportsCallBudgetTruncated is the yield sentinel a
// call-budgeted fold returns when delivering the next page would pass the
// call budget. It never escapes: each call site converts it to the typed
// truncation error with the call stats and the resume cursor.
var errSupplyChainImpactSupportsCallBudgetTruncated = errors.New("supply chain impact supports call stopped at the active-row call budget")

// supplyChainImpactSupportsCallTracker counts one call's delivered active
// rows across every drain and stream it folds, so the streams share one
// budget instead of each spending a fresh one.
type supplyChainImpactSupportsCallTracker struct {
	max           int
	delivered     int
	lastDelivered string
}

// newSupplyChainImpactSupportsCallTracker opens one call's budget window.
func newSupplyChainImpactSupportsCallTracker() *supplyChainImpactSupportsCallTracker {
	return &supplyChainImpactSupportsCallTracker{max: supplyChainImpactSupportsCallBudget()}
}

// head returns the deliverable head of page: the whole page when it fits, the
// first remaining rows when the budget cuts it, or nil when the budget is
// spent. It records what it hands out and the last delivered fact id. The
// caller folds the head and, when the head is shorter than the page, stops
// the call with the truncation error instead of serving a prefix as complete.
func (t *supplyChainImpactSupportsCallTracker) head(page []facts.Envelope) []facts.Envelope {
	serve := page
	if t.delivered+len(serve) > t.max {
		if t.delivered >= t.max {
			serve = nil
		} else {
			serve = serve[:t.max-t.delivered]
		}
	}
	if len(serve) > 0 {
		t.lastDelivered = serve[len(serve)-1].FactID
	}
	t.delivered += len(serve)
	return serve
}
