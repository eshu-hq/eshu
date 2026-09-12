// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contract defines the dependency-neutral reducer registry vocabulary.
//
// Reducer family packages import these domain constants, intent, result,
// ownership, handler, and container-image identity outcome and fact-kind
// contracts without importing the parent reducer package. The parent package
// aliases this surface for compatibility and retains registry assembly,
// runtime execution, queue behavior, and backend wiring. ParseDomain accepts
// the known validation identifiers, including the three reserved
// non-registrable identifiers; shared-projection constants remain names for
// their dedicated runners.
//
// [ProjectionDomains] (issue #6061) is the complete shared/edge projection
// domain enumeration the reducer root's AllDomains reads for the capability
// surface inventory. [RationaleEvidenceSource] is the rationale EXPLAINS edge
// family's evidence_source, kept separate from the shared-projection
// runner's global source because a promoted edge domain keeps its handler's
// original evidence source. [GraphQueryRunner] is a read-only graph query
// port (context plus a Cypher string in, rows out) that a family or the root
// backfill orchestrator accepts to run bounded lookups without depending on
// any specific graph driver.
package contract
