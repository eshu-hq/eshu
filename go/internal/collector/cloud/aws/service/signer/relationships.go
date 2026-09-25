// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package signer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// profileACMCertificateRelationship records a signing profile's reported ACM
// certificate dependency. AWS reports a certificate ARN in the profile's
// SigningMaterial, which matches how the ACM scanner publishes its certificate
// resource_id, so the edge joins the certificate node exactly. It returns nil
// when no certificate is reported. Only the ARN reference is emitted; the
// certificate body and private key are never read.
func profileACMCertificateRelationship(
	boundary aws.Boundary,
	profile SigningProfile,
) *aws.RelationshipObservation {
	targetARN := strings.TrimSpace(profile.CertificateARN)
	if targetARN == "" || !isARN(targetARN) {
		return nil
	}
	sourceID := profileResourceID(profile)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSignerProfileUsesACMCertificate,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(profile.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       aws.ResourceTypeACMCertificate,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSignerProfileUsesACMCertificate + ":" + targetARN,
	}
}

// profileSigningPlatformRelationship records a signing profile's binding to the
// signing platform it was created with. The target is keyed by the bare
// platform id this scanner also publishes as a signing-platform resource_id, so
// the internal edge joins the platform node the scanner emits. It returns nil
// when no platform id is reported. Signer platforms carry no ARN, so the edge
// keys the bare id with no target_arn.
func profileSigningPlatformRelationship(
	boundary aws.Boundary,
	profile SigningProfile,
) *aws.RelationshipObservation {
	platformID := strings.TrimSpace(profile.PlatformID)
	if platformID == "" {
		return nil
	}
	sourceID := profileResourceID(profile)
	if sourceID == "" {
		return nil
	}
	return &aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipSignerProfileUsesSigningPlatform,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(profile.ARN),
		TargetResourceID: platformID,
		TargetType:       aws.ResourceTypeSignerSigningPlatform,
		SourceRecordID:   sourceID + "->" + aws.RelationshipSignerProfileUsesSigningPlatform + ":" + platformID,
	}
}
