// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appflow

import (
	"context"
	"time"
)

// Client lists metadata-only Amazon AppFlow observations for one claimed
// account and region. Implementations must never read flow records, field
// mappings (task transforms), connector credentials, or OAuth tokens.
type Client interface {
	// ListFlows returns the account's AppFlow flows with the metadata-only view
	// the scanner needs, including source/destination connector profile names,
	// S3 bucket references, and the customer KMS key ARN, but never field
	// mappings or transferred record contents.
	ListFlows(ctx context.Context) ([]Flow, error)
	// ListConnectorProfiles returns the account's AppFlow connector profiles
	// with identity, connector type, connection mode, and the Secrets Manager
	// credentials ARN reference only. Credential values are never returned.
	ListConnectorProfiles(ctx context.Context) ([]ConnectorProfile, error)
}

// Flow is the scanner-owned Amazon AppFlow flow view. Field mappings (the
// flow's task transforms, which can encode literal data values), flow run
// record contents, and execution result payloads stay outside the contract.
type Flow struct {
	// ARN is the flow's Amazon Resource Name. It is the partition source for
	// any synthesized S3 bucket ARN and the flow node identity.
	ARN string
	// Name is the AppFlow flow name (unique per account).
	Name string
	// Description is the user-entered flow description.
	Description string
	// Status is the reported flow status (for example Active, Suspended).
	Status string
	// SourceConnectorType is the source connector kind (for example S3,
	// Salesforce).
	SourceConnectorType string
	// DestinationConnectorType is the destination connector kind.
	DestinationConnectorType string
	// SourceConnectorProfileName is the source connector profile name, when the
	// source connector uses a connector profile.
	SourceConnectorProfileName string
	// DestinationConnectorProfileName is the first destination connector profile
	// name, when that destination uses a connector profile. It summarizes
	// Destinations[0] for the resource attributes; the full set of destinations
	// drives the per-destination graph edges.
	DestinationConnectorProfileName string
	// SourceS3Bucket is the source S3 bucket name when the source connector is
	// Amazon S3. It is empty for non-S3 sources.
	SourceS3Bucket string
	// DestinationS3Bucket is the first destination's S3 bucket name when that
	// destination connector is Amazon S3. It summarizes Destinations[0] for the
	// resource attributes; the full set of destinations drives the
	// per-destination flow-to-S3 edges.
	DestinationS3Bucket string
	// Destinations carries every destination AppFlow reports for the flow
	// (DestinationFlowConfigList). AppFlow supports fan-out flows with multiple
	// destinations, so each destination is preserved to emit one flow-to-S3 and
	// one flow-to-connector-profile edge per reported destination rather than
	// only the first.
	Destinations []FlowDestination
	// KMSKeyARN is the customer-provided KMS key ARN used to encrypt transferred
	// data. It is empty when the flow uses the AppFlow-managed key.
	KMSKeyARN string
	// TriggerType is the flow trigger kind (OnDemand, Scheduled, or Event).
	TriggerType string
	// CreatedAt is when the flow was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the flow was last updated.
	LastUpdatedAt time.Time
}

// FlowDestination is one entry from a flow's DestinationFlowConfigList. AppFlow
// flows can fan out to multiple destinations, so each destination carries its
// own connector kind, optional connector profile name, and optional S3 bucket
// so the scanner emits one graph edge per destination. Destination connector
// properties beyond the S3 bucket name (object/entity selectors and error
// handling) are not read.
type FlowDestination struct {
	// ConnectorType is this destination's connector kind (for example S3,
	// Salesforce).
	ConnectorType string
	// ConnectorProfileName is this destination's connector profile name, when the
	// destination connector uses a connector profile. It matches the resource_id
	// the connector-profile node publishes.
	ConnectorProfileName string
	// S3Bucket is this destination's S3 bucket name when the destination connector
	// is Amazon S3. It is empty for non-S3 destinations.
	S3Bucket string
}

// ConnectorProfile is the scanner-owned Amazon AppFlow connector profile view.
// Connector credentials, OAuth tokens, and connector-specific secret material
// stay outside the contract. Only the Secrets Manager credentials ARN reference
// is recorded so the profile can join its secret node.
type ConnectorProfile struct {
	// ARN is the connector profile's Amazon Resource Name.
	ARN string
	// Name is the connector profile name (unique per account). Flows reference
	// connector profiles by this name, so it is the profile node identity.
	Name string
	// ConnectorType is the SaaS or service connector kind (for example
	// Salesforce, ServiceNow, Snowflake).
	ConnectorType string
	// ConnectorLabel is the custom-connector label, when present.
	ConnectorLabel string
	// ConnectionMode is the connection mode (Public or Private).
	ConnectionMode string
	// CredentialsARN is the ARN of the Secrets Manager secret that stores the
	// connector profile's credentials. The credential values are never read;
	// only this ARN reference is recorded to drive the profile-to-secret edge.
	CredentialsARN string
	// CreatedAt is when the connector profile was created.
	CreatedAt time.Time
	// LastUpdatedAt is when the connector profile was last updated.
	LastUpdatedAt time.Time
}
