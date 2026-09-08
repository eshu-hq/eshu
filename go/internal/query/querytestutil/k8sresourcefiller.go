// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// K8sResourceFillerEntities returns `count` K8sResource Deployment rows in
// namespace "other-ns" -- distinct from the "prod" namespace the truncation
// tests use for the real Service/Deployment pair -- so they occupy the typed
// candidate scan without ever matching SELECTS themselves. The implementation
// moved from root's content_relationships_k8s_truncation_test.go for #6060
// so the moved entity-context truncation test can build the same fixtures
// without importing root; package query keeps a thin wrapper under the
// original name.
func K8sResourceFillerEntities(count int) []querycontract.EntityContent {
	filler := make([]querycontract.EntityContent, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("filler-deploy-%05d", i)
		filler = append(filler, querycontract.EntityContent{
			EntityID:     name,
			RepoID:       "repo-1",
			RelativePath: "deploy/" + name + ".yaml",
			EntityType:   "K8sResource",
			EntityName:   name,
			Metadata: map[string]any{
				"kind":                "Deployment",
				"namespace":           "other-ns",
				"qualified_name":      "other-ns/Deployment/" + name,
				"pod_template_labels": "app=" + name,
			},
		})
	}
	return filler
}
