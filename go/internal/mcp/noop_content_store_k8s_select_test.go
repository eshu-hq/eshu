// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// The #5363 ContentStore additions on mcpNoopContentStore live here, split out
// of dispatch_service_story_test.go to keep that file under the repo's 500-line
// package-file cap. The MCP dispatch tests never exercise the impact-trace
// directed SELECTS path, so these remain no-ops.

func (mcpNoopContentStore) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]query.EntityContent, error) {
	return nil, nil
}

func (mcpNoopContentStore) ListRepoK8sSelectCandidates(context.Context, string, int) ([]query.K8sSelectCandidate, error) {
	return nil, nil
}
