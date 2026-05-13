package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	awsecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ecrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ecr"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	awsecr.DescribeImagesAPIClient
	awsecr.DescribeRepositoriesAPIClient
	GetLifecyclePolicy(context.Context, *awsecr.GetLifecyclePolicyInput, ...func(*awsecr.Options)) (*awsecr.GetLifecyclePolicyOutput, error)
	ListTagsForResource(context.Context, *awsecr.ListTagsForResourceInput, ...func(*awsecr.Options)) (*awsecr.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK ECR pagination into scanner-owned ECR records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an ECR SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsecr.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListRepositories returns all ECR repositories visible to the configured AWS
// credentials.
func (c *Client) ListRepositories(ctx context.Context) ([]ecrservice.Repository, error) {
	paginator := awsecr.NewDescribeRepositoriesPaginator(c.client, &awsecr.DescribeRepositoriesInput{})
	var repositories []ecrservice.Repository
	for paginator.HasMorePages() {
		var page *awsecr.DescribeRepositoriesOutput
		err := c.recordAPICall(ctx, "DescribeRepositories", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, repository := range page.Repositories {
			tags, err := c.listTagsForRepository(ctx, aws.ToString(repository.RepositoryArn))
			if err != nil {
				return nil, err
			}
			repositories = append(repositories, mapRepository(repository, tags))
		}
	}
	return repositories, nil
}

// ListImages returns all image details for one ECR repository.
func (c *Client) ListImages(ctx context.Context, repository ecrservice.Repository) ([]ecrservice.Image, error) {
	input := &awsecr.DescribeImagesInput{
		RepositoryName: aws.String(repository.Name),
	}
	if strings.TrimSpace(repository.RegistryID) != "" {
		input.RegistryId = aws.String(repository.RegistryID)
	}
	paginator := awsecr.NewDescribeImagesPaginator(c.client, input)
	var images []ecrservice.Image
	for paginator.HasMorePages() {
		var page *awsecr.DescribeImagesOutput
		err := c.recordAPICall(ctx, "DescribeImages", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, image := range page.ImageDetails {
			images = append(images, mapImageDetail(repository.ARN, image))
		}
	}
	return images, nil
}

// GetLifecyclePolicy returns the lifecycle policy for one repository. A
// missing lifecycle policy is a valid empty result.
func (c *Client) GetLifecyclePolicy(
	ctx context.Context,
	repository ecrservice.Repository,
) (*ecrservice.LifecyclePolicy, error) {
	input := &awsecr.GetLifecyclePolicyInput{
		RepositoryName: aws.String(repository.Name),
	}
	if strings.TrimSpace(repository.RegistryID) != "" {
		input.RegistryId = aws.String(repository.RegistryID)
	}
	var output *awsecr.GetLifecyclePolicyOutput
	err := c.recordAPICall(ctx, "GetLifecyclePolicy", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetLifecyclePolicy(callCtx, input)
		return err
	})
	if isLifecyclePolicyNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return &ecrservice.LifecyclePolicy{
		RepositoryARN:   repository.ARN,
		RepositoryName:  firstNonEmpty(aws.ToString(output.RepositoryName), repository.Name),
		RegistryID:      firstNonEmpty(aws.ToString(output.RegistryId), repository.RegistryID),
		PolicyText:      aws.ToString(output.LifecyclePolicyText),
		LastEvaluatedAt: aws.ToTime(output.LastEvaluatedAt),
	}, nil
}

func (c *Client) listTagsForRepository(ctx context.Context, repositoryARN string) ([]awsecrtypes.Tag, error) {
	if strings.TrimSpace(repositoryARN) == "" {
		return nil, nil
	}
	var output *awsecr.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsecr.ListTagsForResourceInput{
			ResourceArn: aws.String(repositoryARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Tags, nil
}

func mapRepository(repository awsecrtypes.Repository, tags []awsecrtypes.Tag) ecrservice.Repository {
	value := ecrservice.Repository{
		ARN:                aws.ToString(repository.RepositoryArn),
		Name:               aws.ToString(repository.RepositoryName),
		URI:                aws.ToString(repository.RepositoryUri),
		RegistryID:         aws.ToString(repository.RegistryId),
		ImageTagMutability: string(repository.ImageTagMutability),
		CreatedAt:          aws.ToTime(repository.CreatedAt),
		Tags:               mapTags(tags),
	}
	if repository.EncryptionConfiguration != nil {
		value.EncryptionType = string(repository.EncryptionConfiguration.EncryptionType)
		value.KMSKey = aws.ToString(repository.EncryptionConfiguration.KmsKey)
	}
	if repository.ImageScanningConfiguration != nil {
		value.ScanOnPush = repository.ImageScanningConfiguration.ScanOnPush
	}
	return value
}

func mapImageDetail(repositoryARN string, image awsecrtypes.ImageDetail) ecrservice.Image {
	digest := aws.ToString(image.ImageDigest)
	return ecrservice.Image{
		RepositoryARN:     repositoryARN,
		RepositoryName:    aws.ToString(image.RepositoryName),
		RegistryID:        aws.ToString(image.RegistryId),
		ImageDigest:       digest,
		ManifestDigest:    digest,
		Tags:              cloneStrings(image.ImageTags),
		PushedAt:          aws.ToTime(image.ImagePushedAt),
		ImageSizeInBytes:  aws.ToInt64(image.ImageSizeInBytes),
		ManifestMediaType: aws.ToString(image.ImageManifestMediaType),
		ArtifactMediaType: aws.ToString(image.ArtifactMediaType),
	}
}

func mapTags(tags []awsecrtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
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
	if c.instruments != nil {
		attrs := metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		)
		c.instruments.AWSAPICalls.Add(ctx, 1, attrs)
		if isThrottleError(err) {
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

func isLifecyclePolicyNotFound(err error) bool {
	var notFound *awsecrtypes.LifecyclePolicyNotFoundException
	return errors.As(err, &notFound)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var _ ecrservice.Client = (*Client)(nil)

var _ apiClient = (*awsecr.Client)(nil)
