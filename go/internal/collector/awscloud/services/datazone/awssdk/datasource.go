// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatazone "github.com/aws/aws-sdk-go-v2/service/datazone"
	awsdatazonetypes "github.com/aws/aws-sdk-go-v2/service/datazone/types"

	datazoneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/datazone"
)

// listDataSources pages every data source under each project of a domain and
// enriches each with its backing-store configuration through GetDataSource.
func (c *Client) listDataSources(
	ctx context.Context,
	domainID string,
	projects []datazoneservice.Project,
) ([]datazoneservice.DataSource, error) {
	domainID = strings.TrimSpace(domainID)
	if domainID == "" {
		return nil, nil
	}
	var dataSources []datazoneservice.DataSource
	for _, project := range projects {
		projectID := strings.TrimSpace(project.ID)
		if projectID == "" {
			continue
		}
		summaries, err := c.listDataSourceSummaries(ctx, domainID, projectID)
		if err != nil {
			return nil, err
		}
		for _, summary := range summaries {
			dataSource := mapDataSourceSummary(domainID, summary)
			if err := c.enrichDataSource(ctx, domainID, &dataSource); err != nil {
				return nil, err
			}
			dataSources = append(dataSources, dataSource)
		}
	}
	return dataSources, nil
}

func (c *Client) listDataSourceSummaries(
	ctx context.Context,
	domainID, projectID string,
) ([]awsdatazonetypes.DataSourceSummary, error) {
	var summaries []awsdatazonetypes.DataSourceSummary
	var nextToken *string
	for {
		var page *awsdatazone.ListDataSourcesOutput
		err := c.recordAPICall(ctx, "ListDataSources", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDataSources(callCtx, &awsdatazone.ListDataSourcesInput{
				DomainIdentifier:  aws.String(domainID),
				ProjectIdentifier: aws.String(projectID),
				NextToken:         nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return summaries, nil
		}
		summaries = append(summaries, page.Items...)
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return summaries, nil
		}
	}
}

func mapDataSourceSummary(
	domainID string,
	summary awsdatazonetypes.DataSourceSummary,
) datazoneservice.DataSource {
	return datazoneservice.DataSource{
		ID:            strings.TrimSpace(aws.ToString(summary.DataSourceId)),
		DomainID:      firstNonEmpty(strings.TrimSpace(aws.ToString(summary.DomainId)), domainID),
		EnvironmentID: strings.TrimSpace(aws.ToString(summary.EnvironmentId)),
		Name:          strings.TrimSpace(aws.ToString(summary.Name)),
		Description:   strings.TrimSpace(aws.ToString(summary.Description)),
		Type:          strings.TrimSpace(aws.ToString(summary.Type)),
		Status:        strings.TrimSpace(string(summary.Status)),
		Enabled:       summary.EnableSetting == awsdatazonetypes.EnableSettingEnabled,
		ConnectionID:  strings.TrimSpace(aws.ToString(summary.ConnectionId)),
		CreatedAt:     aws.ToTime(summary.CreatedAt),
		UpdatedAt:     aws.ToTime(summary.UpdatedAt),
	}
}

// enrichDataSource reads GetDataSource to recover the parent project id and the
// backing-store identifiers (Glue database names, provisioned Redshift cluster
// name and its account/region) from the data source run configuration. It never
// reads ingested asset content or relational filter expressions; only the
// backing store names are copied so the scanner can join scanned Glue/Redshift
// resources.
func (c *Client) enrichDataSource(
	ctx context.Context,
	domainID string,
	dataSource *datazoneservice.DataSource,
) error {
	id := strings.TrimSpace(dataSource.ID)
	if id == "" {
		return nil
	}
	var output *awsdatazone.GetDataSourceOutput
	err := c.recordAPICall(ctx, "GetDataSource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetDataSource(callCtx, &awsdatazone.GetDataSourceInput{
			DomainIdentifier: aws.String(domainID),
			Identifier:       aws.String(id),
		})
		return err
	})
	if err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	dataSource.ProjectID = strings.TrimSpace(aws.ToString(output.ProjectId))
	if env := strings.TrimSpace(aws.ToString(output.EnvironmentId)); env != "" {
		dataSource.EnvironmentID = env
	}
	if conn := strings.TrimSpace(aws.ToString(output.ConnectionId)); conn != "" {
		dataSource.ConnectionID = conn
	}
	applyConfiguration(dataSource, output.Configuration)
	return nil
}

// applyConfiguration copies the resolvable backing-store identifiers out of the
// data source run configuration union. Glue databases are keyed by name;
// provisioned Redshift clusters are keyed by name plus the config account/region.
// Redshift Serverless workgroups carry an opaque published ARN that cannot be
// synthesized from the workgroup name, so they are intentionally not copied.
func applyConfiguration(
	dataSource *datazoneservice.DataSource,
	configuration awsdatazonetypes.DataSourceConfigurationOutput,
) {
	switch config := configuration.(type) {
	case *awsdatazonetypes.DataSourceConfigurationOutputMemberGlueRunConfiguration:
		dataSource.GlueDatabaseNames = relationalDatabaseNames(config.Value.RelationalFilterConfigurations)
	case *awsdatazonetypes.DataSourceConfigurationOutputMemberRedshiftRunConfiguration:
		dataSource.BackingAccountID = strings.TrimSpace(aws.ToString(config.Value.AccountId))
		dataSource.BackingRegion = strings.TrimSpace(aws.ToString(config.Value.Region))
		applyRedshiftStorage(dataSource, config.Value.RedshiftStorage)
	}
}

func relationalDatabaseNames(configs []awsdatazonetypes.RelationalFilterConfiguration) []string {
	if len(configs) == 0 {
		return nil
	}
	names := make([]string, 0, len(configs))
	for _, config := range configs {
		if name := strings.TrimSpace(aws.ToString(config.DatabaseName)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func applyRedshiftStorage(dataSource *datazoneservice.DataSource, storage awsdatazonetypes.RedshiftStorage) {
	if cluster, ok := storage.(*awsdatazonetypes.RedshiftStorageMemberRedshiftClusterSource); ok {
		dataSource.RedshiftClusterName = strings.TrimSpace(aws.ToString(cluster.Value.ClusterName))
	}
}
