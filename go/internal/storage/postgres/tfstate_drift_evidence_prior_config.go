// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"
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

// loadPriorConfigAddresses returns the canonical Terraform resource addresses
// declared in any of the most recent N prior repo-snapshot generations for
// `scopeID`, excluding the current generation `currentGenerationID`, mapped
// to the module-resolution reason (issue #5572) that made the PRIOR
// generation's own address projection low-confidence, or "" when that prior
// generation's module resolution was clean. Powers removed_from_config
// detection: state-only addresses present in this map get
// PreviouslyDeclaredInConfig=true so the classifier emits
// removed_from_config; operator-imported addresses (never in any prior
// config) stay outside the map and surface as added_in_state.
//
// The confidence value matters because a prior generation's module
// resolution can fail exactly the same way a current generation's can (see
// tfstate_drift_evidence_module_confidence.go): the projected prior-config
// address may be a root-module or masked-prefix fallback that only
// COINCIDENTALLY matches the current state address, not a certainly-correct
// declaration. mergeDriftRows threads this reason onto the promoted
// state-only row's ModuleResolutionReason so BuildCandidates attaches the
// confidence atom and the reducer writer downgrades the resulting
// removed_from_config finding to "derived" — the same treatment a
// current-generation config row already gets.
//
// The walk is bounded by listPriorConfigAddressesQuery's LIMIT. Addresses
// declared more than N generations ago are intentionally invisible — the
// conservative outside-window policy keeps the cost bounded and avoids
// promoting truly-ancient resources whose removal was already actioned.
//
// The loader builds a module-prefix map per prior generation before
// projecting that generation's terraform_resources rows. This preserves
// removed_from_config for the dominant "resource block deleted, module call
// preserved" shape and avoids issue #201's rename false-pair, where current
// module name "network" would otherwise be applied to prior rows that lived
// under prior module name "vpc".
//
// 1→N projection: when the prefix map records multiple callers for the
// same callee (the ADR's "two `module {}` blocks pointing at the same
// source" case), every prior-config address gets emitted N times, one per
// caller prefix. mergeDriftRows then promotes the matching state-side
// addresses to PreviouslyDeclaredInConfig=true individually.
//
// Returns an empty map when:
//   - the scope has no prior generations matching the status filter, OR
//   - prior generations exist but none declare any resources.
//
// Returns a DB error to the caller; the reducer handler's retry path covers
// transient failures.
func (l PostgresDriftEvidenceLoader) loadPriorConfigAddresses(
	ctx context.Context,
	scopeID string,
	currentGenerationID string,
	currentPrefixMap modulePrefixMap,
	recorder unresolvedRecorder,
) (map[string]string, error) {
	if recorder == nil {
		recorder = nopUnresolvedRecorder{}
	}
	depth := l.effectivePriorConfigDepth()
	rows, err := l.DB.QueryContext(ctx, listPriorConfigAddressesQuery, scopeID, currentGenerationID, depth)
	if err != nil {
		return nil, fmt.Errorf("list prior config terraform_resources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type priorConfigGroup struct {
		generationID string
		entries      []map[string]any
	}
	groupsByGeneration := map[string]*priorConfigGroup{}
	var generationOrder []string
	for rows.Next() {
		var generationID string
		var raw []byte
		if err := rows.Scan(&generationID, &raw); err != nil {
			return nil, fmt.Errorf("scan prior config terraform_resources: %w", err)
		}
		entries, err := decodeJSONArray(raw, "prior_terraform_resources")
		if err != nil {
			return nil, err
		}
		generationID = strings.TrimSpace(generationID)
		if generationID == "" {
			continue
		}
		group, ok := groupsByGeneration[generationID]
		if !ok {
			group = &priorConfigGroup{generationID: generationID}
			groupsByGeneration[generationID] = group
			generationOrder = append(generationOrder, generationID)
		}
		group.entries = append(group.entries, entries...)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prior config terraform_resources: %w", err)
	}

	out := map[string]string{}
	seenModuleRename := map[string]struct{}{}
	for _, generationID := range generationOrder {
		group := groupsByGeneration[generationID]
		priorPrefixMap, priorConfidenceMap, err := l.buildModulePrefixMap(ctx, scopeID, group.generationID, recorder)
		if err != nil {
			return nil, fmt.Errorf("build prior config module prefix map for generation %q: %w", group.generationID, err)
		}
		for _, entry := range group.entries {
			recordModuleRenameIfPrefixChanged(
				ctx,
				recorder,
				group.generationID,
				entry,
				currentPrefixMap,
				priorPrefixMap,
				seenModuleRename,
			)
			collectPriorConfigAddresses(entry, priorPrefixMap, priorConfidenceMap, out)
		}
	}
	return out, nil
}

// collectPriorConfigAddresses unions canonical addresses derived from one
// parser-emitted terraform_resources entry into `out`, mapped to the
// module-resolution reason (issue #5572) the PRIOR generation's own
// module-prefix resolution produced for that entry, or "" when resolution
// was clean. Mirrors emitConfigRowsForEntry's prefix-application AND
// confidence contract exactly: zero matching prefixes adds the root-module
// address, N matching prefixes adds N distinct module-prefixed addresses
// (1→N projection), and moduleResolutionReasonForEntry decides the reason
// from the SAME match-specificity comparison emitConfigRowsForEntry uses for
// the current generation.
//
// First-write-wins per address mirrors emitConfigRowsForEntry: a duplicate
// address across prior-generation entries keeps whichever reason (or
// cleanliness) was recorded first, so a later generation's re-declaration of
// the same address cannot silently erase an earlier low-confidence flag.
func collectPriorConfigAddresses(
	entry map[string]any,
	prefixMap modulePrefixMap,
	confidenceMap moduleResolutionConfidenceMap,
	out map[string]string,
) {
	entryPath := strings.TrimSpace(coerceJSONString(entry["path"]))
	prefixes, prefixDepth := prefixMap.modulePrefixForPathWithDepth(entryPath)
	reason := moduleResolutionReasonForEntry(confidenceMap, entryPath, prefixDepth)
	if len(prefixes) == 0 {
		row, ok := configRowFromParserEntry(entry, "", reason)
		if !ok {
			return
		}
		if _, exists := out[row.Address]; !exists {
			out[row.Address] = row.ModuleResolutionReason
		}
		return
	}
	for _, prefix := range prefixes {
		row, ok := configRowFromParserEntry(entry, prefix, reason)
		if !ok {
			return
		}
		if _, exists := out[row.Address]; !exists {
			out[row.Address] = row.ModuleResolutionReason
		}
	}
}

// recordModuleRenameIfPrefixChanged emits one module_renamed signal per
// prior-generation file path whose prior module prefix set differs from the
// current generation's prefix set. The signal is intentionally path-scoped,
// not resource-scoped, so a module containing many resources does not inflate
// the counter.
func recordModuleRenameIfPrefixChanged(
	ctx context.Context,
	recorder unresolvedRecorder,
	generationID string,
	entry map[string]any,
	currentPrefixMap modulePrefixMap,
	priorPrefixMap modulePrefixMap,
	seen map[string]struct{},
) {
	entryPath := strings.TrimSpace(coerceJSONString(entry["path"]))
	currentPrefixes := currentPrefixMap.modulePrefixForPath(entryPath)
	priorPrefixes := priorPrefixMap.modulePrefixForPath(entryPath)
	if len(currentPrefixes) == 0 || len(priorPrefixes) == 0 {
		return
	}
	if samePrefixSet(currentPrefixes, priorPrefixes) {
		return
	}
	key := generationID + "\x00" + entryPath + "\x00" +
		canonicalPrefixSetKey(currentPrefixes) + "\x00" +
		canonicalPrefixSetKey(priorPrefixes)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	recorder.record(ctx, unresolvedReasonModuleRenamed)
}

func samePrefixSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for i := range leftCopy {
		if leftCopy[i] != rightCopy[i] {
			return false
		}
	}
	return true
}

func canonicalPrefixSetKey(prefixes []string) string {
	copyPrefixes := append([]string(nil), prefixes...)
	sort.Strings(copyPrefixes)
	return strings.Join(copyPrefixes, ",")
}
