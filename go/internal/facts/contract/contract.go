// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Disposition classifies how a fact kind with no reducer binding is
// consumed. It is meaningful only on a [Kind] that no [Binding] covers;
// the fact-kind-registry generator rejects a kind that carries both a
// binding and a disposition, and a kind that carries neither.
type Disposition string

const (
	// DispositionReadOnly marks a kind consumed by read surfaces (query or
	// storage read models) with no reducer consumption.
	DispositionReadOnly Disposition = "read_only"
	// DispositionProjectorOnly marks a kind consumed by canonical
	// projection with no reducer consumption.
	DispositionProjectorOnly Disposition = "projector_only"
	// DispositionCollectorOnly marks a kind consumed by an existing
	// collector or discovery path with no reducer, canonical projection,
	// or product read-surface consumption.
	DispositionCollectorOnly Disposition = "collector_only"
	// DispositionUnconsumed marks a kind with no known consumer today. It
	// is a disclosure, not a goal state: the kind still needs a real
	// consumer or an owner decision to retire it.
	DispositionUnconsumed Disposition = "unconsumed"
)

// AuthorityMode records how a [Producer] is authorized to emit core-owned
// fact kinds. Canonical ownership never moves with delegation: the
// [Family] owner repository owns the kind either way.
type AuthorityMode string

const (
	// AuthorityCoreOwned marks a producer that is part of the core
	// repository itself (for example the retained Git/parser core or the
	// reducer emitting derived kinds). No grant is involved.
	AuthorityCoreOwned AuthorityMode = "core_owned"
	// AuthorityCoreIssuedGrant marks a first-party producer component
	// authorized by a core-issued producer grant to emit an approved
	// subset of core-owned kinds. Recording the mode here creates no
	// grant and changes no authorization behavior.
	AuthorityCoreIssuedGrant AuthorityMode = "core_issued_grant"
)

// Family is one typed fact-family declaration: the build-time source of
// truth for a family's identity, fact-definition directory, canonical
// owner, kinds, producers, read surface, and lifecycle metadata. The
// fact-kind-registry generator discovers Family values through [Declare]
// with a static, literal-only source scan; see the package documentation
// for the restricted grammar. Field spellings that mirror the legacy
// registry (schema version, reducer domain, projection/admission hooks,
// truth profile, payload schema, deprecation markers) keep their legacy
// semantics byte-for-byte in the generated registry outputs.
type Family struct {
	// ID is the registry family identifier (for example "aws").
	ID string
	// FactDir is the repo-relative directory holding the family's fact
	// definitions. The declaration file must live in this directory.
	FactDir string
	// OwnerRepository is the canonical core owner repository of every
	// kind in the family, independent of which producers may emit them.
	OwnerRepository string
	// LifecycleOwner is the legacy registry lifecycle owner path.
	LifecycleOwner string
	// SchemaVersion is the family default fact schema version. It must
	// stay blank on an admission-exempt family.
	SchemaVersion string
	// AdmissionExempt registers the family for contract metadata without
	// enrolling its kinds in schema-version admission.
	AdmissionExempt bool
	// ReducerDomain is the legacy pipeline reducer-domain identifier.
	// A domain is a pipeline identifier, not family identity: several
	// families may share one domain and one domain may serve several
	// families.
	ReducerDomain string
	// ReducerDir is the legacy registry reducer_dir hint. It is optional
	// and never substitutes for a verified [Binding].
	ReducerDir string
	// ProjectionHook is the legacy canonical projection hook name.
	ProjectionHook string
	// AdmissionHook is the legacy admission hook name.
	AdmissionHook string
	// ReadSurface is the family default product read surface.
	ReadSurface string
	// TruthProfile is the legacy truth profile name.
	TruthProfile string
	// PolicyGate is the legacy policy gate name, required for the
	// optional_semantic truth profile.
	PolicyGate string
	// ProviderKeyIndependent records whether the family's truth needs no
	// provider key.
	ProviderKeyIndependent bool
	// PayloadSchema is the family default repo-relative payload JSON
	// Schema path. Blank means no family default; a kind may still be
	// without any schema registration, which the generated catalog
	// discloses instead of hiding.
	PayloadSchema string
	// DeprecatedIn is the family default deprecation semver marker.
	DeprecatedIn string
	// RemovedIn is the family default removal semver marker. It requires
	// DeprecatedIn to also be set.
	RemovedIn string
	// Kinds declares every kind the family owns, exactly once each.
	Kinds []Kind
	// Producers declares every current producer of the family's kinds.
	// Each entry names the exact emitted-kind subset it produces.
	Producers []Producer
}

