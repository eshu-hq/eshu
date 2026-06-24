// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicequotas

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Service Quotas metadata-only facts for one claimed account
// and region. It reports each applied service quota, joined against the
// AWS-published default so the override flag is durable, and never requests,
// modifies, or deletes a quota. It emits no relationships: a quota references an
// AWS service code, not a scanned resource, so there is no cross-service edge to
// key without dangling the graph.
type Scanner struct {
	// Client is the metadata-only Service Quotas snapshot source.
	Client Client
}

// Scan observes the applied service quotas for the claimed account/region
// through the configured client and emits one resource fact per quota plus any
// non-fatal warning facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("servicequotas scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceServiceQuotas:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceServiceQuotas
	default:
		return nil, fmt.Errorf("servicequotas scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Service Quotas: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, quota := range snapshot.Quotas {
		envelope, err := awscloud.NewResourceEnvelope(quotaObservation(boundary, quota))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func quotaObservation(boundary awscloud.Boundary, quota ServiceQuota) awscloud.ResourceObservation {
	arn := strings.TrimSpace(quota.ARN)
	serviceCode := strings.TrimSpace(quota.ServiceCode)
	quotaCode := strings.TrimSpace(quota.QuotaCode)
	quotaName := strings.TrimSpace(quota.QuotaName)
	resourceID := quotaResourceID(quota)

	attributes := map[string]any{
		"service_code":  serviceCode,
		"service_name":  strings.TrimSpace(quota.ServiceName),
		"quota_code":    quotaCode,
		"quota_name":    quotaName,
		"description":   strings.TrimSpace(quota.Description),
		"applied_value": floatOrNil(quota.AppliedValue),
		"default_value": floatOrNil(quota.DefaultValue),
		"overridden":    quota.Overridden,
		"adjustable":    quota.Adjustable,
		"global_quota":  quota.GlobalQuota,
		"unit":          strings.TrimSpace(quota.Unit),
		"applied_level": strings.TrimSpace(quota.AppliedLevel),
		"period_unit":   strings.TrimSpace(quota.PeriodUnit),
		"period_value":  int32OrNil(quota.PeriodValue),
		"quota_context": quotaContextAttributes(quota.QuotaContext),
		"usage_metric":  usageMetricAttributes(quota.UsageMetric),
	}

	anchors := []string{arn, resourceID}
	if serviceCode != "" && quotaCode != "" {
		anchors = append(anchors, serviceCode+"/"+quotaCode)
	}

	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                arn,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeServiceQuotasServiceQuota,
		Name:               quotaName,
		Tags:               nil,
		Attributes:         attributes,
		CorrelationAnchors: anchors,
		SourceRecordID:     resourceID,
	}
}

// quotaContextAttributes flattens the resource-level quota context into a
// payload map, or returns an untyped nil for account-level quotas so the
// attribute is omitted rather than carrying a typed-nil map.
func quotaContextAttributes(context *QuotaContext) any {
	if context == nil {
		return nil
	}
	id := strings.TrimSpace(context.ContextID)
	scope := strings.TrimSpace(context.ContextScope)
	scopeType := strings.TrimSpace(context.ContextScopeType)
	if id == "" && scope == "" && scopeType == "" {
		return nil
	}
	return map[string]any{
		"context_id":         id,
		"context_scope":      scope,
		"context_scope_type": scopeType,
	}
}

// usageMetricAttributes flattens the CloudWatch usage-metric identity into a
// payload map, or returns an untyped nil when AWS reports no usage metric so the
// attribute is omitted rather than carrying a typed-nil map. Only the metric
// identity is recorded; no metric sample value is read or emitted.
func usageMetricAttributes(metric *UsageMetric) any {
	if metric == nil {
		return nil
	}
	namespace := strings.TrimSpace(metric.Namespace)
	name := strings.TrimSpace(metric.Name)
	statistic := strings.TrimSpace(metric.StatisticRecommendation)
	dimensions := cloneStringMap(metric.Dimensions)
	if namespace == "" && name == "" && statistic == "" && dimensions == nil {
		return nil
	}
	return map[string]any{
		"metric_namespace":         namespace,
		"metric_name":              name,
		"metric_dimensions":        dimensions,
		"statistic_recommendation": statistic,
	}
}
