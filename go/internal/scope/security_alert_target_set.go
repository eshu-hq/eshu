// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// Payload keys of a security-alert generation's recorded target set in
// scope_generations.payload (#8838). The ingestion store writes them and the
// security-alert collection-state read reads them, so both name these
// constants.
const (
	// SecurityAlertTargetSetPayloadKey holds the canonical JSON array of slugs.
	SecurityAlertTargetSetPayloadKey = "security_alert_target_set"
	// SecurityAlertTargetSetHashPayloadKey holds the set's Hash.
	SecurityAlertTargetSetHashPayloadKey = "security_alert_target_set_hash"
)

// SecurityAlertTargetSet is the set of owner/name repository slugs one
// security-alert target generation collected (#8838): the repository of a
// repository target, or the organization's allowlisted repositories of an
// organization target. The ingestion store records it in the generation
// payload, so the query path can tell a repository the generation collected
// from one added to the target after it.
//
// It holds the canonical JSON array (sorted, deduplicated), not a slice, so
// ScopeGeneration stays comparable. The zero value records no set; a set built
// from no slugs is a recorded empty set.
type SecurityAlertTargetSet struct {
	encoded string
}

// NewSecurityAlertTargetSet returns the canonical set of slugs. It trims each
// slug, drops blanks and duplicates, and sorts the rest. Callers pass slugs
// already normalized the way the collector normalizes them (lower case, no
// .git suffix); this constructor does not re-normalize.
func NewSecurityAlertTargetSet(slugs []string) SecurityAlertTargetSet {
	canonical := make([]string, 0, len(slugs))
	for _, slug := range slugs {
		if slug = strings.TrimSpace(slug); slug != "" {
			canonical = append(canonical, slug)
		}
	}
	slices.Sort(canonical)
	canonical = slices.Compact(canonical)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		// A []string always marshals; keep the zero value if it ever fails so
		// no partial set is recorded.
		return SecurityAlertTargetSet{}
	}
	return SecurityAlertTargetSet{encoded: string(encoded)}
}

// IsZero reports whether no set was recorded.
func (s SecurityAlertTargetSet) IsZero() bool {
	return s.encoded == ""
}

// JSON returns the canonical JSON array, or "" when no set was recorded.
func (s SecurityAlertTargetSet) JSON() string {
	return s.encoded
}

// Slugs returns a fresh copy of the sorted slugs, or nil when no set was
// recorded.
func (s SecurityAlertTargetSet) Slugs() []string {
	if s.IsZero() {
		return nil
	}
	var slugs []string
	if err := json.Unmarshal([]byte(s.encoded), &slugs); err != nil {
		return nil
	}
	return slugs
}

// Hash returns "sha256:" plus the hex SHA-256 of the canonical JSON array, or
// "" when no set was recorded. Equal sets hash equal whatever order or
// duplicates they were built from.
func (s SecurityAlertTargetSet) Hash() string {
	if s.IsZero() {
		return ""
	}
	sum := sha256.Sum256([]byte(s.encoded))
	return "sha256:" + hex.EncodeToString(sum[:])
}
