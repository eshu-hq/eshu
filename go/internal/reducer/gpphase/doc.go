// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gpphase owns the identity vocabulary for graph-projection
// readiness: which conflict domain a write belongs to ([Keyspace]), which
// durable milestone it has reached ([Phase]), the bounded slice identity that
// names one readiness fact ([PhaseKey] and its [PhaseKey.Validate]), and the
// two function shapes a domain family uses to read readiness
// ([ReadinessLookup], [ReadinessPrefetch]).
//
// # Why this is a leaf
//
// Domain families gate their graph writes on a keyspace having reached a
// phase — for example the crossrepo family only replays backward-evidence
// rows once [KeyspaceCrossRepoEvidence] has reached
// [PhaseBackwardEvidenceCommitted] for the relevant scope. Before this
// package existed those five symbols lived in the reducer root beside the
// phase-publishing and repair machinery, so a family that only wanted to read
// or construct a readiness key had to import the root — and the root imports
// the families. That import cycle is the single reason issue #6061 could not
// move the crossrepo family: three symbols from one file blocked it.
//
// This package therefore holds only plain data, constants, and one pure
// validation method. It imports nothing but the standard library, and it must
// never import the reducer root.
//
// # What deliberately stays at the root
//
// PhaseState (one durable readiness publication) and the
// GraphProjectionPhasePublisher interface that persists it stay in
// `graph_projection_phase.go` at the root: they are read and written by the
// phase-publish and phase-repair machinery across roughly two dozen files,
// none of which need to become a leaf subpackage today, and PhaseState adds
// no identity concept beyond what [PhaseKey] and [Phase] already carry. The
// EndpointPresenceRow/Writer/Lookup trio also stays at the root — it is a
// distinct uid-exact, cross-scope presence primitive (issue #1380), not a
// same-scope/same-generation readiness fact, and no family needs it to move.
//
// The root keeps aliases under the original names — GraphProjectionKeyspace,
// GraphProjectionPhase, GraphProjectionPhaseKey, GraphProjectionReadinessLookup,
// GraphProjectionReadinessPrefetch, plus one alias per constant — so no caller
// changed when this moved.
package gpphase
