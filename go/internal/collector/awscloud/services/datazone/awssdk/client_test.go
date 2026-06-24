// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatazone "github.com/aws/aws-sdk-go-v2/service/datazone"
	awsdatazonetypes "github.com/aws/aws-sdk-go-v2/service/datazone/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	datazoneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/datazone"
)

func TestClientSnapshotsDatazoneMetadataOnly(t *testing.T) {
	domainARN := "arn:aws:datazone:us-east-1:123456789012:domain/dzd_abc123"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"
	roleARN := "arn:aws:iam::123456789012:role/AmazonDataZoneDomainExecution"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeDatazoneAPI{
		domainPages: []*awsdatazone.ListDomainsOutput{{
			Items: []awsdatazonetypes.DomainSummary{{
				Arn:       aws.String(domainARN),
				Id:        aws.String("dzd_abc123"),
				Name:      aws.String("analytics"),
				Status:    awsdatazonetypes.DomainStatusAvailable,
				CreatedAt: aws.Time(createdAt),
			}},
		}},
		getDomain: map[string]*awsdatazone.GetDomainOutput{
			"dzd_abc123": {
				Id:                  aws.String("dzd_abc123"),
				KmsKeyIdentifier:    aws.String(kmsARN),
				DomainExecutionRole: aws.String(roleARN),
				ServiceRole:         aws.String("arn:aws:iam::123456789012:role/service-role/AmazonDataZoneService"),
				Tags:                map[string]string{"Environment": "prod"},
			},
		},
		projectPages: map[string][]*awsdatazone.ListProjectsOutput{
			"dzd_abc123": {{
				Items: []awsdatazonetypes.ProjectSummary{{
					Id:            aws.String("prj_xyz789"),
					DomainId:      aws.String("dzd_abc123"),
					Name:          aws.String("sales-analytics"),
					ProjectStatus: awsdatazonetypes.ProjectStatusActive,
				}},
			}},
		},
		environmentPages: map[string][]*awsdatazone.ListEnvironmentsOutput{
			"dzd_abc123/prj_xyz789": {{
				Items: []awsdatazonetypes.EnvironmentSummary{{
					Id:        aws.String("env_def456"),
					DomainId:  aws.String("dzd_abc123"),
					ProjectId: aws.String("prj_xyz789"),
					Name:      aws.String("prod-env"),
					Provider:  aws.String("Amazon DataZone"),
					Status:    awsdatazonetypes.EnvironmentStatusActive,
				}},
			}},
		},
		dataSourcePages: map[string][]*awsdatazone.ListDataSourcesOutput{
			"dzd_abc123/prj_xyz789": {{
				Items: []awsdatazonetypes.DataSourceSummary{
					{
						DataSourceId:  aws.String("dz_glue_source"),
						DomainId:      aws.String("dzd_abc123"),
						Name:          aws.String("glue-catalog"),
						Type:          aws.String("GLUE"),
						Status:        awsdatazonetypes.DataSourceStatusReady,
						EnableSetting: awsdatazonetypes.EnableSettingEnabled,
					},
					{
						DataSourceId:  aws.String("dz_redshift_source"),
						DomainId:      aws.String("dzd_abc123"),
						Name:          aws.String("redshift-warehouse"),
						Type:          aws.String("REDSHIFT"),
						Status:        awsdatazonetypes.DataSourceStatusReady,
						EnableSetting: awsdatazonetypes.EnableSettingEnabled,
					},
				},
			}},
		},
		getDataSource: map[string]*awsdatazone.GetDataSourceOutput{
			"dz_glue_source": {
				Id:        aws.String("dz_glue_source"),
				ProjectId: aws.String("prj_xyz789"),
				Configuration: &awsdatazonetypes.DataSourceConfigurationOutputMemberGlueRunConfiguration{
					Value: awsdatazonetypes.GlueRunConfigurationOutput{
						RelationalFilterConfigurations: []awsdatazonetypes.RelationalFilterConfiguration{
							{DatabaseName: aws.String("sales_db")},
						},
					},
				},
			},
			"dz_redshift_source": {
				Id:        aws.String("dz_redshift_source"),
				ProjectId: aws.String("prj_xyz789"),
				Configuration: &awsdatazonetypes.DataSourceConfigurationOutputMemberRedshiftRunConfiguration{
					Value: awsdatazonetypes.RedshiftRunConfigurationOutput{
						AccountId: aws.String("123456789012"),
						Region:    aws.String("us-east-1"),
						RedshiftStorage: &awsdatazonetypes.RedshiftStorageMemberRedshiftClusterSource{
							Value: awsdatazonetypes.RedshiftClusterStorage{
								ClusterName: aws.String("analytics-cluster"),
							},
						},
					},
				},
			},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Domains) != 1 {
		t.Fatalf("len(Domains) = %d, want 1", len(snapshot.Domains))
	}
	domain := snapshot.Domains[0]
	if domain.KMSKeyIdentifier != kmsARN {
		t.Fatalf("domain KMSKeyIdentifier = %q, want %q", domain.KMSKeyIdentifier, kmsARN)
	}
	if domain.DomainExecutionRole != roleARN {
		t.Fatalf("domain DomainExecutionRole = %q, want %q", domain.DomainExecutionRole, roleARN)
	}
	if domain.Tags["Environment"] != "prod" {
		t.Fatalf("domain tag Environment = %q, want prod", domain.Tags["Environment"])
	}
	if len(domain.Projects) != 1 || domain.Projects[0].ID != "prj_xyz789" {
		t.Fatalf("projects = %#v, want one prj_xyz789", domain.Projects)
	}
	if len(domain.Environments) != 1 || domain.Environments[0].ID != "env_def456" {
		t.Fatalf("environments = %#v, want one env_def456", domain.Environments)
	}
	if len(domain.DataSources) != 2 {
		t.Fatalf("len(DataSources) = %d, want 2", len(domain.DataSources))
	}
	glue := dataSourceByID(t, domain.DataSources, "dz_glue_source")
	if len(glue.GlueDatabaseNames) != 1 || glue.GlueDatabaseNames[0] != "sales_db" {
		t.Fatalf("glue database names = %#v, want [sales_db]", glue.GlueDatabaseNames)
	}
	if glue.ProjectID != "prj_xyz789" {
		t.Fatalf("glue ProjectID = %q, want prj_xyz789", glue.ProjectID)
	}
	redshift := dataSourceByID(t, domain.DataSources, "dz_redshift_source")
	if redshift.RedshiftClusterName != "analytics-cluster" {
		t.Fatalf("redshift cluster name = %q, want analytics-cluster", redshift.RedshiftClusterName)
	}
	if redshift.BackingAccountID != "123456789012" || redshift.BackingRegion != "us-east-1" {
		t.Fatalf("redshift backing account/region = %q/%q, want 123456789012/us-east-1", redshift.BackingAccountID, redshift.BackingRegion)
	}
}

func TestClientHandlesEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeDatazoneAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Domains) != 0 {
		t.Fatalf("len(Domains) = %d, want 0 for empty account", len(snapshot.Domains))
	}
}

func dataSourceByID(t *testing.T, sources []datazoneservice.DataSource, id string) datazoneservice.DataSource {
	t.Helper()
	for _, source := range sources {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("missing data source %q", id)
	return datazoneservice.DataSource{}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDatazone,
	}
}
