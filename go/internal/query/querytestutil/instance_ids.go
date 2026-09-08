// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// InstanceIDs extracts the instance_id column from workload-instance rows.
// It lives here rather than in an entity _test.go file for the reason that
// shapes all of epic #6053 (#6060): a symbol declared in a _test.go file is
// not part of the importable package, so the staying root live determinism
// test cannot reach the helper the moved entity determinism tests declare.
func InstanceIDs(instances []map[string]any) []string {
	ids := make([]string, 0, len(instances))
	for _, instance := range instances {
		ids = append(ids, querycontract.StringVal(instance, "instance_id"))
	}
	return ids
}
