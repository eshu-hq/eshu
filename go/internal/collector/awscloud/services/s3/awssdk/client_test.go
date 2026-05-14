package awssdk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awss3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

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
	if got, want := api.listBucketsMaxBuckets[0], int32(10000); got != want {
		t.Fatalf("ListBuckets MaxBuckets = %d, want %d", got, want)
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

func TestClientListBucketsUsesMaxBucketsAndContinuationToken(t *testing.T) {
	api := &fakeS3API{
		listBucketsPages: []*awss3.ListBucketsOutput{{
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("orders-artifacts"),
				BucketRegion: aws.String("us-east-1"),
			}},
			ContinuationToken: aws.String("next-page"),
		}, {
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("orders-logs"),
				BucketRegion: aws.String("us-east-1"),
			}},
		}},
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
	if got, want := len(buckets), 2; got != want {
		t.Fatalf("len(buckets) = %d, want %d", got, want)
	}
	if got, want := api.listBucketsMaxBuckets, []int32{10000, 10000}; !int32SlicesEqual(got, want) {
		t.Fatalf("ListBuckets MaxBuckets = %#v, want %#v", got, want)
	}
	if got, want := api.listBucketsContinuationTokens, []string{"", "next-page"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListBuckets ContinuationToken = %#v, want %#v", got, want)
	}
}

func TestClientListBucketsRejectsGlobalS3Boundary(t *testing.T) {
	api := &fakeS3API{}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "aws-global",
			ServiceKind: awscloud.ServiceS3,
		},
	}

	_, err := adapter.ListBuckets(context.Background())
	if err == nil {
		t.Fatalf("ListBuckets() error = nil, want global boundary rejection")
	}
	if !strings.Contains(err.Error(), "requires a regional boundary") {
		t.Fatalf("ListBuckets() error = %v, want regional boundary message", err)
	}
	if api.listBucketsCalls != 0 {
		t.Fatalf("ListBuckets calls = %d, want 0 for rejected global boundary", api.listBucketsCalls)
	}
}

func TestClientListBucketsTreatsMissingOptionalBucketConfigAsEmptyMetadata(t *testing.T) {
	api := &fakeS3API{
		listBucketsPages: []*awss3.ListBucketsOutput{{
			Buckets: []awss3types.Bucket{{
				Name:         aws.String("orders-artifacts"),
				BucketRegion: aws.String("us-east-1"),
			}},
		}},
		taggingErr:           apiError("NoSuchTagSet"),
		encryptionErr:        apiError("ServerSideEncryptionConfigurationNotFoundError"),
		publicAccessBlockErr: apiError("NoSuchPublicAccessBlockConfiguration"),
		policyStatusErr:      apiError("NoSuchBucketPolicy"),
		ownershipErr:         apiError("OwnershipControlsNotFoundError"),
		websiteErr:           apiError("NoSuchWebsiteConfiguration"),
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
		t.Fatalf("ListBuckets() error = %v, want nil for missing optional config", err)
	}
	if got, want := len(buckets), 1; got != want {
		t.Fatalf("len(buckets) = %d, want %d", got, want)
	}
	bucket := buckets[0]
	if len(bucket.Tags) != 0 {
		t.Fatalf("Tags = %#v, want empty", bucket.Tags)
	}
	if len(bucket.Encryption.Rules) != 0 {
		t.Fatalf("Encryption.Rules = %#v, want empty", bucket.Encryption.Rules)
	}
	if bucket.PolicyIsPublic != nil {
		t.Fatalf("PolicyIsPublic = %#v, want nil", bucket.PolicyIsPublic)
	}
	if len(bucket.OwnershipControls) != 0 {
		t.Fatalf("OwnershipControls = %#v, want empty", bucket.OwnershipControls)
	}
	if bucket.Website.Enabled {
		t.Fatalf("Website.Enabled = true, want false")
	}
}

func TestIsOptionalMissingS3ConfigRecognizesExpectedCodes(t *testing.T) {
	for _, code := range []string{
		"NoSuchTagSet",
		"ServerSideEncryptionConfigurationNotFoundError",
		"NoSuchPublicAccessBlockConfiguration",
		"NoSuchBucketPolicy",
		"OwnershipControlsNotFoundError",
		"NoSuchWebsiteConfiguration",
	} {
		if !isOptionalMissingS3Config(apiError(code), code) {
			t.Fatalf("isOptionalMissingS3Config(%q) = false, want true", code)
		}
	}
	if isOptionalMissingS3Config(apiError("AccessDenied"), "NoSuchTagSet") {
		t.Fatalf("isOptionalMissingS3Config(AccessDenied) = true, want false")
	}
}

