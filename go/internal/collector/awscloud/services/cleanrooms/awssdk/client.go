// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscleanrooms "github.com/aws/aws-sdk-go-v2/service/cleanrooms"
	awscleanroomstypes "github.com/aws/aws-sdk-go-v2/service/cleanrooms/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cleanroomsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cleanrooms"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Clean Rooms API the adapter
// calls. It is deliberately limited to collaboration/configured-table/membership
// list reads, the configured-table get (needed only to resolve the backing-table
// reference for the Glue edge), and resource-tag reads. It exposes no
// query/job/protected-query run, no analysis-rule read, no result read, and no
// Create/Update/Delete mutation, so the adapter cannot run protected queries,
// read query results, or write Clean Rooms state. The exclusion_test reflects
// over this interface to enforce that contract at build time.
type apiClient interface {
	ListCollaborations(
		context.Context,
		*awscleanrooms.ListCollaborationsInput,
		...func(*awscleanrooms.Options),
	) (*awscleanrooms.ListCollaborationsOutput, error)
	ListConfiguredTables(
		context.Context,
		*awscleanrooms.ListConfiguredTablesInput,
		...func(*awscleanrooms.Options),
	) (*awscleanrooms.ListConfiguredTablesOutput, error)
	GetConfiguredTable(
		context.Context,
		*awscleanrooms.GetConfiguredTableInput,
		...func(*awscleanrooms.Options),
	) (*awscleanrooms.GetConfiguredTableOutput, error)
	ListMemberships(
		context.Context,
		*awscleanrooms.ListMembershipsInput,
		...func(*awscleanrooms.Options),
	) (*awscleanrooms.ListMembershipsOutput, error)
	ListTagsForResource(
		context.Context,
		*awscleanrooms.ListTagsForResourceInput,
		...func(*awscleanrooms.Options),
	) (*awscleanrooms.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Clean Rooms control-plane calls into scanner-owned
// metadata. It never runs protected queries or jobs, never reads query results
// or analysis-rule bodies, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Clean Rooms SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscleanrooms.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Clean Rooms collaboration, configured-table, and membership
// metadata visible to the configured AWS credentials. Protected-query results,
// analysis-rule bodies, and allowed-column names are never read.
func (c *Client) Snapshot(ctx context.Context) (cleanroomsservice.Snapshot, error) {
	collaborations, err := c.listCollaborations(ctx)
	if err != nil {
		return cleanroomsservice.Snapshot{}, err
	}
	tables, err := c.listConfiguredTables(ctx)
	if err != nil {
		return cleanroomsservice.Snapshot{}, err
	}
	memberships, err := c.listMemberships(ctx)
	if err != nil {
		return cleanroomsservice.Snapshot{}, err
	}
	return cleanroomsservice.Snapshot{
		Collaborations:   collaborations,
		ConfiguredTables: tables,
		Memberships:      memberships,
	}, nil
}

func (c *Client) listCollaborations(ctx context.Context) ([]cleanroomsservice.Collaboration, error) {
	var collaborations []cleanroomsservice.Collaboration
	var nextToken *string
	for {
		var page *awscleanrooms.ListCollaborationsOutput
		err := c.recordAPICall(ctx, "ListCollaborations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListCollaborations(callCtx, &awscleanrooms.ListCollaborationsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return collaborations, nil
		}
		for _, summary := range page.CollaborationList {
			mapped, err := c.mapCollaboration(ctx, summary)
			if err != nil {
				return nil, err
			}
			collaborations = append(collaborations, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return collaborations, nil
		}
	}
}

func (c *Client) mapCollaboration(
	ctx context.Context,
	summary awscleanroomstypes.CollaborationSummary,
) (cleanroomsservice.Collaboration, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return cleanroomsservice.Collaboration{}, err
	}
	return cleanroomsservice.Collaboration{
		ARN:                arn,
		ID:                 strings.TrimSpace(aws.ToString(summary.Id)),
		Name:               strings.TrimSpace(aws.ToString(summary.Name)),
		CreatorAccountID:   strings.TrimSpace(aws.ToString(summary.CreatorAccountId)),
		CreatorDisplayName: strings.TrimSpace(aws.ToString(summary.CreatorDisplayName)),
		MemberStatus:       strings.TrimSpace(string(summary.MemberStatus)),
		AnalyticsEngine:    strings.TrimSpace(string(summary.AnalyticsEngine)),
		CreateTime:         aws.ToTime(summary.CreateTime),
		UpdateTime:         aws.ToTime(summary.UpdateTime),
		Tags:               tags,
	}, nil
}

func (c *Client) listConfiguredTables(ctx context.Context) ([]cleanroomsservice.ConfiguredTable, error) {
	var tables []cleanroomsservice.ConfiguredTable
	var nextToken *string
	for {
		var page *awscleanrooms.ListConfiguredTablesOutput
		err := c.recordAPICall(ctx, "ListConfiguredTables", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListConfiguredTables(callCtx, &awscleanrooms.ListConfiguredTablesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return tables, nil
		}
		for _, summary := range page.ConfiguredTableSummaries {
			mapped, err := c.mapConfiguredTable(ctx, summary)
			if err != nil {
				return nil, err
			}
			tables = append(tables, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return tables, nil
		}
	}
}

func (c *Client) mapConfiguredTable(
	ctx context.Context,
	summary awscleanroomstypes.ConfiguredTableSummary,
) (cleanroomsservice.ConfiguredTable, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.Id))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return cleanroomsservice.ConfiguredTable{}, err
	}
	table := cleanroomsservice.ConfiguredTable{
		ARN:               arn,
		ID:                id,
		Name:              strings.TrimSpace(aws.ToString(summary.Name)),
		AnalysisMethod:    strings.TrimSpace(string(summary.AnalysisMethod)),
		AnalysisRuleTypes: analysisRuleTypeNames(summary.AnalysisRuleTypes),
		CreateTime:        aws.ToTime(summary.CreateTime),
		UpdateTime:        aws.ToTime(summary.UpdateTime),
		Tags:              tags,
	}
	if err := c.resolveTableReference(ctx, &table); err != nil {
		return cleanroomsservice.ConfiguredTable{}, err
	}
	return table, nil
}

// resolveTableReference reads the configured-table detail only to learn the
// backing-table reference kind and, for Glue tables, the database/table names
// needed for the Glue edge plus the allowed-column count. It never persists the
// allowed-column names themselves.
func (c *Client) resolveTableReference(ctx context.Context, table *cleanroomsservice.ConfiguredTable) error {
	id := strings.TrimSpace(table.ID)
	if id == "" {
		return nil
	}
	var output *awscleanrooms.GetConfiguredTableOutput
	err := c.recordAPICall(ctx, "GetConfiguredTable", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetConfiguredTable(callCtx, &awscleanrooms.GetConfiguredTableInput{
			ConfiguredTableIdentifier: aws.String(id),
		})
		return err
	})
	if err != nil || output == nil || output.ConfiguredTable == nil {
		return err
	}
	detail := output.ConfiguredTable
	table.AllowedColumnCount = len(detail.AllowedColumns)
	applyTableReference(table, detail.TableReference)
	return nil
}

