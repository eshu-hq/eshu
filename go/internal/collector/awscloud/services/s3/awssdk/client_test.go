package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListBucketsReadsSafeMetadataOnly(t *testing.T) {
	created := time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC)
	api := &fakeS3API{
		listBucketsPages: []*awss3.ListBucketsOutput{{
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("orders-artifacts"),
				BucketRegion: aws.String("us-east-1"),
				CreationDate: aws.Time(created),
			}},
		}},
		tagging: &awss3.GetBucketTaggingOutput{
			TagSet: []awss3types.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
		versioning: &awss3.GetBucketVersioningOutput{
			Status:    awss3types.BucketVersioningStatusEnabled,
			MFADelete: awss3types.MFADeleteStatusDisabled,
		},
		encryption: &awss3.GetBucketEncryptionOutput{
			ServerSideEncryptionConfiguration: &awss3types.ServerSideEncryptionConfiguration{
				Rules: []awss3types.ServerSideEncryptionRule{{
					ApplyServerSideEncryptionByDefault: &awss3types.ServerSideEncryptionByDefault{
						SSEAlgorithm:   awss3types.ServerSideEncryptionAwsKms,
						KMSMasterKeyID: aws.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
					},
					BucketKeyEnabled: aws.Bool(true),
				}},
			},
		},
		publicAccessBlock: &awss3.GetPublicAccessBlockOutput{
			PublicAccessBlockConfiguration: &awss3types.PublicAccessBlockConfiguration{
				BlockPublicAcls:       aws.Bool(true),
				IgnorePublicAcls:      aws.Bool(true),
				BlockPublicPolicy:     aws.Bool(true),
				RestrictPublicBuckets: aws.Bool(true),
			},
		},
		policyStatus: &awss3.GetBucketPolicyStatusOutput{
			PolicyStatus: &awss3types.PolicyStatus{IsPublic: aws.Bool(false)},
		},
		ownership: &awss3.GetBucketOwnershipControlsOutput{
			OwnershipControls: &awss3types.OwnershipControls{
				Rules: []awss3types.OwnershipControlsRule{{
					ObjectOwnership: awss3types.ObjectOwnershipBucketOwnerEnforced,
				}},
			},
		},
		website: &awss3.GetBucketWebsiteOutput{
			IndexDocument:         &awss3types.IndexDocument{Suffix: aws.String("index.html")},
			ErrorDocument:         &awss3types.ErrorDocument{Key: aws.String("404.html")},
			RedirectAllRequestsTo: &awss3types.RedirectAllRequestsTo{HostName: aws.String("assets.example.com")},
			RoutingRules:          []awss3types.RoutingRule{{}},
		},
		logging: &awss3.GetBucketLoggingOutput{
			LoggingEnabled: &awss3types.LoggingEnabled{
				TargetBucket: aws.String("orders-logs"),
				TargetPrefix: aws.String("s3/"),
				TargetGrants: []awss3types.TargetGrant{{
					Grantee: &awss3types.Grantee{DisplayName: aws.String("log-reader")},
				}},
			},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceS3,
		},
	}

	buckets, err := adapter.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets() error = %v, want nil", err)
	}
	if got, want := len(buckets), 1; got != want {
		t.Fatalf("len(buckets) = %d, want %d", got, want)
	}
	if api.listBucketsRegion != "us-east-1" {
		t.Fatalf("ListBuckets BucketRegion = %q, want us-east-1", api.listBucketsRegion)
	}
	bucket := buckets[0]
	if bucket.Name != "orders-artifacts" {
		t.Fatalf("Name = %q, want orders-artifacts", bucket.Name)
	}
	if bucket.ARN != "arn:aws:s3:::orders-artifacts" {
		t.Fatalf("ARN = %q, want arn:aws:s3:::orders-artifacts", bucket.ARN)
	}
	if bucket.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", bucket.Tags)
	}
	if bucket.Versioning.Status != "Enabled" {
		t.Fatalf("Versioning.Status = %q, want Enabled", bucket.Versioning.Status)
	}
	if got := bucket.Encryption.Rules[0].Algorithm; got != "aws:kms" {
		t.Fatalf("encryption algorithm = %q, want aws:kms", got)
	}
	if !bucket.Encryption.Rules[0].BucketKey {
		t.Fatalf("BucketKey = false, want true")
	}
	if bucket.PolicyIsPublic == nil || *bucket.PolicyIsPublic {
		t.Fatalf("PolicyIsPublic = %#v, want false pointer", bucket.PolicyIsPublic)
	}
	if got, want := bucket.OwnershipControls[0], "BucketOwnerEnforced"; got != want {
		t.Fatalf("OwnershipControls[0] = %q, want %q", got, want)
	}
	if !bucket.Website.Enabled || !bucket.Website.HasIndexDocument || !bucket.Website.HasErrorDocument {
		t.Fatalf("Website = %#v, want enabled with index and error document flags", bucket.Website)
	}
	if got, want := bucket.Website.RedirectAllRequestsTo, "assets.example.com"; got != want {
		t.Fatalf("RedirectAllRequestsTo = %q, want %q", got, want)
	}
	if got, want := bucket.Logging.TargetBucket, "orders-logs"; got != want {
		t.Fatalf("Logging.TargetBucket = %q, want %q", got, want)
	}
}

