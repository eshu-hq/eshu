// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query // fetchServiceTraceContext builds the root EntityHandler, which cannot be named from impact/ (#6060).

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// deploymentTraceContextBackend is the production
// impact.DeploymentTraceContextProvider: it enriches a deployment-trace
// request with service workload context. It stays in root because it builds a
// B5-entity handler (EntityHandler), which cannot be named from the impact
// subpackage, and because the overview shaping reuses the service-story build
// context for the same reason. ImpactHandler.TraceContext carries the
// production adapter; tests inject fakes through the same interface. See
// #6060.
type deploymentTraceContextBackend struct{}

// NewDeploymentTraceContext returns the production trace-context backend for
// ImpactHandler wiring.
func NewDeploymentTraceContext() impact.DeploymentTraceContextProvider {
	return deploymentTraceContextBackend{}
}

// FetchServiceTraceContext implements impact.DeploymentTraceContextProvider.
func (deploymentTraceContextBackend) FetchServiceTraceContext(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	logger *slog.Logger,
	serviceName string,
	traceOptions impact.TraceEnrichmentConfig,
) (map[string]any, error) {
	return fetchServiceTraceContext(ctx, graph, content, logger, serviceName, traceOptions)
}

// BuildServiceDeploymentOverview implements
// impact.DeploymentTraceContextProvider.
func (deploymentTraceContextBackend) BuildServiceDeploymentOverview(workloadContext map[string]any) map[string]any {
	return buildServiceDeploymentOverview(workloadContext)
}

// fetchServiceTraceContext stays in root: it builds a B5-entity handler
// (EntityHandler), which cannot be named from the impact subpackage.
// Moved callers reach it through ImpactHandler.TraceContext instead.
// See #6060.
func fetchServiceTraceContext(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	logger *slog.Logger,
	serviceName string,
	traceOptions impact.TraceEnrichmentConfig,
) (map[string]any, error) {
	entityHandler := &EntityHandler{Neo4j: graph, Content: content, Logger: logger}
	workloadID, err := impacttrace.ResolveTraceWorkloadSelector(ctx, graph, serviceName)
	if err != nil {
		return nil, err
	}
	var workloadContext map[string]any
	if workloadID != "" {
		workloadContext, err = entityHandler.fetchWorkloadContextForOperation(
			ctx,
			"w.id = $workload_id",
			map[string]any{"workload_id": workloadID},
			"deployment_trace",
		)
	} else {
		workloadContext, err = entityHandler.fetchServiceReadModelWorkloadContext(ctx, serviceName)
	}
	if err != nil || workloadContext == nil {
		return workloadContext, err
	}

	if err := enrichServiceQueryContextWithOptions(ctx, graph, content, workloadContext, serviceQueryEnrichmentOptions{
		DirectOnly:                !traceOptions.IncludeConsumers,
		IncludeRelatedModuleUsage: traceOptions.IncludeProvisioningChains,
		MaxDepth:                  traceOptions.MaxDepth,
		Logger:                    logger,
		Operation:                 "deployment_trace",
	}); err != nil {
		return nil, fmt.Errorf("enrich service trace context: %w", err)
	}

	return workloadContext, nil
}
