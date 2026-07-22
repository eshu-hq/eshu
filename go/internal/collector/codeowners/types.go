// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import "time"

// CollectorKind is the durable collector family name for codeowners facts. It
// is recorded in fact provenance, source references, and telemetry
// attributes, so it must stay stable once published.
const CollectorKind = "codeowners"

// FixtureContext carries the collector boundary fields copied into every
// emitted codeowners fact envelope: scope, generation, collector instance,
// fencing token, observed time, and the resolved CODEOWNERS source URI. It
// mirrors the service-catalog collector's FixtureContext shape.
type FixtureContext struct {
	// ScopeID is the durable ingestion scope the CODEOWNERS file belongs to.
	ScopeID string
	// GenerationID is the scope generation the facts are emitted under.
	GenerationID string
	// CollectorInstanceID identifies the producing collector instance.
	// Optional: it is operational metadata about the run, not part of the
	// ownership claim's identity (see sdk/go/factschema/codeowners/v1.Ownership).
	CollectorInstanceID *string
	// FencingToken orders emissions from the same collector instance.
	FencingToken int64
	// ObservedAt is the file observation time; zero defaults to now (UTC).
	ObservedAt time.Time
	// SourceURI is the repo-relative CODEOWNERS path emitted into every
	// envelope's SourceRef.SourceURI.
	SourceURI string
}
