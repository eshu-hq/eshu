// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"strings"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsdetective "github.com/aws/aws-sdk-go-v2/service/detective"
	detectivetypes "github.com/aws/aws-sdk-go-v2/service/detective/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	detectiveservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/detective"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK for Go v2 Amazon Detective surface the
// adapter consumes. It exposes only the three read-only list APIs the scanner
// needs: behavior graphs, graph members, and graph tags.
//
// It exposes no investigation read (GetInvestigation, ListInvestigations,
// StartInvestigation, ListIndicators), no finding/datasource detail read
// (BatchGetGraphMemberDatasources, BatchGetMembershipDatasources, GetMembers),
// and no mutation API (CreateGraph, DeleteGraph, CreateMembers, DeleteMembers,
// TagResource, UntagResource, and the organization-admin and invitation
// mutations). The reflection gate in client_test.go enforces this exclusion.
type apiClient interface {
	ListGraphs(context.Context, *awsdetective.ListGraphsInput, ...func(*awsdetective.Options)) (*awsdetective.ListGraphsOutput, error)
	ListMembers(context.Context, *awsdetective.ListMembersInput, ...func(*awsdetective.Options)) (*awsdetective.ListMembersOutput, error)
	ListTagsForResource(context.Context, *awsdetective.ListTagsForResourceInput, ...func(*awsdetective.Options)) (*awsdetective.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Amazon Detective control-plane calls into
// metadata-only scanner records.
type Client struct {
	client      apiClient
	boundary    aws.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Amazon Detective SDK adapter for one claimed AWS
// boundary.
func NewClient(
	config awsv2.Config,
	boundary aws.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdetective.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListGraphs returns the behavior graphs the claimed account administers in the
// boundary region. Each graph carries its ARN and creation time only.
func (c *Client) ListGraphs(ctx context.Context) ([]detectiveservice.Graph, error) {
	var graphs []detectiveservice.Graph
	var nextToken *string
	for {
		var page *awsdetective.ListGraphsOutput
		err := c.recordAPICall(ctx, "ListGraphs", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListGraphs(callCtx, &awsdetective.ListGraphsInput{
				NextToken: nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return graphs, nil
		}
		for _, graph := range page.GraphList {
			arn := strings.TrimSpace(awsv2.ToString(graph.Arn))
			if arn == "" {
				continue
			}
			graphs = append(graphs, detectiveservice.Graph{
				ARN:       arn,
				CreatedAt: formatTime(graph.CreatedTime),
			})
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return graphs, nil
		}
		nextToken = page.NextToken
	}
}

// ListMembers returns the member accounts enrolled in one behavior graph. The
// member's contact email (MemberDetail.EmailAddress) is personal data and is
// never read into the scanner type. Usage volume and finding content are not
// read; only the enabled data-source package names survive.
func (c *Client) ListMembers(ctx context.Context, graphARN string) ([]detectiveservice.MemberAccount, error) {
	graphARN = strings.TrimSpace(graphARN)
	if graphARN == "" {
		return nil, nil
	}
	var members []detectiveservice.MemberAccount
	var nextToken *string
	for {
		var page *awsdetective.ListMembersOutput
		err := c.recordAPICall(ctx, "ListMembers", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListMembers(callCtx, &awsdetective.ListMembersInput{
				GraphArn:  awsv2.String(graphARN),
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
		for _, member := range page.MemberDetails {
			members = append(members, mapMember(member))
		}
		if !hasNextPage(nextToken, page.NextToken) {
			return members, nil
		}
		nextToken = page.NextToken
	}
}

// ListTags returns the AWS resource tags for one behavior graph.
func (c *Client) ListTags(ctx context.Context, graphARN string) (map[string]string, error) {
	graphARN = strings.TrimSpace(graphARN)
	if graphARN == "" {
		return nil, nil
	}
	var output *awsdetective.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.ListTagsForResource(callCtx, &awsdetective.ListTagsForResourceInput{
			ResourceArn: awsv2.String(graphARN),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return cloneStringMap(output.Tags), nil
}

// mapMember reduces a Detective MemberDetail to identity, membership status, and
// the enabled data-source package names. The email address, usage volume, and
// deprecated master-id and graph-utilization fields are intentionally dropped.
func mapMember(member detectivetypes.MemberDetail) detectiveservice.MemberAccount {
	return detectiveservice.MemberAccount{
		AccountID:          strings.TrimSpace(awsv2.ToString(member.AccountId)),
		AdministratorID:    strings.TrimSpace(awsv2.ToString(member.AdministratorId)),
		GraphARN:           strings.TrimSpace(awsv2.ToString(member.GraphArn)),
		Status:             string(member.Status),
		InvitationType:     string(member.InvitationType),
		InvitedAt:          formatTime(member.InvitedTime),
		UpdatedAt:          formatTime(member.UpdatedTime),
		DatasourcePackages: ingestStatePackages(member.DatasourcePackageIngestStates),
	}
}

// ingestStatePackages returns the sorted data-source package names present on a
// member's ingest-state map. Only the package keys (for example DETECTIVE_CORE)
// survive; the per-package ingest state value is not carried.
func ingestStatePackages(states map[string]detectivetypes.DatasourcePackageIngestState) []string {
	if len(states) == 0 {
		return nil
	}
	packages := make([]string, 0, len(states))
	for pkg := range states {
		trimmed := strings.TrimSpace(pkg)
		if trimmed == "" {
			continue
		}
		packages = append(packages, trimmed)
	}
	if len(packages) == 0 {
		return nil
	}
	sortStrings(packages)
	return packages
}

// hasNextPage reports whether a paginated response advanced to a new token,
// guarding against a server that echoes the same non-empty token forever.
func hasNextPage(previous, next *string) bool {
	token := strings.TrimSpace(awsv2.ToString(next))
	if token == "" {
		return false
	}
	return token != strings.TrimSpace(awsv2.ToString(previous))
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
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
