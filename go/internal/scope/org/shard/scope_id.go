// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import "strings"

const (
	// Marker separates a base scope id from a shard's bit prefix. An operator
	// scope_id must not contain it (HasMarker), so a generated shard id
	// can never collide with a configured target.
	Marker = "#shard-"
	// MaxSuffixLength is the most bytes a shard id adds to its base scope id:
	// the marker plus MaxDepth bits. It never carries a repository name.
	MaxSuffixLength = len(Marker) + MaxDepth
)

// ScopeID returns the ingestion scope id of the shard with bit prefix bits
// under baseScopeID.
func ScopeID(baseScopeID, bits string) string {
	return strings.TrimSpace(baseScopeID) + Marker + bits
}

// ParseScopeID splits a shard scope id into its base scope id and bit prefix.
// It reports false for a scope id that is not a well-formed shard id: no
// marker, an empty base, more than one marker, or bits that are empty, longer
// than MaxDepth, or not all 0 and 1.
func ParseScopeID(scopeID string) (baseScopeID, bits string, ok bool) {
	scopeID = strings.TrimSpace(scopeID)
	base, rest, found := strings.Cut(scopeID, Marker)
	if !found || base == "" || rest == "" || len(rest) > MaxDepth || strings.Contains(rest, Marker) {
		return "", "", false
	}
	for _, bit := range rest {
		if bit != '0' && bit != '1' {
			return "", "", false
		}
	}
	return base, rest, true
}

// HasMarker reports whether scopeID contains the shard marker. Target
// validation rejects such an operator scope_id, and the query decoder drops it.
func HasMarker(scopeID string) bool {
	return strings.Contains(scopeID, Marker)
}

// NormalizeRepository returns the canonical owner/name form of a repository
// reference: trimmed, lower case, without surrounding slashes or a trailing
// ".git" on the name. It returns "" for anything that is not exactly
// owner/name. The planner, the collector, and the query layer all normalize
// through this one function, so they agree on allowlist membership.
func NormalizeRepository(repository string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	owner := strings.ToLower(strings.TrimSpace(parts[0]))
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git"))
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
}

// NormalizeOrganization returns the canonical form of an organization login:
// trimmed, lower case, without surrounding slashes.
func NormalizeOrganization(organization string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(organization), "/"))
}
