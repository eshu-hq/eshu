// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

// The container_image_identity DATA CONTRACT: the outcome vocabulary and the
// decision/write/result records the handler, the writer, and every consumer of
// this domain exchange. The handler, its evidence loaders, and the counters
// live in container_image_identity.go; the durable write and the fencing-token
// helpers live in container_image_identity_writer.go.
//
// They are split because container_image_identity.go passed the repository's
// 500-line file cap. The contract is the half that changes for a different
// reason from the handler: a field added here ripples out to the payload, the
// query surface, and the golden snapshot, while a change to how the handler
// loads its evidence does not.

// ContainerImageIdentityOutcome names the reducer decision for one image
// reference seen in Git or runtime evidence.
type ContainerImageIdentityOutcome string

const (
	// ContainerImageIdentityExactDigest means the source reference already
	// named a digest also observed in registry facts.
	ContainerImageIdentityExactDigest ContainerImageIdentityOutcome = "exact_digest"
	// ContainerImageIdentityTagResolved means one registry tag observation
	// resolved the source tag to exactly one digest.
	ContainerImageIdentityTagResolved ContainerImageIdentityOutcome = "tag_resolved"
	// ContainerImageIdentityAmbiguousTag means tag observations for the same
	// image reference point at multiple digests.
	ContainerImageIdentityAmbiguousTag ContainerImageIdentityOutcome = "ambiguous_tag"
	// ContainerImageIdentityUnresolved means no registry digest observation
	// matched the source image reference.
	ContainerImageIdentityUnresolved ContainerImageIdentityOutcome = "unresolved"
	// ContainerImageIdentityStaleTag means runtime evidence resolved a tag to
	// a digest that registry facts report as the previous digest.
	ContainerImageIdentityStaleTag ContainerImageIdentityOutcome = "stale_tag"
)

const (
	// containerImageSourceRevisionOCIConfigLabel marks a SourceRevision drawn
	// from an OCI config image.revision/vcs-ref label matched to an active
	// repository remote — the strongest revision provenance because the label
	// travels inside the image content itself.
	containerImageSourceRevisionOCIConfigLabel = "oci_config_source_label"
	// containerImageSourceRevisionCIRunCommit marks a SourceRevision drawn from
	// the commit SHA of a ci.run whose artifact digest matched the image, used
	// only as a fallback when no OCI config revision label is present (#5423).
	// It is a weaker tier than an in-image label because the binding is the CI
	// provider's run→artifact→digest join rather than the image's own metadata.
	containerImageSourceRevisionCIRunCommit = "ci_run_commit"
	// containerImageSourceRevisionSLSAProvenanceCommit marks a SourceRevision
	// drawn from a signed SLSA provenance predicate's build definition config
	// source commit, matched to the image by digest (#5456). It OUTRANKS both
	// containerImageSourceRevisionOCIConfigLabel and
	// containerImageSourceRevisionCIRunCommit: a signed, third-party-attested
	// digest-to-commit binding is stronger evidence than an in-image label an
	// attacker with build access could forge, and stronger than the CI
	// provider's own run→artifact→digest join.
	containerImageSourceRevisionSLSAProvenanceCommit = "slsa_provenance_commit"
)

