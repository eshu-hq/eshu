package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	dynamodbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dynamodb"
)

func mapTable(
	raw awsdynamodbtypes.TableDescription,
	tags map[string]string,
	ttl dynamodbservice.TTL,
	backups dynamodbservice.ContinuousBackups,
) dynamodbservice.Table {
	return dynamodbservice.Table{
		ARN:                       strings.TrimSpace(aws.ToString(raw.TableArn)),
		Name:                      strings.TrimSpace(aws.ToString(raw.TableName)),
		ID:                        strings.TrimSpace(aws.ToString(raw.TableId)),
		Status:                    string(raw.TableStatus),
		CreationTime:              aws.ToTime(raw.CreationDateTime),
		BillingMode:               billingMode(raw.BillingModeSummary),
		TableClass:                tableClass(raw.TableClassSummary),
		ItemCount:                 aws.ToInt64(raw.ItemCount),
		TableSizeBytes:            aws.ToInt64(raw.TableSizeBytes),
		DeletionProtectionEnabled: aws.ToBool(raw.DeletionProtectionEnabled),
		KeySchema:                 keySchema(raw.KeySchema),
		AttributeDefinitions:      attributeDefinitions(raw.AttributeDefinitions),
		ProvisionedThroughput:     provisionedThroughput(raw.ProvisionedThroughput),
		OnDemandThroughput:        onDemandThroughput(raw.OnDemandThroughput),
		SSE:                       sse(raw.SSEDescription),
		TTL:                       ttl,
		ContinuousBackups:         backups,
		Stream:                    stream(raw.StreamSpecification, raw.LatestStreamArn, raw.LatestStreamLabel),
		GlobalSecondaryIndexes:    globalSecondaryIndexes(raw.GlobalSecondaryIndexes),
		LocalSecondaryIndexes:     localSecondaryIndexes(raw.LocalSecondaryIndexes),
		Replicas:                  replicas(raw.Replicas),
		Tags:                      tags,
	}
}

func keySchema(elements []awsdynamodbtypes.KeySchemaElement) []dynamodbservice.KeySchemaElement {
	var output []dynamodbservice.KeySchemaElement
	for _, element := range elements {
		name := strings.TrimSpace(aws.ToString(element.AttributeName))
		if name == "" {
			continue
		}
		output = append(output, dynamodbservice.KeySchemaElement{
			AttributeName: name,
			KeyType:       string(element.KeyType),
		})
	}
	return output
}

func attributeDefinitions(
	definitions []awsdynamodbtypes.AttributeDefinition,
) []dynamodbservice.AttributeDefinition {
	var output []dynamodbservice.AttributeDefinition
	for _, definition := range definitions {
		name := strings.TrimSpace(aws.ToString(definition.AttributeName))
		if name == "" {
			continue
		}
		output = append(output, dynamodbservice.AttributeDefinition{
			AttributeName: name,
			AttributeType: string(definition.AttributeType),
		})
	}
	return output
}

func billingMode(summary *awsdynamodbtypes.BillingModeSummary) string {
	if summary == nil {
		return ""
	}
	return string(summary.BillingMode)
}

func tableClass(summary *awsdynamodbtypes.TableClassSummary) string {
	if summary == nil {
		return ""
	}
	return string(summary.TableClass)
}

func provisionedThroughput(
	throughput *awsdynamodbtypes.ProvisionedThroughputDescription,
) dynamodbservice.Throughput {
	if throughput == nil {
		return dynamodbservice.Throughput{}
	}
	return dynamodbservice.Throughput{
		ReadCapacityUnits:      aws.ToInt64(throughput.ReadCapacityUnits),
		WriteCapacityUnits:     aws.ToInt64(throughput.WriteCapacityUnits),
		NumberOfDecreasesToday: aws.ToInt64(throughput.NumberOfDecreasesToday),
	}
}

func onDemandThroughput(
	throughput *awsdynamodbtypes.OnDemandThroughput,
) dynamodbservice.OnDemandThroughput {
	if throughput == nil {
		return dynamodbservice.OnDemandThroughput{}
	}
	return dynamodbservice.OnDemandThroughput{
		MaxReadRequestUnits:  aws.ToInt64(throughput.MaxReadRequestUnits),
		MaxWriteRequestUnits: aws.ToInt64(throughput.MaxWriteRequestUnits),
	}
}

func sse(description *awsdynamodbtypes.SSEDescription) dynamodbservice.SSE {
	if description == nil {
		return dynamodbservice.SSE{}
	}
	return dynamodbservice.SSE{
		Status:          string(description.Status),
		Type:            string(description.SSEType),
		KMSMasterKeyARN: strings.TrimSpace(aws.ToString(description.KMSMasterKeyArn)),
	}
}

