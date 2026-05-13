package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	awsecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/checkpoint"
	ecrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr"
)

func TestListImagesResumesFromCheckpointToken(t *testing.T) {
	t.Parallel()

	store := newMemoryCheckpointStore()
	key := testImageCheckpointKey()
	store.saved[key.Identity()] = checkpoint.Checkpoint{Key: key, PageToken: "resume-token", PageNumber: 3}
	api := &checkpointECRAPI{
		imagePages: map[string]checkpointImagePage{
			"resume-token": {
				images: []awsecrtypes.ImageDetail{{
					ImageDigest:    aws.String("sha256:resumed"),
					RepositoryName: aws.String("team/api"),
				}},
			},
		},
	}
	client := testCheckpointClient(api, store)
	images, err := client.ListImages(context.Background(), testRepository())
	if err != nil {
		t.Fatalf("ListImages() error = %v, want nil", err)
	}
	if len(images) != 1 || images[0].ImageDigest != "sha256:resumed" {
		t.Fatalf("ListImages() = %#v, want resumed image", images)
	}
	if len(api.describeImageTokens) != 1 || api.describeImageTokens[0] != "resume-token" {
		t.Fatalf("DescribeImages tokens = %#v, want resume-token", api.describeImageTokens)
	}
	if !store.completed[key.Identity()] {
		t.Fatalf("checkpoint was not completed after terminal page")
	}
}

func TestListImagesDeduplicatesRepeatedPageDelivery(t *testing.T) {
	t.Parallel()

	store := newMemoryCheckpointStore()
	api := &checkpointECRAPI{
		imagePages: map[string]checkpointImagePage{
			"": {
				nextToken: "token-2",
				images: []awsecrtypes.ImageDetail{{
					ImageDigest:    aws.String("sha256:duplicate"),
					RepositoryName: aws.String("team/api"),
				}},
			},
			"token-2": {
				images: []awsecrtypes.ImageDetail{{
					ImageDigest:    aws.String("sha256:duplicate"),
					RepositoryName: aws.String("team/api"),
				}},
			},
		},
	}
	client := testCheckpointClient(api, store)
	images, err := client.ListImages(context.Background(), testRepository())
	if err != nil {
		t.Fatalf("ListImages() error = %v, want nil", err)
	}
	if len(images) != 1 {
		t.Fatalf("ListImages() emitted %d images, want 1: %#v", len(images), images)
	}
}

func TestListImagesCompletesCheckpointForEmptyTerminalPage(t *testing.T) {
	t.Parallel()

	store := newMemoryCheckpointStore()
	api := &checkpointECRAPI{imagePages: map[string]checkpointImagePage{"": {}}}
	client := testCheckpointClient(api, store)
	images, err := client.ListImages(context.Background(), testRepository())
	if err != nil {
		t.Fatalf("ListImages() error = %v, want nil", err)
	}
	if len(images) != 0 {
		t.Fatalf("ListImages() emitted %d images, want 0", len(images))
	}
	if !store.completed[testImageCheckpointKey().Identity()] {
		t.Fatalf("checkpoint was not completed for empty terminal page")
	}
}

func testCheckpointClient(api *checkpointECRAPI, store checkpoint.Store) *Client {
	return &Client{
		client:      api,
		boundary:    testCheckpointBoundary(),
		checkpoints: store,
	}
}

func testImageCheckpointKey() checkpoint.Key {
	return checkpoint.Key{
		Scope: checkpoint.Scope{
			CollectorInstanceID: "aws-prod",
			AccountID:           "123456789012",
			Region:              "us-east-1",
			ServiceKind:         awscloud.ServiceECR,
			GenerationID:        "generation-1",
			FencingToken:        7,
		},
		ResourceParent: "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
		Operation:      "DescribeImages",
	}
}

func testCheckpointBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceECR,
		CollectorInstanceID: "aws-prod",
		GenerationID:        "generation-1",
		FencingToken:        7,
	}
}

func testRepository() ecrservice.Repository {
	return ecrservice.Repository{
		ARN:        "arn:aws:ecr:us-east-1:123456789012:repository/team/api",
		Name:       "team/api",
		RegistryID: "123456789012",
	}
}

type checkpointImagePage struct {
	nextToken string
	images    []awsecrtypes.ImageDetail
}

type checkpointECRAPI struct {
	describeImageTokens []string
	imagePages          map[string]checkpointImagePage
}

func (api *checkpointECRAPI) DescribeImages(
	_ context.Context,
	input *awsecr.DescribeImagesInput,
	_ ...func(*awsecr.Options),
) (*awsecr.DescribeImagesOutput, error) {
	token := aws.ToString(input.NextToken)
	api.describeImageTokens = append(api.describeImageTokens, token)
	page := api.imagePages[token]
	output := &awsecr.DescribeImagesOutput{ImageDetails: page.images}
	if page.nextToken != "" {
		output.NextToken = aws.String(page.nextToken)
	}
	return output, nil
}

func (*checkpointECRAPI) DescribeRepositories(
	context.Context,
	*awsecr.DescribeRepositoriesInput,
	...func(*awsecr.Options),
) (*awsecr.DescribeRepositoriesOutput, error) {
	return nil, nil
}

func (*checkpointECRAPI) GetLifecyclePolicy(
	context.Context,
	*awsecr.GetLifecyclePolicyInput,
	...func(*awsecr.Options),
) (*awsecr.GetLifecyclePolicyOutput, error) {
	return nil, nil
}

func (*checkpointECRAPI) ListTagsForResource(
	context.Context,
	*awsecr.ListTagsForResourceInput,
	...func(*awsecr.Options),
) (*awsecr.ListTagsForResourceOutput, error) {
	return nil, nil
}

type memoryCheckpointStore struct {
	saved     map[string]checkpoint.Checkpoint
	completed map[string]bool
}

func newMemoryCheckpointStore() *memoryCheckpointStore {
	return &memoryCheckpointStore{
		saved:     make(map[string]checkpoint.Checkpoint),
		completed: make(map[string]bool),
	}
}

func (s *memoryCheckpointStore) Load(_ context.Context, key checkpoint.Key) (checkpoint.Checkpoint, bool, error) {
	value, ok := s.saved[key.Identity()]
	return value, ok, nil
}

func (s *memoryCheckpointStore) Save(_ context.Context, value checkpoint.Checkpoint) error {
	s.saved[value.Key.Identity()] = value
	return nil
}

func (s *memoryCheckpointStore) Complete(_ context.Context, key checkpoint.Key) error {
	s.completed[key.Identity()] = true
	delete(s.saved, key.Identity())
	return nil
}

func (s *memoryCheckpointStore) ExpireStale(context.Context, checkpoint.Scope) (int64, error) {
	return 0, nil
}
