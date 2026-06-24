// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package globalaccelerator

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

const (
	// ec2InstanceTargetType is the Eshu resource type an EC2-instance endpoint
	// reference resolves to. The EC2 scanner does not emit instance resources,
	// so this is target evidence only.
	ec2InstanceTargetType = "aws_ec2_instance"
	// genericResourceTargetType keeps an endpoint relationship honest when the
	// reported endpoint id does not match a known target family.
	genericResourceTargetType = "aws_resource"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// endpointTargetType classifies a Global Accelerator endpoint id into the Eshu
// resource-type family it references. AWS reports the endpoint id as an
// ALB/NLB ARN, an Elastic IP allocation id (eipalloc-...), or an EC2 instance
// id (i-...). An unrecognized id keeps a relationship honest by falling back to
// the generic resource type so downstream correlation can resolve it later.
func endpointTargetType(endpointID string) string {
	id := strings.TrimSpace(endpointID)
	switch {
	case strings.Contains(id, ":elasticloadbalancing:"):
		return awscloud.ResourceTypeELBv2LoadBalancer
	case strings.HasPrefix(id, "eipalloc-"):
		return awscloud.ResourceTypeVPCElasticIP
	case strings.HasPrefix(id, "i-"):
		return ec2InstanceTargetType
	default:
		return genericResourceTargetType
	}
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
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
