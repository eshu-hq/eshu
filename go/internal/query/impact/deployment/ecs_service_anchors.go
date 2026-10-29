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

// ecsServiceARNPrefixOK reports whether prefix is an ECS ARN prefix
// (arn:<partition>:ecs:<region>:<12-digit-account>) in a platform partition
// the collector models: commercial, GovCloud, or China. The region and
// account checks match the long-standing commercial validation; only the
// partition set is wider than the old arn:aws:ecs: literal.
func ecsServiceARNPrefixOK(prefix string) bool {
	withoutScheme, ok := strings.CutPrefix(prefix, "arn:")
	if !ok {
		return false
	}
	partition, remainder, ok := strings.Cut(withoutScheme, ":")
	if !ok {
		return false
	}
	switch partition {
	case "aws", "aws-cn", "aws-us-gov":
	default:
		return false
	}
	service, remainder, ok := strings.Cut(remainder, ":")
	if !ok || service != "ecs" {
		return false
	}
	region, account, ok := strings.Cut(remainder, ":")
	if !ok || region == "" || strings.Contains(account, ":") || len(account) != 12 {
		return false
	}
	for _, digit := range account {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func ecsServiceARNFromLiteralCluster(clusterARN, serviceName string) string {
	prefix, clusterName, ok := strings.Cut(clusterARN, ":cluster/")
	if !ok || clusterName == "" || strings.Contains(clusterName, "/") || strings.Contains(serviceName, "/") ||
		!ecsServiceARNPrefixOK(prefix) {
		return ""
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
		svcIdx := strings.Index(arn, ":service/")
		if svcIdx < 0 || !ecsServiceARNPrefixOK(arn[:svcIdx]) {
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
