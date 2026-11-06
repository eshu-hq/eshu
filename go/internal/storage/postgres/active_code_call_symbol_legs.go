// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/php/composer"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

// This file holds the definition-leg machinery for
// LoadActiveCodeCallSymbolDefinitionFacts: the per-group leg binding, the
// leg passes (active scans, union legs, widened anchored base bindings),
// the union-row narrowing, and the load stats line operators read. The query
// texts, the public loader, the page drivers, and the overlay merge stay in
// facts_active_code_call_symbols.go, which points here; nothing here runs
// without that caller.

// codeCallSymbolDefinitionLeg binds one key group's active and base query
// texts with the producer scopes its anchored scan is bound to (nil for the
// unanchored corpus-wide scan) and the load stat its duration accrues to.
//
// A base-only union leg (#10104) runs no active scan: its base pass binds the
// baselined delta-active targets' scope ids at $5 (producerScopeIDs) and the
// candidate base generation ids at $6. $5 and $6 are different domains by
// construction (scope ids vs generation ids, #9611), never the same set.
//
// An anchored leg's base pass widens $5 past active discovery to the union of
// its producers and the baselined delta-active targets (#10182): a delta that
// drops a publisher keeps that publisher's base definitions readable, and the
// match on each definition's own package id gates the wider scopes the same
// way it gates union legs.
type codeCallSymbolDefinitionLeg struct {
	keys             []string
	activeQuery      string
	baseQuery        string
	producerScopeIDs []string
	seconds          *float64
	// baseScopes accrues the widened base binding width (#10182). It stays
	// nil on the unanchored leg (no scope binding) and on union legs (their
	// width already accrues to the union count).
	baseScopes *int
	// narrowVendored marks a package-group union leg whose base page must
	// drop npm-vendored rows before the overlay (#10204). Anchored legs and
	// Go union legs never carry it: anchored behavior is out of scope and
	// Go definitions cannot collide through a vendored path (a nested
	// go.mod would make the scope a producer; without one the outer
	// module stamps an unmatchable import path).
	narrowVendored bool
}

// codeCallSymbolAnchoredUnion remembers an anchored key group whose active
// producer discovery came back empty, so the base pass can still read its
// base definitions through a union leg (#10104). unionCount accrues the
// union scope count to the load stats when the leg is built.
type codeCallSymbolAnchoredUnion struct {
	keys       []string
	seconds    *float64
	unionCount *int
	// narrowVendored carries the leg's group narrowing into the union leg.
	narrowVendored bool
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
			narrowVendored:   group.narrowVendored,
		})
	}
	return legs
}

// widenAnchoredLegBaseScopes unions every anchored leg's producer scopes with
// the baselined delta-active targets' scopes (#10182). Discovery is
// active-generation-anchored, so a delta that drops a publisher hides that
// publisher's base definitions from the standing binding; the widened base
// pass keeps them readable. The unanchored corpus-wide leg carries no scope
// binding and is left alone; union legs already bind the union scopes, so
// widening is a no-op for them. Statement counts do not change: the same
// base pages run, only $5 is wider.
func widenAnchoredLegBaseScopes(legs []codeCallSymbolDefinitionLeg, unionScopes []string) {
	for i := range legs {
		if len(legs[i].producerScopeIDs) == 0 {
			continue
		}
		legs[i].producerScopeIDs = unionScopeIDs(legs[i].producerScopeIDs, unionScopes)
		if legs[i].baseScopes != nil {
			*legs[i].baseScopes = len(legs[i].producerScopeIDs)
		}
	}
}

// unionScopeIDs returns the sorted, compacted union of two scope id sets.
func unionScopeIDs(a, b []string) []string {
	union := make([]string, 0, len(a)+len(b))
	union = append(union, a...)
	union = append(union, b...)
	slices.Sort(union)
	return slices.Compact(union)
}

// excludeVendoredUnionBaseRows drops union-leg base rows no producer anchor
// could have published (#10204): a row whose file sits under a node_modules
// segment (the producer manifest read excludes those manifests, while the
// definition stamp keeps the nearest manifest's name) is a vendored copy,
// unless it carries a queried composer package id — ComposerManifestsQuery
// has no node_modules exclusion and the parser stamps such files, so they
// stay. Rows at any other path stay, as does anything without positive
// vendored evidence (an unparseable payload or a missing path keeps its row):
// the narrowing must never drop a legitimate renamed-publisher base.
func excludeVendoredUnionBaseRows(page []facts.Envelope, keys []string) []facts.Envelope {
	queriedComposer := make(map[string]struct{})
	for _, key := range keys {
		if name := producerstore.PackageName(key); name != "" && composer.ValidPackageName(name) {
			queriedComposer[name] = struct{}{}
		}
	}
	kept := page[:0]
	for _, env := range page {
		if !unionBaseRowIsNpmVendored(env, queriedComposer) {
			kept = append(kept, env)
		}
	}
	return kept
}

