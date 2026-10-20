// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssecuritylake "github.com/aws/aws-sdk-go-v2/service/securitylake"
	awssecuritylaketypes "github.com/aws/aws-sdk-go-v2/service/securitylake/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsSecurityLakeMetadataOnly(t *testing.T) {
	dataLakeARN := "arn:aws:securitylake:us-east-1:123456789012:data-lake/default"
	bucketARN := "arn:aws:s3:::aws-security-data-lake-us-east-1-abcdef"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"
	subscriberARN := "arn:aws:securitylake:us-east-1:123456789012:subscriber/abc"
	roleARN := "arn:aws:iam::123456789012:role/AmazonSecurityLake-subscriber"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeSecurityLakeAPI{
		dataLakes: &awssecuritylake.ListDataLakesOutput{
			DataLakes: []awssecuritylaketypes.DataLakeResource{{
				DataLakeArn:  aws.String(dataLakeARN),
				Region:       aws.String("us-east-1"),
				S3BucketArn:  aws.String(bucketARN),
				CreateStatus: awssecuritylaketypes.DataLakeStatusCompleted,
				EncryptionConfiguration: &awssecuritylaketypes.DataLakeEncryptionConfiguration{
					KmsKeyId: aws.String(kmsARN),
				},
				LifecycleConfiguration: &awssecuritylaketypes.DataLakeLifecycleConfiguration{
					Expiration: &awssecuritylaketypes.DataLakeLifecycleExpiration{Days: aws.Int32(365)},
					Transitions: []awssecuritylaketypes.DataLakeLifecycleTransition{
						{Days: aws.Int32(30)}, {Days: aws.Int32(90)},
					},
				},
				ReplicationConfiguration: &awssecuritylaketypes.DataLakeReplicationConfiguration{
					Regions: []string{"us-west-2"},
				},
			}},
		},
		logSourcePages: []*awssecuritylake.ListLogSourcesOutput{{
			Sources: []awssecuritylaketypes.LogSource{{
				Account: aws.String("123456789012"),
				Region:  aws.String("us-east-1"),
				Sources: []awssecuritylaketypes.LogSourceResource{
					&awssecuritylaketypes.LogSourceResourceMemberAwsLogSource{
						Value: awssecuritylaketypes.AwsLogSourceResource{
							SourceName:    awssecuritylaketypes.AwsLogSourceName("ROUTE53"),
							SourceVersion: aws.String("1.0"),
						},
					},
					&awssecuritylaketypes.LogSourceResourceMemberCustomLogSource{
						Value: awssecuritylaketypes.CustomLogSourceResource{
							SourceName: aws.String("MyCustomSource"),
							Provider: &awssecuritylaketypes.CustomLogSourceProvider{
								RoleArn: aws.String("arn:aws:iam::123456789012:role/SecurityLakeCustom"),
							},
						},
					},
				},
			}},
		}},
		subscriberPages: []*awssecuritylake.ListSubscribersOutput{{
			Subscribers: []awssecuritylaketypes.SubscriberResource{{
				SubscriberArn:    aws.String(subscriberARN),
				SubscriberId:     aws.String("abc"),
				SubscriberName:   aws.String("analytics"),
				SubscriberStatus: awssecuritylaketypes.SubscriberStatusActive,
				AccessTypes:      []awssecuritylaketypes.AccessType{awssecuritylaketypes.AccessTypeS3},
				RoleArn:          aws.String(roleARN),
				S3BucketArn:      aws.String("arn:aws:s3:::subscriber-bucket"),
				CreatedAt:        aws.Time(createdAt),
				SubscriberIdentity: &awssecuritylaketypes.AwsIdentity{
					// ExternalId MUST NOT be persisted; Principal is identity metadata.
					ExternalId: aws.String("super-secret-external-id"),
					Principal:  aws.String("210987654321"),
				},
				// SubscriberEndpoint MUST NOT be persisted.
				SubscriberEndpoint: aws.String("https://private.endpoint.example"),
			}},
		}},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.DataLakes) != 1 {
		t.Fatalf("len(DataLakes) = %d, want 1", len(snapshot.DataLakes))
	}
	lake := snapshot.DataLakes[0]
	if lake.ARN != dataLakeARN {
		t.Fatalf("data lake ARN = %q, want %q", lake.ARN, dataLakeARN)
	}
	if lake.S3BucketARN != bucketARN {
		t.Fatalf("data lake S3BucketARN = %q, want %q", lake.S3BucketARN, bucketARN)
	}
	if lake.KMSKeyID != kmsARN {
		t.Fatalf("data lake KMSKeyID = %q, want %q", lake.KMSKeyID, kmsARN)
	}
	if lake.CreateStatus != "COMPLETED" {
		t.Fatalf("data lake CreateStatus = %q, want COMPLETED", lake.CreateStatus)
	}
	if lake.ExpirationDays != 365 {
		t.Fatalf("data lake ExpirationDays = %d, want 365", lake.ExpirationDays)
	}
	if lake.TransitionCount != 2 {
		t.Fatalf("data lake TransitionCount = %d, want 2", lake.TransitionCount)
	}
	if len(lake.ReplicationRegions) != 1 || lake.ReplicationRegions[0] != "us-west-2" {
		t.Fatalf("data lake ReplicationRegions = %#v, want [us-west-2]", lake.ReplicationRegions)
	}

	if len(snapshot.LogSources) != 2 {
		t.Fatalf("len(LogSources) = %d, want 2", len(snapshot.LogSources))
	}
	var sawAWS, sawCustom bool
	for _, source := range snapshot.LogSources {
		switch source.SourceName {
		case "ROUTE53":
			sawAWS = true
			if source.Custom {
				t.Fatalf("ROUTE53 marked custom")
			}
		case "MyCustomSource":
			sawCustom = true
			if !source.Custom {
				t.Fatalf("MyCustomSource not marked custom")
			}
			if source.ProviderRoleARN != "arn:aws:iam::123456789012:role/SecurityLakeCustom" {
				t.Fatalf("custom source ProviderRoleARN = %q", source.ProviderRoleARN)
			}
		}
	}
	if !sawAWS || !sawCustom {
		t.Fatalf("missing expected log sources: aws=%v custom=%v", sawAWS, sawCustom)
	}

	if len(snapshot.Subscribers) != 1 {
		t.Fatalf("len(Subscribers) = %d, want 1", len(snapshot.Subscribers))
	}
	subscriber := snapshot.Subscribers[0]
	if subscriber.ARN != subscriberARN {
		t.Fatalf("subscriber ARN = %q, want %q", subscriber.ARN, subscriberARN)
	}
	if subscriber.RoleARN != roleARN {
		t.Fatalf("subscriber RoleARN = %q, want %q", subscriber.RoleARN, roleARN)
	}
	if subscriber.PrincipalAccount != "210987654321" {
		t.Fatalf("subscriber PrincipalAccount = %q, want 210987654321", subscriber.PrincipalAccount)
	}
	if subscriber.Status != "ACTIVE" {
		t.Fatalf("subscriber Status = %q, want ACTIVE", subscriber.Status)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityLake,
	}
}

