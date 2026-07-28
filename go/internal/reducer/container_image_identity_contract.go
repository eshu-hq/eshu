// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

// The container_image_identity DATA CONTRACT: the outcome vocabulary and the
// decision/write/result records the handler, the writer, and every consumer of
// this domain exchange. The handler, its evidence loaders, and the counters
// live in container_image_identity.go; the durable statements live in
// container_image_identity_writer_queries.go.
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
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	// EvidenceAsOf is the moment this write's evidence was read, captured
	// immediately before the handler's first fact load. It is the fencing token
	// the generation-authoritative retire ranks writers by, so a worker that
	// stalled past its lease cannot delete the rows of the worker that overtook
	// it with fresher evidence. It is required: a zero value is a hard error,
	// never a defaulted or unfenced write. See containerImageIdentityRetireQuery.
	EvidenceAsOf time.Time
	Decisions    []ContainerImageIdentityDecision
}

// ContainerImageIdentityWriteResult summarizes durable publication.
type ContainerImageIdentityWriteResult struct {
	// CanonicalWrites counts the decisions this execution inserted or upserted.
	CanonicalWrites int
	// Retired counts the durable decisions the generation-authoritative retire
	// DELETED for this write's (scope, generation). It is reported because the
	// retire destroys durable rows and the instrumented ExecContext wrapper
	// records only that a statement ran, never what it removed.
	Retired int
	// RetiredWithoutCanonicalWrites marks a pass that deleted a non-empty prior
	// decision set while producing no canonical decision of its own. That is
	// legitimate for a genuine demotion, but it is also exactly what an
	// evidence-visibility gap looks like from the writer: classifyContainerImageRef
	// answers `unresolved` when the cross-scope registry observations are absent,
	// which is indistinguishable here from "this image really has no digest
	// identity any more". It is surfaced so the shape is findable rather than
	// silent. See containerImageIdentityRetireQuery.
	RetiredWithoutCanonicalWrites bool
	// RetiredMoreThanWritten marks a pass whose retire DELETED more durable
	// decisions than the pass itself wrote — the partition shrank by more than
	// this pass's own write count.
	//
	// RetiredWithoutCanonicalWrites is the total case; this reaches PART of the
	// partial one it cannot see. An evidence-visibility gap need not be total: a
	// pass can see the cross-scope OCI observations for SOME images in a
	// generation and not others, and retire the rest while still writing the ones
	// it did see. Five canonical decisions before and one after is that shape, and
	// it does fire here (retired=4 against canonical_writes=1).
	//
	// # What this signal does NOT reach
	//
	// It is `retired > CanonicalWrites`, so it fires only when the shrink exceeds
	// this pass's OWN write count — in effect only when more than half the
	// partition was lost. A partial gap smaller than the surviving set leaves both
	// flags false. Measured against the production writer:
	//
	//	canonical=6 retired=4  | blind=false moreThanWritten=false  <- 4 lost, un-flagged
	//	canonical=9 retired=1  | blind=false moreThanWritten=false
	//	canonical=5 retired=5  | blind=false moreThanWritten=false
	//	canonical=4 retired=6  | blind=false moreThanWritten=true
	//	canonical=1 retired=4  | blind=false moreThanWritten=true
	//	canonical=0 retired=10 | blind=true  moreThanWritten=true
	//
	// A ten-image partition that writes six and retires four is a real four-image
	// evidence gap that trips neither flag. It is un-FLAGGED rather than
	// un-counted, and the distinction matters when deciding where to look:
	// CanonicalWrites and Retired carry the raw pair, and both summary strings
	// render it (containerImageIdentitySummary emits `canonical_writes=6
	// retired=4`). What is missing is a CONSUMER. On a SUCCEEDING pass nothing
	// carries either string out of the process — Service.recordReducerResult logs
	// SubDurations and SubSignals but not EvidenceSummary, and ReducerQueue.Ack
	// takes the Result as `_` — so only the failure path forwards a summary at
	// all. The boolean is therefore what an operator has HERE, and a sub-threshold
	// shrink has to be found on either side of this writer: downstream on
	// eshu_dp_container_image_identity_decisions_total, where the loss shows up as
	// exact_digest falling and unresolved rising for the same domain, and upstream
	// on the OCI collector's oci_registry.warning facts and
	// eshu_dp_oci_registry_api_calls_total{result="error"}.
	//
	// Reaching it in this struct needs a baseline this writer does not have — the
	// partition's count BEFORE the write — which the two committed statements
	// cannot supply without a third statement or a readback on the shared batched
	// insert. The coarse signal is what is claimed here; the gap below it is not
	// covered.
	//
	// The shrink is the discriminator for the part that IS reached. An ordinary
	// re-classification retires at most one superseded row per image it rewrites,
	// so it can never retire more rows than it wrote; retiring more means rows left
	// the partition with no replacement. That is legitimate for a genuine demotion
	// and is what a gap-induced one looks like too — indistinguishable from here,
	// which is exactly why it is reported rather than judged.
	RetiredMoreThanWritten bool
	// EvidenceSummary is a short operator-facing description of the write.
	EvidenceSummary string
}
