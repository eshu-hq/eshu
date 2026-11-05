// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
)

// This file holds the definition-leg machinery for
// LoadActiveCodeCallSymbolDefinitionFacts: the per-group leg binding, the
// base-only union legs for manifest-changing deltas (#10104), and the load
// stats line operators read. The queries, the loader, the active/base paging,
// and the overlay merge stay in facts_active_code_call_symbols.go, which
// points here; nothing here runs without that caller.

// codeCallSymbolDefinitionLeg binds one key group's active and base query
// texts with the producer scopes its anchored scan is bound to (nil for the
// unanchored corpus-wide scan) and the load stat its duration accrues to.
//
// A base-only union leg (#10104) runs no active scan: its base pass binds the
// baselined delta-active targets' scope ids at $5 (producerScopeIDs) and the
// candidate base generation ids at $6. $5 and $6 are different domains by
// construction (scope ids vs generation ids, #9611), never the same set.
type codeCallSymbolDefinitionLeg struct {
	keys             []string
	activeQuery      string
	baseQuery        string
	producerScopeIDs []string
	seconds          *float64
}

// codeCallSymbolAnchoredUnion remembers an anchored key group whose active
// producer discovery came back empty, so the base pass can still read its
// base definitions through a union leg (#10104). unionCount accrues the
// union scope count to the load stats when the leg is built.
type codeCallSymbolAnchoredUnion struct {
	keys       []string
	seconds    *float64
	unionCount *int
}

// appendCodeCallSymbolUnionLegs appends one base-only union leg per anchored
// group with empty active discovery and records each group's union scope
// count (#10104). A union leg runs no active scan: a delta that rewrote the
// producer manifest still holds the group's base definitions, and the match
// on each definition's own package id gates the wider base scopes. Groups
// with a producer keep their anchored legs untouched.
func appendCodeCallSymbolUnionLegs(
	legs []codeCallSymbolDefinitionLeg,
	empty []codeCallSymbolAnchoredUnion,
	unionScopes []string,
	anchoredBase string,
) []codeCallSymbolDefinitionLeg {
	for _, group := range empty {
		*group.unionCount = len(unionScopes)
		legs = append(legs, codeCallSymbolDefinitionLeg{
			keys:             group.keys,
			activeQuery:      listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
			baseQuery:        anchoredBase,
			producerScopeIDs: unionScopes,
			seconds:          group.seconds,
		})
	}
	return legs
}

// codeCallSymbolLoadStats records how one definition load split its keys and
// what each leg cost, so an operator can tell from one log line which scan made
// a slow code call materialization slow. A producer scope count of zero with
// keys of that group means no stored manifest names a producer, so that leg
// issued no active scan; a non-zero union count means the group still ran a
// base-only union scan over the baselined targets' scopes (#10104). Read zero
// producer scopes as "no scan" only next to a zero union count.
type codeCallSymbolLoadStats struct {
	otherKeys, packageKeys, goKeys              int
	packageProducerScopes, goProducerScopes     int
	packageUnionScopes, goUnionScopes           int
	otherScanSeconds, packageSeconds, goSeconds float64
}

// log emits the load summary at Info, on success and on failure. The loader has
// no scope id, so correlate with the "code call materialization completed" line
// of the same consumer by adjacency. On failure the durations and producer scope
// counts cover only the legs that finished, so read outcome first.
func (l codeCallSymbolLoadStats) log(ctx context.Context, err error) {
	attrs := []any{
		"outcome", "ok",
		"other_key_count", l.otherKeys,
		"package_key_count", l.packageKeys,
		"go_key_count", l.goKeys,
		"package_producer_scope_count", l.packageProducerScopes,
		"go_producer_scope_count", l.goProducerScopes,
		"package_union_scope_count", l.packageUnionScopes,
		"go_union_scope_count", l.goUnionScopes,
		"other_scan_duration_seconds", l.otherScanSeconds,
		"package_leg_duration_seconds", l.packageSeconds,
		"go_leg_duration_seconds", l.goSeconds,
	}
	if err != nil {
		attrs[1] = "error"
		attrs = append(attrs, "error", err.Error())
	}
	slog.Default().InfoContext(ctx, "code call symbol definition load", attrs...)
}
