// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaccessanalyzer "github.com/aws/aws-sdk-go-v2/service/accessanalyzer"
	awsaccessanalyzertypes "github.com/aws/aws-sdk-go-v2/service/accessanalyzer/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	accessanalyzerservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/accessanalyzer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const maxUnusedAccessDetailReads = 100

type apiClient interface {
	ListAnalyzers(context.Context, *awsaccessanalyzer.ListAnalyzersInput, ...func(*awsaccessanalyzer.Options)) (*awsaccessanalyzer.ListAnalyzersOutput, error)
	ListArchiveRules(context.Context, *awsaccessanalyzer.ListArchiveRulesInput, ...func(*awsaccessanalyzer.Options)) (*awsaccessanalyzer.ListArchiveRulesOutput, error)
	ListFindings(context.Context, *awsaccessanalyzer.ListFindingsInput, ...func(*awsaccessanalyzer.Options)) (*awsaccessanalyzer.ListFindingsOutput, error)
	ListFindingsV2(context.Context, *awsaccessanalyzer.ListFindingsV2Input, ...func(*awsaccessanalyzer.Options)) (*awsaccessanalyzer.ListFindingsV2Output, error)
	GetFindingV2(context.Context, *awsaccessanalyzer.GetFindingV2Input, ...func(*awsaccessanalyzer.Options)) (*awsaccessanalyzer.GetFindingV2Output, error)
}

// Client adapts AWS SDK Access Analyzer reads into scanner-owned metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Access Analyzer SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsaccessanalyzer.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListAnalyzers returns Access Analyzer metadata visible to the configured AWS
// credentials. It reads aggregate finding metadata only; external finding
// bodies, archive filters, and unused-action breakdowns are discarded.
func (c *Client) ListAnalyzers(ctx context.Context) ([]accessanalyzerservice.Analyzer, error) {
	var analyzers []accessanalyzerservice.Analyzer
	var nextToken *string
	for {
		var page *awsaccessanalyzer.ListAnalyzersOutput
		err := c.recordAPICall(ctx, "ListAnalyzers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAnalyzers(callCtx, &awsaccessanalyzer.ListAnalyzersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return analyzers, nil
		}
		for _, summary := range page.Analyzers {
			analyzer, err := c.analyzerMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			analyzers = append(analyzers, analyzer)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return analyzers, nil
		}
	}
}

func (c *Client) analyzerMetadata(
	ctx context.Context,
	summary awsaccessanalyzertypes.AnalyzerSummary,
) (accessanalyzerservice.Analyzer, error) {
	analyzer := accessanalyzerservice.Analyzer{
		ARN:                    strings.TrimSpace(aws.ToString(summary.Arn)),
		Name:                   strings.TrimSpace(aws.ToString(summary.Name)),
		Type:                   strings.TrimSpace(string(summary.Type)),
		Status:                 strings.TrimSpace(string(summary.Status)),
		CreatedAt:              aws.ToTime(summary.CreatedAt),
		LastResourceAnalyzed:   strings.TrimSpace(aws.ToString(summary.LastResourceAnalyzed)),
		LastResourceAnalyzedAt: aws.ToTime(summary.LastResourceAnalyzedAt),
		Tags:                   cloneStringMap(summary.Tags),
	}
	if !isSupportedAnalyzerType(analyzer.Type) {
		return analyzer, nil
	}
	if strings.TrimSpace(analyzer.ARN) == "" {
		return analyzer, nil
	}
	archiveRules, err := c.listArchiveRules(ctx, analyzer)
	if err != nil {
		return accessanalyzerservice.Analyzer{}, err
	}
	analyzer.ArchiveRules = archiveRules
	if isUnusedAnalyzerType(analyzer.Type) {
		counts, summaries, warnings, err := c.listUnusedFindings(ctx, analyzer)
		if err != nil {
			return accessanalyzerservice.Analyzer{}, err
		}
		analyzer.FindingCounts = counts
		analyzer.UnusedAccessSummaries = summaries
		analyzer.Warnings = warnings
		return analyzer, nil
	}
	counts, err := c.listExternalFindingCounts(ctx, analyzer.ARN)
	if err != nil {
		return accessanalyzerservice.Analyzer{}, err
	}
	analyzer.FindingCounts = counts
	return analyzer, nil
}

