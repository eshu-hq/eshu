package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	s3service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	ListBuckets(context.Context, *awss3.ListBucketsInput, ...func(*awss3.Options)) (*awss3.ListBucketsOutput, error)
	HeadBucket(context.Context, *awss3.HeadBucketInput, ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error)
	GetBucketTagging(context.Context, *awss3.GetBucketTaggingInput, ...func(*awss3.Options)) (*awss3.GetBucketTaggingOutput, error)
	GetBucketVersioning(context.Context, *awss3.GetBucketVersioningInput, ...func(*awss3.Options)) (*awss3.GetBucketVersioningOutput, error)
	GetBucketEncryption(context.Context, *awss3.GetBucketEncryptionInput, ...func(*awss3.Options)) (*awss3.GetBucketEncryptionOutput, error)
	GetPublicAccessBlock(context.Context, *awss3.GetPublicAccessBlockInput, ...func(*awss3.Options)) (*awss3.GetPublicAccessBlockOutput, error)
	GetBucketPolicyStatus(context.Context, *awss3.GetBucketPolicyStatusInput, ...func(*awss3.Options)) (*awss3.GetBucketPolicyStatusOutput, error)
	GetBucketOwnershipControls(context.Context, *awss3.GetBucketOwnershipControlsInput, ...func(*awss3.Options)) (*awss3.GetBucketOwnershipControlsOutput, error)
	GetBucketWebsite(context.Context, *awss3.GetBucketWebsiteInput, ...func(*awss3.Options)) (*awss3.GetBucketWebsiteOutput, error)
	GetBucketLogging(context.Context, *awss3.GetBucketLoggingInput, ...func(*awss3.Options)) (*awss3.GetBucketLoggingOutput, error)
}

