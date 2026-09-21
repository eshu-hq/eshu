// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rolesanywhere

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS IAM Roles Anywhere metadata-only facts for one claimed
// account and region. It never reads certificate private material, PEM
// certificate bundles, CRL bodies, session policy documents, or vended session
// credentials, and never mutates Roles Anywhere state. It reports trust anchors,
// profiles, and imported certificate revocation lists plus the
// profile-to-IAM-role, trust-anchor-to-ACM-PCA, and CRL-to-trust-anchor
// relationships.
type Scanner struct {
	// Client is the metadata-only Roles Anywhere snapshot source.
	Client Client
}

// Scan observes Roles Anywhere trust anchors, profiles, and CRLs plus their
// direct IAM-role and ACM-PCA dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary aws.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("rolesanywhere scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", aws.ServiceRolesAnywhere:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = aws.ServiceRolesAnywhere
	default:
		return nil, fmt.Errorf("rolesanywhere scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Roles Anywhere metadata: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, anchor := range snapshot.TrustAnchors {
		next, err := trustAnchorEnvelopes(boundary, anchor)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, profile := range snapshot.Profiles {
		next, err := profileEnvelopes(boundary, profile)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, crl := range snapshot.CRLs {
		next, err := crlEnvelopes(boundary, crl)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []aws.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := aws.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func appendRelationships(
	envelopes []facts.Envelope,
	observations ...*aws.RelationshipObservation,
) ([]facts.Envelope, error) {
	for _, observation := range observations {
		if observation == nil {
			continue
		}
		envelope, err := aws.NewRelationshipEnvelope(*observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func trustAnchorEnvelopes(boundary aws.Boundary, anchor TrustAnchor) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(trustAnchorObservation(boundary, anchor))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	return appendRelationships(envelopes, trustAnchorACMPCARelationship(boundary, anchor))
}

func profileEnvelopes(boundary aws.Boundary, profile Profile) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(profileObservation(boundary, profile))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range profileRoleRelationships(boundary, profile) {
		relationship := relationship
		envelopes, err = appendRelationships(envelopes, &relationship)
		if err != nil {
			return nil, err
		}
	}
	return envelopes, nil
}

func crlEnvelopes(boundary aws.Boundary, crl CRL) ([]facts.Envelope, error) {
	resource, err := aws.NewResourceEnvelope(crlObservation(boundary, crl))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	return appendRelationships(envelopes, crlTrustAnchorRelationship(boundary, crl))
}

func trustAnchorObservation(boundary aws.Boundary, anchor TrustAnchor) aws.ResourceObservation {
	arn := strings.TrimSpace(anchor.ARN)
	name := strings.TrimSpace(anchor.Name)
	resourceID := trustAnchorResourceID(anchor)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeRolesAnywhereTrustAnchor,
		Name:         name,
		Tags:         cloneStringMap(anchor.Tags),
		Attributes: map[string]any{
			"trust_anchor_id": strings.TrimSpace(anchor.TrustAnchorID),
			"enabled":         anchor.Enabled,
			"source_type":     strings.TrimSpace(anchor.SourceType),
			"acm_pca_arn":     strings.TrimSpace(anchor.ACMPCAArn),
			"created_at":      timeOrNil(anchor.CreatedAt),
			"updated_at":      timeOrNil(anchor.UpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func profileObservation(boundary aws.Boundary, profile Profile) aws.ResourceObservation {
	arn := strings.TrimSpace(profile.ARN)
	name := strings.TrimSpace(profile.Name)
	resourceID := profileResourceID(profile)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeRolesAnywhereProfile,
		Name:         name,
		Tags:         cloneStringMap(profile.Tags),
		Attributes: map[string]any{
			"profile_id":                  strings.TrimSpace(profile.ProfileID),
			"enabled":                     profile.Enabled,
			"duration_seconds":            profile.DurationSeconds,
			"accept_role_session_name":    profile.AcceptRoleSessionName,
			"require_instance_properties": profile.RequireInstanceProperties,
			"session_policy_configured":   profile.HasSessionPolicy,
			"attribute_mapping_count":     profile.AttributeMappingCount,
			"role_arns":                   cloneStrings(profile.RoleARNs),
			"managed_policy_arns":         cloneStrings(profile.ManagedPolicyARNs),
			"created_at":                  timeOrNil(profile.CreatedAt),
			"updated_at":                  timeOrNil(profile.UpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func crlObservation(boundary aws.Boundary, crl CRL) aws.ResourceObservation {
	arn := strings.TrimSpace(crl.ARN)
	name := strings.TrimSpace(crl.Name)
	resourceID := crlResourceID(crl)
	return aws.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: aws.ResourceTypeRolesAnywhereCRL,
		Name:         name,
		Tags:         cloneStringMap(crl.Tags),
		Attributes: map[string]any{
			"crl_id":           strings.TrimSpace(crl.CRLID),
			"enabled":          crl.Enabled,
			"trust_anchor_arn": strings.TrimSpace(crl.TrustAnchorARN),
			"created_at":       timeOrNil(crl.CreatedAt),
			"updated_at":       timeOrNil(crl.UpdatedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
