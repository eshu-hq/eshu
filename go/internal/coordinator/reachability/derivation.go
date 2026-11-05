// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Derivation is the decoded derive section of one Go reachability activation's
// config block. Unknown keys are ignored, as they are for the vulnerability
// derive sections, because the block also reaches the component verbatim.
type Derivation struct {
	DeriveFromGoModules GoModulesSection `json:"derive_from_go_modules"`
}

// DefaultScanInterval is the plan bucket when the section sets no scan_interval.
const DefaultScanInterval = 24 * time.Hour

// GoModulesSection turns the derivation on and sets its cadence. The page size
// and ecosystem are fixed by the scanner contract.
//
// ScanInterval is a Go duration that widens the plan bucket, blank for
// DefaultScanInterval. It lives here because a component activation has no
// top-level scan_interval: the instance's own interval is the global reconcile
// interval, 30s by default, which would plan a new page every tick and rescan
// repositories almost continuously.
type GoModulesSection struct {
	Enabled      bool   `json:"enabled"`
	ScanInterval string `json:"scan_interval"`
}

// Enabled reports whether the coordinator plans repository scopes for the
// activation.
func (d Derivation) Enabled() bool {
	return d.DeriveFromGoModules.Enabled
}

// Interval returns the plan bucket width. DerivationFromConfig has already
// proven a set value positive.
func (d Derivation) Interval() time.Duration {
	raw := strings.TrimSpace(d.DeriveFromGoModules.ScanInterval)
	if raw == "" {
		return DefaultScanInterval
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return DefaultScanInterval
	}
	return interval
}

// DerivationFromConfig decodes one activation's opaque config block. An absent
// block decodes to nothing enabled. A section of the wrong shape, or a
// scan_interval that is not a positive duration, is an error whether or not the
// section is enabled, so it fails at startup instead of silently planning nothing.
func DerivationFromConfig(raw json.RawMessage) (Derivation, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return Derivation{}, nil
	}
	var decoded Derivation
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return Derivation{}, fmt.Errorf("decode derive_from_go_modules config: %w", err)
	}
	if raw := strings.TrimSpace(decoded.DeriveFromGoModules.ScanInterval); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return Derivation{}, fmt.Errorf("derive_from_go_modules.scan_interval: %w", err)
		}
		if interval <= 0 {
			return Derivation{}, errors.New("derive_from_go_modules.scan_interval must be positive")
		}
	}
	return decoded, nil
}
