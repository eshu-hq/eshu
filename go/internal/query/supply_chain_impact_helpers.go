// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// requestedSupplyChainImpactProfile reads the `profile` query parameter,
// rejects unknown values with a 400, and defaults to precise. `precise`
// returns only findings with an exact installed-version anchor.
// `comprehensive` returns every owned-anchor finding, including range-only,
// SBOM/CPE-derived, malformed, and missing-version rows.
func requestedSupplyChainImpactProfile(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.TrimSpace(QueryParam(r, "profile"))
	if raw == "" {
		return SupplyChainImpactProfilePrecise, true
	}
	switch raw {
	case SupplyChainImpactProfilePrecise, SupplyChainImpactProfileComprehensive:
		return raw, true
	default:
		WriteError(w, http.StatusBadRequest, "profile must be precise or comprehensive")
		return "", false
	}
}

// filterProfile maps the requested API profile to the on-row filter value.
// `comprehensive` matches every row, so the filter remains blank to avoid
// adding an unneeded predicate.
func filterProfile(profile string) string {
	if profile == SupplyChainImpactProfilePrecise {
		return SupplyChainImpactProfilePrecise
	}
	return ""
}

// requiredSupplyChainImpactFindingLimit enforces an explicit, bounded page size
// for the impact findings surface.
func requiredSupplyChainImpactFindingLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > supplyChainImpactFindingMaxLimit {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", supplyChainImpactFindingMaxLimit))
		return 0, false
	}
	return limit, true
}

// isSupportedSupplyChainSuppressionState reports whether the value names a
// known reducer suppression state.
func isSupportedSupplyChainSuppressionState(state string) bool {
	switch state {
	case "active",
		"not_affected",
		"accepted_risk",
		"false_positive",
		"ignored",
		"expired",
		"provider_dismissed",
		"scope_mismatch":
		return true
	default:
		return false
	}
}

// parseSupplyChainImpactIncludeSuppressed parses the optional
// include_suppressed boolean. Default false, so callers see only findings the
// reducer considers actionable. Anything other than true/false returns 400.
func parseSupplyChainImpactIncludeSuppressed(w http.ResponseWriter, r *http.Request) (bool, bool) {
	raw := QueryParam(r, "include_suppressed")
	if raw == "" {
		return false, true
	}
	switch raw {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		WriteError(w, http.StatusBadRequest, "include_suppressed must be true or false")
		return false, false
	}
}
