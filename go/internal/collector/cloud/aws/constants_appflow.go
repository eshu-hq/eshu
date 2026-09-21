// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAppFlow identifies the regional Amazon AppFlow metadata-only scan
	// slice covering flows and connector profiles. The slice never reads flow
	// records, field mappings, connector credentials, or OAuth tokens.
	ServiceAppFlow = "appflow"
)

const (
	// ResourceTypeAppFlowFlow identifies an Amazon AppFlow flow metadata
	// resource. Field mappings (task transforms) and flow run record contents
	// stay outside the contract because they can carry transferred data values.
	ResourceTypeAppFlowFlow = "aws_appflow_flow"
	// ResourceTypeAppFlowConnectorProfile identifies an Amazon AppFlow connector
	// profile metadata resource. Connector credentials and OAuth tokens stay
	// outside the contract; only the Secrets Manager credentials ARN reference
	// is recorded so the profile can join its secret node.
	ResourceTypeAppFlowConnectorProfile = "aws_appflow_connector_profile"
)

const (
	// RelationshipAppFlowFlowReadsFromS3Bucket records an AppFlow flow whose
	// source connector is Amazon S3, targeting the source bucket node.
	RelationshipAppFlowFlowReadsFromS3Bucket = "appflow_flow_reads_from_s3_bucket"
	// RelationshipAppFlowFlowWritesToS3Bucket records an AppFlow flow whose
	// destination connector is Amazon S3, targeting the destination bucket node.
	RelationshipAppFlowFlowWritesToS3Bucket = "appflow_flow_writes_to_s3_bucket"
	// RelationshipAppFlowFlowUsesConnectorProfile records an AppFlow flow's
	// reported source or destination connector profile dependency, keyed by the
	// connector profile name the connector-profile scanner publishes.
	RelationshipAppFlowFlowUsesConnectorProfile = "appflow_flow_uses_connector_profile"
	// RelationshipAppFlowFlowUsesKMSKey records an AppFlow flow's customer
	// provided KMS key used to encrypt the transferred data, targeting the KMS
	// key node by ARN.
	RelationshipAppFlowFlowUsesKMSKey = "appflow_flow_uses_kms_key"
	// RelationshipAppFlowConnectorProfileUsesSecret records an AppFlow connector
	// profile's reference to the Secrets Manager secret that stores its
	// credentials, targeting the secret node by ARN only. The credential values
	// themselves are never read.
	RelationshipAppFlowConnectorProfileUsesSecret = "appflow_connector_profile_uses_secret" // #nosec G101 -- relationship-type identifier recording a secret ARN reference, not a credential value
)
