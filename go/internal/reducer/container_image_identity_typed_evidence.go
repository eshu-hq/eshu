// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds the typed-decode evidence extractors for the
// container-image-identity domain's cross-provider image_reference family
// (#4685): aws_image_reference, azure_image_reference, gcp_image_reference,
// ci.artifact, and ci.run. extractContainerImageRefsWithQuarantine
// (container_image_identity_evidence.go) dispatches to each of these; they
// live in a separate file to keep container_image_identity_evidence.go under
// the repo's 500-line file cap.

// addWorkflowImageEvidenceRef decodes one ci.workflow_image_evidence envelope
// through the typed seam. A missing required repository_id is routed through
// partitionDecodeFailures (returned to the caller for quarantine); an
// EvidenceClass other than "workflow_image_ref" is a valid decode that simply
// contributes no image reference, matching the pre-typing behavior.
func addWorkflowImageEvidenceRef(byRef map[string]containerImageRefEvidence, envelope facts.Envelope) (quarantinedFact, bool, error) {
	evidence, err := decodeCICDWorkflowImageEvidence(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	if trimmedCICDPtr(evidence.EvidenceClass) != "workflow_image_ref" {
		return quarantinedFact{}, false, nil
	}
	addContainerImageRef(
		byRef,
		trimmedCICDPtr(evidence.ImageRef),
		"",
		containerImageAnchorsFromEnvelope(envelope),
		envelope.FactID,
	)
	return quarantinedFact{}, false, nil
}

// containerImageCIRunAnchor is one ci.run's repository anchor, indexed by
// containerImageCIRuns so addCICDArtifactImageReference can attach it to the
// run's artifacts.
type containerImageCIRunAnchor struct {
	repositoryID string
	// commitSHA is the run's head commit, carried so addCICDArtifactImageReference
	// can thread it into a digest-matched image's SourceRevision when no OCI
	// config revision label is present (#5423). Blank when the run fact omits
	// commit_sha (a valid but revision-less observation).
	commitSHA string
	factID    string
}

// containerImageCIRuns decodes every ci.run envelope through the typed seam,
// indexing each by the same run join key
// (cicdRunKeyFromParts/ci_cd_run_correlation_decode.go) the sibling
// ci_cd_run_correlation domain uses, so addCICDArtifactImageReference can
// attach a run's repository anchor to its artifacts. A fact missing its
// required provider/run_id join key is routed through partitionDecodeFailures
// for quarantine rather than silently collapsing onto (or contributing
// nothing under) an empty-string key.
func containerImageCIRuns(envelopes []facts.Envelope) (map[string]containerImageCIRunAnchor, []quarantinedFact, error) {
	out := make(map[string]containerImageCIRunAnchor)
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		run, err := decodeCICDRun(envelope)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(envelope, err)
			if !ok {
				return nil, nil, fatal
			}
			quarantined = append(quarantined, q)
			continue
		}
		// A required identity field can decode as a present-but-blank string
		// (decodeAndValidate accepts an explicit empty value), and
		// cicdRunKeyFromParts still yields a non-empty key like
		// "github_actions::1" from the provider/attempt alone. Guard blank
		// provider/run_id explicitly so a malformed run is not indexed and
		// cannot lend its repository anchor to a matching malformed artifact,
		// restoring the pre-typing raw cicdRunKey's refusal of blank join
		// identity (#5234).
		if strings.TrimSpace(run.Provider) == "" || strings.TrimSpace(run.RunID) == "" {
			continue
		}
		key := cicdRunKeyFromParts(run.Provider, run.RunID, run.RunAttempt)
		repositoryID := trimmedCICDPtr(run.RepositoryID)
		if key == "" || repositoryID == "" {
			continue
		}
		out[key] = containerImageCIRunAnchor{
			repositoryID: repositoryID,
			commitSHA:    trimmedCICDPtr(run.CommitSHA),
			factID:       envelope.FactID,
		}
	}
	return out, quarantined, nil
}

