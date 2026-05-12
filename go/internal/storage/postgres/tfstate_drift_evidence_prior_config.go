package postgres

import (
	"context"
	"fmt"
	"strings"
)

// defaultPriorConfigDepth bounds how many prior repo-snapshot generations the
// drift loader walks to detect removed_from_config. Ten is a conservative seed
// — most operators apply many times per day; ten generations cover roughly a
// trading-day window in production. Override per-deployment via
// ESHU_DRIFT_PRIOR_CONFIG_DEPTH.
const defaultPriorConfigDepth = 10

// effectivePriorConfigDepth returns the configured depth or the package
// default when the field is non-positive (zero or negative). The
// permissive guard keeps tests ergonomic (zero-value field gets default
// behavior with no init step) and matches the upstream contract where
// parsePriorConfigDepth maps invalid env input to 0.
func (l PostgresDriftEvidenceLoader) effectivePriorConfigDepth() int {
	if l.PriorConfigDepth <= 0 {
		return defaultPriorConfigDepth
	}
	return l.PriorConfigDepth
}

// loadPriorConfigAddresses returns the set of canonical Terraform resource
// addresses declared in any of the most recent N prior repo-snapshot
// generations for `scopeID`, excluding the current generation
// `currentGenerationID`. Powers removed_from_config detection: state-only
// addresses present in this set get PreviouslyDeclaredInConfig=true so the
// classifier emits removed_from_config; operator-imported addresses (never in
// any prior config) stay outside the set and surface as added_in_state.
//
// The walk is bounded by listPriorConfigAddressesQuery's LIMIT. Addresses
// declared more than N generations ago are intentionally invisible — the
// conservative outside-window policy keeps the cost bounded and avoids
// promoting truly-ancient resources whose removal was already actioned.
//
// `prefixMap` is the module-prefix map built from the CURRENT generation's
// terraform_modules facts (issue #169 / ADR
// 2026-05-11-module-aware-drift-joining). Applying it to prior-config
// entries keeps removed_from_config alive for module-nested addresses: a
// resource block deleted in the current generation while the surrounding
// module call still exists must still match the state-side prefixed
// address. The current-generation map is the right one because the
// dominant removed_from_config shape is "resource block deleted, module
// call preserved" — a module rename across generations remains a v1
// limitation tracked as a follow-up issue.
//
// 1→N projection: when the prefix map records multiple callers for the
// same callee (the ADR's "two `module {}` blocks pointing at the same
// source" case), every prior-config address gets emitted N times, one per
// caller prefix. mergeDriftRows then promotes the matching state-side
// addresses to PreviouslyDeclaredInConfig=true individually.
//
// Returns an empty set when:
//   - the scope has no prior generations matching the status filter, OR
//   - prior generations exist but none declare any resources.
//
// Returns a DB error to the caller; the reducer handler's retry path covers
// transient failures.
func (l PostgresDriftEvidenceLoader) loadPriorConfigAddresses(
	ctx context.Context,
	scopeID string,
	currentGenerationID string,
	prefixMap modulePrefixMap,
) (map[string]struct{}, error) {
	depth := l.effectivePriorConfigDepth()
	rows, err := l.DB.QueryContext(ctx, listPriorConfigAddressesQuery, scopeID, currentGenerationID, depth)
	if err != nil {
		return nil, fmt.Errorf("list prior config terraform_resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]struct{}{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan prior config terraform_resources: %w", err)
		}
		entries, err := decodeJSONArray(raw, "prior_terraform_resources")
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			collectPriorConfigAddresses(entry, prefixMap, out)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prior config terraform_resources: %w", err)
	}
	return out, nil
}

// collectPriorConfigAddresses unions canonical addresses derived from one
// parser-emitted terraform_resources entry into `out`. Mirrors
// emitConfigRowsForEntry's prefix-application contract: zero matching
// prefixes adds the root-module address, N matching prefixes adds N
// distinct module-prefixed addresses (1→N projection).
func collectPriorConfigAddresses(
	entry map[string]any,
	prefixMap modulePrefixMap,
	out map[string]struct{},
) {
	entryPath := strings.TrimSpace(coerceJSONString(entry["path"]))
	prefixes := prefixMap.modulePrefixForPath(entryPath)
	if len(prefixes) == 0 {
		row, ok := configRowFromParserEntry(entry, "")
		if !ok {
			return
		}
		out[row.Address] = struct{}{}
		return
	}
	for _, prefix := range prefixes {
		row, ok := configRowFromParserEntry(entry, prefix)
		if !ok {
			return
		}
		out[row.Address] = struct{}{}
	}
}