type fakeS3API struct {
	listBucketsPages              []*awss3.ListBucketsOutput
	listBucketsCalls              int
	listBucketsRegion             string
	listBucketsMaxBuckets         []int32
	listBucketsContinuationTokens []string
	tagging                       *awss3.GetBucketTaggingOutput
	taggingErr                    error
	versioning                    *awss3.GetBucketVersioningOutput
	versioningErr                 error
	encryption                    *awss3.GetBucketEncryptionOutput
	encryptionErr                 error
	publicAccessBlock             *awss3.GetPublicAccessBlockOutput
	publicAccessBlockErr          error
	policyStatus                  *awss3.GetBucketPolicyStatusOutput
	policyStatusErr               error
	ownership                     *awss3.GetBucketOwnershipControlsOutput
	ownershipErr                  error
	website                       *awss3.GetBucketWebsiteOutput
	websiteErr                    error
	logging                       *awss3.GetBucketLoggingOutput
	loggingErr                    error
}

func (f *fakeS3API) ListBuckets(
	_ context.Context,
	input *awss3.ListBucketsInput,
	_ ...func(*awss3.Options),
) (*awss3.ListBucketsOutput, error) {
	f.listBucketsRegion = aws.ToString(input.BucketRegion)
	f.listBucketsMaxBuckets = append(f.listBucketsMaxBuckets, aws.ToInt32(input.MaxBuckets))
	f.listBucketsContinuationTokens = append(f.listBucketsContinuationTokens, aws.ToString(input.ContinuationToken))
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
	if f.taggingErr != nil {
		return nil, f.taggingErr
	}
	return f.tagging, nil
}

func (f *fakeS3API) GetBucketVersioning(
	context.Context,
	*awss3.GetBucketVersioningInput,
	...func(*awss3.Options),
) (*awss3.GetBucketVersioningOutput, error) {
	if f.versioningErr != nil {
		return nil, f.versioningErr
	}
	return f.versioning, nil
}

func (f *fakeS3API) GetBucketEncryption(
	context.Context,
	*awss3.GetBucketEncryptionInput,
	...func(*awss3.Options),
) (*awss3.GetBucketEncryptionOutput, error) {
	if f.encryptionErr != nil {
		return nil, f.encryptionErr
	}
	return f.encryption, nil
}

func (f *fakeS3API) GetPublicAccessBlock(
	context.Context,
	*awss3.GetPublicAccessBlockInput,
	...func(*awss3.Options),
) (*awss3.GetPublicAccessBlockOutput, error) {
	if f.publicAccessBlockErr != nil {
		return nil, f.publicAccessBlockErr
	}
	return f.publicAccessBlock, nil
}

func (f *fakeS3API) GetBucketPolicyStatus(
	context.Context,
	*awss3.GetBucketPolicyStatusInput,
	...func(*awss3.Options),
) (*awss3.GetBucketPolicyStatusOutput, error) {
	if f.policyStatusErr != nil {
		return nil, f.policyStatusErr
	}
	return f.policyStatus, nil
}

func (f *fakeS3API) GetBucketOwnershipControls(
	context.Context,
	*awss3.GetBucketOwnershipControlsInput,
	...func(*awss3.Options),
) (*awss3.GetBucketOwnershipControlsOutput, error) {
	if f.ownershipErr != nil {
		return nil, f.ownershipErr
	}
	return f.ownership, nil
}

func (f *fakeS3API) GetBucketWebsite(
	context.Context,
	*awss3.GetBucketWebsiteInput,
	...func(*awss3.Options),
) (*awss3.GetBucketWebsiteOutput, error) {
	if f.websiteErr != nil {
		return nil, f.websiteErr
	}
	return f.website, nil
}

func (f *fakeS3API) GetBucketLogging(
	context.Context,
	*awss3.GetBucketLoggingInput,
	...func(*awss3.Options),
) (*awss3.GetBucketLoggingOutput, error) {
	if f.loggingErr != nil {
		return nil, f.loggingErr
	}
	return f.logging, nil
}

var _ apiClient = (*fakeS3API)(nil)

func apiError(code string) error {
	return &smithy.GenericAPIError{
		Code:    code,
		Message: code,
		Fault:   smithy.FaultClient,
	}
}

func int32SlicesEqual(got []int32, want []int32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func stringSlicesEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
