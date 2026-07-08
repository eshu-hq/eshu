// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

const projectorPersistedVersionlessSchemaVersion = factenvelope.PersistedVersionlessSchemaVersion

// validateFactSchemaVersion rejects a core-owned fact that carries an
// unsupported schema version before it is projected. It gates every core fact
// family uniformly through the central facts schema-version registry
// (facts.ValidateSchemaVersion) instead of the previous per-family validators,
// so a new versioned family is covered automatically. Fact kinds core does not
// own a versioned schema for — including out-of-tree component facts — pass
// through unchanged.
func validateFactSchemaVersion(fact facts.Envelope) error {
	if err := validateCodegraphFactSchemaVersion(fact); err != nil {
		return fmt.Errorf("fact %q: %w", fact.FactID, err)
	}
	if err := facts.ValidateSchemaVersion(fact.FactKind, fact.SchemaVersion); err != nil {
		return fmt.Errorf("fact %q: %w", fact.FactID, err)
	}
	return nil
}

func validateCodegraphFactSchemaVersion(fact facts.Envelope) error {
	switch NormalizeFactKind(fact.FactKind) {
	case factschema.FactKindCodegraphFile, factschema.FactKindCodegraphRepository:
	default:
		return nil
	}

	version := strings.TrimSpace(fact.SchemaVersion)
	// "" is a collector-emitted version-less fact; projectorPersistedVersionlessSchemaVersion
	// ("0.0.0") is the sentinel the Postgres persist layer stamps for one
	// (emptyToDefault in facts_streaming.go). The git collector emits file and
	// repository facts with no SchemaVersion, so a fact LOADED for projection
	// carries "0.0.0" — the admission gate must accept it, mirroring the decode
	// adapter's factschemaEnvelope normalization and the reducer's #4753 fix.
	// Rejecting it here (as #4899 did) dead-letters every real version-less
	// file/repository fact before projection, so no generation ever activates.
	if version == "" || version == projectorPersistedVersionlessSchemaVersion || strings.HasPrefix(version, "1.") {
		return nil
	}
	return fmt.Errorf("%w: %q", factschema.ErrUnsupportedSchemaMajor, version)
}
