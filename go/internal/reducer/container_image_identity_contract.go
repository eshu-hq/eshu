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
	IntentID     string
	ClaimEpoch   int64
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	// EvidenceAsOf is the moment this write's evidence was read, captured
	// immediately before the handler's first fact load. It becomes the row's
	// fact_records.fencing_token, which the shared batched insert's conflict
	// guard ranks colliding passes by: a worker that stalled past its lease
	// cannot overwrite the payload of the worker that overtook it with fresher
	// evidence. Read time, not write time — write time ranks the stalled worker
	// highest, which is backwards. It is required: a zero value is a hard error,
	// never a defaulted or unfenced write. See reducerFactBatchInsertQuery.
	EvidenceAsOf time.Time
	Decisions    []ContainerImageIdentityDecision
	// TombstoneDecisions names evaluated, non-canonical image references whose
	// durable logical identity must be retired. The writer publishes each as a
	// fenced tombstone at the same outcome-independent fact ID a later
	// canonical decision revives. Collector incompleteness holds exclude a
	// decision from this slice.
	TombstoneDecisions []ContainerImageIdentityDecision
	// LegacyFactIDs names outcome-keyed IDs emitted before #5854. The writer
	// deletes these unreachable keys in the same transaction as the new
	// outcome-independent live rows and tombstones.
	LegacyFactIDs []string
}

// ContainerImageIdentityWriteResult summarizes durable publication.
type ContainerImageIdentityWriteResult struct {
	// CanonicalWrites counts the decisions this execution inserted or upserted.
	CanonicalWrites int
	// RetirementAttempts counts logical identities this execution attempted to
	// publish as fenced tombstones. A fresher row can reject the publication at
	// the ON CONFLICT fence, so this is deliberately not an applied-row count.
	RetirementAttempts int
	// LegacyRowsDeleted counts pre-#5854 outcome-keyed rows removed after their
	// fact-ID derivation became unreachable to future writers.
	LegacyRowsDeleted int
	// EvidenceSummary is a short operator-facing description of the write.
	EvidenceSummary string
}
