// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

// TerraformStateOwnershipOutcome classifies how a Terraform state resource's
// backend-ownership resolution concluded THIS materialization cycle (#5623 P1
// review, second finding). It exists because collapsing every non-resolved
// case into a single "not resolved" signal (OwningRepoID == "") makes it
// impossible for a downstream consumer -- specifically
// go/internal/storage/cypher's terraformStateMatchesConfigEdgeRetractStatements
// -- to tell a TRANSIENT resolution failure (an ordinary Postgres timeout or
// pool exhaustion; we simply don't know this cycle, so any existing
// MATCHES_STATE edge must be left alone) apart from an AUTHORITATIVE "not
// uniquely owned" answer (no config repo declares this backend, or more than
// one does; we DO know this cycle, and any existing MATCHES_STATE edge must
// be retracted because it may point at a repo that no longer -- or never
// uniquely -- owns this backend).
//
// The zero value (TerraformStateOwnershipTransientFailure) is the safe
// default: a row that never reaches a resolver at all (no
// TerraformStateOwnershipResolver wired, or the row's backend identity is
// blank) carries this value without any code needing to set it explicitly,
// and every consumer treats it exactly like a genuine transient resolver
// error -- preserve, never retract, never guess.
type TerraformStateOwnershipOutcome int

const (
	// TerraformStateOwnershipTransientFailure is the zero value. It covers
	// two cases a downstream retract decision MUST treat identically:
	// resolution was never attempted this cycle (no resolver wired, or the
	// row's BackendKind/LocatorHash is blank), or it was attempted and failed
	// for a reason that carries no information about actual ownership (a
	// query error other than the two documented authoritative sentinels
	// below). Neither case is an authoritative answer: an existing
	// MATCHES_STATE edge from a prior generation must survive untouched,
	// since this cycle's silence says nothing about whether that edge is
	// still correct.
	TerraformStateOwnershipTransientFailure TerraformStateOwnershipOutcome = iota
	// TerraformStateOwnershipResolved means OwningRepoID names the single
	// config repo that owns this backend this cycle. May be the SAME repo as
	// a prior generation (no-op for any existing edge) or a DIFFERENT one (a
	// stale edge to the prior owner must be retracted).
	TerraformStateOwnershipResolved
	// TerraformStateOwnershipNoOwner means resolution completed and
	// authoritatively found no config repo declaring this backend
	// (tfstatebackend.ErrNoConfigRepoOwnsBackend). An existing MATCHES_STATE
	// edge must be retracted: whatever repo owned this backend before no
	// longer does.
	TerraformStateOwnershipNoOwner
	// TerraformStateOwnershipAmbiguousOwner means resolution completed and
	// authoritatively found more than one distinct repo declaring this
	// backend (tfstatebackend.ErrAmbiguousBackendOwner). An existing
	// MATCHES_STATE edge must be retracted for the same reason as NoOwner:
	// ownership is not uniquely determined this cycle, so no single repo's
	// grant should be authorized by a stale edge to a repo that is, at best,
	// one of several competing claimants.
	TerraformStateOwnershipAmbiguousOwner
)
