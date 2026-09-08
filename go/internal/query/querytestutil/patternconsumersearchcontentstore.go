// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// PatternConsumerSearchContentStore answers pattern and exact-case content
// searches from canned row maps. Exact-case falls back to the pattern rows
// when no exact rows match, mirroring the production read it stands in
// for. It moved here for #6060 lane B B4 because the consumer-search suites
// live in three packages now (impacttrace, service, and the staying root
// deployment-trace tests); a _test.go declaration in any one of them is
// unreachable from the others.
type PatternConsumerSearchContentStore struct {
	querycontract.ContentStore
	FileRows  map[string][]querycontract.FileContent
	ExactRows map[string][]querycontract.FileContent
}

// SearchFileContentAnyRepo answers the pattern search from FileRows.
func (s PatternConsumerSearchContentStore) SearchFileContentAnyRepo(_ context.Context, pattern string, _ int) ([]querycontract.FileContent, error) {
	return append([]querycontract.FileContent(nil), s.FileRows[pattern]...), nil
}

// SearchFileContentAnyRepoExactCase answers the exact-case search from
// ExactRows, falling back to the pattern rows.
func (s PatternConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(_ context.Context, pattern string, _ int) ([]querycontract.FileContent, error) {
	rows := s.ExactRows[pattern]
	if len(rows) == 0 {
		rows = s.FileRows[pattern]
	}
	return append([]querycontract.FileContent(nil), rows...), nil
}
