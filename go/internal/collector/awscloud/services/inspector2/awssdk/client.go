// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsinspector2 "github.com/aws/aws-sdk-go-v2/service/inspector2"
	i2types "github.com/aws/aws-sdk-go-v2/service/inspector2/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	inspector2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/inspector2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK for Go v2 Inspector v2 surface the adapter
// consumes. It is intentionally read-only and metadata-only: it lists account
// status, member accounts, filter metadata, and CIS scan configuration
// metadata. It exposes no mutation API and no finding-body, code-snippet, SBOM,
// or filter-criteria read. The reflection gate in client_test.go enforces this.
type apiClient interface {
	BatchGetAccountStatus(context.Context, *awsinspector2.BatchGetAccountStatusInput, ...func(*awsinspector2.Options)) (*awsinspector2.BatchGetAccountStatusOutput, error)
	ListMembers(context.Context, *awsinspector2.ListMembersInput, ...func(*awsinspector2.Options)) (*awsinspector2.ListMembersOutput, error)
	ListFilters(context.Context, *awsinspector2.ListFiltersInput, ...func(*awsinspector2.Options)) (*awsinspector2.ListFiltersOutput, error)
	ListCisScanConfigurations(context.Context, *awsinspector2.ListCisScanConfigurationsInput, ...func(*awsinspector2.Options)) (*awsinspector2.ListCisScanConfigurationsOutput, error)
}

// Client adapts AWS SDK Inspector v2 control-plane calls into metadata-only
// scanner records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Inspector v2 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsinspector2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// AccountStatus returns the Inspector v2 status and enabled scan features for
// the claimed account.
func (c *Client) AccountStatus(ctx context.Context) (inspector2service.AccountStatus, error) {
	var output *awsinspector2.BatchGetAccountStatusOutput
	err := c.recordAPICall(ctx, "BatchGetAccountStatus", func(callCtx context.Context) error {
		var err error
		output, err = c.client.BatchGetAccountStatus(callCtx, &awsinspector2.BatchGetAccountStatusInput{
			AccountIds: []string{c.boundary.AccountID},
		})
		return err
	})
	if err != nil {
		return inspector2service.AccountStatus{}, err
	}
	if output == nil {
		return inspector2service.AccountStatus{AccountID: c.boundary.AccountID}, nil
	}
	if len(output.Accounts) == 0 {
		// BatchGetAccountStatus can return HTTP success while reporting the
		// requested account as failed (for example ACCESS_DENIED or
		// BLOCKED_BY_ORGANIZATION_POLICY). Surface that as an error rather than
		// emitting a misleading empty-status account record.
		if failure, ok := failedAccountFor(output.FailedAccounts, c.boundary.AccountID); ok {
			return inspector2service.AccountStatus{}, fmt.Errorf(
				"inspector v2 BatchGetAccountStatus reported account %s as failed (%s): %s",
				c.boundary.AccountID,
				failure.ErrorCode,
				aws.ToString(failure.ErrorMessage),
			)
		}
		return inspector2service.AccountStatus{AccountID: c.boundary.AccountID}, nil
	}
	return mapAccountState(output.Accounts[0]), nil
}

// failedAccountFor returns the failure record for the requested account, if the
// SDK reported one. When the request carries a single account id, AWS may still
// return a failure without echoing the id, so a lone failure is attributed to
// the requested account.
func failedAccountFor(failures []i2types.FailedAccount, accountID string) (i2types.FailedAccount, bool) {
	for _, failure := range failures {
		if aws.ToString(failure.AccountId) == accountID {
			return failure, true
		}
	}
	if len(failures) == 1 {
		return failures[0], true
	}
	return i2types.FailedAccount{}, false
}

// ListMembers returns the Inspector v2 member accounts visible to the claimed
// account. AWS returns an empty list for a non-administrator account.
func (c *Client) ListMembers(ctx context.Context) ([]inspector2service.MemberAccount, error) {
	var members []inspector2service.MemberAccount
	var nextToken *string
	for {
		var page *awsinspector2.ListMembersOutput
		err := c.recordAPICall(ctx, "ListMembers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListMembers(callCtx, &awsinspector2.ListMembersInput{
				OnlyAssociated: aws.Bool(false),
				NextToken:      nextToken,
			})
			return err
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
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return members, nil
		}
	}
}

// ListFilters returns Inspector v2 findings filter metadata. It maps the filter
// name and non-criteria identity only; filter criteria expressions,
// descriptions, and reasons are never read into the scanner-owned type.
func (c *Client) ListFilters(ctx context.Context) ([]inspector2service.FilterSummary, error) {
	var filters []inspector2service.FilterSummary
	var nextToken *string
	for {
		var page *awsinspector2.ListFiltersOutput
		err := c.recordAPICall(ctx, "ListFilters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFilters(callCtx, &awsinspector2.ListFiltersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return filters, nil
		}
		for _, filter := range page.Filters {
			filters = append(filters, mapFilter(filter))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return filters, nil
		}
	}
}

