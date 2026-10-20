// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticbeanstalk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits AWS Elastic Beanstalk application, environment, application
// version, and relationship facts for one claimed account and region.
//
// The scanner is metadata-only. It never reads or persists environment
// option-setting values in clear text: every option value is replaced with a
// redaction marker before persistence, and the scanner-owned types carry no
// field for source bundle object contents or environment-info bundles.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes Elastic Beanstalk resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("elasticbeanstalk scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("elasticbeanstalk scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceElasticBeanstalk:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceElasticBeanstalk
	default:
		return nil, fmt.Errorf("elasticbeanstalk scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	applications, err := s.Client.DescribeApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Elastic Beanstalk applications: %w", err)
	}
	applicationARNByName := map[string]string{}
	for _, application := range applications {
		applicationARNByName[strings.TrimSpace(application.Name)] = strings.TrimSpace(application.ARN)
		resource, err := awscloud.NewResourceEnvelope(applicationObservation(boundary, application))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	versions, err := s.Client.DescribeApplicationVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Elastic Beanstalk application versions: %w", err)
	}
	versionARNByKey := map[string]string{}
	for _, version := range versions {
		versionARNByKey[versionKey(version.ApplicationName, version.VersionLabel)] = strings.TrimSpace(version.ARN)
		resource, err := awscloud.NewResourceEnvelope(applicationVersionObservation(boundary, version))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	environments, err := s.Client.DescribeEnvironments(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe Elastic Beanstalk environments: %w", err)
	}
	for _, environment := range environments {
		environmentEnvelopes, err := s.environmentEnvelopes(
			ctx,
			boundary,
			environment,
			applicationARNByName,
			versionARNByKey,
		)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, environmentEnvelopes...)
	}

	return envelopes, nil
}

func (s Scanner) environmentEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	environment Environment,
	applicationARNByName map[string]string,
	versionARNByKey map[string]string,
) ([]facts.Envelope, error) {
	resources, err := s.Client.DescribeEnvironmentResources(ctx, environment.ID)
	if err != nil {
		return nil, fmt.Errorf("describe Elastic Beanstalk environment resources for %q: %w", environment.Name, err)
	}
	settings, err := s.Client.DescribeConfigurationSettings(ctx, environment.ApplicationName, environment.Name)
	if err != nil {
		return nil, fmt.Errorf("describe Elastic Beanstalk configuration settings for %q: %w", environment.Name, err)
	}

	resource, err := awscloud.NewResourceEnvelope(environmentObservation(boundary, environment, settings, s.RedactionKey))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	for _, observation := range environmentRelationships(
		boundary,
		environment,
		resources,
		settings,
		applicationARNByName,
		versionARNByKey,
	) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func applicationObservation(boundary awscloud.Boundary, application Application) awscloud.ResourceObservation {
	applicationARN := strings.TrimSpace(application.ARN)
	name := strings.TrimSpace(application.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          applicationARN,
		ResourceID:   firstNonEmpty(applicationARN, name),
		ResourceType: awscloud.ResourceTypeElasticBeanstalkApplication,
		Name:         name,
		Attributes: map[string]any{
			"description":             strings.TrimSpace(application.Description),
			"configuration_templates": cloneStrings(application.ConfigurationTemplates),
			"version_labels":          cloneStrings(application.VersionLabels),
			"date_created":            timeOrNil(application.DateCreated),
			"date_updated":            timeOrNil(application.DateUpdated),
		},
		CorrelationAnchors: []string{applicationARN, name},
		SourceRecordID:     firstNonEmpty(applicationARN, name),
	}
}

func applicationVersionObservation(boundary awscloud.Boundary, version ApplicationVersion) awscloud.ResourceObservation {
	versionARN := strings.TrimSpace(version.ARN)
	label := strings.TrimSpace(version.VersionLabel)
	appName := strings.TrimSpace(version.ApplicationName)
	resourceID := firstNonEmpty(versionARN, versionKey(appName, label))
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          versionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElasticBeanstalkApplicationVersion,
		Name:         label,
		State:        strings.TrimSpace(version.Status),
		Attributes: map[string]any{
			"application_name":  appName,
			"version_label":     label,
			"description":       strings.TrimSpace(version.Description),
			"source_s3_bucket":  strings.TrimSpace(version.SourceS3Bucket),
			"source_s3_key":     strings.TrimSpace(version.SourceS3Key),
			"source_repository": strings.TrimSpace(version.SourceRepository),
			"build_arn":         strings.TrimSpace(version.BuildARN),
			"date_created":      timeOrNil(version.DateCreated),
			"date_updated":      timeOrNil(version.DateUpdated),
		},
		CorrelationAnchors: []string{versionARN, label, appName},
		SourceRecordID:     resourceID,
	}
}

func environmentObservation(
	boundary awscloud.Boundary,
	environment Environment,
	settings []OptionSetting,
	key redact.Key,
) awscloud.ResourceObservation {
	environmentARN := strings.TrimSpace(environment.ARN)
	environmentID := strings.TrimSpace(environment.ID)
	name := strings.TrimSpace(environment.Name)
	resourceID := firstNonEmpty(environmentARN, environmentID, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          environmentARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElasticBeanstalkEnvironment,
		Name:         name,
		State:        strings.TrimSpace(environment.Status),
		Attributes: map[string]any{
			"environment_id":      environmentID,
			"application_name":    strings.TrimSpace(environment.ApplicationName),
			"status":              strings.TrimSpace(environment.Status),
			"health":              strings.TrimSpace(environment.Health),
			"health_status":       strings.TrimSpace(environment.HealthStatus),
			"tier_name":           strings.TrimSpace(environment.TierName),
			"tier_type":           strings.TrimSpace(environment.TierType),
			"platform_arn":        strings.TrimSpace(environment.PlatformARN),
			"solution_stack_name": strings.TrimSpace(environment.SolutionStackName),
			"cname":               strings.TrimSpace(environment.CNAME),
			"endpoint_url":        strings.TrimSpace(environment.EndpointURL),
			"version_label":       strings.TrimSpace(environment.VersionLabel),
			"template_name":       strings.TrimSpace(environment.TemplateName),
			"operations_role":     strings.TrimSpace(environment.OperationsRole),
			"date_created":        timeOrNil(environment.DateCreated),
			"date_updated":        timeOrNil(environment.DateUpdated),
			"option_settings":     optionSettingMaps(settings, key),
		},
		CorrelationAnchors: []string{environmentARN, environmentID, name},
		SourceRecordID:     resourceID,
	}
}

// optionSettingMaps records the option-setting key identity (namespace and
// option name) and a redaction marker for every value. Elastic Beanstalk option
// values are unknown provider schema that can carry secret environment variable
// values, so the scanner never stores a value in clear text.
func optionSettingMaps(settings []OptionSetting, key redact.Key) []map[string]any {
	if len(settings) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(settings))
	for _, setting := range settings {
		namespace := strings.TrimSpace(setting.Namespace)
		name := strings.TrimSpace(setting.OptionName)
		source := "elasticbeanstalk.environment.option_setting." + namespace + "." + name
		output = append(output, map[string]any{
			"namespace": namespace,
			"name":      name,
			"value":     awscloud.RedactString(setting.Value, source, key),
		})
	}
	return output
}

func versionKey(applicationName, versionLabel string) string {
	return strings.TrimSpace(applicationName) + "/" + strings.TrimSpace(versionLabel)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
