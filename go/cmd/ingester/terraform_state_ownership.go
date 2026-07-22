// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// ingesterTerraformStateOwnershipResolver adapts *tfstatebackend.Resolver to
// sourcecypher.TerraformStateOwnershipResolver (#5443 P1 re-review finding),
// so the ingester's canonical writer -- the binary that actually runs the
// deployed StatefulSet, per cmd/ingester/README.md -- scopes the
// MATCHES_STATE edge the exact same way cmd/projector does
// (cmd/projector/terraform_state_ownership.go). cmd/projector exists only for
// focused local verification and Compose debugging; wiring this resolver
// there alone left MATCHES_STATE permanently unwritten in production because
// no Helm template deploys cmd/projector.
type ingesterTerraformStateOwnershipResolver struct {
	resolver *tfstatebackend.Resolver
}

// ResolveOwningRepoID implements sourcecypher.TerraformStateOwnershipResolver.
// A query failure, an unowned backend (ErrNoConfigRepoOwnsBackend), and an
// ambiguously-owned backend (ErrAmbiguousBackendOwner) all resolve to
// ("", false) -- never a guess. A query failure is logged and otherwise
// treated the same as "not resolved this cycle": the resolution reruns every
// generation, so a transient failure only delays the MATCHES_STATE edge, it
// never fabricates one.
func (r ingesterTerraformStateOwnershipResolver) ResolveOwningRepoID(
	ctx context.Context, backendKind, locatorHash string,
) (string, bool) {
	anchor, err := r.resolver.ResolveConfigCommitForBackend(ctx, backendKind, locatorHash)
	if err != nil {
		if !errors.Is(err, tfstatebackend.ErrNoConfigRepoOwnsBackend) && !errors.Is(err, tfstatebackend.ErrAmbiguousBackendOwner) {
			slog.WarnContext(ctx, "terraform state backend ownership resolution failed",
				"backend_kind", backendKind, "locator_hash", locatorHash, "error", err)
		}
		return "", false
	}
	if anchor.RepoID == "" {
		return "", false
	}
	return anchor.RepoID, true
}
