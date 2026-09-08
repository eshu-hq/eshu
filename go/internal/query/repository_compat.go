// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 root forwarder shim for #6060: Go has no function aliases, so staying root callers keep these names while the implementations live in querycontract.

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Root compatibility shims for the repository-handler family move (Issue
// #6060, lane B B3). Each function below lived in a repository_* file whose
// implementation now lives in querycontract (dependency-neutral row, string,
// and bound helpers shared across families); the wrapper keeps the staying
// root stayers and tests compiling unchanged without touching lane-A files.
// New code must call querycontract directly.

func mapValue(value map[string]any, key string) map[string]any {
	return querycontract.MapValue(value, key)
}

func mapSliceValue(value map[string]any, key string) []map[string]any {
	return querycontract.MapSliceValue(value, key)
}

func sortedUniqueStrings(values []string) []string {
	return querycontract.UniqueSortedStrings(values)
}

func stringSliceValue(value map[string]any, key string) []string {
	return querycontract.StringSliceValue(value, key)
}

func relationshipFloatVal(row map[string]any, key string) float64 {
	return querycontract.FloatVal(row, key)
}

func firstPositiveInt(row map[string]any, keys ...string) int {
	return querycontract.FirstPositiveInt(row, keys...)
}

func maxTime(left, right time.Time) time.Time {
	return querycontract.MaxTime(left, right)
}

// repositorySemanticEntityLimit bounds semantic entity extraction per
// repository. The value lives in querycontract; this declaration keeps the
// staying root stayers compiling unchanged.
const repositorySemanticEntityLimit = querycontract.RepositorySemanticEntityLimit