// unionBaseRowIsNpmVendored reports whether the row is an npm-vendored copy:
// its file sits under a node_modules segment and at least one of its
// definitions carries a non-empty, non-queried-composer package id. The
// segment test mirrors the producer manifest read's
// `relative_path !~* '(^|/)node_modules[^/]*/'` (ASCII case folding, any
// suffix including none); the composer keep-out uses the schema grammar both
// routers share, so the filter cannot disagree with the anchor about which
// names are composer's. Fail open: a vendored-path row whose payload is
// unparseable or carries no definition with a non-empty package id keeps its
// row, since there is no positive vendored evidence to drop it on.
func unionBaseRowIsNpmVendored(env facts.Envelope, queriedComposer map[string]struct{}) bool {
	path, _ := env.Payload["relative_path"].(string)
	if !isNodeModulesPath(path) {
		return false
	}
	seenPackageID := false
	for _, item := range codeCallSymbolDefinitionItems(env) {
		if id, _ := item["package_id"].(string); id != "" {
			seenPackageID = true
			if _, ok := queriedComposer[id]; ok {
				return false
			}
		}
	}
	return seenPackageID
}

// isNodeModulesPath reports whether path sits under a node_modules
// directory: every slash-separated segment but the last whose
// ASCII-lowercased form starts with node_modules, matching the manifest
// read's node_modules[^/]*/ (which also covers node_modules.bak-style
// renamed backups, and the bare directory). The last segment is the file
// name, which the SQL trailing slash never matches, so a file merely named
// node_modules stays.
func isNodeModulesPath(path string) bool {
	segments := strings.Split(path, "/")
	for _, segment := range segments[:len(segments)-1] {
		if strings.HasPrefix(strings.ToLower(segment), "node_modules") {
			return true
		}
	}
	return false
}

// codeCallSymbolDefinitionItems returns every parsed definition object on a
// loaded file fact, across the same five arrays the definition match scans.
// Anything unexpected (a missing payload, a non-object array entry) yields
// nothing, so the caller keeps the row: only positive vendored evidence
// drops it.
func codeCallSymbolDefinitionItems(env facts.Envelope) []map[string]any {
	parsed, _ := env.Payload["parsed_file_data"].(map[string]any)
	if parsed == nil {
		return nil
	}
	var items []map[string]any
	for _, array := range []string{"functions", "classes", "structs", "interfaces", "type_aliases"} {
		definitions, _ := parsed[array].([]any)
		for _, definition := range definitions {
			if item, ok := definition.(map[string]any); ok {
				items = append(items, item)
			}
		}
	}
	return items
}

