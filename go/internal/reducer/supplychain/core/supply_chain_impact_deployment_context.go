// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func supplyChainDeploymentContextFromEnvelope(envelope facts.Envelope) supplychainmodel.DeploymentContext {
	return supplychainmodel.DeploymentContext{
		FactID:         envelope.FactID,
		ArtifactDigest: payloadcore.PayloadStr(envelope.Payload, "artifact_digest"),
		ImageRef:       payloadcore.PayloadStr(envelope.Payload, "image_ref"),
		RepositoryID:   payloadcore.PayloadStr(envelope.Payload, "repository_id"),
		Environment:    payloadcore.PayloadStr(envelope.Payload, "environment"),
		EnvironmentEvidence: normalizeSupplyChainEnvironmentEvidence(
			payloadcore.PayloadStr(envelope.Payload, "environment_evidence"),
		),
		Outcome:        payloadcore.PayloadStr(envelope.Payload, "outcome"),
		ProvenanceOnly: payloadBool(envelope.Payload, "provenance_only"),
	}
}

func supplyChainDeploymentLaneContextFromEnvelope(envelope facts.Envelope) supplychainmodel.DeploymentLaneContext {
	return supplychainmodel.DeploymentLaneContext{
		FactID:        envelope.FactID,
		RepositoryID:  supplyChainWorkloadRepositoryID(envelope),
		DeploymentIDs: supplyChainDeploymentIDsFromPayload(envelope.Payload),
	}
}

func supplyChainDeploymentIDsFromPayload(payload map[string]any) []string {
	var deploymentIDs []string
	if deploymentID := payloadcore.PayloadStr(payload, "deployment_id"); deploymentID != "" {
		deploymentIDs = append(deploymentIDs, deploymentID)
	}
	for _, entityKey := range payloadcore.PayloadOrderedStrings(payload, "entity_keys") {
		if strings.HasPrefix(entityKey, "deployment:") {
			deploymentIDs = append(deploymentIDs, entityKey)
		}
	}
	return payloadcore.UniqueSortedStrings(deploymentIDs)
}
