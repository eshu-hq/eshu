// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonicalwriter

import (
	"context"
	"errors"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// ResolveOwningRepoIDOutcome adapts *tfstatebackend.Resolver.ResolveConfigCommitForBackend
// to the (repoID string, outcome projector.TerraformStateOwnershipOutcome)
// contract every cmd/* canonical-writer wiring site's own
// sourcecypher.TerraformStateOwnershipResolver implementation needs (#5623 P1
// review, second finding). See this package's doc.go and README.md for why
// the classification lives here rather than in tfstatebackend or projector
// directly (a real import cycle, not a style choice).
//
// Centralizing this mapping matters because cmd/bootstrap-index,
// cmd/ingester, and cmd/projector cannot import one another (three separate
// "main" packages), so before this package each carried its own
// byte-identical copy. A prior version of that copy collapsed
// tfstatebackend.ErrNoConfigRepoOwnsBackend, tfstatebackend.ErrAmbiguousBackendOwner,
// and every OTHER error (a genuinely transient Postgres timeout or pool
// exhaustion) into the same "not resolved" signal, discarding exactly the
// distinction the #5623 scoped-token predicate's MATCHES_STATE retract needs:
// a transient failure must preserve any existing MATCHES_STATE edge (we don't
// know this cycle), while the two authoritative sentinels must retract it (we
// DO know this cycle that no repo, or more than one repo, uniquely owns this
// backend). Every cmd/* adapter now delegates here, so the three call sites
// cannot drift out of consistency with each other again.
//
// Logs a warning for a genuine transient failure only -- never for the two
// documented, expected "no owner" / "ambiguous owner" outcomes, matching
// every prior per-adapter copy's own behavior (those are common, honest
// answers, not operational problems).
func ResolveOwningRepoIDOutcome(
	ctx context.Context, resolver *tfstatebackend.Resolver, backendKind, locatorHash string,
) (repoID string, outcome projector.TerraformStateOwnershipOutcome) {
	anchor, err := resolver.ResolveConfigCommitForBackend(ctx, backendKind, locatorHash)
	if err != nil {
		if errors.Is(err, tfstatebackend.ErrNoConfigRepoOwnsBackend) {
			return "", projector.TerraformStateOwnershipNoOwner
		}
		if errors.Is(err, tfstatebackend.ErrAmbiguousBackendOwner) {
			return "", projector.TerraformStateOwnershipAmbiguousOwner
		}
		slog.WarnContext(ctx, "terraform state backend ownership resolution failed",
			"backend_kind", backendKind, "locator_hash", locatorHash, "error", err)
		return "", projector.TerraformStateOwnershipTransientFailure
	}
	if anchor.RepoID == "" {
		// Defensive, not a documented outcome: ResolveConfigCommitForBackend's
		// own contract says a nil error always carries a populated
		// CommitAnchor (the "single-owner: pick the latest sealed snapshot"
		// branch always selects a real row). An empty RepoID here would mean
		// a TerraformBackendQuery implementation returned a row with an empty
		// RepoID, which nothing in tfstatebackend's contract permits. Treat
		// it the same as a transient failure -- conservative, not a guess --
		// rather than assume it means "no owner" or fabricate a resolved
		// answer from data that should not be possible.
		return "", projector.TerraformStateOwnershipTransientFailure
	}
	return anchor.RepoID, projector.TerraformStateOwnershipResolved
}
