// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsquicksight "github.com/aws/aws-sdk-go-v2/service/quicksight"
	awsquicksighttypes "github.com/aws/aws-sdk-go-v2/service/quicksight/types"

	quicksightservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/quicksight"
)

// mapDataSource converts an SDK DataSource into the scanner-owned model. It
// reads the connector type, the resolvable backing-store reference, the VPC
// connection ARN, and tags. It never reads DataSourceParameters secret fields,
// AlternateDataSourceParameters, the Secrets Manager secret value, or any
// credential; only a boolean records that a secret is configured.
func (c *Client) mapDataSource(
	ctx context.Context,
	source awsquicksighttypes.DataSource,
) (quicksightservice.DataSource, error) {
	arn := strings.TrimSpace(aws.ToString(source.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return quicksightservice.DataSource{}, err
	}
	mapped := quicksightservice.DataSource{
		ARN:              arn,
		ID:               strings.TrimSpace(aws.ToString(source.DataSourceId)),
		Name:             strings.TrimSpace(aws.ToString(source.Name)),
		Type:             strings.TrimSpace(string(source.Type)),
		Status:           strings.TrimSpace(string(source.Status)),
		SecretConfigured: strings.TrimSpace(aws.ToString(source.SecretArn)) != "",
		CreatedTime:      aws.ToTime(source.CreatedTime),
		LastUpdatedTime:  aws.ToTime(source.LastUpdatedTime),
		Tags:             tags,
		Backing:          backingStore(source.DataSourceParameters),
	}
	if vpc := source.VpcConnectionProperties; vpc != nil {
		mapped.VPCConnectionARN = strings.TrimSpace(aws.ToString(vpc.VpcConnectionArn))
	}
	return mapped, nil
}

// backingStore extracts the resolvable backing-store reference from a data
// source's connection parameters. It reads only the bare identifiers (cluster
// id, instance id, workgroup name, S3 manifest bucket) and never reads hosts
// with embedded credentials, database names, or any secret field. Connector
// types Eshu does not scan, and connections identified only by host/port,
// resolve to no backing reference.
func backingStore(parameters awsquicksighttypes.DataSourceParameters) quicksightservice.BackingStore {
	switch params := parameters.(type) {
	case *awsquicksighttypes.DataSourceParametersMemberRedshiftParameters:
		if clusterID := strings.TrimSpace(aws.ToString(params.Value.ClusterId)); clusterID != "" {
			return quicksightservice.BackingStore{
				Kind:       quicksightservice.BackingStoreRedshiftCluster,
				Identifier: clusterID,
			}
		}
	case *awsquicksighttypes.DataSourceParametersMemberRdsParameters:
		if instanceID := strings.TrimSpace(aws.ToString(params.Value.InstanceId)); instanceID != "" {
			return quicksightservice.BackingStore{
				Kind:       quicksightservice.BackingStoreRDSInstance,
				Identifier: instanceID,
			}
		}
	case *awsquicksighttypes.DataSourceParametersMemberAthenaParameters:
		if workGroup := strings.TrimSpace(aws.ToString(params.Value.WorkGroup)); workGroup != "" {
			return quicksightservice.BackingStore{
				Kind:       quicksightservice.BackingStoreAthenaWorkGroup,
				Identifier: workGroup,
			}
		}
	case *awsquicksighttypes.DataSourceParametersMemberS3Parameters:
		if location := params.Value.ManifestFileLocation; location != nil {
			if bucket := strings.TrimSpace(aws.ToString(location.Bucket)); bucket != "" {
				return quicksightservice.BackingStore{
					Kind:       quicksightservice.BackingStoreS3Bucket,
					Identifier: bucket,
				}
			}
		}
	}
	return quicksightservice.BackingStore{Kind: quicksightservice.BackingStoreNone}
}

// mapVPCConnection converts an SDK VPC connection summary into the scanner-owned
// network membership view, keyed by the bare VPC connection id. It returns the
// bare security group ids and the bare subnet ids from the connection's network
// interfaces; it reads no DNS resolver or IAM role.
func mapVPCConnection(summary awsquicksighttypes.VPCConnectionSummary) (string, quicksightservice.VPCConnection) {
	id := strings.TrimSpace(aws.ToString(summary.VPCConnectionId))
	if id == "" {
		return "", quicksightservice.VPCConnection{}
	}
	resolved := quicksightservice.VPCConnection{
		SecurityGroupIDs: trimmedStrings(summary.SecurityGroupIds),
	}
	for i := range summary.NetworkInterfaces {
		if subnetID := strings.TrimSpace(aws.ToString(summary.NetworkInterfaces[i].SubnetId)); subnetID != "" {
			resolved.SubnetIDs = append(resolved.SubnetIDs, subnetID)
		}
	}
	return id, resolved
}

// mapDataSet converts an SDK dataset summary into the scanner-owned model and
// resolves the data sources it physically reads through DescribeDataSet. It
// reads only the data-source ARNs from the dataset's physical tables; it never
// reads custom-SQL query bodies, column data, or row-level security values.
func (c *Client) mapDataSet(
	ctx context.Context,
	summary awsquicksighttypes.DataSetSummary,
) (quicksightservice.DataSet, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.DataSetId))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return quicksightservice.DataSet{}, err
	}
	dataSet := quicksightservice.DataSet{
		ARN:             arn,
		ID:              id,
		Name:            strings.TrimSpace(aws.ToString(summary.Name)),
		ImportMode:      strings.TrimSpace(string(summary.ImportMode)),
		CreatedTime:     aws.ToTime(summary.CreatedTime),
		LastUpdatedTime: aws.ToTime(summary.LastUpdatedTime),
		Tags:            tags,
	}
	if id == "" {
		return dataSet, nil
	}
	var output *awsquicksight.DescribeDataSetOutput
	err = c.recordAPICall(ctx, "DescribeDataSet", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeDataSet(callCtx, &awsquicksight.DescribeDataSetInput{
			AwsAccountId: aws.String(c.accountID),
			DataSetId:    aws.String(id),
		})
		return callErr
	})
	if err != nil {
		// A dataset whose definition is hidden (denied or removed) still emits
		// useful summary metadata; only its data-source edges are unavailable.
		if isAccessDenied(err) || isResourceNotFound(err) {
			return dataSet, nil
		}
		return quicksightservice.DataSet{}, err
	}
	if output != nil && output.DataSet != nil {
		dataSet.DataSourceARNs = dataSourceARNsFromPhysicalTables(output.DataSet.PhysicalTableMap)
	}
	return dataSet, nil
}

