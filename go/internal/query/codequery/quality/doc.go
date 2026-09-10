// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package quality implements code-quality inspection for the
// code-family queries: the inspection request, the bounded
// refactoring-candidate scan, and result shaping. It split out of
// package codequery (#6060 naming follow-up) so inspection can be
// read, tested, and changed without pulling in the rest of the code
// surface, and so it can one day move into its own repo.
//
// The split keeps the HTTP handler in codequery/quality_handler.go --
// methods must live in their type's package -- and calls this leaf
// through qualification.
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (querycontract) but NEVER package codequery
// itself and NEVER root package query -- both would create an import
// cycle (codequery calls this leaf, and root aliases codequery).
package quality
