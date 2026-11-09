// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Advisory-group member-set helpers (#10408): the alias-connected component
// a finding's record belongs to, read from
// payload.provenance.advisory_group_members and unioned into the changed and
// recovery CVE sets that feed the holder fan-out. The touched set itself
// stays coalesced group ids only (#10411 contract, consumed verbatim by the
// graph projection); member ids never enter it.

// parseAdvisoryGroupMembers decodes the advisory-group member set the
// stored-finding and recovery selects project from
// payload.provenance.advisory_group_members (#10408). A missing, null, or
// empty array reads as nil. Malformed JSON fails the pass so a corrupt row
// can never silently narrow the fan-out.
func parseAdvisoryGroupMembers(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return nil, nil
	}
	var members []string
	if err := json.Unmarshal([]byte(trimmed), &members); err != nil {
		return nil, fmt.Errorf("decode advisory group members: %w", err)
	}
	out := make([]string, 0, len(members))
	for _, member := range members {
		if member = strings.TrimSpace(member); member != "" {
			out = append(out, member)
		}
	}
	return out, nil
}

// addMemberIDs contributes a row's group id and advisory-group member ids to
// a set (#10408), blanks dropped. A member-less row falls back to its
// singleton id, so ungrouped findings keep per-record keys.
func addMemberIDs(set map[string]struct{}, cveID string, members []string) {
	if id := strings.TrimSpace(cveID); id != "" {
		set[id] = struct{}{}
	}
	for _, member := range members {
		if id := strings.TrimSpace(member); id != "" {
			set[id] = struct{}{}
		}
	}
}

// memberSetsEqual reports whether two member id sets name the same members,
// order-insensitive; nil and empty both read as empty.
func memberSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, id := range a {
		seen[strings.TrimSpace(id)]++
	}
	for _, id := range b {
		key := strings.TrimSpace(id)
		seen[key]--
		if seen[key] < 0 {
			return false
		}
	}
	return true
}
