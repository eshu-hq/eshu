// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// This file held the Postgres-only evidence-boundary disclosures. They moved
// to internal/query/querycontract with lane B2 of #6060, which the impact
// handler family reads; the aliases below keep root callers unchanged.

// PostgresOnlyBoundary records a gap between a Postgres-only reducer domain
// and a graph-sourced story surface. See
// evidence.PostgresOnlyBoundary.
type PostgresOnlyBoundary = evidence.PostgresOnlyBoundary

const boundaryReasonPostgresOnly = evidence.BoundaryReasonPostgresOnly

// attachEvidenceBoundaries adds a non-nil evidence_boundaries field to the
// response map when boundaries exist for the read surface. The implementation
// moved to querycontract for #6060; this wrapper keeps root callers unchanged.
func attachEvidenceBoundaries(data map[string]any, readSurface string) {
	evidence.AttachEvidenceBoundaries(data, readSurface)
}

// evidenceBoundariesFor returns the static Postgres-only boundaries for the
// named read surface. The implementation moved to querycontract for #6060;
// this wrapper keeps root callers unchanged.
func evidenceBoundariesFor(readSurface string) []PostgresOnlyBoundary {
	return evidence.EvidenceBoundariesFor(readSurface)
}
