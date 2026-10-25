// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "time"

// InstanceSummary captures the operator-visible durable shape of one
// configured collector runtime instance.
type InstanceSummary struct {
	InstanceID     string    `json:"instance_id"`
	CollectorKind  string    `json:"collector_kind"`
	Mode           string    `json:"mode"`
	Enabled        bool      `json:"enabled"`
	Bootstrap      bool      `json:"bootstrap"`
	ClaimsEnabled  bool      `json:"claims_enabled"`
	DisplayName    string    `json:"display_name,omitempty"`
	LastObservedAt time.Time `json:"last_observed_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	DeactivatedAt  time.Time `json:"deactivated_at,omitempty"`
	// ScanInterval is the instance's configured scan_interval, or zero when
	// it is unset or malformed. The status reader sizes the terminal-failure
	// window from it (#8803). It is not part of any payload.
	ScanInterval time.Duration `json:"-"`
	// ExtensionHosted reports that the instance was activated from a
	// component, so the extension host runs it and its failure classes are
	// extension-defined whatever CollectorKind says (the host takes the kind
	// from the component manifest, so it can be a first-party kind such as
	// jira). The status reader fills it from the instance configuration's
	// component activation marker. It is a boolean, never a label value, and
	// is not part of any payload (#8951).
	ExtensionHosted bool `json:"-"`
}
