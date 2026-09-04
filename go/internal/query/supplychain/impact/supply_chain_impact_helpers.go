// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RequestedSupplyChainImpactProfile reads the `profile` query parameter,
// rejects unknown values with a 400, and defaults to precise. `precise`
// returns only findings with an exact installed-version anchor.
// `comprehensive` returns every owned-anchor finding, including range-only,
// SBOM/CPE-derived, malformed, and missing-version rows.
func RequestedSupplyChainImpactProfile(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.TrimSpace(querycontract.QueryParam(r, "profile"))
	if raw == "" {
		return SupplyChainImpactProfilePrecise, true
	}
	switch raw {
	case SupplyChainImpactProfilePrecise, SupplyChainImpactProfileComprehensive:
		return raw, true
	default:
		querycontract.WriteError(w, http.StatusBadRequest, "profile must be precise or comprehensive")
		return "", false
	}
}

// FilterProfile maps the requested API profile to the on-row filter value.
// `comprehensive` matches every row, so the filter remains blank to avoid
// adding an unneeded predicate.
func FilterProfile(profile string) string {
	if profile == SupplyChainImpactProfilePrecise {
		return SupplyChainImpactProfilePrecise
	}
	return ""
}

// RequiredSupplyChainImpactFindingLimit enforces an explicit, bounded page size
// for the impact findings surface.
func RequiredSupplyChainImpactFindingLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := querycontract.QueryParam(r, "limit")
	if raw == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "limit is required")
		return 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 || limit > supplyChainImpactFindingMaxLimit {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", supplyChainImpactFindingMaxLimit))
		return 0, false
	}
	return limit, true
}

// IsSupportedSupplyChainSuppressionState reports whether the value names a
// known reducer suppression state.
func IsSupportedSupplyChainSuppressionState(state string) bool {
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

// ParseSupplyChainImpactIncludeSuppressed parses the optional
// include_suppressed boolean. Default false, so callers see only findings the
// reducer considers actionable. Anything other than true/false returns 400.
func ParseSupplyChainImpactIncludeSuppressed(w http.ResponseWriter, r *http.Request) (bool, bool) {
	raw := querycontract.QueryParam(r, "include_suppressed")
	if raw == "" {
		return false, true
	}
	switch raw {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		querycontract.WriteError(w, http.StatusBadRequest, "include_suppressed must be true or false")
		return false, false
	}
}

// SupplyChainImpactProfilePrecise selects exact installed-version
// anchored findings only. Relocated from root package query's supply_chain.go
// (#6060 lane A): the moved profile helpers above read it and this package
// must not import root, so the declaration lives here and root keeps
// `SupplyChainImpactProfilePrecise = impact.SupplyChainImpactProfilePrecise`
// (see root supply_chain_impact_alias.go).
const SupplyChainImpactProfilePrecise = "precise"

// SupplyChainImpactProfileComprehensive selects every owned-anchor
// finding including range-only manifest, SBOM/CPE-derived,
// malformed range, and missing-version rows. Unsupported matcher
// ecosystems are surfaced by readiness, not as finding rows.
// Relocated from root package query's supply_chain.go (#6060 lane A);
// see SupplyChainImpactProfilePrecise for the alias arrangement.
const SupplyChainImpactProfileComprehensive = "comprehensive"

// supplyChainImpactFindingMaxLimit bounds the impact findings page size.
// Family-local copy of root package query's supply_chain.go constant: that
// home file stays in root (the staying handlers read it there) and this
// package must not import root, so the value is duplicated here and MUST
// stay byte-identical to its root source (same rationale as advisory's
// supplyChainDefaultSchemaMajorVersion).
const supplyChainImpactFindingMaxLimit = 200

// maxSupplyChainRuntimeEnvironmentCandidates bounds the finding-bound
// digest/environment candidates one environment-evidence read confirms.
// Family-local copy of root package query's
// supply_chain_impact_runtime_context_probe.go constant, which derives it
// from supplyChainImpactFindingMaxLimit the same way.
const maxSupplyChainRuntimeEnvironmentCandidates = supplyChainImpactFindingMaxLimit

// compactStrings drops blank entries and trims the rest.
// Family-local copy of root package query's compactStrings (catalog.go):
// that home file stays in root and an unexported root symbol cannot be
// called across a package boundary. MUST stay behavior-identical to its
// root source.
func compactStrings(values []string) []string {
	compacted := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			compacted = append(compacted, trimmed)
		}
	}
	return compacted
}

// firstNonEmptyString returns the first non-blank value.
// Family-local copy of root package query's firstNonEmptyString
// (service_story_dossier.go); see compactStrings for why it is copied.
// MUST stay behavior-identical to its root source.
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// appendUniqueString appends candidate unless blank or already present.
// Family-local copy of root package query's appendUniqueString
// (deployment_trace_consumer_search.go); see compactStrings for why it is
// copied. MUST stay behavior-identical to its root source.
func appendUniqueString(values *[]string, candidate string) {
	if candidate = strings.TrimSpace(candidate); candidate == "" {
		return
	}
	for _, existing := range *values {
		if existing == candidate {
			return
		}
	}
	*values = append(*values, candidate)
}

// stringMapVal extracts a string map from a payload value.
// Family-local copy of root package query's stringMapVal
// (security_alert_reconciliation.go); see compactStrings for why it is
// copied. MUST stay behavior-identical to its root source.
func stringMapVal(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		text := strings.TrimSpace(fmt.Sprint(value))
		if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
