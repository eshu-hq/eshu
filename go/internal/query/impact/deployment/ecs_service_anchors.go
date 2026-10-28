// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// SelectDeclaredECSServiceARN accepts a literal Terraform ECS service
// declaration only when its repository defines exactly the selected workload.
// Computed identities and repeated resource instances remain unresolved.
func SelectDeclaredECSServiceARN(workloadName, repoID string, workloadCount int, entities []querycontract.EntityContent) (string, string) {
	if workloadCount != 1 || workloadName == "" || repoID == "" {
		return "", "ecs_declaration_workload_ambiguous"
	}
	arn := ""
	unresolved := false
	for _, entity := range entities {
		if entity.RepoID != repoID || entity.EntityType != "TerraformResource" ||
			querycontract.StringVal(entity.Metadata, "resource_type") != "aws_ecs_service" {
			continue
		}
		metadata := entity.Metadata
		if querycontract.StringVal(metadata, "count") != "" || querycontract.StringVal(metadata, "for_each") != "" ||
			len(querycontract.StringSliceVal(metadata, "unknown_attributes")) != 0 {
			unresolved = true
			continue
		}
		attrs := querycontract.MapValue(metadata, "attributes")
		name := querycontract.StringVal(attrs, "name")
		cluster := querycontract.StringVal(attrs, "cluster")
		if name == "" || cluster == "" {
			unresolved = true
			continue
		}
		if name != workloadName {
			continue
		}
		candidate := ecsServiceARNFromLiteralCluster(cluster, name)
		if candidate == "" {
			unresolved = true
			continue
		}
		if arn != "" {
			return "", "ambiguous_ecs_service_identity"
		}
		arn = candidate
	}
	if unresolved {
		return "", "ecs_declaration_unresolved"
	}
	if arn == "" {
		return "", "ecs_declaration_missing"
	}
	return arn, ""
}

func ecsServiceARNFromLiteralCluster(clusterARN, serviceName string) string {
	prefix, clusterName, ok := strings.Cut(clusterARN, ":cluster/")
	if !ok || clusterName == "" || strings.Contains(clusterName, "/") || strings.Contains(serviceName, "/") ||
		!strings.HasPrefix(prefix, "arn:aws:ecs:") {
		return ""
	}
	parts := strings.Split(prefix, ":")
	if len(parts) != 5 || parts[3] == "" || len(parts[4]) != 12 {
		return ""
	}
	for _, digit := range parts[4] {
		if digit < '0' || digit > '9' {
			return ""
		}
	}
	return prefix + ":service/" + clusterName + "/" + serviceName
}

// ECSServicesFromCloudResources returns exact ECS service ARNs present in the
// traced workload's authorized cloud-resource evidence. A task or a name-only
// service cannot anchor a runtime task count.
func ECSServicesFromCloudResources(resources []map[string]any) []string {
	seen := make(map[string]struct{})
	for _, resource := range resources {
		if querycontract.StringVal(resource, "resource_type") != "aws_ecs_service" {
			continue
		}
		arn := querycontract.StringVal(resource, "arn")
		if !strings.HasPrefix(arn, "arn:aws:ecs:") || !strings.Contains(arn, ":service/") {
			continue
		}
		seen[arn] = struct{}{}
	}
	arns := make([]string, 0, len(seen))
	for arn := range seen {
		arns = append(arns, arn)
	}
	sort.Strings(arns)
	return arns
}
