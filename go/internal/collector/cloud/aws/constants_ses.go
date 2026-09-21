// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSES identifies the regional Amazon Simple Email Service (SES v2)
	// metadata-only scan slice. The scanner reads email-identity, configuration
	// set, configuration-set event-destination, and dedicated-IP-pool
	// control-plane metadata through the SES v2 management APIs
	// (ListEmailIdentities, GetEmailIdentity, ListConfigurationSets,
	// GetConfigurationSet, GetConfigurationSetEventDestinations, and
	// ListDedicatedIpPools). It never sends email, never reads message or
	// template bodies, and never persists DKIM private keys, DKIM signing
	// tokens, identity policies, or SMTP credentials.
	ServiceSES = "ses"
)

const (
	// ResourceTypeSESEmailIdentity identifies an Amazon SES email identity
	// metadata resource (a verified email address or domain). The scanner emits
	// identity, identity type, verification status, DKIM enabled/status and
	// signing-attributes-origin enums, the MAIL FROM domain and behavior, and
	// the feedback-forwarding flag only. DKIM private keys and signing tokens
	// are never read or emitted.
	ResourceTypeSESEmailIdentity = "aws_ses_email_identity"
	// ResourceTypeSESConfigurationSet identifies an Amazon SES configuration set
	// metadata resource. The scanner emits identity, sending/reputation/TLS
	// options, the referenced sending (dedicated IP) pool name, and the tracking
	// custom-redirect domain only.
	ResourceTypeSESConfigurationSet = "aws_ses_configuration_set"
	// ResourceTypeSESEventDestination identifies an Amazon SES configuration-set
	// event destination metadata resource. The scanner emits identity, enabled
	// flag, matching event types, and the destination class (SNS, Firehose,
	// EventBridge, CloudWatch, Pinpoint) only. No destination secret, HEC token,
	// or access key is ever read.
	ResourceTypeSESEventDestination = "aws_ses_event_destination"
	// ResourceTypeSESDedicatedIPPool identifies an Amazon SES dedicated IP pool
	// metadata resource. The scanner emits the pool name only; the dedicated IP
	// addresses themselves are not enumerated.
	ResourceTypeSESDedicatedIPPool = "aws_ses_dedicated_ip_pool"
)

const (
	// RelationshipSESEmailIdentityUsesConfigurationSet records an SES email
	// identity's reported default configuration set. The target is keyed by the
	// configuration set name, which is the resource_id the SES configuration-set
	// node publishes, so the edge joins that node.
	RelationshipSESEmailIdentityUsesConfigurationSet = "ses_email_identity_uses_configuration_set"
	// RelationshipSESEmailIdentityDKIMUsesKMSKey records an SES email identity's
	// reported DKIM customer KMS key dependency. SES v2 does not surface a
	// customer KMS key on the DKIM attributes today, so the scanner emits this
	// edge only on the defensive path where AWS ever reports a key identifier;
	// the target is keyed by the KMS key identifier the KMS scanner publishes.
	RelationshipSESEmailIdentityDKIMUsesKMSKey = "ses_email_identity_dkim_uses_kms_key"
	// RelationshipSESConfigurationSetUsesDedicatedIPPool records a configuration
	// set's reported sending (dedicated IP) pool. The target is keyed by the pool
	// name, the resource_id the SES dedicated-IP-pool node publishes.
	RelationshipSESConfigurationSetUsesDedicatedIPPool = "ses_configuration_set_uses_dedicated_ip_pool"
	// RelationshipSESEventDestinationPublishesToSNSTopic records a configuration-
	// set event destination's reported Amazon SNS topic target. SES reports the
	// topic ARN, which matches how the SNS scanner publishes its topic
	// resource_id, so the edge joins the topic node.
	RelationshipSESEventDestinationPublishesToSNSTopic = "ses_event_destination_publishes_to_sns_topic"
	// RelationshipSESEventDestinationStreamsToFirehose records a configuration-
	// set event destination's reported Amazon Data Firehose delivery stream
	// target. SES reports the delivery stream ARN, which matches how the Firehose
	// scanner publishes its delivery-stream resource_id, so the edge joins the
	// stream node.
	RelationshipSESEventDestinationStreamsToFirehose = "ses_event_destination_streams_to_firehose"
)
