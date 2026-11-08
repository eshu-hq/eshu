// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// SupplyChainStuckPartialDegradedAfter is how long a scope's last recorded
// supply-chain impact pass may stay partial before the scope degrades status
// health (#10389). A scope partial for less than this still converges on its
// next complete pass without paging the operator; at or past it the scope
// keeps serving stale findings, so health names it.
const SupplyChainStuckPartialDegradedAfter = 24 * time.Hour

// SupplyChainStuckPartialScope is one scope whose last recorded supply-chain
// impact pass ended partial for an identity-affecting cause, so the writer
// upserted without retracting and the scope keeps its stale findings. It is
// durable per-scope pass state: the reducer write transaction maintains one
// row per scope, a full complete pass clears it (a scoped complete leaves
// it untouched), and the status reader serves it live. It never carries
// finding payloads, only the scope identity, the closed cause set, and the
// timestamps that size the degraded reason.
type SupplyChainStuckPartialScope struct {
	ScopeID string `json:"scope_id"`
	// Causes is the closed truncation cause set that made the last recorded
	// pass partial (active_expansion_rounds, evidence_budget,
	// manifest_refused).
	Causes []string `json:"causes"`
	// PartialSince is when the scope first became continuously partial: set
	// when a complete scope goes partial, preserved across further partial
	// passes, cleared by a full complete pass.
	PartialSince time.Time `json:"partial_since"`
}

// stuckPartialDegradedReasons returns the operator-facing degraded reasons
// for scopes partial at or past the escalation threshold. Scopes partial for
// less than the threshold converge silently and contribute nothing, so a
// scope that goes partial and completes the same day never pages.
func stuckPartialDegradedReasons(scopes []SupplyChainStuckPartialScope, asOf time.Time) []string {
	reasons := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scopeID := strings.TrimSpace(scope.ScopeID)
		if scopeID == "" {
			continue
		}
		since := scope.PartialSince.UTC()
		if since.IsZero() {
			continue
		}
		if age := asOf.UTC().Sub(since); age >= SupplyChainStuckPartialDegradedAfter {
			reasons = append(reasons, fmt.Sprintf(
				"supply-chain impact scope %q partial for %s (%s); findings are stale",
				scopeID,
				age.Truncate(time.Second),
				strings.Join(slices.Clone(scope.Causes), ","),
			))
		}
	}
	return reasons
}

// renderStuckPartialLines projects the stuck-partial drilldown onto the text
// surface: one line per scope still partial, empty when none is stuck. The
// degraded health reason already carries the age; these lines carry the
// since timestamp an operator needs to find the pass in the logs.
func renderStuckPartialLines(scopes []SupplyChainStuckPartialScope) []string {
	if len(scopes) == 0 {
		return nil
	}
	lines := make([]string, 0, len(scopes)+1)
	lines = append(lines, "Supply-chain stuck partial scopes:")
	for _, scope := range scopes {
		lines = append(lines, fmt.Sprintf(
			"  %s causes=%s partial_since=%s",
			scope.ScopeID,
			strings.Join(scope.Causes, ","),
			scope.PartialSince.UTC().Format(time.RFC3339),
		))
	}
	return lines
}

// supplyChainStuckPartialJSON is the stable JSON projection of one stuck
// scope. Field names are frozen once published, like every other section in
// RenderJSON.
type supplyChainStuckPartialJSON struct {
	ScopeID      string   `json:"scope_id"`
	Causes       []string `json:"causes"`
	PartialSince string   `json:"partial_since"`
}

// supplyChainStuckPartialsJSON projects the drilldown section, nil when no
// scope is stuck so the key stays absent next to the other omitempty
// sections.
func supplyChainStuckPartialsJSON(scopes []SupplyChainStuckPartialScope) []supplyChainStuckPartialJSON {
	if len(scopes) == 0 {
		return nil
	}
	projected := make([]supplyChainStuckPartialJSON, 0, len(scopes))
	for _, scope := range scopes {
		projected = append(projected, supplyChainStuckPartialJSON{
			ScopeID:      scope.ScopeID,
			Causes:       slices.Clone(scope.Causes),
			PartialSince: scope.PartialSince.UTC().Format(time.RFC3339),
		})
	}
	return projected
}

// cloneStuckPartialScopes copies the stuck-partial section so a later reader
// mutation cannot alias the report.
func cloneStuckPartialScopes(scopes []SupplyChainStuckPartialScope) []SupplyChainStuckPartialScope {
	if len(scopes) == 0 {
		return nil
	}
	cloned := make([]SupplyChainStuckPartialScope, 0, len(scopes))
	for _, scope := range scopes {
		scope.PartialSince = scope.PartialSince.UTC()
		scope.Causes = slices.Clone(scope.Causes)
		cloned = append(cloned, scope)
	}
	return cloned
}
