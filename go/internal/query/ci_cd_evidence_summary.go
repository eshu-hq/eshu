// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

// The CI/CD run-artifact evidence cluster moved to repositoryartifacts for
// #6060; these aliases keep the staying CI/CD handler compiling unchanged.
type (
	cicdRunCorrelationEvidenceSummary  = artifacts.CicdRunCorrelationEvidenceSummary
	cicdStaticWorkflowArtifactEvidence = artifacts.CicdStaticWorkflowArtifactEvidence
)

func (h *CICDHandler) runCorrelationEvidenceSummary(
	ctx context.Context,
	repositoryID string,
	rows []CICDRunCorrelationResult,
	liveTruncated bool,
) cicdRunCorrelationEvidenceSummary {
	static := artifacts.StaticWorkflowArtifactEvidence(ctx, h.Content, repositoryID)
	return artifacts.BuildCICDRunCorrelationEvidenceSummary(static, rows, liveTruncated, false)
}
