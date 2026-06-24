// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceServiceQuotas identifies the regional AWS Service Quotas
	// metadata-only scan slice. The scanner reads the Service Quotas
	// control-plane (ListServices, ListServiceQuotas, ListAWSDefaultServiceQuotas)
	// to report the applied quota values for each AWS service in the claimed
	// account and region, joined against the AWS-published defaults so an operator
	// can see which quotas were raised. It never requests, modifies, or deletes a
	// quota, and never associates a quota-increase template.
	ServiceServiceQuotas = "servicequotas"
)

const (
	// ResourceTypeServiceQuotasServiceQuota identifies one applied AWS Service
	// Quotas quota for a service in a claimed account and region. The resource
	// carries the quota identity (service code, quota code, quota name, quota
	// ARN), the applied value, the AWS-published default value, an override flag
	// that is true when the applied value differs from the default, the
	// adjustable/global flags, the unit, the optional rate period, the applied
	// level (ACCOUNT/RESOURCE/ALL), the optional resource-level quota context, and
	// the CloudWatch usage-metric identity (namespace, name, dimensions,
	// statistic) AWS recommends for tracking usage. Quota values and metric
	// identities are limit metadata, not workload data.
	ResourceTypeServiceQuotasServiceQuota = "aws_servicequotas_service_quota"
)
