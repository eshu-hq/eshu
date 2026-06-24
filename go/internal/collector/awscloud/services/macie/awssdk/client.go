// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmacie2 "github.com/aws/aws-sdk-go-v2/service/macie2"
	macietypes "github.com/aws/aws-sdk-go-v2/service/macie2/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	macieservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/macie"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK for Go v2 Amazon Macie surface the adapter
// consumes. It is intentionally the highest-redaction read surface in the
// collector: it reads the account session status, the administrator account id,
// member metadata, classification-job metadata (counts only), allow-list and
// custom data identifier identities, findings filter identities, and aggregate
// finding counts by severity.
//
// It exposes no sensitive-data finding read (GetSensitiveDataOccurrences,
// GetFindings, ListFindings), no custom data identifier regular-expression read
// (GetCustomDataIdentifier, TestCustomDataIdentifier,
// BatchGetCustomDataIdentifiers), no allow-list content read (GetAllowList), no
// findings filter criteria read (GetFindingsFilter), no classification-job
// bucket-criteria read (DescribeClassificationJob, DescribeBuckets,
// SearchResources), and no mutation API. The reflection gate in client_test.go
// enforces this exclusion.
type apiClient interface {
	GetMacieSession(context.Context, *awsmacie2.GetMacieSessionInput, ...func(*awsmacie2.Options)) (*awsmacie2.GetMacieSessionOutput, error)
	GetAdministratorAccount(context.Context, *awsmacie2.GetAdministratorAccountInput, ...func(*awsmacie2.Options)) (*awsmacie2.GetAdministratorAccountOutput, error)
	ListMembers(context.Context, *awsmacie2.ListMembersInput, ...func(*awsmacie2.Options)) (*awsmacie2.ListMembersOutput, error)
	ListClassificationJobs(context.Context, *awsmacie2.ListClassificationJobsInput, ...func(*awsmacie2.Options)) (*awsmacie2.ListClassificationJobsOutput, error)
	ListAllowLists(context.Context, *awsmacie2.ListAllowListsInput, ...func(*awsmacie2.Options)) (*awsmacie2.ListAllowListsOutput, error)
	ListCustomDataIdentifiers(context.Context, *awsmacie2.ListCustomDataIdentifiersInput, ...func(*awsmacie2.Options)) (*awsmacie2.ListCustomDataIdentifiersOutput, error)
	ListFindingsFilters(context.Context, *awsmacie2.ListFindingsFiltersInput, ...func(*awsmacie2.Options)) (*awsmacie2.ListFindingsFiltersOutput, error)
	GetFindingStatistics(context.Context, *awsmacie2.GetFindingStatisticsInput, ...func(*awsmacie2.Options)) (*awsmacie2.GetFindingStatisticsOutput, error)
}

// Client adapts AWS SDK Amazon Macie control-plane calls into metadata-only
// scanner records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Amazon Macie SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsmacie2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Session returns the Macie account session status. Macie returns an
// AccessDeniedException whose message reports that Macie is not enabled when the
// account has no session; the adapter maps that single case to a disabled
// session and surfaces every other error, so a genuine authorization failure is
// never reported as a clean disabled account.
func (c *Client) Session(ctx context.Context) (macieservice.Session, error) {
	var output *awsmacie2.GetMacieSessionOutput
	err := c.recordAPICall(ctx, "GetMacieSession", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetMacieSession(callCtx, &awsmacie2.GetMacieSessionInput{})
		return callErr
	})
	if err != nil {
		if isMacieNotEnabled(err) {
			return macieservice.Session{Enabled: false, Status: "DISABLED"}, nil
		}
		return macieservice.Session{}, err
	}
	if output == nil {
		return macieservice.Session{Enabled: false, Status: "DISABLED"}, nil
	}
	return macieservice.Session{
		Enabled:                    true,
		Status:                     string(output.Status),
		FindingPublishingFrequency: string(output.FindingPublishingFrequency),
		ServiceRoleARN:             strings.TrimSpace(aws.ToString(output.ServiceRole)),
		CreatedAt:                  formatTime(output.CreatedAt),
		UpdatedAt:                  formatTime(output.UpdatedAt),
	}, nil
}

