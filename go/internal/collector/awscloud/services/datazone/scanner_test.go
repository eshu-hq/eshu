// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datazone

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testDomainARN      = "arn:aws:datazone:us-east-1:123456789012:domain/dzd_abc123"
	testDomainID       = "dzd_abc123"
	testKMSARN         = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testExecutionRole  = "arn:aws:iam::123456789012:role/AmazonDataZoneDomainExecution"
	testServiceRole    = "arn:aws:iam::123456789012:role/service-role/AmazonDataZoneService"
	testProjectID      = "prj_xyz789"
	testEnvironmentID  = "env_def456"
	testGlueDataSource = "dz_glue_source"
	testRsDataSource   = "dz_redshift_source"
)

func fullDomain() Domain {
	return Domain{
		ARN:                 testDomainARN,
		ID:                  testDomainID,
		Name:                "analytics",
		Status:              "AVAILABLE",
		KMSKeyIdentifier:    testKMSARN,
		DomainExecutionRole: testExecutionRole,
		ServiceRole:         testServiceRole,
		CreatedAt:           time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastUpdatedAt:       time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		Tags:                map[string]string{"Environment": "prod"},
		Projects: []Project{{
			ID:       testProjectID,
			DomainID: testDomainID,
			Name:     "sales-analytics",
			Status:   "ACTIVE",
		}},
		Environments: []Environment{{
			ID:           testEnvironmentID,
			DomainID:     testDomainID,
			ProjectID:    testProjectID,
			Name:         "prod-env",
			Provider:     "Amazon DataZone",
			Status:       "ACTIVE",
			AWSAccountID: "123456789012",
		}},
		DataSources: []DataSource{
			{
				ID:                testGlueDataSource,
				DomainID:          testDomainID,
				ProjectID:         testProjectID,
				Name:              "glue-catalog",
				Type:              "GLUE",
				Status:            "READY",
				Enabled:           true,
				GlueDatabaseNames: []string{"sales_db"},
			},
			{
				ID:                  testRsDataSource,
				DomainID:            testDomainID,
				ProjectID:           testProjectID,
				Name:                "redshift-warehouse",
				Type:                "REDSHIFT",
				Status:              "READY",
				Enabled:             true,
				RedshiftClusterName: "analytics-cluster",
			},
		},
	}
}

func TestScannerEmitsDatazoneMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Domains: []Domain{fullDomain()}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	domain := resourceByType(t, envelopes, awscloud.ResourceTypeDatazoneDomain)
	if got, want := domain.Payload["resource_id"], testDomainID; got != want {
		t.Fatalf("domain resource_id = %#v, want %q", got, want)
	}
	if got, want := domain.Payload["arn"], testDomainARN; got != want {
		t.Fatalf("domain arn = %#v, want %q", got, want)
	}
	domainAttrs := attributesOf(t, domain)
	assertAttribute(t, domainAttrs, "kms_key_identifier", testKMSARN)
	assertAttribute(t, domainAttrs, "domain_execution_role", testExecutionRole)

	project := resourceByType(t, envelopes, awscloud.ResourceTypeDatazoneProject)
	if got, want := project.Payload["resource_id"], testProjectID; got != want {
		t.Fatalf("project resource_id = %#v, want %q", got, want)
	}
	environment := resourceByType(t, envelopes, awscloud.ResourceTypeDatazoneEnvironment)
	if got, want := environment.Payload["resource_id"], testEnvironmentID; got != want {
		t.Fatalf("environment resource_id = %#v, want %q", got, want)
	}

	// domain -> KMS key edge.
	dbKMS := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDomainUsesKMSKey)
	assertEdgeTarget(t, dbKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := dbKMS.Payload["source_resource_id"], testDomainID; got != want {
		t.Fatalf("domain->kms source_resource_id = %#v, want %q", got, want)
	}

	// domain -> IAM role edges (execution + service): both target the IAM role ARN.
	roleEdges := relationshipsByType(envelopes, awscloud.RelationshipDatazoneDomainUsesIAMRole)
	if len(roleEdges) != 2 {
		t.Fatalf("domain->iam edge count = %d, want 2", len(roleEdges))
	}
	roleTargets := map[string]bool{}
	for _, edge := range roleEdges {
		assertEdgeTarget(t, edge, awscloud.ResourceTypeIAMRole, edge.Payload["target_resource_id"].(string))
		roleTargets[edge.Payload["target_resource_id"].(string)] = true
	}
	if !roleTargets[testExecutionRole] || !roleTargets[testServiceRole] {
		t.Fatalf("domain->iam targets = %#v, want execution+service role ARNs", roleTargets)
	}

	// project -> domain edge keyed by domain id.
	projInDomain := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneProjectInDomain)
	assertEdgeTarget(t, projInDomain, awscloud.ResourceTypeDatazoneDomain, testDomainID)
	if got, want := projInDomain.Payload["source_resource_id"], testProjectID; got != want {
		t.Fatalf("project->domain source_resource_id = %#v, want %q", got, want)
	}

	// environment -> domain edge.
	envInDomain := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneEnvironmentInDomain)
	assertEdgeTarget(t, envInDomain, awscloud.ResourceTypeDatazoneDomain, testDomainID)

	// data source -> domain edge.
	dsInDomain := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDataSourceInDomain)
	assertEdgeTarget(t, dsInDomain, awscloud.ResourceTypeDatazoneDomain, testDomainID)

	// data source -> Glue database edge keyed by database name.
	glueEdge := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDataSourceBacksGlueDatabase)
	assertEdgeTarget(t, glueEdge, awscloud.ResourceTypeGlueDatabase, "sales_db")
	if got, want := glueEdge.Payload["source_resource_id"], testGlueDataSource; got != want {
		t.Fatalf("data_source->glue source_resource_id = %#v, want %q", got, want)
	}

	// data source -> Redshift cluster edge keyed by synthesized partition-aware ARN.
	rsEdge := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDataSourceBacksRedshiftCluster)
	wantClusterARN := "arn:aws:redshift:us-east-1:123456789012:cluster:analytics-cluster"
	assertEdgeTarget(t, rsEdge, awscloud.ResourceTypeRedshiftCluster, wantClusterARN)
	if got, want := rsEdge.Payload["target_arn"], wantClusterARN; got != want {
		t.Fatalf("data_source->redshift target_arn = %#v, want %q", got, want)
	}

	// No glossary / asset / subscription content leakage in any resource payload.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"glossary", "glossary_terms", "assets", "asset_content", "listing",
			"subscriptions", "filter_expressions", "credentials", "rows", "data_points",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; DataZone scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudRedshiftClusterARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	domain := fullDomain()
	domain.ARN = "arn:aws-us-gov:datazone:us-gov-west-1:123456789012:domain/dzd_abc123"
	envelopes, err := (Scanner{Client: fakeClient{snapshot: Snapshot{Domains: []Domain{domain}}}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rsEdge := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDataSourceBacksRedshiftCluster)
	wantARN := "arn:aws-us-gov:redshift:us-gov-west-1:123456789012:cluster:analytics-cluster"
	if got := rsEdge.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud redshift target_resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesCrossAccountRedshiftClusterARN(t *testing.T) {
	domain := fullDomain()
	domain.DataSources = []DataSource{{
		ID:                  testRsDataSource,
		DomainID:            testDomainID,
		ProjectID:           testProjectID,
		Name:                "redshift-warehouse",
		Type:                "REDSHIFT",
		RedshiftClusterName: "shared-cluster",
		BackingAccountID:    "999988887777",
		BackingRegion:       "us-west-2",
	}}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: Snapshot{Domains: []Domain{domain}}}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rsEdge := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDataSourceBacksRedshiftCluster)
	wantARN := "arn:aws:redshift:us-west-2:999988887777:cluster:shared-cluster"
	if got := rsEdge.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("cross-account redshift target_resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Domains: []Domain{{
		ARN:    testDomainARN,
		ID:     testDomainID,
		Name:   "analytics",
		Status: "AVAILABLE",
		// No KMS, no roles, no children: only the domain resource fact.
	}}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsNonRoleIAMTargetAndAliasKeyKMS(t *testing.T) {
	domain := Domain{
		ARN:                 testDomainARN,
		ID:                  testDomainID,
		Name:                "analytics",
		KMSKeyIdentifier:    "alias/datazone-key",
		DomainExecutionRole: "arn:aws:iam::123456789012:user/not-a-role",
	}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: Snapshot{Domains: []Domain{domain}}}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	// KMS edge keeps the alias value but no ARN.
	dbKMS := relationshipByType(t, envelopes, awscloud.RelationshipDatazoneDomainUsesKMSKey)
	if got, want := dbKMS.Payload["target_resource_id"], "alias/datazone-key"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := dbKMS.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for alias identifier", got)
	}
	// A non-role principal must not produce an IAM role edge.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == awscloud.RelationshipDatazoneDomainUsesIAMRole {
			t.Fatalf("non-role principal produced an IAM role edge: %#v", envelope.Payload)
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	domain := fullDomain()
	domainID := domainResourceID(domain)
	var observations []awscloud.RelationshipObservation
	observations = append(observations, domainRelationships(boundary, domain)...)
	for _, project := range domain.Projects {
		if rel := childInDomainRelationship(boundary, awscloud.RelationshipDatazoneProjectInDomain, project.ID, "", domainID); rel != nil {
			observations = append(observations, *rel)
		}
	}
	for _, env := range domain.Environments {
		if rel := childInDomainRelationship(boundary, awscloud.RelationshipDatazoneEnvironmentInDomain, env.ID, "", domainID); rel != nil {
			observations = append(observations, *rel)
		}
	}
	for _, ds := range domain.DataSources {
		if rel := childInDomainRelationship(boundary, awscloud.RelationshipDatazoneDataSourceInDomain, ds.ID, "", domainID); rel != nil {
			observations = append(observations, *rel)
		}
		observations = append(observations, dataSourceGlueRelationships(boundary, ds)...)
		if rel := dataSourceRedshiftRelationship(boundary, ds); rel != nil {
			observations = append(observations, *rel)
		}
	}
	if len(observations) == 0 {
		t.Fatalf("expected relationship observations for fully populated fixture")
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Domains: []Domain{{ARN: testDomainARN, ID: testDomainID, Name: "analytics"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "DataZone ListProjects throttled after SDK retries; project metadata omitted for this scan",
			SourceRecordID: "datazone_projects_throttled",
		}},
	}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}
