// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskeyspaces "github.com/aws/aws-sdk-go-v2/service/keyspaces"
	awskeyspacestypes "github.com/aws/aws-sdk-go-v2/service/keyspaces/types"

	keyspacesservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/keyspaces"
)

// keyspacesSummary aliases the SDK keyspace summary so the client signature does
// not leak the long generated type name.
type keyspacesSummary = awskeyspacestypes.KeyspaceSummary

func mapKeyspace(
	name string,
	summary keyspacesSummary,
	output *awskeyspaces.GetKeyspaceOutput,
) keyspacesservice.Keyspace {
	keyspace := keyspacesservice.Keyspace{
		Name:                name,
		ARN:                 strings.TrimSpace(aws.ToString(summary.ResourceArn)),
		ReplicationStrategy: string(summary.ReplicationStrategy),
		ReplicationRegions:  cloneStrings(summary.ReplicationRegions),
	}
	if output == nil {
		return keyspace
	}
	if arn := strings.TrimSpace(aws.ToString(output.ResourceArn)); arn != "" {
		keyspace.ARN = arn
	}
	if strategy := string(output.ReplicationStrategy); strategy != "" {
		keyspace.ReplicationStrategy = strategy
	}
	if regions := cloneStrings(output.ReplicationRegions); regions != nil {
		keyspace.ReplicationRegions = regions
	}
	return keyspace
}

func mapTable(
	keyspaceARN string,
	output *awskeyspaces.GetTableOutput,
	tags map[string]string,
) keyspacesservice.Table {
	table := keyspacesservice.Table{
		ARN:                 strings.TrimSpace(aws.ToString(output.ResourceArn)),
		Name:                strings.TrimSpace(aws.ToString(output.TableName)),
		KeyspaceName:        strings.TrimSpace(aws.ToString(output.KeyspaceName)),
		KeyspaceARN:         strings.TrimSpace(keyspaceARN),
		Status:              string(output.Status),
		CreationTime:        aws.ToTime(output.CreationTimestamp),
		DefaultTimeToLive:   aws.ToInt32(output.DefaultTimeToLive),
		Encryption:          encryption(output.EncryptionSpecification),
		PointInTimeRecovery: pointInTimeRecovery(output.PointInTimeRecovery),
		LatestStreamARN:     strings.TrimSpace(aws.ToString(output.LatestStreamArn)),
		Schema:              schema(output.SchemaDefinition),
		Tags:                tags,
	}
	capacityMode, readUnits, writeUnits := capacity(output.CapacitySpecification)
	table.CapacityMode = capacityMode
	table.ReadCapacityUnits = readUnits
	table.WriteCapacityUnits = writeUnits
	if output.ClientSideTimestamps != nil {
		table.ClientSideTimestamps = string(output.ClientSideTimestamps.Status)
	}
	if output.Ttl != nil {
		table.TimeToLiveStatus = string(output.Ttl.Status)
	}
	if output.CdcSpecification != nil {
		table.CDCStatus = string(output.CdcSpecification.Status)
	}
	if output.Comment != nil {
		table.Comment = strings.TrimSpace(aws.ToString(output.Comment.Message))
	}
	return table
}

func encryption(spec *awskeyspacestypes.EncryptionSpecification) keyspacesservice.Encryption {
	if spec == nil {
		return keyspacesservice.Encryption{}
	}
	return keyspacesservice.Encryption{
		Type:             string(spec.Type),
		KMSKeyIdentifier: strings.TrimSpace(aws.ToString(spec.KmsKeyIdentifier)),
	}
}

func pointInTimeRecovery(
	summary *awskeyspacestypes.PointInTimeRecoverySummary,
) keyspacesservice.PointInTimeRecovery {
	if summary == nil {
		return keyspacesservice.PointInTimeRecovery{}
	}
	return keyspacesservice.PointInTimeRecovery{
		Status: string(summary.Status),
	}
}

func capacity(
	summary *awskeyspacestypes.CapacitySpecificationSummary,
) (mode string, read int64, write int64) {
	if summary == nil {
		return "", 0, 0
	}
	return string(summary.ThroughputMode),
		aws.ToInt64(summary.ReadCapacityUnits),
		aws.ToInt64(summary.WriteCapacityUnits)
}

func schema(definition *awskeyspacestypes.SchemaDefinition) keyspacesservice.Schema {
	if definition == nil {
		return keyspacesservice.Schema{}
	}
	return keyspacesservice.Schema{
		Columns:        columns(definition.AllColumns),
		PartitionKeys:  partitionKeys(definition.PartitionKeys),
		ClusteringKeys: clusteringKeys(definition.ClusteringKeys),
		StaticColumns:  staticColumns(definition.StaticColumns),
	}
}

func columns(definitions []awskeyspacestypes.ColumnDefinition) []keyspacesservice.Column {
	var output []keyspacesservice.Column
	for _, definition := range definitions {
		name := strings.TrimSpace(aws.ToString(definition.Name))
		if name == "" {
			continue
		}
		output = append(output, keyspacesservice.Column{
			Name: name,
			Type: strings.TrimSpace(aws.ToString(definition.Type)),
		})
	}
	return output
}

func partitionKeys(keys []awskeyspacestypes.PartitionKey) []string {
	var output []string
	for _, key := range keys {
		if name := strings.TrimSpace(aws.ToString(key.Name)); name != "" {
			output = append(output, name)
		}
	}
	return output
}

func clusteringKeys(keys []awskeyspacestypes.ClusteringKey) []keyspacesservice.ClusteringKey {
	var output []keyspacesservice.ClusteringKey
	for _, key := range keys {
		name := strings.TrimSpace(aws.ToString(key.Name))
		if name == "" {
			continue
		}
		output = append(output, keyspacesservice.ClusteringKey{
			Name:    name,
			OrderBy: string(key.OrderBy),
		})
	}
	return output
}

func staticColumns(columns []awskeyspacestypes.StaticColumn) []string {
	var output []string
	for _, column := range columns {
		if name := strings.TrimSpace(aws.ToString(column.Name)); name != "" {
			output = append(output, name)
		}
	}
	return output
}

func mapTags(tags []awskeyspacestypes.Tag) map[string]string {
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

// tableNameFromARN extracts the table name from a Keyspaces table ARN of the
// form ".../keyspace/<ks>/table/<name>". ListTables reports only the table ARN,
// not a bare table name, so the GetTable call needs the name parsed back out. It
// returns "" when the input is not a recognizable table ARN.
func tableNameFromARN(tableARN string) string {
	trimmed := strings.TrimSpace(tableARN)
	marker := "/table/"
	index := strings.LastIndex(trimmed, marker)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[index+len(marker):])
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
