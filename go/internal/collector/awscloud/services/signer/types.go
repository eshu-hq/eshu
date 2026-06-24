// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package signer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Signer signing-profile and
// signing-platform observations for one AWS claim. Implementations read
// control-plane metadata through the Signer management APIs and never read
// signing jobs, signing-material private keys, or signed-object payloads.
type Client interface {
	// Snapshot returns every Signer signing profile and signing platform visible
	// to the configured AWS credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Signer signing-profile and signing-platform metadata plus
// non-fatal scan warnings.
type Snapshot struct {
	// Profiles is the metadata-only set of Signer signing profiles.
	Profiles []SigningProfile
	// Platforms is the metadata-only set of Signer signing platforms available
	// in the account/region.
	Platforms []SigningPlatform
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// SigningProfile is the scanner-owned Signer signing-profile model. It carries
// control-plane metadata only and intentionally excludes signing jobs,
// signing-material private keys, and signed-object payloads. SigningParameters
// values are excluded because they can carry user-supplied data; only the
// parameter names are recorded.
type SigningProfile struct {
	// ARN is the Amazon Resource Name that uniquely identifies the profile.
	ARN string
	// ProfileVersionARN is the ARN including the profile version, when reported.
	ProfileVersionARN string
	// Name is the signing profile name.
	Name string
	// ProfileVersion is the current profile version identifier.
	ProfileVersion string
	// PlatformID is the bare id of the signing platform the profile is bound to.
	PlatformID string
	// PlatformDisplayName is the human-readable platform name.
	PlatformDisplayName string
	// Status is the signing-profile lifecycle status (for example Active).
	Status string
	// SignatureValidityType is the time unit for signature validity (for example
	// DAYS), when reported.
	SignatureValidityType string
	// SignatureValidityValue is the numeric signature-validity value, when
	// reported.
	SignatureValidityValue int32
	// SigningImageFormat is the resolved signing image format the profile uses
	// (from the profile override or platform default), when reported.
	SigningImageFormat string
	// SigningParameterNames are the names of the profile's signing parameters.
	// The values are intentionally excluded because they can carry user data.
	SigningParameterNames []string
	// CertificateARN is the ACM certificate ARN the profile signs with, when a
	// signing material certificate is reported. It is an ARN reference, never the
	// certificate body or private key.
	CertificateARN string
	// Tags carries the signing-profile resource tags.
	Tags map[string]string
}

// SigningPlatform is the scanner-owned Signer signing-platform model. It
// carries control-plane metadata only: identity, category, target, partner,
// maximum signable size, and the revocation-supported flag.
type SigningPlatform struct {
	// PlatformID is the bare id of the signing platform.
	PlatformID string
	// DisplayName is the human-readable platform name.
	DisplayName string
	// Category is the platform category (for example AWSIoT).
	Category string
	// Target is the validation target type the platform signs.
	Target string
	// Partner is any partner entity linked to the platform, when reported.
	Partner string
	// MaxSizeInMB is the maximum code size (in MB) the platform can sign.
	MaxSizeInMB int32
	// RevocationSupported reports whether revocation is supported for the
	// platform.
	RevocationSupported bool
}
