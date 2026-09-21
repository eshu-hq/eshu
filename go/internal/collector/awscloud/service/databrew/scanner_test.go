// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package databrew

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testDatasetARN = "arn:aws:databrew:us-east-1:123456789012:dataset/sales"
	testRecipeARN  = "arn:aws:databrew:us-east-1:123456789012:recipe/clean-sales"
	testJobARN     = "arn:aws:databrew:us-east-1:123456789012:job/profile-sales"
	testProjectARN = "arn:aws:databrew:us-east-1:123456789012:project/sales-prep"
	testRoleARN    = "arn:aws:iam::123456789012:role/databrew-service-role"
)

func TestScannerEmitsDatabrewMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Datasets: []Dataset{{
			Name:             "sales",
			ARN:              testDatasetARN,
			SourceKind:       "S3",
			Format:           "CSV",
			S3Bucket:         "sales-input-bucket",
			S3Key:            "raw/sales/",
			CreateDate:       time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			LastModifiedDate: time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
			Tags:             map[string]string{"Environment": "prod"},
		}},
		Recipes: []Recipe{{
			Name:        "clean-sales",
			ARN:         testRecipeARN,
			Version:     "1.0",
			ProjectName: "sales-prep",
			StepCount:   3,
			Tags:        map[string]string{"Team": "data"},
		}},
		Jobs: []Job{{
			Name:            "profile-sales",
			ARN:             testJobARN,
			Type:            "PROFILE",
			DatasetName:     "sales",
			RecipeName:      "clean-sales",
			RoleARN:         testRoleARN,
			EncryptionMode:  "SSE-KMS",
			OutputS3Buckets: []string{"sales-output-bucket"},
		}},
		Projects: []Project{{
			Name:        "sales-prep",
			ARN:         testProjectARN,
			DatasetName: "sales",
			RecipeName:  "clean-sales",
			RoleARN:     testRoleARN,
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Dataset resource node keyed by name (so internal job/project edges join).
	dataset := resourceByType(t, envelopes, awscloud.ResourceTypeDatabrewDataset)
	if got, want := dataset.Payload["resource_id"], "sales"; got != want {
		t.Fatalf("dataset resource_id = %#v, want %q", got, want)
	}
	if got, want := dataset.Payload["arn"], testDatasetARN; got != want {
		t.Fatalf("dataset arn = %#v, want %q", got, want)
	}
	datasetAttrs := attributesOf(t, dataset)
	assertAttribute(t, datasetAttrs, "source_kind", "S3")
	assertAttribute(t, datasetAttrs, "format", "CSV")

	// Recipe resource node keyed by name. Step count only, never step bodies.
	recipe := resourceByType(t, envelopes, awscloud.ResourceTypeDatabrewRecipe)
	if got, want := recipe.Payload["resource_id"], "clean-sales"; got != want {
		t.Fatalf("recipe resource_id = %#v, want %q", got, want)
	}
	recipeAttrs := attributesOf(t, recipe)
	assertAttribute(t, recipeAttrs, "step_count", 3)
	assertAttribute(t, recipeAttrs, "version", "1.0")

	// Job resource node keyed by ARN.
	job := resourceByType(t, envelopes, awscloud.ResourceTypeDatabrewJob)
	if got, want := job.Payload["resource_id"], testJobARN; got != want {
		t.Fatalf("job resource_id = %#v, want %q", got, want)
	}
	jobAttrs := attributesOf(t, job)
	assertAttribute(t, jobAttrs, "type", "PROFILE")

	// Project resource node keyed by ARN.
	project := resourceByType(t, envelopes, awscloud.ResourceTypeDatabrewProject)
	if got, want := project.Payload["resource_id"], testProjectARN; got != want {
		t.Fatalf("project resource_id = %#v, want %q", got, want)
	}

	// dataset -> S3 bucket edge, keyed by synthesized partition-aware ARN.
	datasetS3 := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewDatasetReadsS3)
	wantBucketARN := "arn:aws:s3:::sales-input-bucket"
	assertEdgeTarget(t, datasetS3, awscloud.ResourceTypeS3Bucket, wantBucketARN)
	if got, want := datasetS3.Payload["source_resource_id"], "sales"; got != want {
		t.Fatalf("dataset->s3 source_resource_id = %#v, want %q", got, want)
	}

	// job -> IAM role edge, keyed by the role ARN the IAM scanner publishes.
	jobRole := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewJobAssumesRole)
	assertEdgeTarget(t, jobRole, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got, want := jobRole.Payload["target_arn"], testRoleARN; got != want {
		t.Fatalf("job->role target_arn = %#v, want %q", got, want)
	}

	// job -> S3 output bucket edge.
	jobS3 := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewJobWritesS3)
	assertEdgeTarget(t, jobS3, awscloud.ResourceTypeS3Bucket, "arn:aws:s3:::sales-output-bucket")

	// job -> dataset internal edge, keyed by the dataset name the dataset node publishes.
	jobDataset := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewJobProcessesDataset)
	assertEdgeTarget(t, jobDataset, awscloud.ResourceTypeDatabrewDataset, "sales")

	// project -> dataset and project -> recipe internal edges.
	projectDataset := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewProjectUsesDataset)
	assertEdgeTarget(t, projectDataset, awscloud.ResourceTypeDatabrewDataset, "sales")
	projectRecipe := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewProjectUsesRecipe)
	assertEdgeTarget(t, projectRecipe, awscloud.ResourceTypeDatabrewRecipe, "clean-sales")

	// project -> IAM role edge.
	projectRole := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewProjectAssumesRole)
	assertEdgeTarget(t, projectRole, awscloud.ResourceTypeIAMRole, testRoleARN)

	// No recipe step expressions, SQL, or sample data leak anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"steps", "recipe_steps", "step_expressions", "parameters",
			"query_string", "query", "sql", "sample", "sample_data", "rows",
			"format_options",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; DataBrew scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerEmitsGlueTableEdgeForCatalogDataset(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Datasets: []Dataset{{
		Name:             "catalog-sales",
		ARN:              "arn:aws:databrew:us-east-1:123456789012:dataset/catalog-sales",
		SourceKind:       "DATA-CATALOG",
		GlueDatabaseName: "analytics",
		GlueTableName:    "sales_fact",
		GlueCatalogID:    "123456789012",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	datasetGlue := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewDatasetReadsGlueTable)
	// The Glue table scanner publishes resource_id as "<database>/<table>".
	assertEdgeTarget(t, datasetGlue, awscloud.ResourceTypeGlueTable, "analytics/sales_fact")
}

func TestScannerSkipsRedshiftDatabaseInputEdge(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Datasets: []Dataset{{
		Name:                   "redshift-sales",
		ARN:                    "arn:aws:databrew:us-east-1:123456789012:dataset/redshift-sales",
		SourceKind:             "DATABASE",
		DatabaseConnectionName: "redshift-conn",
		// QueryString is intentionally never represented in the model.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		t.Fatalf("database input emitted unexpected relationship %#v; Redshift cluster id is not reported, edge must be skipped", envelope.Payload)
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{Datasets: []Dataset{{
		Name:     "gov-sales",
		ARN:      "arn:aws-us-gov:databrew:us-gov-west-1:123456789012:dataset/gov-sales",
		S3Bucket: "gov-input-bucket",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	datasetS3 := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewDatasetReadsS3)
	wantARN := "arn:aws-us-gov:s3:::gov-input-bucket"
	if got := datasetS3.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud dataset->s3 target_resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{Jobs: []Job{{
		Name:            "cn-job",
		ARN:             "arn:aws-cn:databrew:cn-north-1:123456789012:job/cn-job",
		OutputS3Buckets: []string{"cn-output-bucket"},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	jobS3 := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewJobWritesS3)
	wantARN := "arn:aws-cn:s3:::cn-output-bucket"
	if got := jobS3.Payload["target_arn"]; got != wantARN {
		t.Fatalf("China job->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Datasets: []Dataset{{Name: "bare", ARN: testDatasetARN}},
		Recipes:  []Recipe{{Name: "bare-recipe", ARN: testRecipeARN}},
		Jobs:     []Job{{Name: "bare-job", ARN: testJobARN}},
		Projects: []Project{{Name: "bare-project", ARN: testProjectARN, RecipeName: ""}},
	}}

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

func TestScannerOmitsRoleEdgeForNonARNRoleButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Jobs: []Job{{
		Name:    "named-role-job",
		ARN:     testJobARN,
		RoleARN: "databrew-service-role",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	jobRole := relationshipByType(t, envelopes, awscloud.RelationshipDatabrewJobAssumesRole)
	if got, want := jobRole.Payload["target_resource_id"], "databrew-service-role"; got != want {
		t.Fatalf("role target_resource_id = %#v, want %q", got, want)
	}
	if got := jobRole.Payload["target_arn"]; got != "" {
		t.Fatalf("role target_arn = %#v, want empty for non-ARN role identifier", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	dataset := Dataset{
		Name:             "sales",
		ARN:              testDatasetARN,
		S3Bucket:         "sales-input-bucket",
		GlueDatabaseName: "analytics",
		GlueTableName:    "sales_fact",
	}
	job := Job{
		Name:            "profile-sales",
		ARN:             testJobARN,
		DatasetName:     "sales",
		RoleARN:         testRoleARN,
		OutputS3Buckets: []string{"sales-output-bucket"},
	}
	project := Project{
		Name:        "sales-prep",
		ARN:         testProjectARN,
		DatasetName: "sales",
		RecipeName:  "clean-sales",
		RoleARN:     testRoleARN,
	}

	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		datasetReadsS3Relationship(boundary, dataset),
		datasetReadsGlueTableRelationship(boundary, dataset),
		jobAssumesRoleRelationship(boundary, job),
		jobProcessesDatasetRelationship(boundary, job),
		projectUsesDatasetRelationship(boundary, project),
		projectUsesRecipeRelationship(boundary, project),
		projectAssumesRoleRelationship(boundary, project),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	observations = append(observations, jobWritesS3Relationships(boundary, job)...)
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
		Datasets: []Dataset{{Name: "sales", ARN: testDatasetARN}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "DataBrew ListJobs throttled after SDK retries; job metadata omitted for this scan",
			SourceRecordID: "databrew_jobs_throttled",
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
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceDatabrew,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:databrew:1",
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
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
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
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
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
