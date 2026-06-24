// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicequotas

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Service Quotas observations for one AWS
// claim. Implementations read the Service Quotas control-plane (ListServices,
// ListServiceQuotas, ListAWSDefaultServiceQuotas) and never request, modify, or
// delete a quota and never associate a quota-increase template.
type Client interface {
	// Snapshot returns every applied service quota visible to the configured AWS
	// credentials for the claimed account and region, each already joined against
	// its AWS-published default value.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures applied Service Quotas metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Quotas is the metadata-only set of applied service quotas across the
	// services visible to the claim.
	Quotas []ServiceQuota
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// ServiceQuota is the scanner-owned model of one applied AWS Service Quotas
// quota. It carries quota-limit metadata only: there is no workload, resource,
// or usage-sample data, just the configured limit and the CloudWatch metric
// identity AWS recommends for tracking it.
type ServiceQuota struct {
	// ARN is the Amazon Resource Name AWS reports for the quota. It is the
	// resource_id the quota node publishes when present.
	ARN string
	// ServiceCode is the AWS service identifier the quota belongs to (for example
	// "ec2"). It is a service code, not a scanned-resource identifier.
	ServiceCode string
	// ServiceName is the human-readable AWS service name (for example "Amazon
	// Elastic Compute Cloud (Amazon EC2)").
	ServiceName string
	// QuotaCode is the AWS quota identifier within the service (for example
	// "L-1216C47A").
	QuotaCode string
	// QuotaName is the human-readable quota name (for example "Running On-Demand
	// Standard instances").
	QuotaName string
	// Description is the AWS-provided quota description, when present.
	Description string
	// AppliedValue is the quota value currently applied in the account/region.
	// It is the value the API returns from ListServiceQuotas, which reflects any
	// approved increase. It is nil when AWS reports no value.
	AppliedValue *float64
	// DefaultValue is the AWS-published default quota value from
	// ListAWSDefaultServiceQuotas, joined by quota code. It is nil when AWS
	// reports no matching default.
	DefaultValue *float64
	// Overridden is true when AppliedValue and DefaultValue are both known and
	// differ, indicating an approved quota increase (or decrease) override.
	Overridden bool
	// Adjustable reports whether the quota value can be increased via a request.
	Adjustable bool
	// GlobalQuota reports whether AWS treats the quota as global rather than
	// per-region.
	GlobalQuota bool
	// Unit is the unit of measurement for the quota value (for example "None" or
	// "Count").
	Unit string
	// AppliedLevel is the level the applied value is scoped to: ACCOUNT,
	// RESOURCE, or ALL. It is empty when AWS reports no level.
	AppliedLevel string
	// PeriodUnit is the rate-period time unit for rate quotas (for example
	// "SECOND"). It is empty for non-rate quotas.
	PeriodUnit string
	// PeriodValue is the rate-period magnitude paired with PeriodUnit. It is nil
	// for non-rate quotas.
	PeriodValue *int32
	// QuotaContext describes the resource-level scope for resource-level quotas.
	// It is nil for account-level quotas.
	QuotaContext *QuotaContext
	// UsageMetric is the CloudWatch metric identity AWS recommends for tracking
	// quota usage. It is metric identity metadata (namespace, name, dimensions,
	// statistic), not metric data. It is nil when AWS reports no usage metric.
	UsageMetric *UsageMetric
}

// QuotaContext captures the resource-level scope of a resource-level quota. It
// records which resource scope the quota value applies to, without resolving the
// referenced resource into a scanned node.
type QuotaContext struct {
	// ContextID is the resource ARN or "*" the quota value applies to.
	ContextID string
	// ContextScope is the scope the quota value is applied to (RESOURCE or
	// ACCOUNT).
	ContextScope string
	// ContextScopeType is the resource type the quota can be applied to.
	ContextScopeType string
}

// UsageMetric captures the CloudWatch metric identity AWS recommends for
// tracking a quota's usage. It carries only the metric's identity (namespace,
// name, dimensions, recommended statistic), never any metric sample value.
type UsageMetric struct {
	// Namespace is the CloudWatch metric namespace (for example "AWS/Usage").
	Namespace string
	// Name is the CloudWatch metric name.
	Name string
	// Dimensions is the metric dimension name/value identity set.
	Dimensions map[string]string
	// StatisticRecommendation is the metric statistic AWS recommends for usage
	// tracking (for example "Maximum").
	StatisticRecommendation string
}
