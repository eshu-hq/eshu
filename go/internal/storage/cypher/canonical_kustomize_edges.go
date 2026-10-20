// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// This file is a thin adapter over the canonical/node leaf: it owns the
// Executor-adjacent writer surface for the Kustomize overlay edge family
// (the two CanonicalNodeWriter methods), while the leaf owns the overlay row
// type and resolver port (node.KustomizeOverlayRow,
// node.KustomizeOverlayResolver), the Cypher templates, the Go resolution,
// and the statement-free parts (node.KustomizeEdgeParts). Existing callers
// keep compiling against the same method names; only the resolver port's
// package path changed.

// kustomizeExtendsBaseEdgeStatements wraps the canonical/node leaf's
// Statement-free Kustomize parts into executable statements, passing the
// writer's configured overlay resolver through. Fail-closed behavior (nil
// resolver or resolver error yields no edge rebuild) lives in the leaf and is
// preserved exactly.
func (w *CanonicalNodeWriter) kustomizeExtendsBaseEdgeStatements(
	ctx context.Context,
	mat canonical.CanonicalMaterialization,
) []Statement {
	return wrapNodeEdgeParts(node.KustomizeEdgeParts(ctx, mat, w.kustomizeOverlayResolver))
}

// KustomizeOverlayResolverConfigured reports whether the #5445
// KustomizeOverlayResolver is wired on this writer. Mirrors
// TerraformStateResolversConfigured (canonical_node_writer_tfstate_resolvers.go):
// cmd/*-level wiring tests type-assert their constructed
// runtime.CanonicalWriter to *CanonicalNodeWriter and call this accessor to
// prove the deployed construction path actually attaches the resolver, not
// just that the isolated adapter type behaves correctly in unit tests.
func (w *CanonicalNodeWriter) KustomizeOverlayResolverConfigured() bool {
	if w == nil {
		return false
	}
	return w.kustomizeOverlayResolver != nil
}
