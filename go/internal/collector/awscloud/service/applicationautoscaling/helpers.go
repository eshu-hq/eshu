// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// scalableTargetResourceID returns the resource_id the scalable-target node
// publishes. Application Auto Scaling has no single ARN per target across all
// namespaces, so the scanner keys a scalable target by the composite of its
// service namespace, scalable dimension, and scaled resource id. That triple is
// exactly what uniquely identifies a registered target, so policy and scheduled
// action edges can key the same value. The scalable dimension is part of that
// identity: one resource can register multiple targets (for example a DynamoDB
// table with both read- and write-capacity targets), so dropping the dimension
// would collapse distinct targets onto one id. It returns "" when any of the
// three identifying parts is missing.
func scalableTargetResourceID(serviceNamespace, scalableDimension, resourceID string) string {
	namespace := strings.TrimSpace(serviceNamespace)
	dimension := strings.TrimSpace(scalableDimension)
	resource := strings.TrimSpace(resourceID)
	if namespace == "" || dimension == "" || resource == "" {
		return ""
	}
	return namespace + "/" + dimension + "/" + resource
}

// targetResourceARN synthesizes the partition-aware ARN of the resource a
// scalable target governs, matching the resource_id the target service's
// scanner publishes, so the scale edge joins the real resource node instead of
// dangling. It returns "" with a false targetType for any namespace the repo
// does not scan to a stable ARN-keyed node (spot fleet, appstream, sagemaker,
// comprehend, cassandra, kafka, elasticache, neptune, workspaces, custom
// resource, DynamoDB global secondary index), so the caller skips the edge.
//
// partition is derived from the scan boundary by the caller; the function never
// hardcodes arn:aws:.
func targetResourceARN(
	partition, accountID, region, serviceNamespace, resourceID string,
) (arn, targetType string) {
	namespace := strings.TrimSpace(serviceNamespace)
	resource := strings.TrimSpace(resourceID)
	account := strings.TrimSpace(accountID)
	reg := strings.TrimSpace(region)
	if namespace == "" || resource == "" || account == "" || reg == "" {
		return "", ""
	}
	switch namespace {
	case "dynamodb":
		// Only a base table target maps to a DynamoDB table node. A global
		// secondary index target (table/<t>/index/<i>) has no dedicated node, so
		// it is skipped rather than mis-keyed to the table ARN.
		name, ok := strings.CutPrefix(resource, "table/")
		if !ok || name == "" || strings.Contains(name, "/") {
			return "", ""
		}
		return arnPrefix(partition, "dynamodb", reg, account) + ":table/" + name,
			awscloud.ResourceTypeDynamoDBTable
	case "ecs":
		// service/<cluster>/<service> -> the long-format ECS service ARN.
		path, ok := strings.CutPrefix(resource, "service/")
		if !ok || strings.Count(path, "/") != 1 || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
			return "", ""
		}
		return arnPrefix(partition, "ecs", reg, account) + ":service/" + path,
			awscloud.ResourceTypeECSService
	case "rds":
		// cluster:<name> -> the Aurora DB cluster ARN.
		name, ok := strings.CutPrefix(resource, "cluster:")
		if !ok || name == "" {
			return "", ""
		}
		return arnPrefix(partition, "rds", reg, account) + ":cluster:" + name,
			awscloud.ResourceTypeRDSDBCluster
	case "lambda":
		// function:<name>[:<qualifier>] -> the base function ARN the Lambda node
		// publishes (the version/alias qualifier is dropped).
		name, ok := lambdaFunctionName(resource)
		if !ok {
			return "", ""
		}
		return arnPrefix(partition, "lambda", reg, account) + ":function:" + name,
			awscloud.ResourceTypeLambdaFunction
	default:
		return "", ""
	}
}

// policyResourceID returns the resource_id the scaling-policy node publishes.
// It prefers the policy ARN and falls back to a composite of the owning
// namespace, dimension, resource id, and policy name so the node and its edges
// share a stable id even when AWS omits the ARN.
func policyResourceID(policy ScalingPolicy) string {
	if arn := strings.TrimSpace(policy.ARN); arn != "" {
		return arn
	}
	target := scalableTargetResourceID(policy.ServiceNamespace, policy.ScalableDimension, policy.ResourceID)
	name := strings.TrimSpace(policy.Name)
	switch {
	case target != "" && name != "":
		return target + "/policy/" + name
	case name != "":
		return name
	default:
		return ""
	}
}

// scheduledActionResourceID returns the resource_id the scheduled-action node
// publishes. It prefers the action ARN and falls back to a composite of the
// owning namespace, dimension, resource id, and action name.
func scheduledActionResourceID(action ScheduledAction) string {
	if arn := strings.TrimSpace(action.ARN); arn != "" {
		return arn
	}
	target := scalableTargetResourceID(action.ServiceNamespace, action.ScalableDimension, action.ResourceID)
	name := strings.TrimSpace(action.Name)
	switch {
	case target != "" && name != "":
		return target + "/action/" + name
	case name != "":
		return name
	default:
		return ""
	}
}

// lambdaFunctionName extracts the base function name from a Lambda scalable
// target resource id of the form function:<name>[:<qualifier>]. It returns the
// bare name without the version or alias qualifier so the synthesized ARN
// matches the function node's resource_id, and false when the shape is unknown.
func lambdaFunctionName(resourceID string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(resourceID), "function:")
	if !ok {
		return "", false
	}
	name := rest
	if idx := strings.IndexByte(rest, ':'); idx >= 0 {
		name = rest[:idx]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	return name, true
}

// arnPrefix builds the leading arn:<partition>:<service>:<region>:<account>
// segment of an AWS ARN. The partition is supplied by the caller from the scan
// boundary, never hardcoded, so synthesized identities match the resource node
// in every partition.
func arnPrefix(partition, service, region, account string) string {
	return "arn:" + partition + ":" + service + ":" + region + ":" + account
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// int32OrNil returns the dereferenced value, or nil when the pointer is unset,
// so the attribute payload omits an unknown bound instead of emitting zero.
func int32OrNil(value *int32) any {
	if value == nil {
		return nil
	}
	return *value
}

// boolOrNil returns the dereferenced value, or nil when the pointer is unset.
func boolOrNil(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
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
