// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func supplyChainDeploymentContextFromEnvelope(envelope facts.Envelope) supplyChainDeploymentContext {
	return supplyChainDeploymentContext{
		factID:         envelope.FactID,
		artifactDigest: payloadStr(envelope.Payload, "artifact_digest"),
		imageRef:       payloadStr(envelope.Payload, "image_ref"),
		repositoryID:   payloadStr(envelope.Payload, "repository_id"),
		environment:    payloadStr(envelope.Payload, "environment"),
		outcome:        payloadStr(envelope.Payload, "outcome"),
		provenanceOnly: payloadBool(envelope.Payload, "provenance_only"),
	}
}

func supplyChainDeploymentLaneContextFromEnvelope(envelope facts.Envelope) supplyChainDeploymentLaneContext {
	return supplyChainDeploymentLaneContext{
		factID:        envelope.FactID,
		repositoryID:  supplyChainWorkloadRepositoryID(envelope),
		deploymentIDs: supplyChainDeploymentIDsFromPayload(envelope.Payload),
	}
}

func supplyChainDeploymentIDsFromPayload(payload map[string]any) []string {
	var deploymentIDs []string
	if deploymentID := payloadStr(payload, "deployment_id"); deploymentID != "" {
		deploymentIDs = append(deploymentIDs, deploymentID)
	}
	for _, entityKey := range payloadOrderedStrings(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, "deployment:") {
			deploymentIDs = append(deploymentIDs, entityKey)
		}
	}
	return uniqueSortedStrings(deploymentIDs)
}
