// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// bootstrapTerraformStateOwnershipResolver adapts *tfstatebackend.Resolver to
// sourcecypher.TerraformStateOwnershipResolver (#5443), mirroring
// cmd/projector/terraform_state_ownership.go and
// cmd/ingester/terraform_state_ownership.go. bootstrap-index performs the
// one-shot initial index: without this resolver wired, the first full index
// would produce zero MATCHES_STATE edges and nothing would backfill them
// until a later ingester cycle re-processes every Terraform state resource,
// which is not guaranteed to happen soon (or at all, for a
// local/Compose-only deployment that only ever runs bootstrap-index).
type bootstrapTerraformStateOwnershipResolver struct {
	resolver *tfstatebackend.Resolver
}

// ResolveOwningRepoID implements sourcecypher.TerraformStateOwnershipResolver.
// A query failure, an unowned backend (ErrNoConfigRepoOwnsBackend), and an
// ambiguously-owned backend (ErrAmbiguousBackendOwner) all resolve to
// ("", false) -- never a guess. A query failure is logged and otherwise
// treated the same as "not resolved this cycle": the resolution reruns every
// generation, so a transient failure only delays the MATCHES_STATE edge, it
// never fabricates one.
func (r bootstrapTerraformStateOwnershipResolver) ResolveOwningRepoID(
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
