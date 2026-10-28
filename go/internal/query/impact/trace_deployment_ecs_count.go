// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"go.opentelemetry.io/otel/attribute"
)

// fetchWorkloadECSTaskSummary counts tasks for one graph-bound or own-repo
// literal ECS service identity. Missing or ambiguous identity fails closed.
func (h *Handler) fetchWorkloadECSTaskSummary(ctx context.Context, workloadName, repoID string, cloudResources CloudResourceResult, access querycontract.RepositoryAccessFilter) (summary deployment.ECSTaskSummary, err error) {
	ctx, span := tracing.HandlerTracer().Start(ctx, "impact.ecs_running_task_count")
	defer func() {
		span.SetAttributes(attribute.Bool("eshu.ecs_task_count_observed", summary.Count != nil))
		if summary.Count != nil {
			span.SetAttributes(attribute.Int("eshu.ecs_running_task_count", *summary.Count))
		} else if summary.Reason != "" {
			span.SetAttributes(attribute.String("eshu.ecs_task_count_skip_reason", summary.Reason))
		}
		span.End()
	}()
	if querycontract.BoolVal(cloudResources.Limits, "truncated") {
		return deployment.ECSTaskSummary{Reason: "ecs_service_identity_truncated"}, nil
	}
	if cloudResources.IdentityWithheld {
		if !access.Scoped() {
			return deployment.ECSTaskSummary{Reason: "ecs_service_identity_coverage_unknown"}, nil
		}
		cloudResources.Rows = nil // Context fallback rows cannot establish scoped graph identity.
	} else if len(cloudResources.Limits) == 0 {
		return deployment.ECSTaskSummary{Reason: "ecs_service_identity_coverage_unknown"}, nil
	}
	anchors := deployment.ECSServicesFromCloudResources(cloudResources.Rows)
	if len(anchors) == 0 {
		if h != nil && h.Neo4j != nil && h.Content != nil && access.AllowsCanonicalRepositoryID(repoID) {
			workloadCount, err := countWorkloadsDefinedByRepo(ctx, h.Neo4j, repoID)
			if err != nil {
				span.RecordError(err)
				return deployment.ECSTaskSummary{Reason: "ecs_declaration_workload_probe_failed"}, nil
			}
			if workloadCount == 1 {
				entities, err := h.Content.ListRepoEntitiesByType(ctx, repoID, "TerraformResource", querycontract.RepositorySemanticEntityLimit+1)
				if err != nil {
					span.RecordError(err)
					return deployment.ECSTaskSummary{Reason: "ecs_declaration_read_failed"}, nil
				}
				if len(entities) > querycontract.RepositorySemanticEntityLimit {
					return deployment.ECSTaskSummary{Reason: "ecs_declaration_truncated"}, nil
				}
				arn, reason := deployment.SelectDeclaredECSServiceARN(workloadName, repoID, workloadCount, entities)
				if arn != "" {
					anchors = []string{arn}
				} else if reason != "ecs_declaration_missing" {
					return deployment.ECSTaskSummary{Reason: reason}, nil
				}
			} else if workloadCount > 1 {
				return deployment.ECSTaskSummary{Reason: "ecs_declaration_workload_ambiguous"}, nil
			}
		}
	}
	if len(anchors) == 0 {
		for _, resource := range cloudResources.Rows {
			if querycontract.StringVal(resource, "resource_type") == "aws_ecs_service" {
				span.SetAttributes(attribute.String("eshu.ecs_task_count_skip_reason", "ecs_service_identity_missing"))
				return deployment.ECSTaskSummary{Reason: "ecs_service_identity_missing"}, nil
			}
		}
		return deployment.ECSTaskSummary{Reason: "ecs_service_identity_missing"}, nil
	}
	if len(anchors) != 1 {
		span.SetAttributes(attribute.String("eshu.ecs_task_count_skip_reason", "ambiguous_ecs_service_identity"))
		return deployment.ECSTaskSummary{Reason: "ambiguous_ecs_service_identity"}, nil
	}
	if h == nil || h.ECSTasks == nil {
		return deployment.ECSTaskSummary{Reason: "ecs_task_store_unavailable"}, nil
	}
	if access.Empty() {
		return deployment.ECSTaskSummary{Reason: "scoped_caller_no_grants"}, nil
	}
	evidence, err := h.ECSTasks.ListRunningTasks(ctx, anchors[0], access)
	if err != nil {
		span.RecordError(err)
		return deployment.ECSTaskSummary{Reason: "ecs_task_read_failed"}, err
	}
	if evidence == nil {
		return deployment.ECSTaskSummary{Reason: "active_service_fact_missing"}, nil
	}
	if evidence.RunningCount == nil {
		return deployment.ECSTaskSummary{Reason: "running_count_unobserved"}, nil
	}
	if evidence.Truncated {
		return deployment.ECSTaskSummary{Reason: "task_evidence_truncated"}, nil
	}
	summary = deployment.SummarizeECSTasks(*evidence.RunningCount, evidence.Tasks, evidence.ImageDigests)
	return summary, nil
}
