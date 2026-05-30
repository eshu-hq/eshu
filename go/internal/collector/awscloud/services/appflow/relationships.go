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

// flowS3DestinationRelationship records a flow whose destination connector is
// Amazon S3, targeting the destination bucket node. It returns nil when the
// destination bucket is absent.
func flowS3DestinationRelationship(boundary awscloud.Boundary, flow Flow) *awscloud.RelationshipObservation {
	return flowS3Relationship(
		boundary, flow, flow.DestinationS3Bucket,
		awscloud.RelationshipAppFlowFlowWritesToS3Bucket, "destination",
	)
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
// connector-profile scanner publishes as its resource_id. A flow may reference
// the same profile for both its source and destination; that case collapses to
// a single edge.
func flowConnectorProfileRelationships(boundary awscloud.Boundary, flow Flow) []awscloud.RelationshipObservation {
	flowID := flowResourceID(flow)
	if flowID == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	seen := make(map[string]struct{}, 2)
	for _, candidate := range []struct {
		name      string
		direction string
	}{
		{name: flow.SourceConnectorProfileName, direction: "source"},
		{name: flow.DestinationConnectorProfileName, direction: "destination"},
	} {
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