// ContainerImageIdentityDecision records one bounded image identity decision.
type ContainerImageIdentityDecision struct {
	ImageRef            string
	Digest              string
	RepositoryID        string
	SourceRepositoryIDs []string
	SourceRevision      string
	// SourceRevisionProvenance names where SourceRevision came from
	// (containerImageSourceRevisionOCIConfigLabel or
	// containerImageSourceRevisionCIRunCommit), empty when no revision was
	// resolved. It keeps the in-image-label tier distinguishable from the
	// weaker CI-run-commit fallback (#5423).
	SourceRevisionProvenance string
	// BaseImageForRepositoryIDs names the repositories whose Dockerfile FROM
	// declared this image as their runtime base (#5460). It is what separates a
	// base image from a built image: a base reference is extracted from the
	// declaring repository's own Dockerfile `file` fact, so it inherits the
	// same repository anchor its built images carry, and the repository anchor
	// alone cannot tell the two apart. Empty for every image that is not some
	// repository's declared base.
	BaseImageForRepositoryIDs []string
	// BuildProvenanceRepositoryIDs names the repositories that genuinely BUILT
	// this image, established only by build evidence: an OCI config source label
	// the image itself carries, or a CI run that reported producing this digest.
	// SourceRepositoryIDs is deliberately broader -- it also collects the
	// repository whose Kubernetes manifest merely REFERENCES a third-party
	// digest -- so base-image lineage (#5460) gates its child side on this field
	// instead. Attributing a referenced image to the referencing repository's
	// Dockerfile base would fabricate CVE-inheritance truth.
	BuildProvenanceRepositoryIDs []string
	WorkloadIDs                  []string
	ServiceIDs                   []string
	Outcome                      ContainerImageIdentityOutcome
	Reason                       string
	CanonicalWrites              int
	EvidenceFactIDs              []string
	IdentityStrength             string
}

// ContainerImageIdentityWrite carries decisions for durable publication.
type ContainerImageIdentityWrite struct {
	IntentID        string
	ClaimEpoch      int64
	ActivationEpoch int64
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Cause           string
	// EvidenceAsOf is the moment this write's evidence was read, captured
	// immediately before the handler's first fact load. The legacy writer stores
	// it as fact_records.fencing_token; the digest-v3 writer stores the same
	// ordering token on the scope's active support-set pointer. Read time, not
	// write time, prevents a worker that stalled past its lease from advertising
	// stale evidence as fresher. A zero value is always a hard error.
	EvidenceAsOf time.Time
	Decisions    []ContainerImageIdentityDecision
	// TombstoneDecisions names evaluated, non-canonical image references whose
	// durable logical identity must be retired. The legacy writer publishes a
	// fenced tombstone; the digest-v3 writer omits the support from its complete
	// replacement set. Collector incompleteness holds exclude a decision from
	// this slice.
	TombstoneDecisions []ContainerImageIdentityDecision
	// HeldDecisions identifies evaluated references whose prior canonical
	// support must remain authoritative because collector completeness is not
	// strong enough to prove their absence.
	HeldDecisions []ContainerImageIdentityDecision
	// LegacyFactIDs names outcome-keyed IDs emitted before #5854. Legacy writers
	// use the exact list; the digest-v3 authority switch deletes all legacy
	// container-image-identity rows for the exact active scope generation.
	LegacyFactIDs []string
}

// ContainerImageIdentityWriteResult summarizes durable publication.
type ContainerImageIdentityWriteResult struct {
	// CanonicalWrites counts current canonical decisions published by this
	// execution. Held prior supports carried into a digest-v3 set are excluded.
	CanonicalWrites int
	// RetirementAttempts counts logical identities this execution attempted to
	// retire. A legacy writer may publish tombstones; a digest-v3 writer omits
	// them from its complete set. This is deliberately not an applied-row count.
	RetirementAttempts int
	// LegacyRowsDeleted counts pre-digest-v3 fact rows removed atomically when
	// the exact scope generation switches to typed support-set authority.
	LegacyRowsDeleted int
	// effectiveSupports is the normalized digest-v3 support set accepted by the
	// writer's publication fence. It deliberately remains internal so the graph
	// projection can borrow the writer-owned immutable slice without a second
	// public contract or a deep copy of every nested support field.
	effectiveSupports []containerImageIdentitySupport
	// effectiveDecisions is the compatibility projection for the pre-v3 writer
	// and test writers. Production digest-v3 publication uses effectiveSupports.
	effectiveDecisions []ContainerImageIdentityDecision
	// effectiveProjectionPresent distinguishes an accepted empty support set
	// from a writer that omitted the graph projection contract entirely.
	effectiveProjectionPresent bool
	// EvidenceSummary is a short operator-facing description of the write.
	EvidenceSummary string
}
