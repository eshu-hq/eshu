// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import "time"

// CollectorKind is the durable collector family name for submodule facts. It
// is recorded in fact provenance, source references, and telemetry
// attributes, so it must stay stable once published.
const CollectorKind = "submodule"

// Entry is one parsed ".gitmodules" submodule declaration: a `[submodule
// "name"]` section carrying both a "path" and a "url" key (see Parse). The
// section's quoted name itself is not carried here — it is git-config
// bookkeeping, not part of the join identity a consumer keys off (see
// sdk/go/factschema/submodule/v1.Pin).
type Entry struct {
	// Path is the section's "path = ..." value: the submodule's location
	// within the parent repository's tree.
	Path string
	// URL is the section's "url = ..." value: the raw configured remote for
	// the submodule, exactly as declared (not yet resolved to a repo_id).
	URL string
}

// FixtureContext carries the collector boundary fields copied into every
// emitted submodule fact envelope: scope, generation, collector instance,
// fencing token, observed time, and the source ".gitmodules" URI. It mirrors
// the codeowners collector's FixtureContext shape (issue #5420 Phase 2a).
type FixtureContext struct {
	// ScopeID is the durable ingestion scope the ".gitmodules" file belongs
	// to.
	ScopeID string
	// GenerationID is the scope generation the facts are emitted under.
	GenerationID string
	// CollectorInstanceID identifies the producing collector instance.
	// Optional: it is operational metadata about the run, not part of the
	// submodule pin's join identity (see
	// sdk/go/factschema/submodule/v1.Pin).
	CollectorInstanceID *string
	// FencingToken orders emissions from the same collector instance.
	FencingToken int64
	// ObservedAt is the file observation time; zero defaults to now (UTC).
	ObservedAt time.Time
	// SourceURI is the repo-relative ".gitmodules" path emitted into every
	// envelope's SourceRef.SourceURI.
	SourceURI string
	// PinnedSHAResolver resolves one submodule's pinned gitlink commit SHA
	// (issue #5420 Phase 2b) by its ".gitmodules" "path" value. It returns
	// nil when the path carries no gitlink tree entry (declared but never
	// added, or the path now names something else) — the resolver must
	// never guess. Optional: nil (the zero value) leaves every emitted
	// Pin.PinnedSHA nil, matching Phase 2a's behavior, since a caller with
	// no repository tree to read (for example a synthetic ".gitmodules"
	// body with no backing repoPath) has nothing to resolve against.
	PinnedSHAResolver func(path string) *string
}
