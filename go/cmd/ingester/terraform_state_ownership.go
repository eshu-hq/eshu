// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend/canonicalwriter"
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

// ResolveOwningRepoID implements sourcecypher.TerraformStateOwnershipResolver
// by delegating to canonicalwriter.ResolveOwningRepoIDOutcome (#5623 P1
// review, second finding), the single place that classifies a resolution
// result into (repoID, outcome) shared by every cmd/* wiring site --
// cmd/bootstrap-index and cmd/projector's own terraform_state_ownership.go
// call the identical function. Do not reimplement the classification here:
// see canonicalwriter's own doc comment for why a query failure, an unowned
// backend, and an ambiguously-owned backend must NOT collapse into the same
// outcome.
func (r ingesterTerraformStateOwnershipResolver) ResolveOwningRepoID(
	ctx context.Context, backendKind, locatorHash string,
) (string, projector.TerraformStateOwnershipOutcome) {
	return canonicalwriter.ResolveOwningRepoIDOutcome(ctx, r.resolver, backendKind, locatorHash)
}
