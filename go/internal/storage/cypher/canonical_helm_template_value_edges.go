// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// This file is a thin adapter over the canonical/node leaf: it owns the
// Statement wrapping for the Helm template-value reference edge family,
// while the leaf owns the Cypher templates, chart-scoped Go resolution, and
// statement-free parts (node.HelmTemplateValueEdgeParts). The explicit append
// in buildStructuralEdgeStatements keeps calling
// helmTemplateValueEdgeStatements, so the family order (atlantis < flux <
// gitlab < helm) and the retract-before-upsert order are preserved exactly.

// helmTemplateValueEdgeStatements wraps the canonical/node leaf's
// Statement-free Helm template-value parts into executable statements.
// Returns nil for non-Helm repos, mirroring the leaf.
func helmTemplateValueEdgeStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapNodeEdgeParts(node.HelmTemplateValueEdgeParts(mat))
}
