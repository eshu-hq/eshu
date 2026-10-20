// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstimestreamwrite "github.com/aws/aws-sdk-go-v2/service/timestreamwrite"
	awstimestreamwritetypes "github.com/aws/aws-sdk-go-v2/service/timestreamwrite/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsTimestreamMetadataOnly(t *testing.T) {
	databaseARN := "arn:aws:timestream:us-east-1:123456789012:database/metrics"
	tableARN := "arn:aws:timestream:us-east-1:123456789012:database/metrics/table/cpu"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeTimestreamAPI{
		databasePages: []*awstimestreamwrite.ListDatabasesOutput{{
			Databases: []awstimestreamwritetypes.Database{{
				Arn:             aws.String(databaseARN),
				DatabaseName:    aws.String("metrics"),
				KmsKeyId:        aws.String(kmsARN),
				TableCount:      1,
				CreationTime:    aws.Time(createdAt),
				LastUpdatedTime: aws.Time(createdAt),
			}},
		}},
		tablePages: map[string][]*awstimestreamwrite.ListTablesOutput{
			"metrics": {{
				Tables: []awstimestreamwritetypes.Table{{
					Arn:          aws.String(tableARN),
					TableName:    aws.String("cpu"),
					DatabaseName: aws.String("metrics"),
					TableStatus:  awstimestreamwritetypes.TableStatusActive,
					RetentionProperties: &awstimestreamwritetypes.RetentionProperties{
						MemoryStoreRetentionPeriodInHours:  aws.Int64(24),
						MagneticStoreRetentionPeriodInDays: aws.Int64(365),
					},
					MagneticStoreWriteProperties: &awstimestreamwritetypes.MagneticStoreWriteProperties{
						EnableMagneticStoreWrites: aws.Bool(true),
						MagneticStoreRejectedDataLocation: &awstimestreamwritetypes.MagneticStoreRejectedDataLocation{
							S3Configuration: &awstimestreamwritetypes.S3Configuration{
								BucketName:       aws.String("rejected-data-bucket"),
								ObjectKeyPrefix:  aws.String("errors/"),
								EncryptionOption: awstimestreamwritetypes.S3EncryptionOptionSseKms,
							},
						},
					},
					Schema: &awstimestreamwritetypes.Schema{
						CompositePartitionKey: []awstimestreamwritetypes.PartitionKey{{
							Name: aws.String("host"),
							Type: awstimestreamwritetypes.PartitionKeyTypeDimension,
						}},
					},
				}},
			}},
		},
		tags: map[string][]awstimestreamwritetypes.Tag{
			databaseARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			tableARN:    {{Key: aws.String("Team"), Value: aws.String("observability")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Databases) != 1 {
		t.Fatalf("len(Databases) = %d, want 1", len(snapshot.Databases))
	}
	database := snapshot.Databases[0]
	if database.ARN != databaseARN {
		t.Fatalf("database ARN = %q, want %q", database.ARN, databaseARN)
	}
	if database.KMSKeyID != kmsARN {
		t.Fatalf("database KMSKeyID = %q, want %q", database.KMSKeyID, kmsARN)
	}
	if database.Tags["Environment"] != "prod" {
		t.Fatalf("database tag Environment = %q, want prod", database.Tags["Environment"])
	}
	if len(database.Tables) != 1 {
		t.Fatalf("len(Tables) = %d, want 1", len(database.Tables))
	}
	table := database.Tables[0]
	if table.ARN != tableARN {
		t.Fatalf("table ARN = %q, want %q", table.ARN, tableARN)
	}
	if table.State != "ACTIVE" {
		t.Fatalf("table State = %q, want ACTIVE", table.State)
	}
	if table.MemoryStoreRetentionPeriodInHours != 24 {
		t.Fatalf("memory retention = %d, want 24", table.MemoryStoreRetentionPeriodInHours)
	}
	if table.MagneticStoreRetentionPeriodInDays != 365 {
		t.Fatalf("magnetic retention = %d, want 365", table.MagneticStoreRetentionPeriodInDays)
	}
	if !table.MagneticStoreWritesEnabled {
		t.Fatalf("MagneticStoreWritesEnabled = false, want true")
	}
	if table.RejectedDataS3Bucket != "rejected-data-bucket" {
		t.Fatalf("RejectedDataS3Bucket = %q, want rejected-data-bucket", table.RejectedDataS3Bucket)
	}
	if table.RejectedDataS3EncryptionOption != "SSE_KMS" {
		t.Fatalf("RejectedDataS3EncryptionOption = %q, want SSE_KMS", table.RejectedDataS3EncryptionOption)
	}
	if len(table.PartitionKeyNames) != 1 || table.PartitionKeyNames[0] != "host" {
		t.Fatalf("PartitionKeyNames = %#v, want [host]", table.PartitionKeyNames)
	}
}

type fakeTimestreamAPI struct {
	databasePages []*awstimestreamwrite.ListDatabasesOutput
	databaseCall  int
	tablePages    map[string][]*awstimestreamwrite.ListTablesOutput
	tableCalls    map[string]int
	tags          map[string][]awstimestreamwritetypes.Tag
}

func (f *fakeTimestreamAPI) ListDatabases(
	_ context.Context,
	_ *awstimestreamwrite.ListDatabasesInput,
	_ ...func(*awstimestreamwrite.Options),
) (*awstimestreamwrite.ListDatabasesOutput, error) {
	if f.databaseCall >= len(f.databasePages) {
		return &awstimestreamwrite.ListDatabasesOutput{}, nil
	}
	page := f.databasePages[f.databaseCall]
	f.databaseCall++
	return page, nil
}

func (f *fakeTimestreamAPI) ListTables(
	_ context.Context,
	input *awstimestreamwrite.ListTablesInput,
	_ ...func(*awstimestreamwrite.Options),
) (*awstimestreamwrite.ListTablesOutput, error) {
	if f.tableCalls == nil {
		f.tableCalls = map[string]int{}
	}
	name := aws.ToString(input.DatabaseName)
	pages := f.tablePages[name]
	idx := f.tableCalls[name]
	if idx >= len(pages) {
		return &awstimestreamwrite.ListTablesOutput{}, nil
	}
	f.tableCalls[name] = idx + 1
	return pages[idx], nil
}

func (f *fakeTimestreamAPI) ListTagsForResource(
	_ context.Context,
	input *awstimestreamwrite.ListTagsForResourceInput,
	_ ...func(*awstimestreamwrite.Options),
) (*awstimestreamwrite.ListTagsForResourceOutput, error) {
	return &awstimestreamwrite.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceARN)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceTimestream,
	}
}
