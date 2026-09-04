// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gpphase owns the identity vocabulary for graph-projection
// readiness — which conflict domain a write belongs to ([Keyspace]), which
// durable milestone it has reached ([Phase]), the bounded slice identity that
// names one readiness fact ([PhaseKey] and its [PhaseKey.Validate]), the
// standard construction of that identity from a scope generation and entity
// keys ([KeyFromScope]), and the two function shapes a domain family uses to
// read readiness ([ReadinessLookup], [ReadinessPrefetch]) — plus, since issue
// #6061's second pass, the publish side: building and writing a readiness
// state ([StateForIntent], [StateForIntentValue], [PublishIntentGraphPhase]),
// and the uid-exact cross-scope presence primitive
// ([EndpointPresenceRow], [EndpointPresenceWriter], [PublishEndpointPresence],
// [EndpointPresenceLookup]).
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
// the families. That import cycle was a blocker for the crossrepo family's
// move in issue #6061: three of its symbols (PhaseKey, ReadinessLookup,
// ReadinessPrefetch) were defined only at the root. It was not the family's
// only obstacle — a trial move of all five crossrepo-prefixed files also
// needed several already-hoisted sibling leaves (sharedintent, contract,
// payloadcore) reached by their own names instead of through the root's
// aliases and forwarders — but those were mechanical import/call-site
// rewrites against symbols that already had a leaf home; this package is
// what had none.
//
// [PhaseState] moved here first, for the same reason [KeyFromScope] did — a
// family subpackage has to name the type to accept a publisher without
// importing the reducer root, and the root now aliases it as
// GraphProjectionPhaseState. It stays plain data: [StateForIntent] builds one
// and reports false rather than writing anything.
//
// [KeyFromScope] is a related case: the observability-coverage materialization
// family (issue #6061) only reads readiness — it calls [ReadinessLookup] with
// a key, never [PhasePublisher] — so it needs the identity the root's
// (now-forwarding) graphProjectionPhaseStateForIntent constructs without the
// publish state that wraps it. [KeyFromScope] is that construction extracted
// to this leaf; the root helper now calls it too, so the two call sites
// cannot drift.
//
// # Revision: the publish path moved here too
//
// [PublishIntentGraphPhase], [StateForIntentValue], [EndpointPresenceRow],
// [EndpointPresenceWriter], and [PublishEndpointPresence] also moved here
// (issue #6061), superseding two statements this file used to make: that the
// publish machinery "stays in graph_projection_phase.go at the root...none of
// which need to become a leaf subpackage today", and that the
// EndpointPresenceRow/Writer pair "stays at the root...no family needs it to
// move." Neither was wrong when written — platformfam's local
// publishIntentPhase wrapper (issue #6061, platform_materialization.go),
// built on [StateForIntent] plus its own [PhasePublisher], is exactly the
// per-family pattern those statements pointed callers toward, and it is
// still a valid choice for a family with no shared consumer. What changed is
// scope: these five symbols (four hoisted here, plus StateForIntent already
// here) are the last root-owned pieces blocking the ec2, s3, iam, and
// security_group families from splitting out of the reducer root without
// importing it, so a shared home here avoids each of those four families
// re-deriving its own copy of the same ~15-line wrapper platformfam wrote
// once. [PublishIntentGraphPhase] and [PublishEndpointPresence] are the one
// exception to "plain data, constants, and pure builders" below: they
// perform the publish/upsert I/O through the [PhasePublisher] and
// [EndpointPresenceWriter] interfaces a caller supplies — the node
// materializers that call PublishEndpointPresence (CloudResource,
// KubernetesWorkload) still live at the root; only the primitive moved.
//
// This package therefore holds plain data, constants, pure builders, one pure
// validation method, and — since the revision above — two functions that
// publish through a caller-supplied interface. It imports the standard
// library and [reducercontract] (for the [reducercontract.Intent] value type
// [StateForIntentValue] and [PublishIntentGraphPhase] accept), and it must
// never import the reducer root.
//
// # What deliberately stays at the root
//
// The phase-repair machinery (retry / dead-letter on a failed publish) stays
// at the root: it is orchestration over a publisher and a repair queue, not a
// pure builder, and no family needs it today. A future family that does can
// follow the pattern this revision used for the publish path.
//
// The root keeps aliases and forwarders under the original names —
// GraphProjectionKeyspace, GraphProjectionPhase, GraphProjectionPhaseKey,
// GraphProjectionReadinessLookup, GraphProjectionReadinessPrefetch,
// EndpointPresenceRow, EndpointPresenceWriter, plus one alias per constant,
// plus thin forwarder functions for publishIntentGraphPhase,
// graphProjectionPhaseStateForIntent, and publishEndpointPresence — so no
// caller changed when any of this moved.
package gpphase