// Kind is one fact kind owned by a [Family], with its per-kind overrides
// and, when no reducer [Binding] covers it, its no-reducer disposition.
type Kind struct {
	// Kind is the wire fact kind string.
	Kind string
	// SchemaVersion overrides the family default schema version when
	// non-blank.
	SchemaVersion string
	// ReadSurface overrides the family default read surface when
	// non-blank.
	ReadSurface string
	// PayloadSchema overrides the family default payload schema when
	// non-blank.
	PayloadSchema string
	// DeprecatedIn overrides the family default deprecation marker when
	// non-blank.
	DeprecatedIn string
	// RemovedIn overrides the family default removal marker when
	// non-blank.
	RemovedIn string
	// Disposition classifies consumption when no binding covers the
	// kind. It must be blank on a bound kind.
	Disposition Disposition
	// NoReducerReason explains, in prose, why the kind has no reducer
	// binding. It is required with a disposition and forbidden on a
	// bound kind.
	NoReducerReason string
	// NoReducerEvidence cites the falsifiable evidence for the reason:
	// a code anchor, a ledger entry, or the exact search that found no
	// reducer consumer. Required with a disposition.
	NoReducerEvidence string
}

// Producer is one current emitter of a subset of a family's kinds. It
// distinguishes the current implementation repository and path from a
// planned destination repository under the collector-repository
// migration; it never asserts that the destination exists yet.
type Producer struct {
	// ID is the producer identity: a collector runtime name or a core
	// component name.
	ID string
	// Repository is the repository that holds the producer today.
	Repository string
	// Path is the producer's current path inside Repository.
	Path string
	// PlannedRepository is the planned destination repository, or blank
	// when the producer stays in Repository.
	PlannedRepository string
	// AuthorityMode records the producer's existing authority to emit.
	AuthorityMode AuthorityMode
	// Kinds is the exact subset of the family's kinds this producer
	// emits.
	Kinds []string
}

// Binding declares that one reducer implementation consumes an exact
// subset of one family's kinds. Bindings live beside their reducer
// implementation: the declaration file must sit in ReducerDir, and
// Symbol must name a top-level declaration in that package. The
// generator additionally verifies that each bound kind has reducer
// consumption evidence in that directory.
type Binding struct {
	// ID is the binding identifier in "directory/name" form. The
	// directory component is the final element of ReducerDir.
	ID string
	// FamilyID is the bound family's ID.
	FamilyID string
	// ReducerDir is the repo-relative reducer package directory.
	ReducerDir string
	// Symbol is the top-level implementation symbol in ReducerDir that
	// anchors the binding.
	Symbol string
	// Kinds is the exact subset of the family's kinds consumed there.
	Kinds []string
}

// Declare marks a [Family] literal as a fact-family contract declaration
// for the static generator. It is the identity function: declarations
// are build-time metadata and carry no runtime behavior, so nothing in
// this package runs at admission, projection, or query time.
func Declare(family Family) Family { return family }

// Bind marks a [Binding] literal as a reducer-consumption declaration
// for the static generator. Like [Declare] it is the identity function.
func Bind(binding Binding) Binding { return binding }