// dataSourceARNsFromPhysicalTables extracts the data-source ARNs a dataset's
// physical tables read. It reads the DataSourceArn from relational, custom-SQL,
// and S3 physical tables. The custom-SQL SQL body and input column schemas are
// intentionally not read.
func dataSourceARNsFromPhysicalTables(tables map[string]awsquicksighttypes.PhysicalTable) []string {
	if len(tables) == 0 {
		return nil
	}
	var arns []string
	for _, table := range tables {
		switch physical := table.(type) {
		case *awsquicksighttypes.PhysicalTableMemberRelationalTable:
			arns = appendTrimmed(arns, aws.ToString(physical.Value.DataSourceArn))
		case *awsquicksighttypes.PhysicalTableMemberCustomSql:
			arns = appendTrimmed(arns, aws.ToString(physical.Value.DataSourceArn))
		case *awsquicksighttypes.PhysicalTableMemberS3Source:
			arns = appendTrimmed(arns, aws.ToString(physical.Value.DataSourceArn))
		}
	}
	return arns
}

// mapDashboard converts an SDK dashboard summary into the scanner-owned model
// and resolves the datasets the published version reads through DescribeDashboard.
// It reads only the version's DataSetArns; it never reads the visual definition.
func (c *Client) mapDashboard(
	ctx context.Context,
	summary awsquicksighttypes.DashboardSummary,
) (quicksightservice.Dashboard, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.DashboardId))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return quicksightservice.Dashboard{}, err
	}
	dashboard := quicksightservice.Dashboard{
		ARN:                    arn,
		ID:                     id,
		Name:                   strings.TrimSpace(aws.ToString(summary.Name)),
		PublishedVersionNumber: aws.ToInt64(summary.PublishedVersionNumber),
		CreatedTime:            aws.ToTime(summary.CreatedTime),
		LastUpdatedTime:        aws.ToTime(summary.LastUpdatedTime),
		Tags:                   tags,
	}
	if id == "" {
		return dashboard, nil
	}
	var output *awsquicksight.DescribeDashboardOutput
	err = c.recordAPICall(ctx, "DescribeDashboard", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeDashboard(callCtx, &awsquicksight.DescribeDashboardInput{
			AwsAccountId: aws.String(c.accountID),
			DashboardId:  aws.String(id),
		})
		return callErr
	})
	if err != nil {
		if isAccessDenied(err) || isResourceNotFound(err) {
			return dashboard, nil
		}
		return quicksightservice.Dashboard{}, err
	}
	if output != nil && output.Dashboard != nil && output.Dashboard.Version != nil {
		dashboard.DataSetARNs = trimmedStrings(output.Dashboard.Version.DataSetArns)
	}
	return dashboard, nil
}

