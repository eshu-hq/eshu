// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedaccess

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Verified Access observations for one AWS
// claim. Implementations read control-plane metadata through the EC2 Verified
// Access describe APIs and never read or persist trust-provider client secrets,
// policy bodies, or any data-plane payload.
type Client interface {
	// Snapshot returns every Verified Access instance, group, endpoint, and trust
	// provider visible to the configured AWS credentials in the boundary region.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Verified Access control-plane metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Instances are the metadata-only Verified Access instances.
	Instances []Instance
	// Groups are the metadata-only Verified Access groups.
	Groups []Group
	// Endpoints are the metadata-only Verified Access endpoints.
	Endpoints []Endpoint
	// TrustProviders are the metadata-only Verified Access trust providers.
	TrustProviders []TrustProvider
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Instance is the scanner-owned Verified Access instance model. It carries
// control-plane metadata only.
type Instance struct {
	// ID is the Verified Access instance id (vai-...).
	ID string
	// Description is the instance description, when set.
	Description string
	// FIPSEnabled reports whether FIPS support is enabled on the instance.
	FIPSEnabled bool
	// CustomerManagedKeyEnabled reports whether customer-managed KMS keys are in
	// use for server-side encryption.
	CustomerManagedKeyEnabled bool
	// TrustProviderIDs are the ids of the trust providers attached to the
	// instance.
	TrustProviderIDs []string
	// CreationTime is when the instance was created.
	CreationTime time.Time
	// LastUpdatedTime is when the instance was last updated.
	LastUpdatedTime time.Time
	// Tags carries the instance resource tags.
	Tags map[string]string
}

// Group is the scanner-owned Verified Access group model. It carries
// control-plane metadata only; the group policy document stays out of the
// contract.
type Group struct {
	// ARN is the Amazon Resource Name that uniquely identifies the group.
	ARN string
	// ID is the Verified Access group id (vagr-...).
	ID string
	// InstanceID is the id of the parent Verified Access instance.
	InstanceID string
	// Owner is the AWS account number that owns the group.
	Owner string
	// Description is the group description, when set.
	Description string
	// CustomerManagedKeyEnabled reports whether customer-managed KMS keys are in
	// use for server-side encryption.
	CustomerManagedKeyEnabled bool
	// CreationTime is when the group was created.
	CreationTime time.Time
	// LastUpdatedTime is when the group was last updated.
	LastUpdatedTime time.Time
	// Tags carries the group resource tags.
	Tags map[string]string
}

// Endpoint is the scanner-owned Verified Access endpoint model. It carries
// control-plane metadata only; the endpoint policy document stays out of the
// contract.
type Endpoint struct {
	// ID is the Verified Access endpoint id (vae-...).
	ID string
	// GroupID is the id of the parent Verified Access group.
	GroupID string
	// InstanceID is the id of the parent Verified Access instance.
	InstanceID string
	// Description is the endpoint description, when set.
	Description string
	// EndpointType is the endpoint type (load-balancer, network-interface, cidr,
	// rds).
	EndpointType string
	// AttachmentType is the attachment type used to connect the endpoint to the
	// application (for example vpc).
	AttachmentType string
	// ApplicationDomain is the DNS name users reach the application through.
	ApplicationDomain string
	// EndpointDomain is the DNS name AWS generates for the endpoint.
	EndpointDomain string
	// Status is the endpoint status code.
	Status string
	// DomainCertificateARN is the ARN of the public ACM TLS certificate, when
	// configured.
	DomainCertificateARN string
	// SubnetIDs are the bare subnet ids the endpoint is placed in.
	SubnetIDs []string
	// SecurityGroupIDs are the bare security-group ids attached to the endpoint.
	SecurityGroupIDs []string
	// LoadBalancerARN is the ARN of the backing load balancer for a
	// load-balancer-type endpoint, when reported.
	LoadBalancerARN string
	// NetworkInterfaceID is the id of the backing network interface for a
	// network-interface-type endpoint, when reported.
	NetworkInterfaceID string
	// CreationTime is when the endpoint was created.
	CreationTime time.Time
	// LastUpdatedTime is when the endpoint was last updated.
	LastUpdatedTime time.Time
	// Tags carries the endpoint resource tags.
	Tags map[string]string
}

// TrustProvider is the scanner-owned Verified Access trust provider model. It
// carries control-plane metadata only. OIDC client identifiers and client
// secrets are never read or persisted; only the OIDC issuer reference is kept.
type TrustProvider struct {
	// ID is the Verified Access trust provider id (vatp-...).
	ID string
	// Description is the trust provider description, when set.
	Description string
	// TrustProviderType is the trust provider type (user or device).
	TrustProviderType string
	// UserTrustProviderType is the user-based trust provider type
	// (iam-identity-center or oidc), when the trust provider is user-based.
	UserTrustProviderType string
	// DeviceTrustProviderType is the device-based trust provider type (jamf,
	// crowdstrike, jumpcloud), when the trust provider is device-based.
	DeviceTrustProviderType string
	// PolicyReferenceName is the identifier used when writing policy rules.
	PolicyReferenceName string
	// OIDCIssuer is the OIDC issuer reference, when the trust provider is OIDC
	// user-based. Client identifiers, client secrets, and token/userinfo
	// endpoints are intentionally excluded.
	OIDCIssuer string
	// CustomerManagedKeyEnabled reports whether customer-managed KMS keys are in
	// use for server-side encryption.
	CustomerManagedKeyEnabled bool
	// CreationTime is when the trust provider was created.
	CreationTime time.Time
	// LastUpdatedTime is when the trust provider was last updated.
	LastUpdatedTime time.Time
	// Tags carries the trust provider resource tags.
	Tags map[string]string
}
