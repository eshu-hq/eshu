// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// OverflowingInfrastructureEntities returns `n` K8sResource rows for the
// infrastructure-truncation tests. The implementation moved from root's
// context_story_limits_test.go for #6060 so the moved entity workload
// tests can build the same fixtures without importing root; package query
// keeps a thin wrapper under the original name.
func OverflowingInfrastructureEntities(n int) []querycontract.EntityContent {
	entities := make([]querycontract.EntityContent, 0, n)
	for i := 0; i < n; i++ {
		entities = append(entities, querycontract.EntityContent{
			RepoID:       "repo-1",
			EntityType:   "K8sResource",
			EntityName:   fmt.Sprintf("resource-%05d", i),
			RelativePath: fmt.Sprintf("deploy/resource-%05d.yaml", i),
		})
	}
	return entities
}