func (c *Client) listArchiveRules(
	ctx context.Context,
	analyzer accessanalyzerservice.Analyzer,
) ([]accessanalyzerservice.ArchiveRule, error) {
	if strings.TrimSpace(analyzer.Name) == "" {
		return nil, nil
	}
	var rules []accessanalyzerservice.ArchiveRule
	var nextToken *string
	for {
		var page *awsaccessanalyzer.ListArchiveRulesOutput
		err := c.recordAPICall(ctx, "ListArchiveRules", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListArchiveRules(callCtx, &awsaccessanalyzer.ListArchiveRulesInput{
				AnalyzerName: aws.String(analyzer.Name),
				NextToken:    nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return rules, nil
		}
		for _, rule := range page.ArchiveRules {
			rules = append(rules, accessanalyzerservice.ArchiveRule{
				Name:        strings.TrimSpace(aws.ToString(rule.RuleName)),
				AnalyzerARN: analyzer.ARN,
				CreatedAt:   aws.ToTime(rule.CreatedAt),
				UpdatedAt:   aws.ToTime(rule.UpdatedAt),
			})
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return rules, nil
		}
	}
}

func (c *Client) listExternalFindingCounts(
	ctx context.Context,
	analyzerARN string,
) ([]accessanalyzerservice.FindingCount, error) {
	counts := map[findingBucket]int64{}
	var nextToken *string
	for {
		var page *awsaccessanalyzer.ListFindingsOutput
		err := c.recordAPICall(ctx, "ListFindings", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFindings(callCtx, &awsaccessanalyzer.ListFindingsInput{
				AnalyzerArn: aws.String(analyzerARN),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return findingCounts(counts), nil
		}
		for _, finding := range page.Findings {
			incrementFindingCount(counts, string(finding.Status), string(finding.ResourceType))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return findingCounts(counts), nil
		}
	}
}

func (c *Client) listUnusedFindings(
	ctx context.Context,
	analyzer accessanalyzerservice.Analyzer,
) ([]accessanalyzerservice.FindingCount, []accessanalyzerservice.UnusedAccessSummary, []awscloud.WarningObservation, error) {
	counts := map[findingBucket]int64{}
	var summaries []accessanalyzerservice.UnusedAccessSummary
	var warnings []awscloud.WarningObservation
	var nextToken *string
	detailReads := 0
	detailBudgetExceeded := false
	for {
		var page *awsaccessanalyzer.ListFindingsV2Output
		err := c.recordAPICall(ctx, "ListFindingsV2", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFindingsV2(callCtx, &awsaccessanalyzer.ListFindingsV2Input{
				AnalyzerArn: aws.String(analyzer.ARN),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, nil, nil, err
		}
		if page == nil {
			return findingCounts(counts), summaries, warnings, nil
		}
		for _, finding := range page.Findings {
			incrementFindingCount(counts, string(finding.Status), string(finding.ResourceType))
			if detailReads >= maxUnusedAccessDetailReads {
				if !detailBudgetExceeded {
					warnings = append(warnings, unusedAccessDetailBudgetWarning(c.boundary, analyzer.ARN))
					detailBudgetExceeded = true
				}
				continue
			}
			detailReads++
			summary, err := c.unusedAccessSummary(ctx, analyzer.ARN, finding)
			if err != nil {
				return nil, nil, nil, err
			}
			if summary.ResourceID != "" {
				summaries = append(summaries, summary)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return findingCounts(counts), summaries, warnings, nil
		}
	}
}

func unusedAccessDetailBudgetWarning(boundary awscloud.Boundary, analyzerARN string) awscloud.WarningObservation {
	return awscloud.WarningObservation{
		Boundary:       boundary,
		WarningKind:    awscloud.WarningBudgetExhausted,
		ErrorClass:     "unused_access_detail_budget_exhausted",
		Message:        "unused access detail reads exceeded the bounded Access Analyzer detail-read budget",
		SourceRecordID: strings.TrimSpace(analyzerARN) + "#unused-access-detail-budget",
		Attributes: map[string]any{
			"detail_read_limit": int64(maxUnusedAccessDetailReads),
		},
	}
}

func (c *Client) unusedAccessSummary(
	ctx context.Context,
	analyzerARN string,
	finding awsaccessanalyzertypes.FindingSummaryV2,
) (accessanalyzerservice.UnusedAccessSummary, error) {
	if strings.TrimSpace(aws.ToString(finding.Id)) == "" {
		return accessanalyzerservice.UnusedAccessSummary{}, nil
	}
	summary := accessanalyzerservice.UnusedAccessSummary{
		FindingID:            strings.TrimSpace(aws.ToString(finding.Id)),
		FindingType:          strings.TrimSpace(string(finding.FindingType)),
		ResourceID:           strings.TrimSpace(aws.ToString(finding.Resource)),
		ResourceOwnerAccount: strings.TrimSpace(aws.ToString(finding.ResourceOwnerAccount)),
		ResourceType:         strings.TrimSpace(string(finding.ResourceType)),
		Status:               strings.TrimSpace(string(finding.Status)),
		AnalyzedAt:           aws.ToTime(finding.AnalyzedAt),
		UpdatedAt:            aws.ToTime(finding.UpdatedAt),
	}
	var nextToken *string
	for {
		var output *awsaccessanalyzer.GetFindingV2Output
		err := c.recordAPICall(ctx, "GetFindingV2", func(callCtx context.Context) error {
			var err error
			output, err = c.client.GetFindingV2(callCtx, &awsaccessanalyzer.GetFindingV2Input{
				AnalyzerArn: aws.String(analyzerARN),
				Id:          finding.Id,
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return accessanalyzerservice.UnusedAccessSummary{}, err
		}
		if output == nil {
			return summary, nil
		}
		summary.LastAccessedAt = latestTime(summary.LastAccessedAt, lastAccessedFromDetails(output.FindingDetails))
		if summary.ResourceID == "" {
			summary.ResourceID = strings.TrimSpace(aws.ToString(output.Resource))
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return summary, nil
		}
	}
}

func lastAccessedFromDetails(details []awsaccessanalyzertypes.FindingDetails) time.Time {
	var latest time.Time
	for _, detail := range details {
		switch typed := detail.(type) {
		case *awsaccessanalyzertypes.FindingDetailsMemberUnusedIamRoleDetails:
			latest = latestTime(latest, aws.ToTime(typed.Value.LastAccessed))
		case *awsaccessanalyzertypes.FindingDetailsMemberUnusedIamUserAccessKeyDetails:
			latest = latestTime(latest, aws.ToTime(typed.Value.LastAccessed))
		case *awsaccessanalyzertypes.FindingDetailsMemberUnusedIamUserPasswordDetails:
			latest = latestTime(latest, aws.ToTime(typed.Value.LastAccessed))
		case *awsaccessanalyzertypes.FindingDetailsMemberUnusedPermissionDetails:
			latest = latestTime(latest, aws.ToTime(typed.Value.LastAccessed))
		}
	}
	return latest
}

type findingBucket struct {
	status       string
	resourceType string
}

func incrementFindingCount(counts map[findingBucket]int64, status string, resourceType string) {
	status = strings.TrimSpace(status)
	resourceType = strings.TrimSpace(resourceType)
	if status == "" || resourceType == "" {
		return
	}
	counts[findingBucket{status: status, resourceType: resourceType}]++
}

func findingCounts(counts map[findingBucket]int64) []accessanalyzerservice.FindingCount {
	output := make([]accessanalyzerservice.FindingCount, 0, len(counts))
	for bucket, count := range counts {
		output = append(output, accessanalyzerservice.FindingCount{
			Status:       bucket.status,
			ResourceType: bucket.resourceType,
			Count:        count,
		})
	}
	sort.Slice(output, func(i, j int) bool {
		if output[i].Status == output[j].Status {
			return output[i].ResourceType < output[j].ResourceType
		}
		return output[i].Status < output[j].Status
	})
	return output
}

func isSupportedAnalyzerType(value string) bool {
	switch strings.TrimSpace(value) {
	case "ACCOUNT", "ORGANIZATION", "ACCOUNT_UNUSED_ACCESS", "ORGANIZATION_UNUSED_ACCESS":
		return true
	default:
		return false
	}
}

func isUnusedAnalyzerType(value string) bool {
	return strings.Contains(strings.TrimSpace(value), "UNUSED_ACCESS")
}

func latestTime(left time.Time, right time.Time) time.Time {
	if right.IsZero() {
		return left
	}
	if left.IsZero() || right.After(left) {
		return right.UTC()
	}
	return left.UTC()
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
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
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

var _ accessanalyzerservice.Client = (*Client)(nil)

var _ apiClient = (*awsaccessanalyzer.Client)(nil)