// loadCodeCallSymbolDefinitionLegRows runs the three scans and returns the
// active rows and the base candidates separately for the overlay, recording
// producer, union, and base scope counts and leg durations in load. Active
// pages, the delta-target lookup, then base pages run in that order on the one
// snapshot; any failure aborts the read. The base pages run only when a
// delta-active scope with a recorded baseline exists; an anchored group with
// keys but no producer then contributes a base-only union leg, and an
// anchored group with a surviving producer widens its base $5 to the
// producers plus the baselined targets' scopes (#10104, #10182), and a
// package-group union leg narrows its base page past vendored non-producers
// (#10204). The page driver it pages through lives in
// facts_active_code_call_symbols.go.
func (s FactStore) loadCodeCallSymbolDefinitionLegRows(
	ctx context.Context,
	snapshot *deltaOverlaySnapshot,
	packageKeys, goKeys, otherKeys []string,
	unanchoredBase, anchoredBase string,
	sink deltaOverlayTelemetry,
	load *codeCallSymbolLoadStats,
) (active, base []facts.Envelope, err error) {
	producers := producerstore.New(s.database).WithInstruments(s.instruments)

	// The statement order matches the pre-overlay read exactly (unanchored
	// scan, then per anchored leg its producer discovery and scan), so the
	// only new statements are the overlay's own: the delta-target lookup
	// after the active pages and the base pages after that.
	var legs []codeCallSymbolDefinitionLeg
	var emptyAnchored []codeCallSymbolAnchoredUnion
	var activeRows []facts.Envelope
	if len(otherKeys) > 0 {
		started := time.Now()
		page, err := s.loadActiveCodeCallSymbolDefinitionFacts(
			ctx, listActiveCodeCallSymbolDefinitionFactsQuery, otherKeys, nil, nil,
		)
		if err != nil {
			return nil, nil, err
		}
		activeRows = append(activeRows, page...)
		load.otherScanSeconds = time.Since(started).Seconds()
		legs = append(legs, codeCallSymbolDefinitionLeg{
			keys:        otherKeys,
			activeQuery: listActiveCodeCallSymbolDefinitionFactsQuery,
			baseQuery:   unanchoredBase,
			seconds:     &load.otherScanSeconds,
		})
	}
	for _, leg := range []struct {
		keys           []string
		producers      func(context.Context, []string) ([]string, error)
		scopeCount     *int
		seconds        *float64
		unionCount     *int
		baseScopes     *int
		narrowVendored bool
	}{
		{packageKeys, producers.PackageScopeIDs, &load.packageProducerScopes, &load.packageSeconds, &load.packageUnionScopes, &load.packageBaseScopes, true},
		{goKeys, producers.GoModuleScopeIDs, &load.goProducerScopes, &load.goSeconds, &load.goUnionScopes, &load.goBaseScopes, false},
	} {
		if len(leg.keys) == 0 {
			continue
		}
		started := time.Now()
		producerScopeIDs, err := leg.producers(ctx, leg.keys)
		if err != nil {
			return nil, nil, err
		}
		*leg.scopeCount = len(producerScopeIDs)
		if len(producerScopeIDs) > 0 {
			anchored, err := s.loadActiveCodeCallSymbolDefinitionFacts(
				ctx, listAnchoredActiveCodeCallSymbolDefinitionFactsQuery, leg.keys, producerScopeIDs, nil,
			)
			if err != nil {
				return nil, nil, err
			}
			activeRows = appendUniqueFactEnvelopes(activeRows, anchored)
			legs = append(legs, codeCallSymbolDefinitionLeg{
				keys:             leg.keys,
				activeQuery:      listAnchoredActiveCodeCallSymbolDefinitionFactsQuery,
				baseQuery:        anchoredBase,
				producerScopeIDs: producerScopeIDs,
				seconds:          leg.seconds,
				baseScopes:       leg.baseScopes,
			})
		} else {
			// Empty discovery issues no active scan. A delta that rewrote
			// the producer manifest can still hold this group's base
			// definitions, so remember it for the union leg below (#10104).
			emptyAnchored = append(emptyAnchored, codeCallSymbolAnchoredUnion{
				keys:           leg.keys,
				seconds:        leg.seconds,
				unionCount:     leg.unionCount,
				narrowVendored: leg.narrowVendored,
			})
		}
		*leg.seconds = time.Since(started).Seconds()
	}
	targets, err := snapshot.deltaTargets(ctx)
	if err != nil {
		s.recordDeltaOverlayFailure(ctx, sink)
		return nil, nil, err
	}
	var baseRows []facts.Envelope
	if baseGenerations, runBase := deltaOverlayBaseGenerations(targets); runBase {
		// Base-only union legs (#10104): per anchored group with keys but
		// no producer, the base pass binds the baselined targets' scope
		// ids at $5 and the candidate base generation ids at $6 — split
		// domains, never the same set (#9611).
		var unionScopes []string
		for _, target := range targets {
			if target.commits.Baseline == "" {
				continue
			}
			unionScopes = append(unionScopes, target.scopeID)
		}
		slices.Sort(unionScopes)
		unionScopes = slices.Compact(unionScopes)
		legs = appendCodeCallSymbolUnionLegs(legs, emptyAnchored, unionScopes, anchoredBase)
		// Widened anchored base bindings (#10182): per anchored group
		// with a producer, the base pass binds the union of its active
		// producers and the baselined targets' scopes at $5, so a delta
		// that drops a publisher keeps that publisher's base definitions
		// readable. Same base pages, only $5 is wider.
		widenAnchoredLegBaseScopes(legs, unionScopes)
		for i := range legs {
			started := time.Now()
			page, err := s.loadActiveCodeCallSymbolDefinitionFacts(
				ctx, legs[i].baseQuery, legs[i].keys, legs[i].producerScopeIDs, baseGenerations,
			)
			if err != nil {
				s.recordDeltaOverlayFailure(ctx, sink)
				return nil, nil, err
			}
			if legs[i].narrowVendored {
				// Union-leg narrowing (#10204): the scope filter still
				// covers every baselined delta-active scope (the base
				// manifest content a scope-side lookup would need is
				// last-writer-wins in content_files), so vendored rows
				// drop here, per group, before the overlay can reach them.
				page = excludeVendoredUnionBaseRows(page, legs[i].keys)
			}
			baseRows = appendUniqueFactEnvelopes(baseRows, page)
			*legs[i].seconds += time.Since(started).Seconds()
		}
	}
	return activeRows, baseRows, nil
}

// codeCallSymbolLoadStats records how one definition load split its keys and
// what each leg cost, so an operator can tell from one log line which scan made
// a slow code call materialization slow. A producer scope count of zero with
// keys of that group means no stored manifest names a producer, so that leg
// issued no active scan; a non-zero union count means the group still ran a
// base-only union scan over the baselined targets' scopes (#10104). Read zero
// producer scopes as "no scan" only next to a zero union count. A base scope
// count above the producer count means the anchored base scan widened past
// active discovery to the baselined delta-active targets (#10182); zero means
// the read ran no base pass.
type codeCallSymbolLoadStats struct {
	otherKeys, packageKeys, goKeys              int
	packageProducerScopes, goProducerScopes     int
	packageUnionScopes, goUnionScopes           int
	packageBaseScopes, goBaseScopes             int
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
		"package_base_scope_count", l.packageBaseScopes,
		"go_base_scope_count", l.goBaseScopes,
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
