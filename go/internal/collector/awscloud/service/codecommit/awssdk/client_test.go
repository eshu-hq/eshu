// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscodecommit "github.com/aws/aws-sdk-go-v2/service/codecommit"
	awscodecommittypes "github.com/aws/aws-sdk-go-v2/service/codecommit/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// fakeAPIClient is a metadata-only CodeCommit SDK stub. It satisfies the
// adapter-local apiClient interface, which by construction exposes no commit,
// ref, blob, file-content, pull-request, comment, or mutation methods.
type fakeAPIClient struct {
	repositories []awscodecommittypes.RepositoryNameIdPair
	metadata     map[string]awscodecommittypes.RepositoryMetadata
	triggers     map[string][]awscodecommittypes.RepositoryTrigger
	tags         map[string]map[string]string

	batchCalls int
}

func (c *fakeAPIClient) ListRepositories(_ context.Context, _ *awscodecommit.ListRepositoriesInput, _ ...func(*awscodecommit.Options)) (*awscodecommit.ListRepositoriesOutput, error) {
	return &awscodecommit.ListRepositoriesOutput{Repositories: c.repositories}, nil
}

func (c *fakeAPIClient) BatchGetRepositories(_ context.Context, input *awscodecommit.BatchGetRepositoriesInput, _ ...func(*awscodecommit.Options)) (*awscodecommit.BatchGetRepositoriesOutput, error) {
	c.batchCalls++
	out := &awscodecommit.BatchGetRepositoriesOutput{}
	for _, name := range input.RepositoryNames {
		if meta, ok := c.metadata[name]; ok {
			out.Repositories = append(out.Repositories, meta)
		}
	}
	return out, nil
}

func (c *fakeAPIClient) GetRepositoryTriggers(_ context.Context, input *awscodecommit.GetRepositoryTriggersInput, _ ...func(*awscodecommit.Options)) (*awscodecommit.GetRepositoryTriggersOutput, error) {
	return &awscodecommit.GetRepositoryTriggersOutput{Triggers: c.triggers[aws.ToString(input.RepositoryName)]}, nil
}

func (c *fakeAPIClient) ListTagsForResource(_ context.Context, input *awscodecommit.ListTagsForResourceInput, _ ...func(*awscodecommit.Options)) (*awscodecommit.ListTagsForResourceOutput, error) {
	return &awscodecommit.ListTagsForResourceOutput{Tags: c.tags[aws.ToString(input.ResourceArn)]}, nil
}

func newTestClient(api apiClient) *Client {
	return &Client{
		client:   api,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceCodeCommit},
	}
}

func TestClientMapsRepositoryMetadataTriggersAndTags(t *testing.T) {
	const repositoryARN = "arn:aws:codecommit:us-east-1:123456789012:payments-api"
	const topicARN = "arn:aws:sns:us-east-1:123456789012:codecommit-notifications"
	created := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	modified := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)

	api := &fakeAPIClient{
		repositories: []awscodecommittypes.RepositoryNameIdPair{
			{RepositoryName: aws.String("payments-api"), RepositoryId: aws.String("repo-1234")},
		},
		metadata: map[string]awscodecommittypes.RepositoryMetadata{
			"payments-api": {
				Arn:              aws.String(repositoryARN),
				RepositoryName:   aws.String("payments-api"),
				RepositoryId:     aws.String("repo-1234"),
				AccountId:        aws.String("123456789012"),
				DefaultBranch:    aws.String("main"),
				CloneUrlHttp:     aws.String("https://git-codecommit.us-east-1.amazonaws.com/v1/repos/payments-api"),
				CloneUrlSsh:      aws.String("ssh://git-codecommit.us-east-1.amazonaws.com/v1/repos/payments-api"),
				KmsKeyId:         aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
				CreationDate:     aws.Time(created),
				LastModifiedDate: aws.Time(modified),
			},
		},
		triggers: map[string][]awscodecommittypes.RepositoryTrigger{
			"payments-api": {{
				Name:           aws.String("notify-main"),
				DestinationArn: aws.String(topicARN),
				Events:         []awscodecommittypes.RepositoryTriggerEventEnum{awscodecommittypes.RepositoryTriggerEventEnumAll},
				Branches:       []string{"main"},
			}},
		},
		tags: map[string]map[string]string{
			repositoryARN: {"Environment": "Prod"},
		},
	}

	repositories, err := newTestClient(api).ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories returned error: %v", err)
	}
	if len(repositories) != 1 {
		t.Fatalf("repository count = %d, want 1", len(repositories))
	}
	repository := repositories[0]
	if repository.ARN != repositoryARN {
		t.Fatalf("ARN = %q, want %q", repository.ARN, repositoryARN)
	}
	if repository.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", repository.DefaultBranch)
	}
	if repository.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/abc" {
		t.Fatalf("KMSKeyID = %q", repository.KMSKeyID)
	}
	if !repository.CreatedAt.Equal(created) || !repository.LastModifiedAt.Equal(modified) {
		t.Fatalf("timestamps not mapped: created=%v modified=%v", repository.CreatedAt, repository.LastModifiedAt)
	}
	if len(repository.Triggers) != 1 || repository.Triggers[0].DestinationARN != topicARN {
		t.Fatalf("trigger not mapped: %#v", repository.Triggers)
	}
	if repository.Tags["Environment"] != "Prod" {
		t.Fatalf("tags not mapped: %#v", repository.Tags)
	}
}

func TestClientChunksBatchGetRepositories(t *testing.T) {
	pairs := make([]awscodecommittypes.RepositoryNameIdPair, 0, batchRepositoryLimit+5)
	metadata := make(map[string]awscodecommittypes.RepositoryMetadata, batchRepositoryLimit+5)
	for i := 0; i < batchRepositoryLimit+5; i++ {
		name := "repo-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		pairs = append(pairs, awscodecommittypes.RepositoryNameIdPair{RepositoryName: aws.String(name)})
		metadata[name] = awscodecommittypes.RepositoryMetadata{
			Arn:            aws.String("arn:aws:codecommit:us-east-1:123456789012:" + name),
			RepositoryName: aws.String(name),
		}
	}
	api := &fakeAPIClient{repositories: pairs, metadata: metadata}

	repositories, err := newTestClient(api).ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories returned error: %v", err)
	}
	if len(repositories) != batchRepositoryLimit+5 {
		t.Fatalf("repository count = %d, want %d", len(repositories), batchRepositoryLimit+5)
	}
	if api.batchCalls != 2 {
		t.Fatalf("BatchGetRepositories called %d times, want 2 (chunked by %d)", api.batchCalls, batchRepositoryLimit)
	}
}

func TestClientReturnsNilForNoRepositories(t *testing.T) {
	repositories, err := newTestClient(&fakeAPIClient{}).ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories returned error: %v", err)
	}
	if repositories != nil {
		t.Fatalf("expected nil repositories for empty account, got %#v", repositories)
	}
}
