// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package signer

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Signer (code-signing) metadata-only facts for one claimed
// account and region. It never reads signing jobs, signing-material private
// keys, or signed-object payloads, and never starts, cancels, or mutates a
// signing operation. It reports signing profiles and signing platforms plus the
// profile-to-ACM-certificate and profile-to-signing-platform relationships.
type Scanner struct {
	// Client is the metadata-only Signer snapshot source.
	Client Client
}

// Scan observes Signer signing profiles, signing platforms, and the profile
// ACM-certificate and signing-platform dependency metadata through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("signer scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceSigner:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceSigner
	default:
		return nil, fmt.Errorf("signer scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Signer signing profiles: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, platform := range snapshot.Platforms {
		envelope, err := awscloud.NewResourceEnvelope(platformObservation(boundary, platform))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, profile := range snapshot.Profiles {
		next, err := profileEnvelopes(boundary, profile)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func profileEnvelopes(boundary awscloud.Boundary, profile SigningProfile) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(profileObservation(boundary, profile))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range []*awscloud.RelationshipObservation{
		profileACMCertificateRelationship(boundary, profile),
		profileSigningPlatformRelationship(boundary, profile),
	} {
		if relationship == nil {
			continue
		}
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func profileObservation(boundary awscloud.Boundary, profile SigningProfile) awscloud.ResourceObservation {
	profileARN := strings.TrimSpace(profile.ARN)
	name := strings.TrimSpace(profile.Name)
	resourceID := profileResourceID(profile)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          profileARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeSignerSigningProfile,
		Name:         name,
		State:        strings.TrimSpace(profile.Status),
		Tags:         cloneStringMap(profile.Tags),
		Attributes: map[string]any{
			"profile_name":             name,
			"profile_version":          strings.TrimSpace(profile.ProfileVersion),
			"profile_version_arn":      strings.TrimSpace(profile.ProfileVersionARN),
			"platform_id":              strings.TrimSpace(profile.PlatformID),
			"platform_display_name":    strings.TrimSpace(profile.PlatformDisplayName),
			"signature_validity_type":  strings.TrimSpace(profile.SignatureValidityType),
			"signature_validity_value": validityValueOrNil(profile),
			"signing_image_format":     strings.TrimSpace(profile.SigningImageFormat),
			"signing_parameter_names":  cloneStrings(profile.SigningParameterNames),
			"certificate_arn":          strings.TrimSpace(profile.CertificateARN),
		},
		CorrelationAnchors: []string{profileARN, name},
		SourceRecordID:     resourceID,
	}
}

func platformObservation(boundary awscloud.Boundary, platform SigningPlatform) awscloud.ResourceObservation {
	platformID := platformResourceID(platform)
	displayName := strings.TrimSpace(platform.DisplayName)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   platformID,
		ResourceType: awscloud.ResourceTypeSignerSigningPlatform,
		Name:         firstNonEmpty(displayName, platformID),
		Attributes: map[string]any{
			"platform_id":          platformID,
			"display_name":         displayName,
			"category":             strings.TrimSpace(platform.Category),
			"target":               strings.TrimSpace(platform.Target),
			"partner":              strings.TrimSpace(platform.Partner),
			"max_size_in_mb":       platform.MaxSizeInMB,
			"revocation_supported": platform.RevocationSupported,
		},
		CorrelationAnchors: []string{platformID, displayName},
		SourceRecordID:     platformID,
	}
}

// validityValueOrNil returns the signature validity value, or nil when no
// validity period is reported, so the attribute payload omits an unknown value
// instead of emitting a meaningless zero.
func validityValueOrNil(profile SigningProfile) any {
	if strings.TrimSpace(profile.SignatureValidityType) == "" {
		return nil
	}
	return profile.SignatureValidityValue
}
