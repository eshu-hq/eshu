// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sagemaker

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func domainObservation(domain Domain) awscloud.ResourceObservation {
	arn := strings.TrimSpace(domain.ARN)
	id := firstNonEmpty(arn, domain.ID, domain.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerDomain,
		Name:         strings.TrimSpace(domain.Name),
		State:        strings.TrimSpace(domain.Status),
		Tags:         cloneStringMap(domain.Tags),
		Attributes: map[string]any{
			"domain_id":          strings.TrimSpace(domain.ID),
			"auth_mode":          strings.TrimSpace(domain.AuthMode),
			"vpc_id":             strings.TrimSpace(domain.VPCID),
			"subnet_ids":         cloneStrings(domain.SubnetIDs),
			"url":                strings.TrimSpace(domain.URL),
			"creation_time":      timeOrNil(domain.CreationTime),
			"last_modified_time": timeOrNil(domain.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, domain.ID, domain.Name},
		SourceRecordID:     id,
	}
}

func userProfileObservation(profile UserProfile) awscloud.ResourceObservation {
	name := strings.TrimSpace(profile.Name)
	domainID := strings.TrimSpace(profile.DomainID)
	// Studio user profiles have no ARN in the list summary; the durable id
	// combines the parent domain and profile name so it stays stable.
	id := firstNonEmpty(userProfileID(domainID, name), name)
	return awscloud.ResourceObservation{
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerUserProfile,
		Name:         name,
		State:        strings.TrimSpace(profile.Status),
		Attributes: map[string]any{
			"domain_id":          domainID,
			"creation_time":      timeOrNil(profile.CreationTime),
			"last_modified_time": timeOrNil(profile.LastModifiedTime),
		},
		CorrelationAnchors: []string{id, name},
		SourceRecordID:     id,
	}
}

func appObservation(app App) awscloud.ResourceObservation {
	arn := strings.TrimSpace(app.ARN)
	id := firstNonEmpty(arn, appID(app))
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerApp,
		Name:         strings.TrimSpace(app.Name),
		State:        strings.TrimSpace(app.Status),
		Attributes: map[string]any{
			"app_type":          strings.TrimSpace(app.Type),
			"domain_id":         strings.TrimSpace(app.DomainID),
			"user_profile_name": strings.TrimSpace(app.UserProfile),
			"space_name":        strings.TrimSpace(app.SpaceName),
			"creation_time":     timeOrNil(app.CreationTime),
		},
		CorrelationAnchors: []string{arn, app.Name},
		SourceRecordID:     id,
	}
}

func inferenceComponentObservation(component InferenceComponent) awscloud.ResourceObservation {
	arn := strings.TrimSpace(component.ARN)
	id := firstNonEmpty(arn, component.Name)
	return awscloud.ResourceObservation{
		ARN:          arn,
		ResourceID:   id,
		ResourceType: awscloud.ResourceTypeSageMakerInferenceComponent,
		Name:         strings.TrimSpace(component.Name),
		State:        strings.TrimSpace(component.Status),
		Attributes: map[string]any{
			"endpoint_name":      strings.TrimSpace(component.EndpointName),
			"endpoint_arn":       strings.TrimSpace(component.EndpointARN),
			"variant_name":       strings.TrimSpace(component.VariantName),
			"creation_time":      timeOrNil(component.CreationTime),
			"last_modified_time": timeOrNil(component.LastModifiedTime),
		},
		CorrelationAnchors: []string{arn, component.Name},
		SourceRecordID:     id,
	}
}

// userProfileID builds a stable identity for a Studio user profile that lacks
// an ARN in its list summary.
func userProfileID(domainID, name string) string {
	if domainID == "" || name == "" {
		return ""
	}
	return "sagemaker:user-profile:" + domainID + ":" + name
}

// appID builds a stable identity for a Studio app that lacks an ARN.
func appID(app App) string {
	parts := []string{
		strings.TrimSpace(app.DomainID),
		firstNonEmpty(app.UserProfile, app.SpaceName),
		strings.TrimSpace(app.Type),
		strings.TrimSpace(app.Name),
	}
	for _, part := range parts {
		if part == "" {
			return strings.TrimSpace(app.Name)
		}
	}
	return "sagemaker:app:" + strings.Join(parts, ":")
}
