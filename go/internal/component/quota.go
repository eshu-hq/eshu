// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import "fmt"

// Quota declares per-component governance bounds (#8471): facts per
// generation and generations per hour. A nil field is unlimited; a set
// field must be positive. The manifest carries the component author's
// defaults; the operator override wins per field (see Resolve). Only the
// generations-per-hour bound is enforced at claim-grant time today, per
// (component, scope) per hour: each scope row independently allows up to
// the limit, so a component with items in N scopes can grant up to N times
// the limit per hour. No per-generation emitted-fact counter exists yet, so
// the facts bound is declared and validated but not runtime-enforced
// (follow-up on #8471).
type Quota struct {
	MaxFactsPerGeneration *int `yaml:"maxFactsPerGeneration,omitempty" json:"maxFactsPerGeneration,omitempty"`
	MaxGenerationsPerHour *int `yaml:"maxGenerationsPerHour,omitempty" json:"maxGenerationsPerHour,omitempty"`
}

// Validate rejects set-but-non-positive bounds. Absent bounds stay valid:
// a component without declared quotas is unthrottled.
func (q Quota) Validate() error {
	if q.MaxFactsPerGeneration != nil && *q.MaxFactsPerGeneration < 1 {
		return fmt.Errorf("spec.quotas.maxFactsPerGeneration must be at least 1")
	}
	if q.MaxGenerationsPerHour != nil && *q.MaxGenerationsPerHour < 1 {
		return fmt.Errorf("spec.quotas.maxGenerationsPerHour must be at least 1")
	}
	return nil
}

// Resolve returns the effective quota: every override field that is set
// wins, every unset field falls back to the manifest declaration. This is
// the owner-decision-1 shape (manifest declares, operator overrides);
// the coordinator builds its claim-path lookup from the result.
func (q Quota) Resolve(override Quota) Quota {
	effective := q
	if override.MaxFactsPerGeneration != nil {
		effective.MaxFactsPerGeneration = override.MaxFactsPerGeneration
	}
	if override.MaxGenerationsPerHour != nil {
		effective.MaxGenerationsPerHour = override.MaxGenerationsPerHour
	}
	return effective
}

// GenerationsPerHourLimit reports the bound and whether one is set.
func (q Quota) GenerationsPerHourLimit() (int, bool) {
	if q.MaxGenerationsPerHour == nil {
		return 0, false
	}
	return *q.MaxGenerationsPerHour, true
}

// FactsPerGenerationLimit reports the bound and whether one is set.
func (q Quota) FactsPerGenerationLimit() (int, bool) {
	if q.MaxFactsPerGeneration == nil {
		return 0, false
	}
	return *q.MaxFactsPerGeneration, true
}
