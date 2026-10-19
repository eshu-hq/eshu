// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// This file is a thin adapter over the canonical/node leaf: it owns the
// Statement wrapping for the GitLab CI pipeline edge family, while the leaf
// owns the Cypher templates, entity collection, Go resolution, and
// statement-free parts (node.GitlabEdgeParts). The explicit append in
// buildStructuralEdgeStatements keeps calling gitlabEdgeStatements, so the
// family order (atlantis < flux < gitlab < helm) and the retract-before-upsert
// order are preserved exactly.

// gitlabEdgeStatements wraps the canonical/node leaf's Statement-free GitLab
// parts into executable statements. Returns nil for non-GitLab repos,
// mirroring the leaf.
func gitlabEdgeStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapNodeEdgeParts(node.GitlabEdgeParts(mat))
}
