// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package payloadcore holds the generic fact-payload accessors and string
// helpers shared by the parent reducer package and its domain-family
// subpackages.
//
// Every symbol here is generic in the strict sense used by the package
// restructure: it would still be meaningful if the family whose file it used to
// live in were deleted. That is the whole criterion. Some symbols do name a
// domain concept — OCIRepositoryID composes a registry reference,
// SupplyChainWorkloadIDsFromPayload is scoped to supply-chain workload keys,
// SourceOrderKeyField names the field the graph owner gate reads — and they
// qualify anyway, because none of them depends on a handler, writer, queue,
// graph, or storage type. What disqualifies a symbol is a dependency, not a
// noun in its name. This package imports internal/facts and nothing else
// outside the standard library.
//
// Several accessors overlap in signature but not in behavior, and the
// differences are load-bearing. PayloadStr renders any value and treats a
// "<nil>" rendering as absent; PayloadString renders any value but only maps a
// real nil to absent; SemanticPayloadString accepts a real string and nothing
// else. PayloadBool accepts a bool or a "true"/"false" string, while
// BoolPayload accepts only a bool. Callers depend on these distinctions, so
// they are not candidates for consolidation.
package payloadcore
