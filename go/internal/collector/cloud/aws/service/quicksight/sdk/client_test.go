// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsquicksight "github.com/aws/aws-sdk-go-v2/service/quicksight"
	awsquicksighttypes "github.com/aws/aws-sdk-go-v2/service/quicksight/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	quicksightservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/quicksight"
)

const (
	dataSourceARN = "arn:aws:quicksight:us-east-1:123456789012:datasource/redshift-prod"
	dataSetARN    = "arn:aws:quicksight:us-east-1:123456789012:dataset/sales"
	dashboardARN  = "arn:aws:quicksight:us-east-1:123456789012:dashboard/exec"
	analysisARN   = "arn:aws:quicksight:us-east-1:123456789012:analysis/explore"
)

func TestClientSnapshotsQuickSightMetadataOnly(t *testing.T) {
	api := &fakeQuickSightAPI{
		dataSources: []awsquicksighttypes.DataSource{{
			Arn:          awsv2.String(dataSourceARN),
			DataSourceId: awsv2.String("redshift-prod"),
			Name:         awsv2.String("Redshift Prod"),
			Type:         awsquicksighttypes.DataSourceTypeRedshift,
			Status:       awsquicksighttypes.ResourceStatusCreationSuccessful,
			SecretArn:    awsv2.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:qs-XYZ"),
			VpcConnectionProperties: &awsquicksighttypes.VpcConnectionProperties{
				VpcConnectionArn: awsv2.String("arn:aws:quicksight:us-east-1:123456789012:vpcConnection/vpc-conn-1"),
			},
			DataSourceParameters: &awsquicksighttypes.DataSourceParametersMemberRedshiftParameters{
				Value: awsquicksighttypes.RedshiftParameters{
					Database:  awsv2.String("analytics"),
					ClusterId: awsv2.String("analytics-cluster"),
				},
			},
		}},
		vpcConnections: []awsquicksighttypes.VPCConnectionSummary{{
			VPCConnectionId:  awsv2.String("vpc-conn-1"),
			SecurityGroupIds: []string{"sg-0a1b2c3d"},
			NetworkInterfaces: []awsquicksighttypes.NetworkInterface{
				{SubnetId: awsv2.String("subnet-1111")},
				{SubnetId: awsv2.String("subnet-2222")},
			},
		}},
		dataSets: []awsquicksighttypes.DataSetSummary{{
			Arn:        awsv2.String(dataSetARN),
			DataSetId:  awsv2.String("sales"),
			Name:       awsv2.String("Sales"),
			ImportMode: awsquicksighttypes.DataSetImportModeSpice,
		}},
		dataSetDetail: map[string]*awsquicksighttypes.DataSet{
			"sales": {
				Arn:       awsv2.String(dataSetARN),
				DataSetId: awsv2.String("sales"),
				PhysicalTableMap: map[string]awsquicksighttypes.PhysicalTable{
					"t1": &awsquicksighttypes.PhysicalTableMemberRelationalTable{
						Value: awsquicksighttypes.RelationalTable{
							DataSourceArn: awsv2.String(dataSourceARN),
							Name:          awsv2.String("public.sales"),
						},
					},
					"t2": &awsquicksighttypes.PhysicalTableMemberCustomSql{
						Value: awsquicksighttypes.CustomSql{
							DataSourceArn: awsv2.String(dataSourceARN),
							Name:          awsv2.String("custom"),
							SqlQuery:      awsv2.String("SELECT secret FROM private.credentials"),
						},
					},
				},
			},
		},
		dashboards: []awsquicksighttypes.DashboardSummary{{
			Arn:                    awsv2.String(dashboardARN),
			DashboardId:            awsv2.String("exec"),
			Name:                   awsv2.String("Exec"),
			PublishedVersionNumber: awsv2.Int64(3),
		}},
		dashboardDetail: map[string]*awsquicksighttypes.Dashboard{
			"exec": {
				Arn: awsv2.String(dashboardARN),
				Version: &awsquicksighttypes.DashboardVersion{
					DataSetArns: []string{dataSetARN},
				},
			},
		},
		analyses: []awsquicksighttypes.AnalysisSummary{{
			Arn:        awsv2.String(analysisARN),
			AnalysisId: awsv2.String("explore"),
			Name:       awsv2.String("Explore"),
			Status:     awsquicksighttypes.ResourceStatusCreationSuccessful,
		}},
		analysisDetail: map[string]*awsquicksighttypes.Analysis{
			"explore": {
				Arn:         awsv2.String(analysisARN),
				DataSetArns: []string{dataSetARN},
			},
		},
		tags: map[string][]awsquicksighttypes.Tag{
			dataSourceARN: {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.DataSources) != 1 {
		t.Fatalf("len(DataSources) = %d, want 1", len(snapshot.DataSources))
	}
	source := snapshot.DataSources[0]
	if source.Backing.Kind != quicksightservice.BackingStoreRedshiftCluster || source.Backing.Identifier != "analytics-cluster" {
		t.Fatalf("backing = %#v, want redshift_cluster/analytics-cluster", source.Backing)
	}
	if !source.SecretConfigured {
		t.Fatalf("SecretConfigured = false, want true")
	}
	if source.VPCConnectionARN == "" {
		t.Fatalf("VPCConnectionARN empty, want the connection ARN")
	}
	if source.Tags["Environment"] != "prod" {
		t.Fatalf("data source tag Environment = %q, want prod", source.Tags["Environment"])
	}

	conn, ok := snapshot.VPCConnections["vpc-conn-1"]
	if !ok {
		t.Fatalf("VPCConnections missing vpc-conn-1")
	}
	if len(conn.SecurityGroupIDs) != 1 || conn.SecurityGroupIDs[0] != "sg-0a1b2c3d" {
		t.Fatalf("SecurityGroupIDs = %#v, want [sg-0a1b2c3d]", conn.SecurityGroupIDs)
	}
	if len(conn.SubnetIDs) != 2 {
		t.Fatalf("SubnetIDs = %#v, want 2", conn.SubnetIDs)
	}

	if len(snapshot.DataSets) != 1 {
		t.Fatalf("len(DataSets) = %d, want 1", len(snapshot.DataSets))
	}
	if got := snapshot.DataSets[0].DataSourceARNs; len(got) != 2 || got[0] != dataSourceARN {
		t.Fatalf("dataset DataSourceARNs = %#v, want two %q entries", got, dataSourceARN)
	}
	if got := snapshot.Dashboards[0].DataSetARNs; len(got) != 1 || got[0] != dataSetARN {
		t.Fatalf("dashboard DataSetARNs = %#v, want [%q]", got, dataSetARN)
	}
	if got := snapshot.Analyses[0].DataSetARNs; len(got) != 1 || got[0] != dataSetARN {
		t.Fatalf("analysis DataSetARNs = %#v, want [%q]", got, dataSetARN)
	}
}

func TestClientNotSubscribedReturnsEmptyWithWarning(t *testing.T) {
	api := &fakeQuickSightAPI{
		listDataSourcesErr: &awsquicksighttypes.ResourceNotFoundException{
			Message: awsv2.String("Account 123456789012 is not signed up for QuickSight"),
		},
	}
	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil for not-subscribed account", err)
	}
	if len(snapshot.DataSources) != 0 {
		t.Fatalf("DataSources = %d, want 0", len(snapshot.DataSources))
	}
	if len(snapshot.Warnings) != 1 {
		t.Fatalf("Warnings = %d, want 1", len(snapshot.Warnings))
	}
	if snapshot.Warnings[0].WarningKind != "quicksight_not_subscribed" {
		t.Fatalf("warning kind = %q, want quicksight_not_subscribed", snapshot.Warnings[0].WarningKind)
	}
}

func TestClientGenuineAccessDeniedIsSurfaced(t *testing.T) {
	api := &fakeQuickSightAPI{
		listDataSourcesErr: &awsquicksighttypes.AccessDeniedException{
			Message: awsv2.String("User is not authorized to perform quicksight:ListDataSources"),
		},
	}
	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}

	if _, err := client.Snapshot(context.Background()); err == nil {
		t.Fatalf("Snapshot() error = nil, want a genuine authorization failure surfaced")
	}
}