// Client adapts AWS SDK S3 bucket-control-plane calls into scanner-owned
// metadata. It never calls object inventory, bucket policy, ACL, notification,
// replication, lifecycle, inventory, analytics, metrics, or mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an S3 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awss3.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListBuckets returns S3 bucket metadata visible to the configured AWS
// credentials. Optional missing bucket-control-plane configurations are mapped
// to empty metadata so an unconfigured bucket does not fail the scan.
func (c *Client) ListBuckets(ctx context.Context) ([]s3service.Bucket, error) {
	var buckets []s3service.Bucket
	var continuationToken *string
	for {
		var page *awss3.ListBucketsOutput
		err := c.recordAPICall(ctx, "ListBuckets", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListBuckets(callCtx, &awss3.ListBucketsInput{
				BucketRegion:      bucketRegion(c.boundary.Region),
				ContinuationToken: continuationToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return buckets, nil
		}
		for _, listed := range page.Buckets {
			if aws.ToString(listed.Name) == "" {
				continue
			}
			bucket, err := c.bucketMetadata(ctx, listed)
			if err != nil {
				return nil, err
			}
			buckets = append(buckets, bucket)
		}
		if page.ContinuationToken == nil || aws.ToString(page.ContinuationToken) == "" {
			break
		}
		continuationToken = page.ContinuationToken
	}
	return buckets, nil
}

func bucketRegion(region string) *string {
	region = strings.TrimSpace(region)
	if region == "" || region == "aws-global" {
		return nil
	}
	return aws.String(region)
}

func (c *Client) bucketMetadata(ctx context.Context, listed awss3types.Bucket) (s3service.Bucket, error) {
	name := aws.ToString(listed.Name)
	head, err := c.headBucket(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	tags, err := c.getBucketTagging(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	versioning, err := c.getBucketVersioning(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	encryption, err := c.getBucketEncryption(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	publicAccessBlock, err := c.getPublicAccessBlock(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	policyIsPublic, err := c.getBucketPolicyStatus(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	ownership, err := c.getBucketOwnershipControls(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	website, err := c.getBucketWebsite(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	logging, err := c.getBucketLogging(ctx, name)
	if err != nil {
		return s3service.Bucket{}, err
	}
	return s3service.Bucket{
		ARN:               bucketARN(name),
		Name:              name,
		Region:            firstNonEmpty(aws.ToString(listed.BucketRegion), aws.ToString(head.BucketRegion), c.boundary.Region),
		CreationTime:      aws.ToTime(listed.CreationDate),
		Tags:              tags,
		Versioning:        versioning,
		Encryption:        encryption,
		PublicAccessBlock: publicAccessBlock,
		PolicyIsPublic:    policyIsPublic,
		OwnershipControls: ownership,
		Website:           website,
		Logging:           logging,
	}, nil
}

func (c *Client) headBucket(ctx context.Context, name string) (*awss3.HeadBucketOutput, error) {
	var output *awss3.HeadBucketOutput
	err := c.recordAPICall(ctx, "HeadBucket", func(callCtx context.Context) error {
		var err error
		output, err = c.client.HeadBucket(callCtx, &awss3.HeadBucketInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awss3.HeadBucketOutput{}, nil
	}
	return output, nil
}

func (c *Client) getBucketTagging(ctx context.Context, name string) (map[string]string, error) {
	var output *awss3.GetBucketTaggingOutput
	err := c.recordAPICall(ctx, "GetBucketTagging", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketTagging(callCtx, &awss3.GetBucketTaggingInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "NoSuchTagSet") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	tags := make(map[string]string, len(output.TagSet))
	for _, tag := range output.TagSet {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func (c *Client) getBucketVersioning(ctx context.Context, name string) (s3service.Versioning, error) {
	var output *awss3.GetBucketVersioningOutput
	err := c.recordAPICall(ctx, "GetBucketVersioning", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketVersioning(callCtx, &awss3.GetBucketVersioningInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		return err
	})
	if err != nil || output == nil {
		return s3service.Versioning{}, err
	}
	return s3service.Versioning{
		Status:    string(output.Status),
		MFADelete: string(output.MFADelete),
	}, nil
}

func (c *Client) getBucketEncryption(ctx context.Context, name string) (s3service.Encryption, error) {
	var output *awss3.GetBucketEncryptionOutput
	err := c.recordAPICall(ctx, "GetBucketEncryption", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketEncryption(callCtx, &awss3.GetBucketEncryptionInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "ServerSideEncryptionConfigurationNotFoundError") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil || output.ServerSideEncryptionConfiguration == nil {
		return s3service.Encryption{}, err
	}
	var rules []s3service.EncryptionRule
	for _, rule := range output.ServerSideEncryptionConfiguration.Rules {
		byDefault := rule.ApplyServerSideEncryptionByDefault
		if byDefault == nil {
			continue
		}
		rules = append(rules, s3service.EncryptionRule{
			Algorithm:      string(byDefault.SSEAlgorithm),
			KMSMasterKeyID: aws.ToString(byDefault.KMSMasterKeyID),
			BucketKey:      aws.ToBool(rule.BucketKeyEnabled),
		})
	}
	return s3service.Encryption{Rules: rules}, nil
}

func (c *Client) getPublicAccessBlock(ctx context.Context, name string) (s3service.PublicAccessBlock, error) {
	var output *awss3.GetPublicAccessBlockOutput
	err := c.recordAPICall(ctx, "GetPublicAccessBlock", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetPublicAccessBlock(callCtx, &awss3.GetPublicAccessBlockInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "NoSuchPublicAccessBlockConfiguration") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil || output.PublicAccessBlockConfiguration == nil {
		return s3service.PublicAccessBlock{}, err
	}
	config := output.PublicAccessBlockConfiguration
	return s3service.PublicAccessBlock{
		BlockPublicACLs:       cloneBool(config.BlockPublicAcls),
		IgnorePublicACLs:      cloneBool(config.IgnorePublicAcls),
		BlockPublicPolicy:     cloneBool(config.BlockPublicPolicy),
		RestrictPublicBuckets: cloneBool(config.RestrictPublicBuckets),
	}, nil
}

func (c *Client) getBucketPolicyStatus(ctx context.Context, name string) (*bool, error) {
	var output *awss3.GetBucketPolicyStatusOutput
	err := c.recordAPICall(ctx, "GetBucketPolicyStatus", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketPolicyStatus(callCtx, &awss3.GetBucketPolicyStatusInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "NoSuchBucketPolicy") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil || output.PolicyStatus == nil {
		return nil, err
	}
	return cloneBool(output.PolicyStatus.IsPublic), nil
}

func (c *Client) getBucketOwnershipControls(ctx context.Context, name string) ([]string, error) {
	var output *awss3.GetBucketOwnershipControlsOutput
	err := c.recordAPICall(ctx, "GetBucketOwnershipControls", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketOwnershipControls(callCtx, &awss3.GetBucketOwnershipControlsInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "OwnershipControlsNotFoundError") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil || output.OwnershipControls == nil {
		return nil, err
	}
	var controls []string
	for _, rule := range output.OwnershipControls.Rules {
		if ownership := strings.TrimSpace(string(rule.ObjectOwnership)); ownership != "" {
			controls = append(controls, ownership)
		}
	}
	return controls, nil
}

func (c *Client) getBucketWebsite(ctx context.Context, name string) (s3service.Website, error) {
	var output *awss3.GetBucketWebsiteOutput
	err := c.recordAPICall(ctx, "GetBucketWebsite", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketWebsite(callCtx, &awss3.GetBucketWebsiteInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		if isOptionalMissingS3Config(err, "NoSuchWebsiteConfiguration") {
			output = nil
			return nil
		}
		return err
	})
	if err != nil || output == nil {
		return s3service.Website{}, err
	}
	return s3service.Website{
		Enabled:               true,
		HasIndexDocument:      output.IndexDocument != nil,
		HasErrorDocument:      output.ErrorDocument != nil,
		RedirectAllRequestsTo: redirectHost(output.RedirectAllRequestsTo),
		RoutingRuleCount:      len(output.RoutingRules),
	}, nil
}

func (c *Client) getBucketLogging(ctx context.Context, name string) (s3service.Logging, error) {
	var output *awss3.GetBucketLoggingOutput
	err := c.recordAPICall(ctx, "GetBucketLogging", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetBucketLogging(callCtx, &awss3.GetBucketLoggingInput{
			Bucket:              aws.String(name),
			ExpectedBucketOwner: expectedBucketOwner(c.boundary.AccountID),
		})
		return err
	})
	if err != nil || output == nil || output.LoggingEnabled == nil {
		return s3service.Logging{}, err
	}
	return s3service.Logging{
		Enabled:      true,
		TargetBucket: aws.ToString(output.LoggingEnabled.TargetBucket),
		TargetPrefix: aws.ToString(output.LoggingEnabled.TargetPrefix),
	}, nil
}

func redirectHost(redirect *awss3types.RedirectAllRequestsTo) string {
	if redirect == nil {
		return ""
	}
	return aws.ToString(redirect.HostName)
}

func bucketARN(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:aws:s3:::" + name
}

func expectedBucketOwner(accountID string) *string {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil
	}
	return aws.String(accountID)
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func isOptionalMissingS3Config(err error, codes ...string) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.ErrorCode() == code {
			return true
		}
	}
	return false
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

var _ s3service.Client = (*Client)(nil)

var _ apiClient = (*awss3.Client)(nil)
