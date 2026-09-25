// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awssq "github.com/aws/aws-sdk-go-v2/service/servicequotas"
	awssqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	sqservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/quotas"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Service Quotas API the
// adapter calls. It is deliberately limited to the three control-plane list
// reads the scanner needs: the visible services, each service's applied quotas,
// and each service's AWS-published defaults. It exposes no
// RequestServiceQuotaIncrease, no template association, and no Put/Delete
// mutation, so the adapter cannot change quota state. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	ListServices(
		context.Context,
		*awssq.ListServicesInput,
		...func(*awssq.Options),
	) (*awssq.ListServicesOutput, error)
	ListServiceQuotas(
		context.Context,
		*awssq.ListServiceQuotasInput,
		...func(*awssq.Options),
	) (*awssq.ListServiceQuotasOutput, error)
	ListAWSDefaultServiceQuotas(
		context.Context,
		*awssq.ListAWSDefaultServiceQuotasInput,
		...func(*awssq.Options),
	) (*awssq.ListAWSDefaultServiceQuotasOutput, error)
}

// Client adapts AWS SDK Service Quotas control-plane calls into scanner-owned
// metadata. It only lists services, applied quotas, and AWS default quotas, and
// never requests, modifies, or deletes a quota.
type Client struct {
	client      apiClient
	boundary    aws.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Service Quotas SDK adapter for one claimed AWS boundary.
func NewClient(
	config awsv2.Config,
	boundary aws.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssq.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns the applied service quotas for every service visible to the
// configured AWS credentials, each joined against its AWS-published default so
// the scanner can mark approved overrides. No quota is requested or mutated.
func (c *Client) Snapshot(ctx context.Context) (sqservice.Snapshot, error) {
	services, err := c.listServices(ctx)
	if err != nil {
		return sqservice.Snapshot{}, err
	}
	var quotas []sqservice.ServiceQuota
	for _, service := range services {
		serviceCode := strings.TrimSpace(awsv2.ToString(service.ServiceCode))
		if serviceCode == "" {
			continue
		}
		defaults, err := c.listDefaultQuotas(ctx, serviceCode)
		if err != nil {
			return sqservice.Snapshot{}, err
		}
		applied, err := c.listAppliedQuotas(ctx, serviceCode)
		if err != nil {
			return sqservice.Snapshot{}, err
		}
		for _, quota := range applied {
			quotas = append(quotas, mapQuota(quota, defaults))
		}
	}
	return sqservice.Snapshot{Quotas: quotas}, nil
}

func (c *Client) listServices(ctx context.Context) ([]awssqtypes.ServiceInfo, error) {
	var services []awssqtypes.ServiceInfo
	var nextToken *string
	for {
		var page *awssq.ListServicesOutput
		err := c.recordAPICall(ctx, "ListServices", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServices(callCtx, &awssq.ListServicesInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return services, nil
		}
		services = append(services, page.Services...)
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return services, nil
		}
	}
}

func (c *Client) listAppliedQuotas(ctx context.Context, serviceCode string) ([]awssqtypes.ServiceQuota, error) {
	var quotas []awssqtypes.ServiceQuota
	var nextToken *string
	for {
		var page *awssq.ListServiceQuotasOutput
		err := c.recordAPICall(ctx, "ListServiceQuotas", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListServiceQuotas(callCtx, &awssq.ListServiceQuotasInput{
				ServiceCode: awsv2.String(serviceCode),
				NextToken:   nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return quotas, nil
		}
		quotas = append(quotas, page.Quotas...)
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return quotas, nil
		}
	}
}

func (c *Client) listDefaultQuotas(ctx context.Context, serviceCode string) (map[string]float64, error) {
	defaults := map[string]float64{}
	var nextToken *string
	for {
		var page *awssq.ListAWSDefaultServiceQuotasOutput
		err := c.recordAPICall(ctx, "ListAWSDefaultServiceQuotas", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAWSDefaultServiceQuotas(callCtx, &awssq.ListAWSDefaultServiceQuotasInput{
				ServiceCode: awsv2.String(serviceCode),
				NextToken:   nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return defaults, nil
		}
		for _, quota := range page.Quotas {
			code := strings.TrimSpace(awsv2.ToString(quota.QuotaCode))
			if code == "" || quota.Value == nil {
				continue
			}
			defaults[code] = awsv2.ToFloat64(quota.Value)
		}
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return defaults, nil
		}
	}
}

// mapQuota translates one applied SDK quota into the scanner-owned type, joining
// the AWS-published default by quota code and computing the override flag.
func mapQuota(quota awssqtypes.ServiceQuota, defaults map[string]float64) sqservice.ServiceQuota {
	quotaCode := strings.TrimSpace(awsv2.ToString(quota.QuotaCode))
	mapped := sqservice.ServiceQuota{
		ARN:          strings.TrimSpace(awsv2.ToString(quota.QuotaArn)),
		ServiceCode:  strings.TrimSpace(awsv2.ToString(quota.ServiceCode)),
		ServiceName:  strings.TrimSpace(awsv2.ToString(quota.ServiceName)),
		QuotaCode:    quotaCode,
		QuotaName:    strings.TrimSpace(awsv2.ToString(quota.QuotaName)),
		Description:  strings.TrimSpace(awsv2.ToString(quota.Description)),
		Adjustable:   quota.Adjustable,
		GlobalQuota:  quota.GlobalQuota,
		Unit:         strings.TrimSpace(awsv2.ToString(quota.Unit)),
		AppliedLevel: strings.TrimSpace(string(quota.QuotaAppliedAtLevel)),
	}
	if quota.Value != nil {
		applied := awsv2.ToFloat64(quota.Value)
		mapped.AppliedValue = &applied
	}
	if defaultValue, ok := defaults[quotaCode]; ok {
		value := defaultValue
		mapped.DefaultValue = &value
	}
	mapped.Overridden = mapped.AppliedValue != nil &&
		mapped.DefaultValue != nil &&
		*mapped.AppliedValue != *mapped.DefaultValue
	applyPeriod(&mapped, quota.Period)
	mapped.QuotaContext = mapQuotaContext(quota.QuotaContext)
	mapped.UsageMetric = mapUsageMetric(quota.UsageMetric)
	return mapped
}

func applyPeriod(quota *sqservice.ServiceQuota, period *awssqtypes.QuotaPeriod) {
	if period == nil {
		return
	}
	quota.PeriodUnit = strings.TrimSpace(string(period.PeriodUnit))
	if period.PeriodValue != nil {
		value := awsv2.ToInt32(period.PeriodValue)
		quota.PeriodValue = &value
	}
}

func mapQuotaContext(context *awssqtypes.QuotaContextInfo) *sqservice.QuotaContext {
	if context == nil {
		return nil
	}
	id := strings.TrimSpace(awsv2.ToString(context.ContextId))
	scope := strings.TrimSpace(string(context.ContextScope))
	scopeType := strings.TrimSpace(awsv2.ToString(context.ContextScopeType))
	if id == "" && scope == "" && scopeType == "" {
		return nil
	}
	return &sqservice.QuotaContext{
		ContextID:        id,
		ContextScope:     scope,
		ContextScopeType: scopeType,
	}
}

func mapUsageMetric(metric *awssqtypes.MetricInfo) *sqservice.UsageMetric {
	if metric == nil {
		return nil
	}
	namespace := strings.TrimSpace(awsv2.ToString(metric.MetricNamespace))
	name := strings.TrimSpace(awsv2.ToString(metric.MetricName))
	statistic := strings.TrimSpace(awsv2.ToString(metric.MetricStatisticRecommendation))
	dimensions := cloneStringMap(metric.MetricDimensions)
	if namespace == "" && name == "" && statistic == "" && dimensions == nil {
		return nil
	}
	return &sqservice.UsageMetric{
		Namespace:               namespace,
		Name:                    name,
		Dimensions:              dimensions,
		StatisticRecommendation: statistic,
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	aws.RecordAPICall(ctx, aws.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ sqservice.Client = (*Client)(nil)

var _ apiClient = (*awssq.Client)(nil)