type fakeSecurityLakeAPI struct {
	dataLakes       *awssecuritylake.ListDataLakesOutput
	logSourcePages  []*awssecuritylake.ListLogSourcesOutput
	logSourceCall   int
	subscriberPages []*awssecuritylake.ListSubscribersOutput
	subscriberCall  int
}

func (f *fakeSecurityLakeAPI) ListDataLakes(
	_ context.Context,
	_ *awssecuritylake.ListDataLakesInput,
	_ ...func(*awssecuritylake.Options),
) (*awssecuritylake.ListDataLakesOutput, error) {
	if f.dataLakes == nil {
		return &awssecuritylake.ListDataLakesOutput{}, nil
	}
	return f.dataLakes, nil
}

func (f *fakeSecurityLakeAPI) ListLogSources(
	_ context.Context,
	_ *awssecuritylake.ListLogSourcesInput,
	_ ...func(*awssecuritylake.Options),
) (*awssecuritylake.ListLogSourcesOutput, error) {
	if f.logSourceCall >= len(f.logSourcePages) {
		return &awssecuritylake.ListLogSourcesOutput{}, nil
	}
	page := f.logSourcePages[f.logSourceCall]
	f.logSourceCall++
	return page, nil
}

func (f *fakeSecurityLakeAPI) ListSubscribers(
	_ context.Context,
	_ *awssecuritylake.ListSubscribersInput,
	_ ...func(*awssecuritylake.Options),
) (*awssecuritylake.ListSubscribersOutput, error) {
	if f.subscriberCall >= len(f.subscriberPages) {
		return &awssecuritylake.ListSubscribersOutput{}, nil
	}
	page := f.subscriberPages[f.subscriberCall]
	f.subscriberCall++
	return page, nil
}
