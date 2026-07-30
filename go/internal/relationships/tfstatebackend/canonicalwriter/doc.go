// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package canonicalwriter adapts tfstatebackend.Resolver's backend-ownership
// resolution to the (repoID string, outcome projector.TerraformStateOwnershipOutcome)
// contract cmd/{bootstrap-index,ingester,projector}'s canonical-writer wiring
// needs to implement sourcecypher.TerraformStateOwnershipResolver (#5623 P1
// review, second finding).
//
// This package exists ONLY to break an import cycle: package projector
// (owner of TerraformStateOwnershipOutcome) transitively imports package
// tfstatebackend already (projector -> reducer ->
// correlation/drift/tfconfigstate -> relationships/tfstatebackend), so
// tfstatebackend itself cannot import projector back without creating one.
// This package sits above both, importing each directly, so it can classify
// a resolution result into the outcome enum without either of its two
// dependencies needing to know about the other.
package canonicalwriter
