// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend/canonicalwriter"
)

// projectorTerraformStateOwnershipResolver adapts *tfstatebackend.Resolver to
// sourcecypher.TerraformStateOwnershipResolver (#5443), so the canonical
// writer's MATCHES_STATE edge scoping reuses the exact same backend-ownership
// selection the drift-correlation reducer already uses
// (cmd/reducer/wiring_handlers.go), rather than re-deriving it.
type projectorTerraformStateOwnershipResolver struct {
	resolver *tfstatebackend.Resolver
}

// ResolveOwningRepoID implements sourcecypher.TerraformStateOwnershipResolver
// by delegating to canonicalwriter.ResolveOwningRepoIDOutcome (#5623 P1
// review, second finding), the single place that classifies a resolution
// result into (repoID, outcome) shared by every cmd/* wiring site --
// cmd/bootstrap-index and cmd/ingester's own terraform_state_ownership.go
// call the identical function. Do not reimplement the classification here:
// see canonicalwriter's own doc comment for why a query failure, an unowned
// backend, and an ambiguously-owned backend must NOT collapse into the same
// outcome.
func (r projectorTerraformStateOwnershipResolver) ResolveOwningRepoID(
	ctx context.Context, backendKind, locatorHash string,
) (string, projector.TerraformStateOwnershipOutcome) {
	return canonicalwriter.ResolveOwningRepoIDOutcome(ctx, r.resolver, backendKind, locatorHash)
}
