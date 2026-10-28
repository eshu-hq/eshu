// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ECSServicesFromCloudResources returns exact ECS service ARNs present in the
// traced workload's authorized cloud-resource evidence. A task or a name-only
// service cannot anchor a runtime task count.
func ECSServicesFromCloudResources(resources []map[string]any) []string {
	seen := make(map[string]struct{})
	for _, resource := range resources {
		if querycontract.StringVal(resource, "resource_type") != "ecs.service" {
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
