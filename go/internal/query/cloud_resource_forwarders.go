// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
)

// Cloud-resource evidence loaders moved to impacttrace for #6060 (they back
// deployment-trace cloud evidence). The names below forward so root callers
// and staying tests keep working; the single home is impacttrace.

const (
	uncorrelatedCloudResourceCandidateLimit = impacttrace.UncorrelatedCloudResourceCandidateLimit
	serviceCloudResourceDependencyLimit     = impacttrace.ServiceCloudResourceDependencyLimit
	infraResourceFreeTextPredicate          = impacttrace.InfraResourceFreeTextPredicate
)

func loadUncorrelatedCloudResourceCandidates(
	ctx context.Context,
	graph GraphQuery,
	serviceName string,
	limit int,
) ([]map[string]any, error) {
	return impacttrace.LoadUncorrelatedCloudResourceCandidates(ctx, graph, serviceName, limit)
}

func loadUncorrelatedCloudResourceCandidatesBounded(
	ctx context.Context,
	graph GraphQuery,
	serviceName string,
	limit int,
) ([]map[string]any, bool, error) {
	return impacttrace.LoadUncorrelatedCloudResourceCandidatesBounded(ctx, graph, serviceName, limit)
}

func loadMaterializedServiceCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	repoID string,
	workloadID string,
	limit int,
) ([]map[string]any, error) {
	return impacttrace.LoadMaterializedServiceCloudResourceDependencies(ctx, graph, repoID, workloadID, limit)
}

func loadConfigDerivedCloudResourceDependencies(
	ctx context.Context,
	graph GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, error) {
	return impacttrace.LoadConfigDerivedCloudResourceDependencies(ctx, graph, deploymentEvidence, limit)
}

func loadConfigDerivedCloudResourceDependenciesBounded(
	ctx context.Context,
	graph GraphQuery,
	deploymentEvidence map[string]any,
	limit int,
) ([]map[string]any, bool, error) {
	return impacttrace.LoadConfigDerivedCloudResourceDependenciesBounded(ctx, graph, deploymentEvidence, limit)
}

func configReadCloudResourceAnchors(deploymentEvidence map[string]any) ([]string, bool) {
	return impacttrace.ConfigReadCloudResourceAnchors(deploymentEvidence)
}