func TestClientRequiresAccountID(t *testing.T) {
	client := &Client{client: &fakeQuickSightAPI{}, boundary: aws.Boundary{}, accountID: ""}
	if _, err := client.Snapshot(context.Background()); err == nil {
		t.Fatalf("Snapshot() error = nil, want account-id-required error")
	}
}

func TestClientDescribeDataSetAccessDeniedKeepsSummary(t *testing.T) {
	api := &fakeQuickSightAPI{
		dataSets: []awsquicksighttypes.DataSetSummary{{
			Arn:       awsv2.String(dataSetARN),
			DataSetId: awsv2.String("sales"),
			Name:      awsv2.String("Sales"),
		}},
		describeDataSetErr: &awsquicksighttypes.AccessDeniedException{Message: awsv2.String("denied")},
	}
	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.DataSets) != 1 {
		t.Fatalf("DataSets = %d, want 1 (summary retained)", len(snapshot.DataSets))
	}
	if snapshot.DataSets[0].DataSourceARNs != nil {
		t.Fatalf("DataSourceARNs = %#v, want nil when describe is denied", snapshot.DataSets[0].DataSourceARNs)
	}
}

func TestClientPaginatesDataSources(t *testing.T) {
	api := &fakeQuickSightAPI{
		dataSourcePages: [][]awsquicksighttypes.DataSource{
			{{Arn: awsv2.String(dataSourceARN + "-1"), DataSourceId: awsv2.String("ds1"), Type: awsquicksighttypes.DataSourceTypeS3}},
			{{Arn: awsv2.String(dataSourceARN + "-2"), DataSourceId: awsv2.String("ds2"), Type: awsquicksighttypes.DataSourceTypeS3}},
		},
	}
	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}

	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.DataSources) != 2 {
		t.Fatalf("DataSources = %d, want 2 across pages", len(snapshot.DataSources))
	}
}