func stream(
	specification *awsdynamodbtypes.StreamSpecification,
	latestARN *string,
	latestLabel *string,
) dynamodbservice.Stream {
	stream := dynamodbservice.Stream{
		LatestStreamARN: strings.TrimSpace(aws.ToString(latestARN)),
		LatestLabel:     strings.TrimSpace(aws.ToString(latestLabel)),
	}
	if specification == nil {
		return stream
	}
	stream.Enabled = aws.ToBool(specification.StreamEnabled)
	stream.ViewType = string(specification.StreamViewType)
	return stream
}

func globalSecondaryIndexes(
	indexes []awsdynamodbtypes.GlobalSecondaryIndexDescription,
) []dynamodbservice.SecondaryIndex {
	var output []dynamodbservice.SecondaryIndex
	for _, index := range indexes {
		name := strings.TrimSpace(aws.ToString(index.IndexName))
		if name == "" {
			continue
		}
		output = append(output, dynamodbservice.SecondaryIndex{
			Name:                  name,
			ARN:                   strings.TrimSpace(aws.ToString(index.IndexArn)),
			Status:                string(index.IndexStatus),
			ItemCount:             aws.ToInt64(index.ItemCount),
			SizeBytes:             aws.ToInt64(index.IndexSizeBytes),
			Backfilling:           aws.ToBool(index.Backfilling),
			KeySchema:             keySchema(index.KeySchema),
			ProjectionType:        projectionType(index.Projection),
			NonKeyAttributes:      nonKeyAttributes(index.Projection),
			ProvisionedThroughput: provisionedThroughput(index.ProvisionedThroughput),
			OnDemandThroughput:    onDemandThroughput(index.OnDemandThroughput),
		})
	}
	return output
}

func localSecondaryIndexes(
	indexes []awsdynamodbtypes.LocalSecondaryIndexDescription,
) []dynamodbservice.SecondaryIndex {
	var output []dynamodbservice.SecondaryIndex
	for _, index := range indexes {
		name := strings.TrimSpace(aws.ToString(index.IndexName))
		if name == "" {
			continue
		}
		output = append(output, dynamodbservice.SecondaryIndex{
			Name:             name,
			ARN:              strings.TrimSpace(aws.ToString(index.IndexArn)),
			ItemCount:        aws.ToInt64(index.ItemCount),
			SizeBytes:        aws.ToInt64(index.IndexSizeBytes),
			KeySchema:        keySchema(index.KeySchema),
			ProjectionType:   projectionType(index.Projection),
			NonKeyAttributes: nonKeyAttributes(index.Projection),
		})
	}
	return output
}

func projectionType(projection *awsdynamodbtypes.Projection) string {
	if projection == nil {
		return ""
	}
	return string(projection.ProjectionType)
}

func nonKeyAttributes(projection *awsdynamodbtypes.Projection) []string {
	if projection == nil {
		return nil
	}
	var output []string
	for _, attribute := range projection.NonKeyAttributes {
		if trimmed := strings.TrimSpace(attribute); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

func replicas(replicas []awsdynamodbtypes.ReplicaDescription) []dynamodbservice.Replica {
	var output []dynamodbservice.Replica
	for _, replica := range replicas {
		region := strings.TrimSpace(aws.ToString(replica.RegionName))
		if region == "" {
			continue
		}
		output = append(output, dynamodbservice.Replica{
			RegionName:     region,
			Status:         string(replica.ReplicaStatus),
			KMSMasterKeyID: strings.TrimSpace(aws.ToString(replica.KMSMasterKeyId)),
			TableClass:     tableClass(replica.ReplicaTableClassSummary),
		})
	}
	return output
}

func mapTTL(description *awsdynamodbtypes.TimeToLiveDescription) dynamodbservice.TTL {
	if description == nil {
		return dynamodbservice.TTL{}
	}
	return dynamodbservice.TTL{
		Status:        string(description.TimeToLiveStatus),
		AttributeName: strings.TrimSpace(aws.ToString(description.AttributeName)),
	}
}

func mapContinuousBackups(
	description *awsdynamodbtypes.ContinuousBackupsDescription,
) dynamodbservice.ContinuousBackups {
	if description == nil {
		return dynamodbservice.ContinuousBackups{}
	}
	output := dynamodbservice.ContinuousBackups{
		Status: string(description.ContinuousBackupsStatus),
	}
	if pitr := description.PointInTimeRecoveryDescription; pitr != nil {
		output.PointInTimeRecoveryStatus = string(pitr.PointInTimeRecoveryStatus)
		output.RecoveryPeriodInDays = aws.ToInt32(pitr.RecoveryPeriodInDays)
	}
	return output
}

func mapTags(tags []awsdynamodbtypes.Tag) map[string]string {
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
	if len(output) == 0 {
		return nil
	}
	return output
}