// ListCisScanConfigurations returns Inspector v2 CIS scan configuration
// metadata, including the configured target account set.
func (c *Client) ListCisScanConfigurations(ctx context.Context) ([]inspector2service.CisScanConfiguration, error) {
	var configs []inspector2service.CisScanConfiguration
	var nextToken *string
	for {
		var page *awsinspector2.ListCisScanConfigurationsOutput
		err := c.recordAPICall(ctx, "ListCisScanConfigurations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListCisScanConfigurations(callCtx, &awsinspector2.ListCisScanConfigurationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return configs, nil
		}
		for _, config := range page.ScanConfigurations {
			configs = append(configs, mapCisScanConfiguration(config))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return configs, nil
		}
	}
}

func mapAccountState(state i2types.AccountState) inspector2service.AccountStatus {
	status := ""
	if state.State != nil {
		status = string(state.State.Status)
	}
	return inspector2service.AccountStatus{
		AccountID: strings.TrimSpace(aws.ToString(state.AccountId)),
		Status:    status,
		Features:  mapResourceState(state.ResourceState),
	}
}

func mapResourceState(resource *i2types.ResourceState) []inspector2service.FeatureStatus {
	if resource == nil {
		return nil
	}
	features := make([]inspector2service.FeatureStatus, 0, 4)
	features = appendFeature(features, "ec2", resource.Ec2)
	features = appendFeature(features, "ecr", resource.Ecr)
	features = appendFeature(features, "lambda", resource.Lambda)
	features = appendFeature(features, "lambda_code", resource.LambdaCode)
	if len(features) == 0 {
		return nil
	}
	return features
}

func appendFeature(features []inspector2service.FeatureStatus, key string, state *i2types.State) []inspector2service.FeatureStatus {
	if state == nil {
		return features
	}
	return append(features, inspector2service.FeatureStatus{
		Feature: key,
		Status:  string(state.Status),
	})
}

func mapMember(member i2types.Member) inspector2service.MemberAccount {
	return inspector2service.MemberAccount{
		AccountID:          strings.TrimSpace(aws.ToString(member.AccountId)),
		AdministratorID:    strings.TrimSpace(aws.ToString(member.DelegatedAdminAccountId)),
		RelationshipStatus: string(member.RelationshipStatus),
		UpdatedAt:          formatTime(member.UpdatedAt),
	}
}

func mapFilter(filter i2types.Filter) inspector2service.FilterSummary {
	return inspector2service.FilterSummary{
		ARN:     strings.TrimSpace(aws.ToString(filter.Arn)),
		Name:    strings.TrimSpace(aws.ToString(filter.Name)),
		Action:  string(filter.Action),
		OwnerID: strings.TrimSpace(aws.ToString(filter.OwnerId)),
	}
}

func mapCisScanConfiguration(config i2types.CisScanConfiguration) inspector2service.CisScanConfiguration {
	return inspector2service.CisScanConfiguration{
		ARN:            strings.TrimSpace(aws.ToString(config.ScanConfigurationArn)),
		Name:           strings.TrimSpace(aws.ToString(config.ScanName)),
		OwnerID:        strings.TrimSpace(aws.ToString(config.OwnerId)),
		SecurityLevel:  string(config.SecurityLevel),
		ScheduleKind:   scheduleKind(config.Schedule),
		TargetAccounts: cisTargetAccounts(config.Targets),
		Tags:           cloneStringMap(config.Tags),
	}
}

// scheduleKind reduces the Inspector v2 schedule union to a coarse frequency
// label. The concrete schedule fields (start times, days) are not persisted.
func scheduleKind(schedule i2types.Schedule) string {
	switch schedule.(type) {
	case *i2types.ScheduleMemberDaily:
		return "daily"
	case *i2types.ScheduleMemberWeekly:
		return "weekly"
	case *i2types.ScheduleMemberMonthly:
		return "monthly"
	case *i2types.ScheduleMemberOneTime:
		return "one_time"
	default:
		return ""
	}
}

func cisTargetAccounts(targets *i2types.CisTargets) []string {
	if targets == nil || len(targets.AccountIds) == 0 {
		return nil
	}
	accounts := make([]string, 0, len(targets.AccountIds))
	for _, account := range targets.AccountIds {
		if trimmed := strings.TrimSpace(account); trimmed != "" {
			accounts = append(accounts, trimmed)
		}
	}
	if len(accounts) == 0 {
		return nil
	}
	return accounts
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
