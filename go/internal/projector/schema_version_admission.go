// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// validateFactSchemaVersion rejects a core-owned fact that carries an
// unsupported schema version before it is projected. It gates every core fact
// family uniformly through the central facts schema-version registry
// (facts.ValidateSchemaVersion) instead of the previous per-family validators,
// so a new versioned family is covered automatically. Fact kinds core does not
// own a versioned schema for — including out-of-tree component facts — pass
// through unchanged.
func validateFactSchemaVersion(fact facts.Envelope) error {
	if err := facts.ValidateSchemaVersion(fact.FactKind, fact.SchemaVersion); err != nil {
		return fmt.Errorf("fact %q: %w", fact.FactID, err)
	}
	return nil
}
