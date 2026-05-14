package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awsdynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListsDynamoDBMetadataOnly(t *testing.T) {
	tableARN := "arn:aws:dynamodb:us-east-1:123456789012:table/orders"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeDynamoDBAPI{
		tablePages: []*awsdynamodb.ListTablesOutput{{
			TableNames:             []string{"orders"},
			LastEvaluatedTableName: aws.String("orders"),
		}, {
			TableNames: []string{"customers"},
		}},
		tables: map[string]*awsdynamodb.DescribeTableOutput{
			"orders": {
				Table: &awsdynamodbtypes.TableDescription{
					TableArn:                  aws.String(tableARN),
					TableName:                 aws.String("orders"),
					TableId:                   aws.String("table-123"),
					TableStatus:               awsdynamodbtypes.TableStatusActive,
					CreationDateTime:          aws.Time(createdAt),
					ItemCount:                 aws.Int64(42),
					TableSizeBytes:            aws.Int64(1024),
					DeletionProtectionEnabled: aws.Bool(true),
					BillingModeSummary: &awsdynamodbtypes.BillingModeSummary{
						BillingMode: awsdynamodbtypes.BillingModePayPerRequest,
					},
					TableClassSummary: &awsdynamodbtypes.TableClassSummary{
						TableClass: awsdynamodbtypes.TableClassStandard,
					},
					KeySchema: []awsdynamodbtypes.KeySchemaElement{{
						AttributeName: aws.String("tenant_id"),
						KeyType:       awsdynamodbtypes.KeyTypeHash,
					}},
					AttributeDefinitions: []awsdynamodbtypes.AttributeDefinition{{
						AttributeName: aws.String("tenant_id"),
						AttributeType: awsdynamodbtypes.ScalarAttributeTypeS,
					}},
					ProvisionedThroughput: &awsdynamodbtypes.ProvisionedThroughputDescription{
						ReadCapacityUnits:      aws.Int64(5),
						WriteCapacityUnits:     aws.Int64(10),
						NumberOfDecreasesToday: aws.Int64(1),
					},
					SSEDescription: &awsdynamodbtypes.SSEDescription{
						Status:          awsdynamodbtypes.SSEStatusEnabled,
						SSEType:         awsdynamodbtypes.SSETypeKms,
						KMSMasterKeyArn: aws.String(kmsARN),
					},
					StreamSpecification: &awsdynamodbtypes.StreamSpecification{
						StreamEnabled:  aws.Bool(true),
						StreamViewType: awsdynamodbtypes.StreamViewTypeNewAndOldImages,
					},
					LatestStreamArn:   aws.String("arn:aws:dynamodb:us-east-1:123456789012:table/orders/stream/2026-05-14T12:00:00.000"),
					LatestStreamLabel: aws.String("2026-05-14T12:00:00.000"),
					GlobalSecondaryIndexes: []awsdynamodbtypes.GlobalSecondaryIndexDescription{{
						IndexName:      aws.String("by_status"),
						IndexArn:       aws.String("arn:aws:dynamodb:us-east-1:123456789012:table/orders/index/by_status"),
						IndexStatus:    awsdynamodbtypes.IndexStatusActive,
						ItemCount:      aws.Int64(10),
						IndexSizeBytes: aws.Int64(256),
						KeySchema: []awsdynamodbtypes.KeySchemaElement{{
							AttributeName: aws.String("status"),
							KeyType:       awsdynamodbtypes.KeyTypeHash,
						}},
						Projection: &awsdynamodbtypes.Projection{
							ProjectionType: awsdynamodbtypes.ProjectionTypeKeysOnly,
						},
					}},
					LocalSecondaryIndexes: []awsdynamodbtypes.LocalSecondaryIndexDescription{{
						IndexName:      aws.String("by_created_at"),
						ItemCount:      aws.Int64(4),
						IndexSizeBytes: aws.Int64(128),
						KeySchema: []awsdynamodbtypes.KeySchemaElement{{
							AttributeName: aws.String("tenant_id"),
							KeyType:       awsdynamodbtypes.KeyTypeHash,
						}},
						Projection: &awsdynamodbtypes.Projection{
							ProjectionType: awsdynamodbtypes.ProjectionTypeAll,
						},
					}},
					Replicas: []awsdynamodbtypes.ReplicaDescription{{
						RegionName:     aws.String("us-west-2"),
						ReplicaStatus:  awsdynamodbtypes.ReplicaStatusActive,
						KMSMasterKeyId: aws.String("alias/orders-replica"),
						ReplicaTableClassSummary: &awsdynamodbtypes.TableClassSummary{
							TableClass: awsdynamodbtypes.TableClassStandard,
						},
					}},
				},
			},
			"customers": {
				Table: &awsdynamodbtypes.TableDescription{
					TableArn:    aws.String("arn:aws:dynamodb:us-east-1:123456789012:table/customers"),
					TableName:   aws.String("customers"),
					TableStatus: awsdynamodbtypes.TableStatusActive,
				},
			},
		},
		tags: map[string][]*awsdynamodb.ListTagsOfResourceOutput{
			tableARN: {{
				Tags:      []awsdynamodbtypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
				NextToken: aws.String("tags-next"),
			}, {
				Tags: []awsdynamodbtypes.Tag{{Key: aws.String("Team"), Value: aws.String("orders")}},
			}},
		},
		ttl: map[string]*awsdynamodb.DescribeTimeToLiveOutput{
			"orders": {
				TimeToLiveDescription: &awsdynamodbtypes.TimeToLiveDescription{
					TimeToLiveStatus: awsdynamodbtypes.TimeToLiveStatusEnabled,
					AttributeName:    aws.String("expires_at"),
				},
			},
		},
		backups: map[string]*awsdynamodb.DescribeContinuousBackupsOutput{
			"orders": {
				ContinuousBackupsDescription: &awsdynamodbtypes.ContinuousBackupsDescription{
					ContinuousBackupsStatus: awsdynamodbtypes.ContinuousBackupsStatusEnabled,
					PointInTimeRecoveryDescription: &awsdynamodbtypes.PointInTimeRecoveryDescription{
						PointInTimeRecoveryStatus: awsdynamodbtypes.PointInTimeRecoveryStatusEnabled,
						RecoveryPeriodInDays:      aws.Int32(35),
					},
				},
			},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	tables, err := adapter.ListTables(context.Background())
	if err != nil {
		t.Fatalf("ListTables() error = %v, want nil", err)
	}

	if got, want := len(tables), 2; got != want {
		t.Fatalf("len(tables) = %d, want %d", got, want)
	}
	if got, want := api.tableLimits, []int32{100, 100}; !int32SlicesEqual(got, want) {
		t.Fatalf("ListTables Limit = %#v, want %#v", got, want)
	}
	if got, want := api.tableStartNames, []string{"", "orders"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListTables ExclusiveStartTableName = %#v, want %#v", got, want)
	}
	if got, want := api.tagTokens[tableARN], []string{"", "tags-next"}; !stringSlicesEqual(got, want) {
		t.Fatalf("ListTagsOfResource tokens = %#v, want %#v", got, want)
	}
	orders := tables[0]
	if orders.ARN != tableARN || orders.Name != "orders" || orders.ID != "table-123" {
		t.Fatalf("orders table identity = %#v, want ARN/name/id", orders)
	}
	if orders.BillingMode != "PAY_PER_REQUEST" || orders.TableClass != "STANDARD" {
		t.Fatalf("orders capacity class = %#v, want mapped billing and table class", orders)
	}
	if orders.SSE.KMSMasterKeyARN != kmsARN || orders.TTL.AttributeName != "expires_at" {
		t.Fatalf("orders encryption/ttl = %#v, want mapped metadata", orders)
	}
	if orders.ContinuousBackups.RecoveryPeriodInDays != 35 {
		t.Fatalf("recovery period = %d, want 35", orders.ContinuousBackups.RecoveryPeriodInDays)
	}
	if len(orders.GlobalSecondaryIndexes) != 1 || orders.GlobalSecondaryIndexes[0].Name != "by_status" {
		t.Fatalf("global indexes = %#v, want by_status", orders.GlobalSecondaryIndexes)
	}
	if orders.Tags["Environment"] != "prod" || orders.Tags["Team"] != "orders" {
		t.Fatalf("tags = %#v, want merged tag pages", orders.Tags)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDynamoDB,
	}
}

type fakeDynamoDBAPI struct {
	tablePages      []*awsdynamodb.ListTablesOutput
	tableCalls      int
	tableLimits     []int32
	tableStartNames []string
	tables          map[string]*awsdynamodb.DescribeTableOutput
	describeNames   []string
	tags            map[string][]*awsdynamodb.ListTagsOfResourceOutput
	tagCalls        map[string]int
	tagTokens       map[string][]string
	ttl             map[string]*awsdynamodb.DescribeTimeToLiveOutput
	backups         map[string]*awsdynamodb.DescribeContinuousBackupsOutput
}

func (f *fakeDynamoDBAPI) ListTables(
	_ context.Context,
	input *awsdynamodb.ListTablesInput,
	_ ...func(*awsdynamodb.Options),
) (*awsdynamodb.ListTablesOutput, error) {
	f.tableLimits = append(f.tableLimits, aws.ToInt32(input.Limit))
	f.tableStartNames = append(f.tableStartNames, aws.ToString(input.ExclusiveStartTableName))
	if f.tableCalls >= len(f.tablePages) {
		return &awsdynamodb.ListTablesOutput{}, nil
	}
	page := f.tablePages[f.tableCalls]
	f.tableCalls++
	return page, nil
}

func (f *fakeDynamoDBAPI) DescribeTable(
	_ context.Context,
	input *awsdynamodb.DescribeTableInput,
	_ ...func(*awsdynamodb.Options),
) (*awsdynamodb.DescribeTableOutput, error) {
	tableName := aws.ToString(input.TableName)
	f.describeNames = append(f.describeNames, tableName)
	if output := f.tables[tableName]; output != nil {
		return output, nil
	}
	return &awsdynamodb.DescribeTableOutput{}, nil
}

func (f *fakeDynamoDBAPI) ListTagsOfResource(
	_ context.Context,
	input *awsdynamodb.ListTagsOfResourceInput,
	_ ...func(*awsdynamodb.Options),
) (*awsdynamodb.ListTagsOfResourceOutput, error) {
	resourceARN := aws.ToString(input.ResourceArn)
	if f.tagCalls == nil {
		f.tagCalls = map[string]int{}
	}
	if f.tagTokens == nil {
		f.tagTokens = map[string][]string{}
	}
	f.tagTokens[resourceARN] = append(f.tagTokens[resourceARN], aws.ToString(input.NextToken))
	pages := f.tags[resourceARN]
	if f.tagCalls[resourceARN] >= len(pages) {
		return &awsdynamodb.ListTagsOfResourceOutput{}, nil
	}
	page := pages[f.tagCalls[resourceARN]]
	f.tagCalls[resourceARN]++
	return page, nil
}

func (f *fakeDynamoDBAPI) DescribeTimeToLive(
	_ context.Context,
	input *awsdynamodb.DescribeTimeToLiveInput,
	_ ...func(*awsdynamodb.Options),
) (*awsdynamodb.DescribeTimeToLiveOutput, error) {
	if output := f.ttl[aws.ToString(input.TableName)]; output != nil {
		return output, nil
	}
	return &awsdynamodb.DescribeTimeToLiveOutput{}, nil
}

func (f *fakeDynamoDBAPI) DescribeContinuousBackups(
	_ context.Context,
	input *awsdynamodb.DescribeContinuousBackupsInput,
	_ ...func(*awsdynamodb.Options),
) (*awsdynamodb.DescribeContinuousBackupsOutput, error) {
	if output := f.backups[aws.ToString(input.TableName)]; output != nil {
		return output, nil
	}
	return &awsdynamodb.DescribeContinuousBackupsOutput{}, nil
}

var _ apiClient = (*fakeDynamoDBAPI)(nil)

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