type fakeS3API struct {
	listBucketsPages  []*awss3.ListBucketsOutput
	listBucketsCalls  int
	listBucketsRegion string
	tagging           *awss3.GetBucketTaggingOutput
	versioning        *awss3.GetBucketVersioningOutput
	encryption        *awss3.GetBucketEncryptionOutput
	publicAccessBlock *awss3.GetPublicAccessBlockOutput
	policyStatus      *awss3.GetBucketPolicyStatusOutput
	ownership         *awss3.GetBucketOwnershipControlsOutput
	website           *awss3.GetBucketWebsiteOutput
	logging           *awss3.GetBucketLoggingOutput
}

func (f *fakeS3API) ListBuckets(
	_ context.Context,
	input *awss3.ListBucketsInput,
	_ ...func(*awss3.Options),
) (*awss3.ListBucketsOutput, error) {
	f.listBucketsRegion = aws.ToString(input.BucketRegion)
	if f.listBucketsCalls >= len(f.listBucketsPages) {
		return &awss3.ListBucketsOutput{}, nil
	}
	page := f.listBucketsPages[f.listBucketsCalls]
	f.listBucketsCalls++
	return page, nil
}

func (f *fakeS3API) HeadBucket(
	context.Context,
	*awss3.HeadBucketInput,
	...func(*awss3.Options),
) (*awss3.HeadBucketOutput, error) {
	return &awss3.HeadBucketOutput{BucketRegion: aws.String("us-east-1")}, nil
}

func (f *fakeS3API) GetBucketTagging(
	context.Context,
	*awss3.GetBucketTaggingInput,
	...func(*awss3.Options),
) (*awss3.GetBucketTaggingOutput, error) {
	return f.tagging, nil
}

func (f *fakeS3API) GetBucketVersioning(
	context.Context,
	*awss3.GetBucketVersioningInput,
	...func(*awss3.Options),
) (*awss3.GetBucketVersioningOutput, error) {
	return f.versioning, nil
}

func (f *fakeS3API) GetBucketEncryption(
	context.Context,
	*awss3.GetBucketEncryptionInput,
	...func(*awss3.Options),
) (*awss3.GetBucketEncryptionOutput, error) {
	return f.encryption, nil
}

func (f *fakeS3API) GetPublicAccessBlock(
	context.Context,
	*awss3.GetPublicAccessBlockInput,
	...func(*awss3.Options),
) (*awss3.GetPublicAccessBlockOutput, error) {
	return f.publicAccessBlock, nil
}

func (f *fakeS3API) GetBucketPolicyStatus(
	context.Context,
	*awss3.GetBucketPolicyStatusInput,
	...func(*awss3.Options),
) (*awss3.GetBucketPolicyStatusOutput, error) {
	return f.policyStatus, nil
}

func (f *fakeS3API) GetBucketOwnershipControls(
	context.Context,
	*awss3.GetBucketOwnershipControlsInput,
	...func(*awss3.Options),
) (*awss3.GetBucketOwnershipControlsOutput, error) {
	return f.ownership, nil
}

func (f *fakeS3API) GetBucketWebsite(
	context.Context,
	*awss3.GetBucketWebsiteInput,
	...func(*awss3.Options),
) (*awss3.GetBucketWebsiteOutput, error) {
	return f.website, nil
}

func (f *fakeS3API) GetBucketLogging(
	context.Context,
	*awss3.GetBucketLoggingInput,
	...func(*awss3.Options),
) (*awss3.GetBucketLoggingOutput, error) {
	return f.logging, nil
}

var _ apiClient = (*fakeS3API)(nil)
