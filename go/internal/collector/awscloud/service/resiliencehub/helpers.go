// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resiliencehub

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// appResourceID returns the resource_id the application node publishes: its ARN
// (always present from the Resilience Hub API), falling back to the app name.
func appResourceID(app App) string {
	return firstNonEmpty(app.ARN, app.Name)
}

// policyResourceID returns the resource_id the resiliency-policy node publishes:
// its ARN, falling back to the policy name.
func policyResourceID(policy ResiliencyPolicy) string {
	return firstNonEmpty(policy.ARN, policy.Name)
}

// componentResourceID returns the resource_id an application-component node
// publishes. Components have no API ARN, so the scanner qualifies the component
// name with the parent application ARN to keep the id stable and unique across
// applications.
func componentResourceID(appID string, component AppComponent) string {
	appID = strings.TrimSpace(appID)
	name := strings.TrimSpace(component.Name)
	switch {
	case appID != "" && name != "":
		return appID + "/component/" + name
	case name != "":
		return name
	default:
		return ""
	}
}

// inputSourceResourceID returns the resource_id an input-source node publishes.
// It prefers the source ARN when reported and otherwise qualifies the source
// name with the parent application ARN so the id is stable and unique.
func inputSourceResourceID(appID string, source InputSource) string {
	if arn := strings.TrimSpace(source.SourceARN); arn != "" {
		return arn
	}
	appID = strings.TrimSpace(appID)
	name := strings.TrimSpace(source.SourceName)
	switch {
	case appID != "" && name != "":
		return appID + "/input-source/" + name
	case name != "":
		return name
	default:
		return ""
	}
}

// assessmentResourceID returns the resource_id an assessment node publishes:
// its ARN.
func assessmentResourceID(assessment Assessment) string {
	return strings.TrimSpace(assessment.ARN)
}

// protectedResourceTargetType maps a Resilience Hub-reported physical resource
// type to the Eshu resource type the owning scanner publishes, for the subset
// of types Resilience Hub identifies by a full ARN that the owning scanner also
// keys by ARN. It returns "" for any type the scanner cannot key safely (the
// Resilience Hub-native, non-ARN families), so the protected-resource edge is
// skipped rather than dangled.
func protectedResourceTargetType(resilienceHubType string) string {
	switch strings.TrimSpace(resilienceHubType) {
	case "AWS::ECS::Service":
		return awscloud.ResourceTypeECSService
	case "AWS::EFS::FileSystem":
		return awscloud.ResourceTypeEFSFileSystem
	case "AWS::ElasticLoadBalancingV2::LoadBalancer":
		return awscloud.ResourceTypeELBv2LoadBalancer
	case "AWS::Lambda::Function":
		return awscloud.ResourceTypeLambdaFunction
	case "AWS::SNS::Topic":
		return awscloud.ResourceTypeSNSTopic
	default:
		return ""
	}
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// int32OrNil returns the int32 value pointed to by value, or nil when the
// pointer is nil, so the attribute payload omits an unknown objective instead
// of emitting a misleading zero.
func int32OrNil(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
