// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// PreScan returns Kotlin names used by the collector import-map pre-scan.
func PreScan(repoRoot string, path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(repoRoot, path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	return shared.DedupeNonEmptyStrings(shared.CollectBucketNames(payload, "functions", "classes", "interfaces")), nil
}