// addAWSImageReference decodes one aws_image_reference envelope through the
// typed seam. A missing required field (account_id, region, repository_name,
// image_digest, manifest_digest) is routed through partitionDecodeFailures for
// quarantine; the empty-string guards below stay in place because a required
// field's key can still be present with an explicit empty-string value (a
// valid decode decodeAndValidate does not reject), matching the pre-typing
// defensive checks.
func addAWSImageReference(byRef map[string]containerImageRefEvidence, envelope facts.Envelope) (quarantinedFact, bool, error) {
	reference, err := decodeAWSImageReference(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	digest := firstNonBlank(reference.ManifestDigest, reference.ImageDigest)
	if reference.RepositoryName == "" || digest == "" {
		return quarantinedFact{}, false, nil
	}
	registryID := firstNonBlank(derefString(reference.RegistryID), reference.AccountID)
	if registryID == "" {
		return quarantinedFact{}, false, nil
	}
	registry := registryID + ".dkr.ecr." + reference.Region + ".amazonaws.com"
	imageRef := registry + "/" + reference.RepositoryName + "@" + digest
	addContainerImageRef(byRef, imageRef, imageRef, containerImageAnchorsFromEnvelope(envelope), envelope.FactID)
	return quarantinedFact{}, false, nil
}

// addAzureImageReference decodes one azure_image_reference envelope through
// the typed seam. A missing required field (owning_arm_resource_id,
// owning_normalized_id, owning_resource_type, tag_digest_confidence) is
// routed through partitionDecodeFailures for quarantine; ImageReference and
// ImageDigest stay optional pointer reads because the collector emits
// digest-only or reference-only evidence (see azurecloud.NewImageReferenceEnvelope).
func addAzureImageReference(byRef map[string]containerImageRefEvidence, envelope facts.Envelope) (quarantinedFact, bool, error) {
	reference, err := decodeAzureImageReference(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	imageRef := derefString(reference.ImageReference)
	digest := derefString(reference.ImageDigest)
	anchors := containerImageAnchorsFromEnvelope(envelope)
	if digest != "" {
		if digestImageRef := imageRefWithDigest(imageRef, digest); digestImageRef != "" {
			addContainerImageRef(byRef, digestImageRef, digestImageRef, anchors, envelope.FactID)
			return quarantinedFact{}, false, nil
		}
		addContainerImageDigestRef(byRef, digest, anchors, envelope.FactID)
		return quarantinedFact{}, false, nil
	}
	addContainerImageRef(byRef, imageRef, "", anchors, envelope.FactID)
	return quarantinedFact{}, false, nil
}

// addGCPImageReference decodes one gcp_image_reference envelope through the
// typed seam. A missing required field (owning_full_resource_name,
// tag_digest_confidence) is routed through partitionDecodeFailures for
// quarantine; ImageReference and ImageDigest stay optional pointer reads for
// the same digest-only/reference-only evidence reason as Azure above.
func addGCPImageReference(byRef map[string]containerImageRefEvidence, envelope facts.Envelope) (quarantinedFact, bool, error) {
	reference, err := decodeGCPImageReference(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	imageRef := derefString(reference.ImageReference)
	digest := derefString(reference.ImageDigest)
	anchors := containerImageAnchorsFromEnvelope(envelope)
	if digest != "" {
		if digestImageRef := imageRefWithDigest(imageRef, digest); digestImageRef != "" {
			addContainerImageRef(byRef, digestImageRef, digestImageRef, anchors, envelope.FactID)
			return quarantinedFact{}, false, nil
		}
		addContainerImageDigestRef(byRef, digest, anchors, envelope.FactID)
		return quarantinedFact{}, false, nil
	}
	addContainerImageRef(byRef, imageRef, "", anchors, envelope.FactID)
	return quarantinedFact{}, false, nil
}

// addCICDArtifactImageReference decodes one ci.artifact envelope through the
// typed seam. A missing required field (provider, run_id) is routed through
// partitionDecodeFailures for quarantine. The real ci.artifact collector
// (go/internal/collector/cicdrun) never emits an "image_ref" payload key —
// only artifact_type and artifact_digest — so this only reads the digest path;
// the pre-typing raw imageRef fallback branch matched no real payload and is
// dropped rather than re-added as an unmodeled struct field.
func addCICDArtifactImageReference(
	byRef map[string]containerImageRefEvidence,
	envelope facts.Envelope,
	runs map[string]containerImageCIRunAnchor,
	ciRunDigest map[string]ciRunDigestAnchor,
) (quarantinedFact, bool, error) {
	artifact, err := decodeCICDArtifact(envelope)
	if err != nil {
		return partitionDecodeFailures(envelope, err)
	}
	if trimmedCICDPtr(artifact.ArtifactType) != "container_image" {
		return quarantinedFact{}, false, nil
	}
	digest := trimmedCICDPtr(artifact.ArtifactDigest)
	if digest == "" {
		return quarantinedFact{}, false, nil
	}
	anchors := containerImageAnchorsFromEnvelope(envelope)
	evidenceFactIDs := []string{envelope.FactID}
	run := runs[cicdRunKeyFromParts(artifact.Provider, artifact.RunID, artifact.RunAttempt)]
	if run.repositoryID != "" {
		anchors.sourceRepositoryIDs = append(anchors.sourceRepositoryIDs, run.repositoryID)
		evidenceFactIDs = append(evidenceFactIDs, run.factID)
	}
	// No image_ref key exists in the typed Artifact struct (the real collector
	// never emits one — see the doc comment above), so a digest is always
	// recorded as a bare digest reference, matching the pre-typing behavior
	// where imageRefWithDigest("", digest) always returned "" for real payloads.
	addContainerImageDigestRef(byRef, digest, anchors, evidenceFactIDs...)
	// Record the matched run's commit + source repository against the digest so
	// applyCIRunDigestRevision can attach it to whichever decision resolves this
	// digest, surviving a competing content_entity decision for the same image
	// (#5423). A no-op for a run with no commit or repository anchor.
	recordCIRunDigestAnchor(ciRunDigest, digest, run)
	return quarantinedFact{}, false, nil
}
