// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsquicksight "github.com/aws/aws-sdk-go-v2/service/quicksight"
	awsquicksighttypes "github.com/aws/aws-sdk-go-v2/service/quicksight/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	quicksightservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/quicksight"
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
			Arn:          aws.String(dataSourceARN),
			DataSourceId: aws.String("redshift-prod"),
			Name:         aws.String("Redshift Prod"),
			Type:         awsquicksighttypes.DataSourceTypeRedshift,
			Status:       awsquicksighttypes.ResourceStatusCreationSuccessful,
			SecretArn:    aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:qs-XYZ"),
			VpcConnectionProperties: &awsquicksighttypes.VpcConnectionProperties{
				VpcConnectionArn: aws.String("arn:aws:quicksight:us-east-1:123456789012:vpcConnection/vpc-conn-1"),
			},
			DataSourceParameters: &awsquicksighttypes.DataSourceParametersMemberRedshiftParameters{
				Value: awsquicksighttypes.RedshiftParameters{
					Database:  aws.String("analytics"),
					ClusterId: aws.String("analytics-cluster"),
				},
			},
		}},
		vpcConnections: []awsquicksighttypes.VPCConnectionSummary{{
			VPCConnectionId:  aws.String("vpc-conn-1"),
			SecurityGroupIds: []string{"sg-0a1b2c3d"},
			NetworkInterfaces: []awsquicksighttypes.NetworkInterface{
				{SubnetId: aws.String("subnet-1111")},
				{SubnetId: aws.String("subnet-2222")},
			},
		}},
		dataSets: []awsquicksighttypes.DataSetSummary{{
			Arn:        aws.String(dataSetARN),
			DataSetId:  aws.String("sales"),
			Name:       aws.String("Sales"),
			ImportMode: awsquicksighttypes.DataSetImportModeSpice,
		}},
		dataSetDetail: map[string]*awsquicksighttypes.DataSet{
			"sales": {
				Arn:       aws.String(dataSetARN),
				DataSetId: aws.String("sales"),
				PhysicalTableMap: map[string]awsquicksighttypes.PhysicalTable{
					"t1": &awsquicksighttypes.PhysicalTableMemberRelationalTable{
						Value: awsquicksighttypes.RelationalTable{
							DataSourceArn: aws.String(dataSourceARN),
							Name:          aws.String("public.sales"),
						},
					},
					"t2": &awsquicksighttypes.PhysicalTableMemberCustomSql{
						Value: awsquicksighttypes.CustomSql{
							DataSourceArn: aws.String(dataSourceARN),
							Name:          aws.String("custom"),
							SqlQuery:      aws.String("SELECT secret FROM private.credentials"),
						},
					},
				},
			},
		},
		dashboards: []awsquicksighttypes.DashboardSummary{{
			Arn:                    aws.String(dashboardARN),
			DashboardId:            aws.String("exec"),
			Name:                   aws.String("Exec"),
			PublishedVersionNumber: aws.Int64(3),
		}},
		dashboardDetail: map[string]*awsquicksighttypes.Dashboard{
			"exec": {
				Arn: aws.String(dashboardARN),
				Version: &awsquicksighttypes.DashboardVersion{
					DataSetArns: []string{dataSetARN},
				},
			},
		},
		analyses: []awsquicksighttypes.AnalysisSummary{{
			Arn:        aws.String(analysisARN),
			AnalysisId: aws.String("explore"),
			Name:       aws.String("Explore"),
			Status:     awsquicksighttypes.ResourceStatusCreationSuccessful,
		}},
		analysisDetail: map[string]*awsquicksighttypes.Analysis{
			"explore": {
				Arn:         aws.String(analysisARN),
				DataSetArns: []string{dataSetARN},
			},
		},
		tags: map[string][]awsquicksighttypes.Tag{
			dataSourceARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
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
			Message: aws.String("Account 123456789012 is not signed up for QuickSight"),
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
			Message: aws.String("User is not authorized to perform quicksight:ListDataSources"),
		},
	}
	client := &Client{client: api, boundary: testBoundary(), accountID: "123456789012"}

	if _, err := client.Snapshot(context.Background()); err == nil {
		t.Fatalf("Snapshot() error = nil, want a genuine authorization failure surfaced")
	}
}

func TestClientRequiresAccountID(t *testing.T) {
	client := &Client{client: &fakeQuickSightAPI{}, boundary: awscloud.Boundary{}, accountID: ""}
	if _, err := client.Snapshot(context.Background()); err == nil {
		t.Fatalf("Snapshot() error = nil, want account-id-required error")
	}
}

func TestClientDescribeDataSetAccessDeniedKeepsSummary(t *testing.T) {
	api := &fakeQuickSightAPI{
		dataSets: []awsquicksighttypes.DataSetSummary{{
			Arn:       aws.String(dataSetARN),
			DataSetId: aws.String("sales"),
			Name:      aws.String("Sales"),
		}},
		describeDataSetErr: &awsquicksighttypes.AccessDeniedException{Message: aws.String("denied")},
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
			{{Arn: aws.String(dataSourceARN + "-1"), DataSourceId: aws.String("ds1"), Type: awsquicksighttypes.DataSourceTypeS3}},
			{{Arn: aws.String(dataSourceARN + "-2"), DataSourceId: aws.String("ds2"), Type: awsquicksighttypes.DataSourceTypeS3}},
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
	throttle := &awsquicksighttypes.ThrottlingException{Message: aws.String("rate exceeded")}
	if !isThrottleError(throttle) {
		t.Fatalf("isThrottleError(ThrottlingException) = false, want true")
	}
	if isThrottleError(errors.New("plain")) {
		t.Fatalf("isThrottleError(plain) = true, want false")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceQuickSight,
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
			out.NextToken = aws.String("more")
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
		if aws.ToString(f.dataSources[i].DataSourceId) == aws.ToString(input.DataSourceId) {
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
	return &awsquicksight.DescribeDataSetOutput{DataSet: f.dataSetDetail[aws.ToString(input.DataSetId)]}, nil
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
	return &awsquicksight.DescribeDashboardOutput{Dashboard: f.dashboardDetail[aws.ToString(input.DashboardId)]}, nil
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
	return &awsquicksight.DescribeAnalysisOutput{Analysis: f.analysisDetail[aws.ToString(input.AnalysisId)]}, nil
}

func (f *fakeQuickSightAPI) ListTagsForResource(
	_ context.Context,
	input *awsquicksight.ListTagsForResourceInput,
	_ ...func(*awsquicksight.Options),
) (*awsquicksight.ListTagsForResourceOutput, error) {
	return &awsquicksight.ListTagsForResourceOutput{Tags: f.tags[aws.ToString(input.ResourceArn)]}, nil
}
