// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "time"

// Spec kinds a classified version string can take. They are the closed set the
// runtime_declaration.version contract names; readers must tolerate a member
// they do not know (sdk/go/factschema/runtimedeclaration/v1).
const (
	// SpecKindExact is a full major.minor.patch version such as 20.11.1.
	SpecKindExact = "exact"
	// SpecKindPartial is a version with fewer than three components such as 20.
	SpecKindPartial = "partial"
	// SpecKindRange is a comparator expression such as >=18 <21.
	SpecKindRange = "range"
	// SpecKindAlias is a named reference such as lts/iron, node or system.
	// Aliases are never resolved offline.
	SpecKindAlias = "alias"
	// SpecKindUnresolved is a spec holding an unexpanded placeholder such as
	// ${X}. It is reported as written and never guessed.
	SpecKindUnresolved = "unresolved"
)

// Spec is the classification of one declared version string. Major, Minor and
// Patch are set only for SpecKindExact and SpecKindPartial. The Range fields
// are set only for SpecKindRange and never fill Major. Every field is a pointer
// or empty so an absent value stays distinct from zero.
type Spec struct {
	// Kind is one of the SpecKind constants.
	Kind string `json:"kind"`
	// Major, Minor and Patch are the parsed numeric components of an exact or
	// partial version. A leading "v" is dropped from the parts.
	Major *int `json:"major,omitempty"`
	Minor *int `json:"minor,omitempty"`
	Patch *int `json:"patch,omitempty"`
	// RangeLower and RangeUpper are the raw bound text, for example ">=18".
	RangeLower *string `json:"range_lower,omitempty"`
	RangeUpper *string `json:"range_upper,omitempty"`
	// RangeLowerMajor and RangeUpperMajor are the integer major of each bound.
	RangeLowerMajor *int `json:"range_lower_major,omitempty"`
	RangeUpperMajor *int `json:"range_upper_major,omitempty"`
	// RangeLowerInclusive and RangeUpperInclusive say whether each bound
	// includes its major.
	RangeLowerInclusive *bool `json:"range_lower_inclusive,omitempty"`
	RangeUpperInclusive *bool `json:"range_upper_inclusive,omitempty"`
}

// CollectorKind is the durable collector family name for runtime_declaration
// facts. It is recorded in fact provenance, source references, and telemetry
// attributes, so it must stay stable once published.
const CollectorKind = "runtime_declaration"

// FixtureContext carries the collector boundary fields copied into every
// emitted runtime_declaration fact envelope: scope, generation, collector
// instance, fencing token, observed time, and the declaring file's URI. It
// mirrors the codeowners collector's FixtureContext shape.
type FixtureContext struct {
	// ScopeID is the durable ingestion scope the declaring file belongs to.
	ScopeID string
	// GenerationID is the scope generation the facts are emitted under.
	GenerationID string
	// CollectorInstanceID identifies the producing collector instance.
	// Optional: it is operational metadata about the run, not part of the
	// declaration (see sdk/go/factschema/runtimedeclaration/v1.Version).
	CollectorInstanceID *string
	// FencingToken orders emissions from the same collector instance.
	FencingToken int64
	// ObservedAt is the file observation time; zero defaults to now (UTC).
	ObservedAt time.Time
	// SourceURI is the repo-relative path of the declaring file emitted into
	// every envelope's SourceRef.SourceURI.
	SourceURI string
}
