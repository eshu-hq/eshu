// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceSigner identifies the regional AWS Signer (code-signing) metadata
	// scan slice. The scanner reads signing-profile and signing-platform
	// control-plane metadata through the Signer management APIs
	// (ListSigningProfiles, GetSigningProfile, ListSigningPlatforms); profile
	// tags come from the ListSigningProfiles response rather than a separate
	// tag read. It never reads signing jobs, signing material private keys,
	// signed-object payloads, or revocation records, and never starts,
	// cancels, or mutates a signing operation.
	ServiceSigner = "signer"
)

const (
	// ResourceTypeSignerSigningProfile identifies an AWS Signer signing-profile
	// metadata resource. The scanner emits identity, the bound signing platform,
	// the profile version, status, signature validity period, signing image
	// format, the names (never the values) of signing parameters, and the ACM
	// certificate ARN reference only. Signing jobs, private key material, and
	// signed-object payloads stay outside the contract.
	ResourceTypeSignerSigningProfile = "aws_signer_signing_profile"
	// ResourceTypeSignerSigningPlatform identifies an AWS Signer signing-platform
	// metadata resource. The scanner emits the platform id, display name,
	// category, signing target, partner, maximum signable size, and the
	// revocation-supported flag only. Algorithm option sets are recorded as
	// names so no signing-material detail is persisted.
	ResourceTypeSignerSigningPlatform = "aws_signer_signing_platform"
)

const (
	// RelationshipSignerProfileUsesACMCertificate records a signing profile's
	// reported ACM certificate dependency. AWS reports the certificate ARN in
	// the profile's SigningMaterial, which matches how the ACM scanner publishes
	// its certificate resource_id, so the edge joins the certificate node
	// exactly.
	RelationshipSignerProfileUsesACMCertificate = "signer_profile_uses_acm_certificate"
	// RelationshipSignerProfileUsesSigningPlatform records a signing profile's
	// binding to the signing platform it was created with. The target is keyed
	// by the bare platform id this scanner also publishes as a
	// signing-platform resource_id, so the internal edge joins the platform node
	// the scanner emits.
	RelationshipSignerProfileUsesSigningPlatform = "signer_profile_uses_signing_platform"
)
