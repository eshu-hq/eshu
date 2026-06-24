// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ses

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon SES (v2) email-identity, configuration
// set, configuration-set event-destination, and dedicated-IP-pool observations
// for one AWS claim. Implementations read control-plane metadata through the
// SES v2 management APIs and never send email, read message or template bodies,
// or persist DKIM private keys, DKIM signing tokens, identity policies, or SMTP
// credentials.
type Client interface {
	// Snapshot returns every SES email identity, configuration set, and
	// dedicated IP pool visible to the configured AWS credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures SES metadata plus non-fatal scan warnings.
type Snapshot struct {
	// EmailIdentities is the metadata-only set of SES email identities.
	EmailIdentities []EmailIdentity
	// ConfigurationSets is the metadata-only set of SES configuration sets, each
	// carrying its event destinations.
	ConfigurationSets []ConfigurationSet
	// DedicatedIPPools is the metadata-only set of SES dedicated IP pool names.
	DedicatedIPPools []DedicatedIPPool
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// EmailIdentity is the scanner-owned SES email identity model. It carries
// control-plane metadata only and intentionally excludes DKIM private keys,
// DKIM CNAME/signing tokens, and identity policy documents.
type EmailIdentity struct {
	// Name is the email address or domain that names the identity.
	Name string
	// IdentityType is the SES identity type (EMAIL_ADDRESS, DOMAIN, or
	// MANAGED_DOMAIN) reported for the identity.
	IdentityType string
	// VerificationStatus is the identity verification status (for example
	// SUCCESS, PENDING, FAILED, NOT_STARTED).
	VerificationStatus string
	// VerifiedForSendingStatus reports whether the identity is verified for
	// sending.
	VerifiedForSendingStatus bool
	// SendingEnabled reports whether email can currently be sent from the
	// identity.
	SendingEnabled bool
	// FeedbackForwardingStatus reports whether bounce/complaint feedback
	// forwarding by email is enabled.
	FeedbackForwardingStatus bool
	// ConfigurationSetName is the default configuration set used when sending
	// from this identity, when one is configured.
	ConfigurationSetName string
	// DKIMEnabled reports whether DKIM signing is enabled for the identity.
	DKIMEnabled bool
	// DKIMStatus is the DKIM verification status enum (for example SUCCESS,
	// PENDING, NOT_STARTED). Signing tokens are intentionally excluded.
	DKIMStatus string
	// DKIMSigningAttributesOrigin reports how DKIM was configured (AWS_SES easy
	// DKIM, EXTERNAL bring-your-own-DKIM, or a DEED replication origin enum). The
	// private key material behind these origins is never read.
	DKIMSigningAttributesOrigin string
	// DKIMKMSKeyID is the customer KMS key identifier SES reports for the
	// identity's DKIM material, when one is ever reported. SES v2 does not
	// surface a customer KMS key on the DKIM attributes today, so this stays
	// empty in practice; it exists so the graph edge can join a real key node if
	// AWS ever reports one.
	DKIMKMSKeyID string
	// MailFromDomain is the custom MAIL FROM domain, when configured.
	MailFromDomain string
	// MailFromDomainStatus is the MAIL FROM domain status enum.
	MailFromDomainStatus string
	// MailFromBehaviorOnMxFailure is the configured behavior when the MAIL FROM
	// MX record lookup fails (USE_DEFAULT_VALUE or REJECT_MESSAGE).
	MailFromBehaviorOnMxFailure string
	// Tags carries the identity resource tags.
	Tags map[string]string
}

// ConfigurationSet is the scanner-owned SES configuration set model. It carries
// control-plane metadata only.
type ConfigurationSet struct {
	// Name is the configuration set name.
	Name string
	// SendingEnabled reports whether sending is enabled for the configuration
	// set.
	SendingEnabled bool
	// ReputationMetricsEnabled reports whether reputation metrics are emitted to
	// CloudWatch for the configuration set.
	ReputationMetricsEnabled bool
	// TLSPolicy is the delivery TLS policy enum (REQUIRE or OPTIONAL).
	TLSPolicy string
	// SendingPoolName is the dedicated IP pool the configuration set sends
	// through, when one is configured.
	SendingPoolName string
	// CustomRedirectDomain is the open/click tracking custom redirect domain,
	// when configured.
	CustomRedirectDomain string
	// Tags carries the configuration set resource tags.
	Tags map[string]string
	// EventDestinations are the metadata-only event destinations attached to the
	// configuration set.
	EventDestinations []EventDestination
}

// EventDestination is the scanner-owned SES configuration-set event-destination
// model. It carries the destination class and resolvable target references
// only; it never carries destination secrets, HEC tokens, or access keys.
type EventDestination struct {
	// Name is the event destination name within the configuration set.
	Name string
	// Enabled reports whether the event destination is enabled.
	Enabled bool
	// MatchingEventTypes are the SES event types the destination receives (for
	// example SEND, DELIVERY, BOUNCE, COMPLAINT).
	MatchingEventTypes []string
	// SNSTopicARN is the Amazon SNS topic ARN the destination publishes to, when
	// configured. It is a resolvable reference, not a secret.
	SNSTopicARN string
	// FirehoseDeliveryStreamARN is the Amazon Data Firehose delivery stream ARN
	// the destination streams to, when configured.
	FirehoseDeliveryStreamARN string
	// FirehoseIAMRoleARN is the IAM role ARN SES assumes to write to the Firehose
	// delivery stream, when configured.
	FirehoseIAMRoleARN string
	// EventBridgeBusARN is the Amazon EventBridge event bus ARN the destination
	// targets, when configured.
	EventBridgeBusARN string
	// CloudWatchEnabled reports whether a CloudWatch dimension destination is
	// configured. Only presence is recorded; no dimension payload is persisted.
	CloudWatchEnabled bool
	// PinpointApplicationARN is the Amazon Pinpoint application ARN the
	// destination targets, when configured.
	PinpointApplicationARN string
}

// DedicatedIPPool is the scanner-owned SES dedicated IP pool model. It carries
// the pool name only; the dedicated IP addresses are not enumerated.
type DedicatedIPPool struct {
	// Name is the dedicated IP pool name.
	Name string
}