// applyTableReference records only the backing-table source kind and, for a Glue
// table, the database and table names used to key the Glue edge. Athena and
// Snowflake references record their kind only; the Snowflake secret ARN and any
// query/output location are intentionally never mapped.
func applyTableReference(table *cleanroomsservice.ConfiguredTable, reference awscleanroomstypes.TableReference) {
	switch ref := reference.(type) {
	case *awscleanroomstypes.TableReferenceMemberGlue:
		table.TableReferenceKind = "glue"
		table.GlueDatabaseName = strings.TrimSpace(aws.ToString(ref.Value.DatabaseName))
		table.GlueTableName = strings.TrimSpace(aws.ToString(ref.Value.TableName))
	case *awscleanroomstypes.TableReferenceMemberAthena:
		table.TableReferenceKind = "athena"
	case *awscleanroomstypes.TableReferenceMemberSnowflake:
		table.TableReferenceKind = "snowflake"
	}
}

func (c *Client) listMemberships(ctx context.Context) ([]cleanroomsservice.Membership, error) {
	var memberships []cleanroomsservice.Membership
	var nextToken *string
	for {
		var page *awscleanrooms.ListMembershipsOutput
		err := c.recordAPICall(ctx, "ListMemberships", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListMemberships(callCtx, &awscleanrooms.ListMembershipsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return memberships, nil
		}
		for _, summary := range page.MembershipSummaries {
			mapped, err := c.mapMembership(ctx, summary)
			if err != nil {
				return nil, err
			}
			memberships = append(memberships, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return memberships, nil
		}
	}
}

func (c *Client) mapMembership(
	ctx context.Context,
	summary awscleanroomstypes.MembershipSummary,
) (cleanroomsservice.Membership, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return cleanroomsservice.Membership{}, err
	}
	return cleanroomsservice.Membership{
		ARN:                           arn,
		ID:                            strings.TrimSpace(aws.ToString(summary.Id)),
		CollaborationARN:              strings.TrimSpace(aws.ToString(summary.CollaborationArn)),
		CollaborationID:               strings.TrimSpace(aws.ToString(summary.CollaborationId)),
		CollaborationName:             strings.TrimSpace(aws.ToString(summary.CollaborationName)),
		CollaborationCreatorAccountID: strings.TrimSpace(aws.ToString(summary.CollaborationCreatorAccountId)),
		MemberAbilities:               memberAbilityNames(summary.MemberAbilities),
		Status:                        strings.TrimSpace(string(summary.Status)),
		CreateTime:                    aws.ToTime(summary.CreateTime),
		UpdateTime:                    aws.ToTime(summary.UpdateTime),
		Tags:                          tags,
	}, nil
}

func analysisRuleTypeNames(types []awscleanroomstypes.ConfiguredTableAnalysisRuleType) []string {
	if len(types) == 0 {
		return nil
	}
	names := make([]string, 0, len(types))
	for _, value := range types {
		if name := strings.TrimSpace(string(value)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func memberAbilityNames(abilities []awscleanroomstypes.MemberAbility) []string {
	if len(abilities) == 0 {
		return nil
	}
	names := make([]string, 0, len(abilities))
	for _, value := range abilities {
		if name := strings.TrimSpace(string(value)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awscleanrooms.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awscleanrooms.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for key, value := range output.Tags {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = value
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
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

var _ cleanroomsservice.Client = (*Client)(nil)

var _ apiClient = (*awscleanrooms.Client)(nil)
