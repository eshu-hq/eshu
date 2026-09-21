// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appflow

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// flowResourceID is the identity the flow node publishes as its resource_id.
// A flow's own outgoing edges are sourced on this same id so they attach to the
// flow node rather than dangling. The flow ARN is preferred; the flow name is
// the fallback when AWS omits the ARN.
func flowResourceID(flow Flow) string {
	if arn := strings.TrimSpace(flow.ARN); arn != "" {
		return arn
	}
	return strings.TrimSpace(flow.Name)
}

// flowS3SourceRelationship records a flow whose source connector is Amazon S3,
// targeting the source bucket node by the `arn:<partition>:s3:::<bucket>`
// identity the S3 scanner publishes. It returns nil when the source bucket is
// absent.
func flowS3SourceRelationship(boundary awscloud.Boundary, flow Flow) *awscloud.RelationshipObservation {
	return flowS3Relationship(
		boundary, flow, flow.SourceS3Bucket,
		awscloud.RelationshipAppFlowFlowReadsFromS3Bucket, "source",
	)
}

// flowS3DestinationRelationships records one edge per destination whose
// connector is Amazon S3, targeting each destination bucket node by the
// `arn:<partition>:s3:::<bucket>` identity the S3 scanner publishes. AppFlow
// fan-out flows can write to several S3 buckets, so every S3 destination yields
// its own edge. Buckets seen more than once collapse to a single edge so a
// flow listing the same destination twice does not double-count. It returns nil
// when no destination uses S3.
func flowS3DestinationRelationships(boundary awscloud.Boundary, flow Flow) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	seen := make(map[string]struct{}, len(flow.Destinations))
	for _, destination := range flow.Destinations {
		bucket := strings.TrimSpace(destination.S3Bucket)
		if bucket == "" {
			continue
		}
		if _, ok := seen[bucket]; ok {
			continue
		}
		seen[bucket] = struct{}{}
		observation := flowS3Relationship(
			boundary, flow, bucket,
			awscloud.RelationshipAppFlowFlowWritesToS3Bucket, "destination",
		)
		if observation != nil {
			observations = append(observations, *observation)
		}
	}
	return observations
}

func flowS3Relationship(
	boundary awscloud.Boundary,
	flow Flow,
	bucket string,
	relationshipType string,
	direction string,
) *awscloud.RelationshipObservation {
	bucket = strings.TrimSpace(bucket)
	flowID := flowResourceID(flow)
	if bucket == "" || flowID == "" {
		return nil
	}
	arn := bucketARN(boundary, flow.ARN, bucket)
	if arn == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: flowID,
		SourceARN:        strings.TrimSpace(flow.ARN),
		TargetResourceID: arn,
		TargetARN:        arn,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		Attributes: map[string]any{
			"bucket":    bucket,
			"direction": direction,
		},
		SourceRecordID: flowID + "->" + relationshipType + ":" + arn,
	}
}

// flowConnectorProfileRelationships records the flow's source and destination
// connector profile dependencies, keyed by the connector profile name the
// connector-profile scanner publishes as its resource_id. The source profile is
// the scalar source-side reference; the destination profiles come from every
// destination AppFlow reports, since a fan-out flow can target several
// connector-profile destinations. A profile referenced more than once (source
// and destination, or two destinations) collapses to a single edge.
func flowConnectorProfileRelationships(boundary awscloud.Boundary, flow Flow) []awscloud.RelationshipObservation {
	flowID := flowResourceID(flow)
	if flowID == "" {
		return nil
	}
	candidates := []struct {
		name      string
		direction string
	}{
		{name: flow.SourceConnectorProfileName, direction: "source"},
	}
	for _, destination := range flow.Destinations {
		candidates = append(candidates, struct {
			name      string
			direction string
		}{name: destination.ConnectorProfileName, direction: "destination"})
	}
	var observations []awscloud.RelationshipObservation
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		name := strings.TrimSpace(candidate.name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAppFlowFlowUsesConnectorProfile,
			SourceResourceID: flowID,
			SourceARN:        strings.TrimSpace(flow.ARN),
			TargetResourceID: name,
			TargetType:       awscloud.ResourceTypeAppFlowConnectorProfile,
			Attributes:       map[string]any{"direction": candidate.direction},
			SourceRecordID:   flowID + "->" + awscloud.RelationshipAppFlowFlowUsesConnectorProfile + ":" + name,
		})
	}
	return observations
}

// flowKMSKeyRelationship records the customer-provided KMS key the flow uses to
// encrypt transferred data, targeting the KMS key node by ARN. It returns nil
// when the flow uses the AppFlow-managed key (no customer KMS ARN reported) or
// the reported value is not ARN-shaped.
func flowKMSKeyRelationship(boundary awscloud.Boundary, flow Flow) *awscloud.RelationshipObservation {
	keyARN := strings.TrimSpace(flow.KMSKeyARN)
	flowID := flowResourceID(flow)
	if !isARN(keyARN) || flowID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppFlowFlowUsesKMSKey,
		SourceResourceID: flowID,
		SourceARN:        strings.TrimSpace(flow.ARN),
		TargetResourceID: keyARN,
		TargetARN:        keyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   flowID + "->" + awscloud.RelationshipAppFlowFlowUsesKMSKey + ":" + keyARN,
	}
}

// connectorProfileSecretRelationship records the Secrets Manager secret that
// stores the connector profile's credentials, targeting the secret node by ARN
// only. It returns nil unless the reported credentials ARN parses as a Secrets
// Manager ARN (exact service-segment match), so a non-secret reference never
// produces a dangling edge. The credential values are never read.
func connectorProfileSecretRelationship(
	boundary awscloud.Boundary,
	profile ConnectorProfile,
) *awscloud.RelationshipObservation {
	secretARN := strings.TrimSpace(profile.CredentialsARN)
	name := strings.TrimSpace(profile.Name)
	if name == "" || !isSecretsManagerARN(secretARN) {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAppFlowConnectorProfileUsesSecret,
		SourceResourceID: name,
		SourceARN:        strings.TrimSpace(profile.ARN),
		TargetResourceID: secretARN,
		TargetARN:        secretARN,
		TargetType:       awscloud.ResourceTypeSecretsManagerSecret,
		SourceRecordID:   name + "->" + awscloud.RelationshipAppFlowConnectorProfileUsesSecret + ":" + secretARN,
	}
}
