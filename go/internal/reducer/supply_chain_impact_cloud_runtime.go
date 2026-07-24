// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyChainCloudRuntimeObservation is one observed cloud compute resource
// (a running ECS task or an image-package Lambda function) whose running image
// digest was captured by the AWS collector. It is the runtime-observed
// deployment evidence issue #5452 joins against a finding's subject digest,
// distinct from the CI-declared cicd_run_correlation evidence: this proves a
// live workload actually runs the affected image, not that CI declared a
// deployment of it.
type supplyChainCloudRuntimeObservation struct {
	factID      string
	digest      string
	resourceRef string
}

// supplyChainCloudRuntimeObservationFromEnvelope decodes one aws_resource fact
// into a runtime observation IFF it is a gated running-image resource type (an
// ECS task or an image-package Lambda function) that resolves an unambiguous
// running image digest. It reuses the exact #5450 decode helpers
// (cloudResourceRunningImageFields, resource identity fallback) so the digest
// shape and gating stay byte-identical to the CloudResource node projection —
// a divergence there would silently break the digest join.
//
// ok=false (no observation, not an error) for a non-gated resource_type, an
// ambiguous or absent running image (multi-container task, digest-less image),
// or an incomplete identity. A non-nil error means the payload carried a
// present-but-malformed image attribute; the caller MUST route it to the same
// quarantine/dead-letter path an envelope decode failure uses, never index it.
func supplyChainCloudRuntimeObservationFromEnvelope(
	envelope facts.Envelope,
) (supplyChainCloudRuntimeObservation, bool, error) {
	resource, err := decodeAWSResource(envelope)
	if err != nil {
		return supplyChainCloudRuntimeObservation{}, false, err
	}
	fields, err := cloudResourceRunningImageFields(resource)
	if err != nil {
		return supplyChainCloudRuntimeObservation{}, false, err
	}
	digest := strings.TrimSpace(anyToString(fields["running_image_digest"]))
	if digest == "" {
		return supplyChainCloudRuntimeObservation{}, false, nil
	}
	resourceRef := strings.TrimSpace(resource.ResourceID)
	if resourceRef == "" {
		resourceRef = strings.TrimSpace(derefString(resource.ARN))
	}
	if resourceRef == "" {
		return supplyChainCloudRuntimeObservation{}, false, nil
	}
	return supplyChainCloudRuntimeObservation{
		factID:      envelope.FactID,
		digest:      digest,
		resourceRef: resourceRef,
	}, true, nil
}

// applySupplyChainCloudRuntimeObservations attaches the observed cloud
// resources that run the finding's subject digest as runtime-observed
// deployment evidence: it appends each observation's aws_resource fact ID and
// an aws_resource evidence-path hop, and records the resource ref
// (ARN/resource_id) in CloudRuntimeResourceRefs so the finding names which live
// resource carries the affected digest. A blank SubjectDigest matches nothing,
// so a finding with no resolved image digest never gains fabricated runtime
// evidence.
func applySupplyChainCloudRuntimeObservations(
	finding *SupplyChainImpactFinding,
	index supplyChainImpactIndex,
) {
	if finding == nil {
		return
	}
	digest := strings.TrimSpace(finding.SubjectDigest)
	if digest == "" || len(index.cloudRuntimeObservations) == 0 {
		return
	}
	observations := index.cloudRuntimeObservations[digest]
	if len(observations) == 0 {
		return
	}
	for _, observation := range observations {
		finding.EvidenceFactIDs = append(finding.EvidenceFactIDs, observation.factID)
		finding.EvidencePath = append(finding.EvidencePath, facts.AWSResourceFactKind)
		finding.CloudRuntimeResourceRefs = append(finding.CloudRuntimeResourceRefs, observation.resourceRef)
	}
	finding.CloudRuntimeResourceRefs = uniqueSortedStrings(finding.CloudRuntimeResourceRefs)
}
