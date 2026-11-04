// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachability

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Derivation is the decoded derive section of one Go reachability activation's
// config block. Unknown keys are ignored, as they are for the vulnerability
// derive sections, because the block also reaches the component verbatim.
type Derivation struct {
	DeriveFromGoModules GoModulesSection `json:"derive_from_go_modules"`
}

// GoModulesSection turns the derivation on. The page size and ecosystem are
// fixed by the scanner contract, so there is nothing else to configure; the
// operator sets the cadence with the instance's scan_interval.
type GoModulesSection struct {
	Enabled bool `json:"enabled"`
}

// Enabled reports whether the coordinator plans repository scopes for the
// activation.
func (d Derivation) Enabled() bool {
	return d.DeriveFromGoModules.Enabled
}

// DerivationFromConfig decodes one activation's opaque config block. An absent
// block decodes to nothing enabled; a section of the wrong shape is an error, so
// it fails at startup instead of silently planning nothing.
func DerivationFromConfig(raw json.RawMessage) (Derivation, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return Derivation{}, nil
	}
	var decoded Derivation
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return Derivation{}, fmt.Errorf("decode derive_from_go_modules config: %w", err)
	}
	return decoded, nil
}
