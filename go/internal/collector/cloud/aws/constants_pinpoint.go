// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServicePinpoint identifies the regional Amazon Pinpoint metadata-only scan
	// slice. The scanner reads application, segment, and channel-settings
	// control-plane metadata through the Pinpoint management APIs (GetApps,
	// GetSegments, GetChannels, and GetEmailChannel) and never reads or persists
	// endpoint records, addresses, message or template content, segment targeting
	// criteria values, channel credentials, or API/auth keys. It never sends a
	// message and never mutates Pinpoint state.
	ServicePinpoint = "pinpoint"
)

const (
	// ResourceTypePinpointApplication identifies an Amazon Pinpoint application
	// (project) metadata resource. The scanner emits identity (id, ARN, name),
	// creation time, and resource tags only.
	ResourceTypePinpointApplication = "aws_pinpoint_application"
	// ResourceTypePinpointSegment identifies an Amazon Pinpoint segment metadata
	// resource. The scanner emits identity (id, ARN, name, parent application
	// id), segment type (DIMENSIONAL or IMPORT), version, an imported-from-S3
	// presence flag with the import format and reported size, and lifecycle
	// timestamps only. Targeting dimensions, segment group criteria, the import
	// S3 URL, the import external id, and any endpoint attribute value are never
	// read or emitted.
	ResourceTypePinpointSegment = "aws_pinpoint_segment"
	// ResourceTypePinpointChannel identifies an Amazon Pinpoint channel-settings
	// metadata resource for one application and channel type (for example EMAIL,
	// SMS, APNS, GCM, VOICE). The scanner emits the channel type, the enabled and
	// archived flags, the version, and lifecycle timestamps only. For the email
	// channel it additionally records the referenced SES configuration set name
	// and SES identity. Channel credentials, API keys, certificates, tokens, and
	// the email from-address are never read or emitted.
	ResourceTypePinpointChannel = "aws_pinpoint_channel"
)

const (
	// RelationshipPinpointApplicationHasSegment records a Pinpoint segment's
	// membership in its parent application. The target is keyed by the
	// application id the application node publishes, so the edge joins that node.
	RelationshipPinpointApplicationHasSegment = "pinpoint_application_has_segment"
	// RelationshipPinpointChannelInApplication records a Pinpoint channel's
	// membership in its parent application. The target is keyed by the
	// application id the application node publishes, so the edge joins that node.
	RelationshipPinpointChannelInApplication = "pinpoint_channel_in_application"
	// RelationshipPinpointEmailChannelUsesSESIdentity records a Pinpoint email
	// channel's reported SES sending identity. Pinpoint reports the identity ARN;
	// the scanner extracts the identity name from the ARN to match the resource_id
	// the SES email-identity node publishes (the bare verified email/domain name),
	// so the edge joins that node.
	RelationshipPinpointEmailChannelUsesSESIdentity = "pinpoint_email_channel_uses_ses_identity"
	// RelationshipPinpointEmailChannelUsesSESConfigurationSet records a Pinpoint
	// email channel's reported SES configuration set. Pinpoint reports the
	// configuration set name, which matches the resource_id the SES
	// configuration-set node publishes, so the edge joins that node.
	RelationshipPinpointEmailChannelUsesSESConfigurationSet = "pinpoint_email_channel_uses_ses_configuration_set"
)
