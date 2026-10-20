// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package proton

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// environmentObservation maps a Proton environment into a reported-confidence
// resource observation. It records identity, the environment template name and
// version, provisioning mode, deployment status, the Proton service-role ARN,
// and the provisioning account id only. The environment spec manifest body and
// any deployment input parameter values are intentionally excluded.
func environmentObservation(boundary awscloud.Boundary, environment Environment) awscloud.ResourceObservation {
	arn := strings.TrimSpace(environment.ARN)
	name := strings.TrimSpace(environment.Name)
	resourceID := environmentResourceID(environment)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeProtonEnvironment,
		Name:         name,
		State:        strings.TrimSpace(environment.DeploymentStatus),
		Tags:         cloneStringMap(environment.Tags),
		Attributes: map[string]any{
			"environment_name":          name,
			"template_name":             strings.TrimSpace(environment.TemplateName),
			"template_major_version":    strings.TrimSpace(environment.TemplateMajorVersion),
			"template_minor_version":    strings.TrimSpace(environment.TemplateMinorVersion),
			"provisioning":              strings.TrimSpace(environment.Provisioning),
			"deployment_status":         strings.TrimSpace(environment.DeploymentStatus),
			"description":               strings.TrimSpace(environment.Description),
			"proton_service_role_arn":   strings.TrimSpace(environment.ProtonServiceRoleArn),
			"environment_account_id":    strings.TrimSpace(environment.EnvironmentAccountID),
			"creation_time":             timeOrNil(environment.CreatedAt),
			"last_deployment_succeeded": timeOrNil(environment.LastDeploymentSucceededAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

// serviceObservation maps a Proton service into a reported-confidence resource
// observation. It records identity, the service template name, status, and the
// source repository linkage by reference only. The service spec manifest body
// and pipeline spec body are intentionally excluded.
func serviceObservation(boundary awscloud.Boundary, service Service) awscloud.ResourceObservation {
	arn := strings.TrimSpace(service.ARN)
	name := strings.TrimSpace(service.Name)
	resourceID := serviceResourceID(service)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeProtonService,
		Name:         name,
		State:        strings.TrimSpace(service.Status),
		Tags:         cloneStringMap(service.Tags),
		Attributes: map[string]any{
			"service_name":              name,
			"template_name":             strings.TrimSpace(service.TemplateName),
			"status":                    strings.TrimSpace(service.Status),
			"description":               strings.TrimSpace(service.Description),
			"branch_name":               strings.TrimSpace(service.BranchName),
			"repository_id":             strings.TrimSpace(service.RepositoryID),
			"repository_connection_arn": strings.TrimSpace(service.RepositoryConnectionArn),
			"creation_time":             timeOrNil(service.CreatedAt),
			"last_modified_time":        timeOrNil(service.LastModifiedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

// templateObservation maps a Proton environment or service template into a
// reported-confidence resource observation under resourceType. It records
// identity, display name, provisioning mode, and the recommended version only;
// every template version schema body is intentionally excluded.
func templateObservation(boundary awscloud.Boundary, template Template, resourceType string) awscloud.ResourceObservation {
	arn := strings.TrimSpace(template.ARN)
	name := strings.TrimSpace(template.Name)
	resourceID := templateResourceID(template)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: resourceType,
		Name:         name,
		Tags:         cloneStringMap(template.Tags),
		Attributes: map[string]any{
			"template_name":       name,
			"display_name":        strings.TrimSpace(template.DisplayName),
			"description":         strings.TrimSpace(template.Description),
			"provisioning":        strings.TrimSpace(template.Provisioning),
			"recommended_version": strings.TrimSpace(template.RecommendedVersion),
			"creation_time":       timeOrNil(template.CreatedAt),
			"last_modified_time":  timeOrNil(template.LastModifiedAt),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}
