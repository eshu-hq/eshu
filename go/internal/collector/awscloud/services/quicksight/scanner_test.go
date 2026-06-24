// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quicksight

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testRedshiftDataSourceARN = "arn:aws:quicksight:us-east-1:123456789012:datasource/redshift-prod"
	testS3DataSourceARN       = "arn:aws:quicksight:us-east-1:123456789012:datasource/s3-manifest"
	testDataSetARN            = "arn:aws:quicksight:us-east-1:123456789012:dataset/sales"
	testDashboardARN          = "arn:aws:quicksight:us-east-1:123456789012:dashboard/exec"
	testAnalysisARN           = "arn:aws:quicksight:us-east-1:123456789012:analysis/explore"
	testVPCConnectionARN      = "arn:aws:quicksight:us-east-1:123456789012:vpcConnection/vpc-conn-1"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		DataSources: []DataSource{
			{
				ARN:              testRedshiftDataSourceARN,
				ID:               "redshift-prod",
				Name:             "Redshift Prod",
				Type:             "REDSHIFT",
				Status:           "CREATION_SUCCESSFUL",
				SecretConfigured: true,
				VPCConnectionARN: testVPCConnectionARN,
				Backing:          BackingStore{Kind: BackingStoreRedshiftCluster, Identifier: "analytics-cluster"},
				CreatedTime:      time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
				Tags:             map[string]string{"Environment": "prod"},
			},
			{
				ARN:     testS3DataSourceARN,
				ID:      "s3-manifest",
				Name:    "S3 Manifest",
				Type:    "S3",
				Status:  "CREATION_SUCCESSFUL",
				Backing: BackingStore{Kind: BackingStoreS3Bucket, Identifier: "analytics-manifests"},
			},
		},
		DataSets: []DataSet{{
			ARN:            testDataSetARN,
			ID:             "sales",
			Name:           "Sales",
			ImportMode:     "SPICE",
			DataSourceARNs: []string{testRedshiftDataSourceARN, testRedshiftDataSourceARN},
		}},
		Dashboards: []Dashboard{{
			ARN:                    testDashboardARN,
			ID:                     "exec",
			Name:                   "Exec",
			PublishedVersionNumber: 3,
			DataSetARNs:            []string{testDataSetARN},
		}},
		Analyses: []Analysis{{
			ARN:         testAnalysisARN,
			ID:          "explore",
			Name:        "Explore",
			Status:      "CREATION_SUCCESSFUL",
			DataSetARNs: []string{testDataSetARN},
		}},
		VPCConnections: map[string]VPCConnection{
			"vpc-conn-1": {
				SecurityGroupIDs: []string{"sg-0a1b2c3d", "sg-0a1b2c3d"},
				SubnetIDs:        []string{"subnet-1111", "subnet-2222"},
			},
		},
	}
}

func TestScannerEmitsQuickSightMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	dataSource := resourceByID(t, envelopes, awscloud.ResourceTypeQuickSightDataSource, testRedshiftDataSourceARN)
	if got, want := dataSource.Payload["state"], "CREATION_SUCCESSFUL"; got != want {
		t.Fatalf("data source state = %#v, want %q", got, want)
	}
	dsAttrs := attributesOf(t, dataSource)
	assertAttribute(t, dsAttrs, "connector_type", "REDSHIFT")
	assertAttribute(t, dsAttrs, "secret_configured", true)
	assertAttribute(t, dsAttrs, "backing_store_kind", "redshift_cluster")
	assertAttribute(t, dsAttrs, "backing_store_identifier", "analytics-cluster")

	dataSet := resourceByType(t, envelopes, awscloud.ResourceTypeQuickSightDataSet)
	assertAttribute(t, attributesOf(t, dataSet), "import_mode", "SPICE")
	dashboard := resourceByType(t, envelopes, awscloud.ResourceTypeQuickSightDashboard)
	assertAttribute(t, attributesOf(t, dashboard), "published_version_number", int64(3))
	resourceByType(t, envelopes, awscloud.ResourceTypeQuickSightAnalysis)

	// data source -> Redshift cluster, keyed by the bare cluster id the Redshift
	// scanner publishes as a fallback resource_id.
	redshiftEdge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightDataSourceUsesRedshiftCluster)
	assertEdgeTarget(t, redshiftEdge, awscloud.ResourceTypeRedshiftCluster, "analytics-cluster")
	if got, want := redshiftEdge.Payload["source_resource_id"], testRedshiftDataSourceARN; got != want {
		t.Fatalf("redshift edge source_resource_id = %#v, want %q", got, want)
	}

	// data source -> S3 bucket, keyed by the synthesized partition-aware ARN.
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightDataSourceUsesS3Bucket)
	assertEdgeTarget(t, s3Edge, awscloud.ResourceTypeS3Bucket, "arn:aws:s3:::analytics-manifests")
	if got, want := s3Edge.Payload["target_arn"], "arn:aws:s3:::analytics-manifests"; got != want {
		t.Fatalf("s3 edge target_arn = %#v, want %q", got, want)
	}

	// data source -> security group / subnet (deduped, bare ids).
	sgEdges := relationshipsByType(envelopes, awscloud.RelationshipQuickSightDataSourceUsesSecurityGroup)
	if len(sgEdges) != 1 {
		t.Fatalf("security group edges = %d, want 1 (deduped)", len(sgEdges))
	}
	assertEdgeTarget(t, sgEdges[0], awscloud.ResourceTypeEC2SecurityGroup, "sg-0a1b2c3d")
	subnetEdges := relationshipsByType(envelopes, awscloud.RelationshipQuickSightDataSourceUsesSubnet)
	if len(subnetEdges) != 2 {
		t.Fatalf("subnet edges = %d, want 2", len(subnetEdges))
	}

	// data set -> data source (internal), deduped to one despite duplicate input.
	dsEdges := relationshipsByType(envelopes, awscloud.RelationshipQuickSightDataSetReadsDataSource)
	if len(dsEdges) != 1 {
		t.Fatalf("data set -> data source edges = %d, want 1 (deduped)", len(dsEdges))
	}
	assertEdgeTarget(t, dsEdges[0], awscloud.ResourceTypeQuickSightDataSource, testRedshiftDataSourceARN)

	// dashboard/analysis -> data set (internal).
	dashEdge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightDashboardReadsDataSet)
	assertEdgeTarget(t, dashEdge, awscloud.ResourceTypeQuickSightDataSet, testDataSetARN)
	analysisEdge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightAnalysisReadsDataSet)
	assertEdgeTarget(t, analysisEdge, awscloud.ResourceTypeQuickSightDataSet, testDataSetARN)

	// Metadata-only: no secrets, SQL, or visual definitions leak into payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"credentials", "password", "secret_arn", "credential_pair",
			"sql", "custom_sql", "query", "definition", "visual_definition",
			"data_source_parameters", "alternate_data_source_parameters",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; QuickSight scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	snapshot := Snapshot{DataSources: []DataSource{{
		ARN:     "arn:aws-us-gov:quicksight:us-gov-west-1:123456789012:datasource/s3",
		ID:      "s3",
		Type:    "S3",
		Backing: BackingStore{Kind: BackingStoreS3Bucket, Identifier: "gov-manifests"},
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightDataSourceUsesS3Bucket)
	if got, want := s3Edge.Payload["target_resource_id"], "arn:aws-us-gov:s3:::gov-manifests"; got != want {
		t.Fatalf("GovCloud s3 edge target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerEmitsRDSAndAthenaBackingEdges(t *testing.T) {
	snapshot := Snapshot{DataSources: []DataSource{
		{
			ARN:     "arn:aws:quicksight:us-east-1:123456789012:datasource/rds",
			ID:      "rds",
			Type:    "RDS",
			Backing: BackingStore{Kind: BackingStoreRDSInstance, Identifier: "prod-db"},
		},
		{
			ARN:     "arn:aws:quicksight:us-east-1:123456789012:datasource/athena",
			ID:      "athena",
			Type:    "ATHENA",
			Backing: BackingStore{Kind: BackingStoreAthenaWorkGroup, Identifier: "analytics-wg"},
		},
	}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rdsEdge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightDataSourceUsesRDSInstance)
	assertEdgeTarget(t, rdsEdge, awscloud.ResourceTypeRDSDBInstance, "prod-db")
	athenaEdge := relationshipByType(t, envelopes, awscloud.RelationshipQuickSightDataSourceUsesAthenaWorkGroup)
	assertEdgeTarget(t, athenaEdge, awscloud.ResourceTypeAthenaWorkGroup, "analytics-wg")
}

func TestScannerOmitsBackingEdgeForUnscannedConnector(t *testing.T) {
	snapshot := Snapshot{DataSources: []DataSource{{
		ARN:     "arn:aws:quicksight:us-east-1:123456789012:datasource/snowflake",
		ID:      "snowflake",
		Type:    "SNOWFLAKE",
		Backing: BackingStore{Kind: BackingStoreNone},
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship for unscanned connector: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsVPCEdgesWhenConnectionUnresolved(t *testing.T) {
	snapshot := Snapshot{DataSources: []DataSource{{
		ARN:              testRedshiftDataSourceARN,
		ID:               "redshift-prod",
		Type:             "REDSHIFT",
		VPCConnectionARN: testVPCConnectionARN,
		Backing:          BackingStore{Kind: BackingStoreRedshiftCluster, Identifier: "analytics-cluster"},
		// VPCConnections map intentionally empty: the connection did not resolve.
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := relationshipsByType(envelopes, awscloud.RelationshipQuickSightDataSourceUsesSecurityGroup); len(got) != 0 {
		t.Fatalf("security group edges = %d, want 0 when connection unresolved", len(got))
	}
	if got := relationshipsByType(envelopes, awscloud.RelationshipQuickSightDataSourceUsesSubnet); len(got) != 0 {
		t.Fatalf("subnet edges = %d, want 0 when connection unresolved", len(got))
	}
}

func TestScannerEmptyAccountReturnsNoEnvelopes(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() returned %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerSurfacesNotSubscribedWarning(t *testing.T) {
	snapshot := Snapshot{Warnings: []awscloud.WarningObservation{{
		Boundary:       testBoundary(),
		WarningKind:    "quicksight_not_subscribed",
		ErrorClass:     "ResourceNotFoundException",
		Message:        "account is not signed up for Amazon QuickSight",
		SourceRecordID: "quicksight_not_subscribed:123456789012",
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, "quicksight_not_subscribed")
	if got := warning.Payload["error_class"]; got != "ResourceNotFoundException" {
		t.Fatalf("warning error_class = %#v, want ResourceNotFoundException", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	var observations []awscloud.RelationshipObservation
	for _, dataSource := range fullSnapshot().DataSources {
		if rel := dataSourceBackingRelationship(boundary, dataSource); rel != nil {
			observations = append(observations, *rel)
		}
		observations = append(observations, dataSourceVPCRelationships(boundary, dataSource, fullSnapshot().VPCConnections)...)
	}
	for _, dataSet := range fullSnapshot().DataSets {
		observations = append(observations, dataSetDataSourceRelationships(boundary, dataSet)...)
	}
	for _, dashboard := range fullSnapshot().Dashboards {
		observations = append(observations, dashboardDataSetRelationships(boundary, dashboard)...)
	}
	for _, analysis := range fullSnapshot().Analyses {
		observations = append(observations, analysisDataSetRelationships(boundary, analysis)...)
	}
	if len(observations) == 0 {
		t.Fatalf("expected relationship observations for the full fixture")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceQuickSight,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:quicksight:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceType, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		gotType, _ := envelope.Payload["resource_type"].(string)
		gotID, _ := envelope.Payload["resource_id"].(string)
		if gotType == resourceType && gotID == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q resource_id %q", resourceType, resourceID)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	edges := relationshipsByType(envelopes, relationshipType)
	if len(edges) == 0 {
		t.Fatalf("missing relationship_type %q", relationshipType)
	}
	return edges[0]
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			out = append(out, envelope)
		}
	}
	return out
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q", warningKind)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
