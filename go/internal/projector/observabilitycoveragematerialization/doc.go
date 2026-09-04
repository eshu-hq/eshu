// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observabilitycoveragematerialization builds the reducer intent that
// projects a scope generation's observability coverage decisions into canonical
// COVERS graph edges (issue #391).
//
// It is the materialization half of observability coverage; the correlation
// half lives in the sibling internal/projector/observabilitycoverage. The two
// are separate packages rather than one because each family in this series
// exports exactly one builder, and the sibling's scoped AGENTS.md pins that
// rule explicitly.
//
// observabilityResourceTypes here is one leg of a three-way mirror: the sibling
// correlation package keeps the same closed set, and both mirror the reducer's
// observabilityResourceSignals. A resource type added to one copy must be added
// to all three.
//
// The package consumes the neutral internal/projector/intent contract and must
// not import the root projector package; root imports it to dispatch.
package observabilitycoveragematerialization
