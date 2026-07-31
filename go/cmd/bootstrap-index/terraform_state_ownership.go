// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend/canonicalwriter"
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

// ResolveOwningRepoID implements sourcecypher.TerraformStateOwnershipResolver
// by delegating to canonicalwriter.ResolveOwningRepoIDOutcome (#5623 P1
// review, second finding), the single place that classifies a resolution
// result into (repoID, outcome) shared by every cmd/* wiring site --
// cmd/ingester and cmd/projector's own terraform_state_ownership.go call the
// identical function. Do not reimplement the classification here: see
// canonicalwriter's own doc comment for why a query failure, an unowned
// backend, and an ambiguously-owned backend must NOT collapse into the same
// outcome.
func (r bootstrapTerraformStateOwnershipResolver) ResolveOwningRepoID(
	ctx context.Context, backendKind, locatorHash string,
) (string, projector.TerraformStateOwnershipOutcome) {
	return canonicalwriter.ResolveOwningRepoIDOutcome(ctx, r.resolver, backendKind, locatorHash)
}