// mapAnalysis converts an SDK analysis summary into the scanner-owned model and
// resolves the datasets it reads through DescribeAnalysis. It reads only the
// analysis DataSetArns; it never reads the visual definition.
func (c *Client) mapAnalysis(
	ctx context.Context,
	summary awsquicksighttypes.AnalysisSummary,
) (quicksightservice.Analysis, error) {
	arn := strings.TrimSpace(aws.ToString(summary.Arn))
	id := strings.TrimSpace(aws.ToString(summary.AnalysisId))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return quicksightservice.Analysis{}, err
	}
	analysis := quicksightservice.Analysis{
		ARN:             arn,
		ID:              id,
		Name:            strings.TrimSpace(aws.ToString(summary.Name)),
		Status:          strings.TrimSpace(string(summary.Status)),
		CreatedTime:     aws.ToTime(summary.CreatedTime),
		LastUpdatedTime: aws.ToTime(summary.LastUpdatedTime),
		Tags:            tags,
	}
	if id == "" {
		return analysis, nil
	}
	var output *awsquicksight.DescribeAnalysisOutput
	err = c.recordAPICall(ctx, "DescribeAnalysis", func(callCtx context.Context) error {
		var callErr error
		output, callErr = c.client.DescribeAnalysis(callCtx, &awsquicksight.DescribeAnalysisInput{
			AwsAccountId: aws.String(c.accountID),
			AnalysisId:   aws.String(id),
		})
		return callErr
	})
	if err != nil {
		if isAccessDenied(err) || isResourceNotFound(err) {
			return analysis, nil
		}
		return quicksightservice.Analysis{}, err
	}
	if output != nil && output.Analysis != nil {
		analysis.DataSetARNs = trimmedStrings(output.Analysis.DataSetArns)
	}
	return analysis, nil
}

// tagsFromSDK converts SDK resource tags into a trimmed-key map, dropping empty
// keys. It returns nil when nothing survives so the payload omits empty tags.
func tagsFromSDK(tags []awsquicksighttypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for i := range tags {
		key := strings.TrimSpace(aws.ToString(tags[i].Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tags[i].Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// trimmedStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func trimmedStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appendTrimmed appends value to dst when it trims to a non-empty string.
func appendTrimmed(dst []string, value string) []string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return append(dst, trimmed)
	}
	return dst
}