func TestIsThrottleError(t *testing.T) {
	throttle := &awsquicksighttypes.ThrottlingException{Message: awsv2.String("rate exceeded")}
	if !isThrottleError(throttle) {
		t.Fatalf("isThrottleError(ThrottlingException) = false, want true")
	}
	if isThrottleError(errors.New("plain")) {
		t.Fatalf("isThrottleError(plain) = true, want false")
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceQuickSight,
	}
}

type fakeQuickSightAPI struct {
	dataSources        []awsquicksighttypes.DataSource
	dataSourcePages    [][]awsquicksighttypes.DataSource
	dataSourceCall     int
	listDataSourcesErr error

	vpcConnections []awsquicksighttypes.VPCConnectionSummary

	dataSets           []awsquicksighttypes.DataSetSummary
	dataSetDetail      map[string]*awsquicksighttypes.DataSet
	describeDataSetErr error

	dashboards      []awsquicksighttypes.DashboardSummary
	dashboardDetail map[string]*awsquicksighttypes.Dashboard

	analyses       []awsquicksighttypes.AnalysisSummary
	analysisDetail map[string]*awsquicksighttypes.Analysis

	tags map[string][]awsquicksighttypes.Tag
}

func (f *fakeQuickSightAPI) ListDataSources(
	_ context.Context,
	_ *awsquicksight.ListDataSourcesInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListDataSourcesOutput, error) {
	if f.listDataSourcesErr != nil {
		return nil, f.listDataSourcesErr
	}
	if len(f.dataSourcePages) > 0 {
		if f.dataSourceCall >= len(f.dataSourcePages) {
			return &awsquicksight.ListDataSourcesOutput{}, nil
		}
		page := f.dataSourcePages[f.dataSourceCall]
		f.dataSourceCall++
		out := &awsquicksight.ListDataSourcesOutput{DataSources: page}
		if f.dataSourceCall < len(f.dataSourcePages) {
			out.NextToken = awsv2.String("more")
		}
		return out, nil
	}
	return &awsquicksight.ListDataSourcesOutput{DataSources: f.dataSources}, nil
}

func (f *fakeQuickSightAPI) DescribeDataSource(
	_ context.Context,
	input *awsquicksight.DescribeDataSourceInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.DescribeDataSourceOutput, error) {
	for i := range f.dataSources {
		if awsv2.ToString(f.dataSources[i].DataSourceId) == awsv2.ToString(input.DataSourceId) {
			return &awsquicksight.DescribeDataSourceOutput{DataSource: &f.dataSources[i]}, nil
		}
	}
	return &awsquicksight.DescribeDataSourceOutput{}, nil
}

func (f *fakeQuickSightAPI) ListVPCConnections(
	_ context.Context,
	_ *awsquicksight.ListVPCConnectionsInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListVPCConnectionsOutput, error) {
	return &awsquicksight.ListVPCConnectionsOutput{VPCConnectionSummaries: f.vpcConnections}, nil
}

func (f *fakeQuickSightAPI) ListDataSets(
	_ context.Context,
	_ *awsquicksight.ListDataSetsInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListDataSetsOutput, error) {
	return &awsquicksight.ListDataSetsOutput{DataSetSummaries: f.dataSets}, nil
}

func (f *fakeQuickSightAPI) DescribeDataSet(
	_ context.Context,
	input *awsquicksight.DescribeDataSetInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.DescribeDataSetOutput, error) {
	if f.describeDataSetErr != nil {
		return nil, f.describeDataSetErr
	}
	return &awsquicksight.DescribeDataSetOutput{DataSet: f.dataSetDetail[awsv2.ToString(input.DataSetId)]}, nil
}

func (f *fakeQuickSightAPI) ListDashboards(
	_ context.Context,
	_ *awsquicksight.ListDashboardsInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListDashboardsOutput, error) {
	return &awsquicksight.ListDashboardsOutput{DashboardSummaryList: f.dashboards}, nil
}

func (f *fakeQuickSightAPI) DescribeDashboard(
	_ context.Context,
	input *awsquicksight.DescribeDashboardInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.DescribeDashboardOutput, error) {
	return &awsquicksight.DescribeDashboardOutput{Dashboard: f.dashboardDetail[awsv2.ToString(input.DashboardId)]}, nil
}

func (f *fakeQuickSightAPI) ListAnalyses(
	_ context.Context,
	_ *awsquicksight.ListAnalysesInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListAnalysesOutput, error) {
	return &awsquicksight.ListAnalysesOutput{AnalysisSummaryList: f.analyses}, nil
}

func (f *fakeQuickSightAPI) DescribeAnalysis(
	_ context.Context,
	input *awsquicksight.DescribeAnalysisInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.DescribeAnalysisOutput, error) {
	return &awsquicksight.DescribeAnalysisOutput{Analysis: f.analysisDetail[awsv2.ToString(input.AnalysisId)]}, nil
}

func (f *fakeQuickSightAPI) ListTagsForResource(
	_ context.Context,
	input *awsquicksight.ListTagsForResourceInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListTagsForResourceOutput, error) {
	return &awsquicksight.ListTagsForResourceOutput{Tags: f.tags[awsv2.ToString(input.ResourceArn)]}, nil
}
