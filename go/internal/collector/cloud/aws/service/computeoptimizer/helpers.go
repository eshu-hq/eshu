// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package computeoptimizer

import (
	"strings"
	"time"
)

// instanceRecommendationID returns the resource_id the instance-recommendation
// node publishes. Each recommendation is identified by the analyzed instance
// ARN so the node and its own edge share one stable id within a generation.
func instanceRecommendationID(rec InstanceRecommendation) string {
	return firstNonEmpty(rec.InstanceARN, rec.InstanceName)
}

// autoScalingGroupRecommendationID returns the resource_id the ASG-recommendation
// node publishes, preferring the analyzed group ARN and falling back to the
// group name.
func autoScalingGroupRecommendationID(rec AutoScalingGroupRecommendation) string {
	return firstNonEmpty(rec.AutoScalingGroupARN, rec.AutoScalingGroupName)
}

// volumeRecommendationID returns the resource_id the volume-recommendation node
// publishes, keyed by the analyzed EBS volume ARN.
func volumeRecommendationID(rec VolumeRecommendation) string {
	return strings.TrimSpace(rec.VolumeARN)
}

// lambdaFunctionRecommendationID returns the resource_id the
// function-recommendation node publishes, keyed by the analyzed function ARN.
func lambdaFunctionRecommendationID(rec LambdaFunctionRecommendation) string {
	return strings.TrimSpace(rec.FunctionARN)
}

// instanceIDFromARN extracts the bare EC2 instance id (i-...) from an instance
// ARN of the form arn:<partition>:ec2:<region>:<account>:instance/<id>. EC2
// instance relationship targets are keyed by the bare id, not the ARN, so the
// edge joins the instance identity other scanners publish. It returns "" when
// the value is not an instance ARN.
func instanceIDFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return ""
	}
	idx := strings.LastIndex(arn, ":instance/")
	if idx < 0 {
		return ""
	}
	id := strings.TrimSpace(arn[idx+len(":instance/"):])
	if !strings.HasPrefix(id, "i-") {
		return ""
	}
	return id
}

// volumeIDFromARN extracts the bare EBS volume id (vol-...) from a volume ARN of
// the form arn:<partition>:ec2:<region>:<account>:volume/<id>. It returns "" when
// the value is not a volume ARN. The id is recorded as recommendation metadata
// only until recommendation-to-volume relationship projection lands.
func volumeIDFromARN(arn string) string {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return ""
	}
	idx := strings.LastIndex(arn, ":volume/")
	if idx < 0 {
		return ""
	}
	id := strings.TrimSpace(arn[idx+len(":volume/"):])
	if !strings.HasPrefix(id, "vol-") {
		return ""
	}
	return id
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

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
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

// cloneFloatMap returns a copy of input with empty keys dropped, or nil when
// nothing survives, so an empty finding-count map omits cleanly.
func cloneFloatMap(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]float64, len(input))
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