// AdministratorAccountID returns the delegated administrator account id for a
// member account. Macie returns a ResourceNotFoundException when the account has
// no administrator (it is standalone or is itself the administrator); the
// adapter maps that to an empty id and surfaces every other error.
func (c *Client) AdministratorAccountID(ctx context.Context) (string, error) {
	var output *awsmacie2.GetAdministratorAccountOutput
	err := c.recordAPICall(ctx, "GetAdministratorAccount", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetAdministratorAccount(callCtx, &awsmacie2.GetAdministratorAccountInput{})
		return callErr
	})
	if err != nil {
		if isMacieNoAdministrator(err) {
			return "", nil
		}
		return "", err
	}
	if output == nil || output.Administrator == nil {
		return "", nil
	}
	return strings.TrimSpace(aws.ToString(output.Administrator.AccountId)), nil
}

// ListMembers returns the Macie member accounts visible to the claimed account.
// AWS returns an empty list for a non-administrator account. Member email
// addresses are personal contact data and are never read into the scanner type.
func (c *Client) ListMembers(ctx context.Context) ([]macieservice.MemberAccount, error) {
	var members []macieservice.MemberAccount
	var nextToken *string
	for {
		var page *awsmacie2.ListMembersOutput
		err := c.recordAPICall(ctx, "ListMembers", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListMembers(callCtx, &awsmacie2.ListMembersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return members, nil
		}
		for _, member := range page.Members {
			members = append(members, mapMember(member))
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return members, nil
		}
		nextToken = page.NextToken
	}
}

// ListClassificationJobs returns Macie classification-job metadata. It reduces
// the job's bucket definitions and bucket criteria to counts only: the explicit
// bucket list (S3BucketDefinitionForJob.Buckets) and the property/tag criteria
// (S3BucketCriteriaForJob) are never read into the scanner type.
func (c *Client) ListClassificationJobs(ctx context.Context) ([]macieservice.ClassificationJob, error) {
	var jobs []macieservice.ClassificationJob
	var nextToken *string
	for {
		var page *awsmacie2.ListClassificationJobsOutput
		err := c.recordAPICall(ctx, "ListClassificationJobs", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListClassificationJobs(callCtx, &awsmacie2.ListClassificationJobsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return jobs, nil
		}
		for _, job := range page.Items {
			jobs = append(jobs, mapJob(job))
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return jobs, nil
		}
		nextToken = page.NextToken
	}
}

// ListAllowLists returns Macie allow-list identities. Allow-list contents
// (literal allow text and S3-hosted regex references) and descriptions are never
// read into the scanner type.
func (c *Client) ListAllowLists(ctx context.Context) ([]macieservice.AllowList, error) {
	var allowLists []macieservice.AllowList
	var nextToken *string
	for {
		var page *awsmacie2.ListAllowListsOutput
		err := c.recordAPICall(ctx, "ListAllowLists", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAllowLists(callCtx, &awsmacie2.ListAllowListsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return allowLists, nil
		}
		for _, allowList := range page.AllowLists {
			allowLists = append(allowLists, macieservice.AllowList{
				ID:   strings.TrimSpace(aws.ToString(allowList.Id)),
				Name: strings.TrimSpace(aws.ToString(allowList.Name)),
			})
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return allowLists, nil
		}
		nextToken = page.NextToken
	}
}

// ListCustomDataIdentifiers returns Macie custom data identifier identities. The
// regular-expression body, keyword list, ignore words, and description are never
// read into the scanner type because they describe the sensitive data the
// customer is detecting.
func (c *Client) ListCustomDataIdentifiers(ctx context.Context) ([]macieservice.CustomDataIdentifier, error) {
	var identifiers []macieservice.CustomDataIdentifier
	var nextToken *string
	for {
		var page *awsmacie2.ListCustomDataIdentifiersOutput
		err := c.recordAPICall(ctx, "ListCustomDataIdentifiers", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListCustomDataIdentifiers(callCtx, &awsmacie2.ListCustomDataIdentifiersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return identifiers, nil
		}
		for _, identifier := range page.Items {
			identifiers = append(identifiers, macieservice.CustomDataIdentifier{
				ID:   strings.TrimSpace(aws.ToString(identifier.Id)),
				Name: strings.TrimSpace(aws.ToString(identifier.Name)),
			})
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return identifiers, nil
		}
		nextToken = page.NextToken
	}
}

// ListFindingsFilters returns Macie findings filter identities. The filter
// criteria expressions are never read into the scanner type.
func (c *Client) ListFindingsFilters(ctx context.Context) ([]macieservice.FindingsFilter, error) {
	var filters []macieservice.FindingsFilter
	var nextToken *string
	for {
		var page *awsmacie2.ListFindingsFiltersOutput
		err := c.recordAPICall(ctx, "ListFindingsFilters", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListFindingsFilters(callCtx, &awsmacie2.ListFindingsFiltersInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return filters, nil
		}
		for _, filter := range page.FindingsFilterListItems {
			filters = append(filters, macieservice.FindingsFilter{
				ID:     strings.TrimSpace(aws.ToString(filter.Id)),
				Name:   strings.TrimSpace(aws.ToString(filter.Name)),
				Action: string(filter.Action),
			})
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return filters, nil
		}
		nextToken = page.NextToken
	}
}

// FindingCountsBySeverity returns the aggregate count of Macie findings grouped
// by severity label only (GroupBy=severity.description). The response carries a
// severity label and a count per group; no finding body, finding type, finding
// identifier, or affected-resource identity is read.
func (c *Client) FindingCountsBySeverity(ctx context.Context) (map[string]int64, error) {
	var output *awsmacie2.GetFindingStatisticsOutput
	err := c.recordAPICall(ctx, "GetFindingStatistics", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.GetFindingStatistics(callCtx, &awsmacie2.GetFindingStatisticsInput{
			GroupBy: macietypes.GroupBySeverityDescription,
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil || len(output.CountsByGroup) == 0 {
		return nil, nil
	}
	counts := make(map[string]int64, len(output.CountsByGroup))
	for _, group := range output.CountsByGroup {
		key := strings.TrimSpace(aws.ToString(group.GroupKey))
		if key == "" {
			continue
		}
		counts[key] = aws.ToInt64(group.Count)
	}
	if len(counts) == 0 {
		return nil, nil
	}
	return counts, nil
}

func mapMember(member macietypes.Member) macieservice.MemberAccount {
	return macieservice.MemberAccount{
		AccountID:          strings.TrimSpace(aws.ToString(member.AccountId)),
		AdministratorID:    strings.TrimSpace(aws.ToString(member.AdministratorAccountId)),
		RelationshipStatus: string(member.RelationshipStatus),
		InvitedAt:          formatTime(member.InvitedAt),
		UpdatedAt:          formatTime(member.UpdatedAt),
		Tags:               cloneStringMap(member.Tags),
	}
}

// mapJob reduces a Macie job summary to identity, type, status, and bucket and
// account counts. BucketDefinitions and BucketCriteria are read only to compute
// counts; their contents are discarded immediately and never stored.
func mapJob(job macietypes.JobSummary) macieservice.ClassificationJob {
	bucketCount := 0
	for _, definition := range job.BucketDefinitions {
		bucketCount += len(definition.Buckets)
	}
	return macieservice.ClassificationJob{
		JobID:             strings.TrimSpace(aws.ToString(job.JobId)),
		Name:              strings.TrimSpace(aws.ToString(job.Name)),
		JobType:           string(job.JobType),
		JobStatus:         string(job.JobStatus),
		CreatedAt:         formatTime(job.CreatedAt),
		BucketCount:       bucketCount,
		AccountCount:      len(job.BucketDefinitions),
		HasBucketCriteria: job.BucketCriteria != nil,
	}
}

// isMacieNotEnabled reports whether the error is Macie's "not enabled" response.
// Macie returns AccessDeniedException with a message stating Macie is not
// enabled for an account that has never turned it on. Matching on the message is
// required because the AWS error code alone (AccessDeniedException) is shared
// with genuine authorization failures, which must be surfaced.
func isMacieNotEnabled(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.ErrorMessage()), "macie is not enabled")
}

// isMacieNoAdministrator reports whether the error means the account has no
// Macie administrator (it is standalone or is itself the administrator). Macie
// returns ResourceNotFoundException in that case.
func isMacieNoAdministrator(err error) bool {
	var notFound *macietypes.ResourceNotFoundException
	return errors.As(err, &notFound)
}

// hasNextPage reports whether a paginated response advanced to a new token,
// guarding against a server that echoes the same non-empty token forever.
func hasNextPage(previous, next *string) bool {
	token := strings.TrimSpace(aws.ToString(next))
	if token == "" {
		return false
	}
	return token != strings.TrimSpace(aws.ToString(previous))
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
