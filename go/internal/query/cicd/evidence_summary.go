// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

func (h *Handler) runCorrelationEvidenceSummary(
	ctx context.Context,
	repositoryID string,
	rows []querycontract.CICDRunCorrelationResult,
	liveTruncated bool,
) artifacts.CicdRunCorrelationEvidenceSummary {
	static := artifacts.StaticWorkflowArtifactEvidence(ctx, h.Content, repositoryID)
	return artifacts.BuildCICDRunCorrelationEvidenceSummary(static, rows, liveTruncated, false)
}
