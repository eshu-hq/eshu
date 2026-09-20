// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "fmt"

// LatencyExemption is a tracked, temporary carve-out from a route's latency
// ceiling. Issue and Reason are both mandatory: an exemption with no issue
// reference has nothing that will ever remove it, so ValidateLatencyExemptions
// rejects it. An exemption never covers a HardFailed (5xx) sample -- that
// always breaches the gate regardless of any entry here -- and it never
// covers the route's Postgres work budget, which is the mechanism this gate
// actually relies on to catch a regression (see the read-API work-metric
// evidence note). Only the latency ceiling becomes advisory.
type LatencyExemption struct {
	// Issue is the tracking reference (e.g. "#6858") whose resolution is
	// expected to remove this entry.
	Issue string
	// Reason states, in one sentence, why the route cannot meet its ceiling
	// today.
	Reason string
}

// LatencyExemptions is the closed, hand-curated set of routes whose latency
// ceiling is advisory rather than blocking. It is empty: an unlisted route
// is never exempt, so this map cannot widen coverage by omission -- only an
// explicit, reviewed addition can.
//
// GET /api/v0/iac/resources was exempt under #6858 while its unscoped path
// read the active-inventory CTE; that issue moved the unscoped search and
// summary onto infra_resource_entities, so the grant is removed and the 2s
// ceiling blocks again.
var LatencyExemptions = map[string]LatencyExemption{}

// ValidateLatencyExemptions rejects any entry with an empty Issue or Reason.
// An untracked exemption can never be found and removed, so the gate refuses
// to start with one rather than silently granting a permanent pass.
func ValidateLatencyExemptions(exemptions map[string]LatencyExemption) error {
	for route, ex := range exemptions {
		if ex.Issue == "" {
			return fmt.Errorf("latency exemption for %s has no issue reference", route)
		}
		if ex.Reason == "" {
			return fmt.Errorf("latency exemption for %s has no reason", route)
		}
	}
	return nil
}
