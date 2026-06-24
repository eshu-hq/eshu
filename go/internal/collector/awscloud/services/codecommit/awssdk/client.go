// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscodecommit "github.com/aws/aws-sdk-go-v2/service/codecommit"
	awscodecommittypes "github.com/aws/aws-sdk-go-v2/service/codecommit/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ccservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codecommit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// batchRepositoryLimit caps BatchGetRepositories input per the AWS contract,
// which accepts at most 25 repository names per call. The adapter chunks larger
// name lists so a repository-heavy account does not fail the whole scan.
const batchRepositoryLimit = 25

// apiClient is the metadata-only CodeCommit SDK surface the adapter consumes. It
// intentionally omits every mutation API (CreateRepository, DeleteRepository,
// UpdateRepository*, PutRepositoryTriggers, PutFile, DeleteFile,
// CreateCommit, CreateBranch, CreatePullRequest, MergePullRequest*, and the
// rest) and every commit/ref/blob/file-content reader (GetFile, GetBlob,
// GetCommit, GetDifferences, GetBranch, GetMergeCommit, BatchGetCommits, ...).
// The reflection guard test asserts the omission.
type apiClient interface {
	ListRepositories(context.Context, *awscodecommit.ListRepositoriesInput, ...func(*awscodecommit.Options)) (*awscodecommit.ListRepositoriesOutput, error)
	BatchGetRepositories(context.Context, *awscodecommit.BatchGetRepositoriesInput, ...func(*awscodecommit.Options)) (*awscodecommit.BatchGetRepositoriesOutput, error)
	GetRepositoryTriggers(context.Context, *awscodecommit.GetRepositoryTriggersInput, ...func(*awscodecommit.Options)) (*awscodecommit.GetRepositoryTriggersOutput, error)
	ListTagsForResource(context.Context, *awscodecommit.ListTagsForResourceInput, ...func(*awscodecommit.Options)) (*awscodecommit.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK CodeCommit pagination into scanner-owned metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CodeCommit SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscodecommit.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListRepositories returns every CodeCommit repository visible to the
// configured AWS credentials with its metadata, trigger, and tag evidence
// resolved.
func (c *Client) ListRepositories(ctx context.Context) ([]ccservice.Repository, error) {
	names, err := c.listRepositoryNames(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	metadata, err := c.batchGetRepositories(ctx, names)
	if err != nil {
		return nil, err
	}
	repositories := make([]ccservice.Repository, 0, len(metadata))
	for _, repository := range metadata {
		name := aws.ToString(repository.RepositoryName)
		triggers, err := c.getRepositoryTriggers(ctx, name)
		if err != nil {
			return nil, err
		}
		tags, err := c.listTags(ctx, aws.ToString(repository.Arn))
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, mapRepository(repository, triggers, tags))
	}
	return repositories, nil
}

func (c *Client) listRepositoryNames(ctx context.Context) ([]string, error) {
	paginator := awscodecommit.NewListRepositoriesPaginator(c.client, &awscodecommit.ListRepositoriesInput{})
	var names []string
	for paginator.HasMorePages() {
		var page *awscodecommit.ListRepositoriesOutput
		err := c.recordAPICall(ctx, "ListRepositories", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, pair := range page.Repositories {
			if name := strings.TrimSpace(aws.ToString(pair.RepositoryName)); name != "" {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

func (c *Client) batchGetRepositories(ctx context.Context, names []string) ([]awscodecommittypes.RepositoryMetadata, error) {
	var metadata []awscodecommittypes.RepositoryMetadata
	for start := 0; start < len(names); start += batchRepositoryLimit {
		end := start + batchRepositoryLimit
		if end > len(names) {
			end = len(names)
		}
		var output *awscodecommit.BatchGetRepositoriesOutput
		err := c.recordAPICall(ctx, "BatchGetRepositories", func(callCtx context.Context) error {
			var err error
			output, err = c.client.BatchGetRepositories(callCtx, &awscodecommit.BatchGetRepositoriesInput{
				RepositoryNames: names[start:end],
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output != nil {
			metadata = append(metadata, output.Repositories...)
		}
	}
	return metadata, nil
}

func (c *Client) getRepositoryTriggers(ctx context.Context, name string) ([]awscodecommittypes.RepositoryTrigger, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	var output *awscodecommit.GetRepositoryTriggersOutput
	err := c.recordAPICall(ctx, "GetRepositoryTriggers", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetRepositoryTriggers(callCtx, &awscodecommit.GetRepositoryTriggersInput{
			RepositoryName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Triggers, nil
}

func (c *Client) listTags(ctx context.Context, repositoryARN string) (map[string]string, error) {
	repositoryARN = strings.TrimSpace(repositoryARN)
	if repositoryARN == "" {
		return nil, nil
	}
	tags := make(map[string]string)
	var nextToken *string
	for {
		var output *awscodecommit.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTagsForResource(callCtx, &awscodecommit.ListTagsForResourceInput{
				ResourceArn: aws.String(repositoryARN),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for key, value := range output.Tags {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				tags[trimmed] = value
			}
		}
		nextToken = output.NextToken
		if nextToken == nil || strings.TrimSpace(aws.ToString(nextToken)) == "" {
			break
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func mapRepository(
	repository awscodecommittypes.RepositoryMetadata,
	triggers []awscodecommittypes.RepositoryTrigger,
	tags map[string]string,
) ccservice.Repository {
	return ccservice.Repository{
		ARN:            aws.ToString(repository.Arn),
		Name:           aws.ToString(repository.RepositoryName),
		ID:             aws.ToString(repository.RepositoryId),
		AccountID:      aws.ToString(repository.AccountId),
		DefaultBranch:  aws.ToString(repository.DefaultBranch),
		CloneURLHTTP:   aws.ToString(repository.CloneUrlHttp),
		CloneURLSSH:    aws.ToString(repository.CloneUrlSsh),
		KMSKeyID:       aws.ToString(repository.KmsKeyId),
		CreatedAt:      aws.ToTime(repository.CreationDate),
		LastModifiedAt: aws.ToTime(repository.LastModifiedDate),
		Triggers:       mapTriggers(triggers),
		Tags:           tags,
	}
}

func mapTriggers(triggers []awscodecommittypes.RepositoryTrigger) []ccservice.Trigger {
	if len(triggers) == 0 {
		return nil
	}
	output := make([]ccservice.Trigger, 0, len(triggers))
	for _, trigger := range triggers {
		output = append(output, ccservice.Trigger{
			Name:           aws.ToString(trigger.Name),
			DestinationARN: aws.ToString(trigger.DestinationArn),
			Events:         mapTriggerEvents(trigger.Events),
			Branches:       cloneStrings(trigger.Branches),
		})
	}
	return output
}

func mapTriggerEvents(events []awscodecommittypes.RepositoryTriggerEventEnum) []string {
	if len(events) == 0 {
		return nil
	}
	output := make([]string, 0, len(events))
	for _, event := range events {
		if trimmed := strings.TrimSpace(string(event)); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
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
		attrs := metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		)
		c.instruments.AWSAPICalls.Add(ctx, 1, attrs)
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

var _ ccservice.Client = (*Client)(nil)

var _ apiClient = (*awscodecommit.Client)(nil)
