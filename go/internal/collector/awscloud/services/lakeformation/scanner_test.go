// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lakeformation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsLakeFormationMetadataResourcesAndRelationships(t *testing.T) {
	registeredARN := "arn:aws:s3:::analytics-lake/governed/"
	registerRoleARN := "arn:aws:iam::123456789012:role/LakeFormationRegistrationRole"
	adminARN := "arn:aws:iam::123456789012:role/LakeFormationAdmin"
	analystARN := "arn:aws:iam::123456789012:role/DataAnalyst"
	client := fakeClient{
		settings: Settings{
			Admins:         []string{adminARN},
			ReadOnlyAdmins: []string{"arn:aws:iam::123456789012:role/Auditor"},
		},
		resources: []RegisteredResource{{
			ResourceARN:                  registeredARN,
			RoleARN:                      registerRoleARN,
			HybridAccessEnabled:          true,
			WithFederation:               false,
			VerificationStatus:           "VERIFIED",
			ExpectedResourceOwnerAccount: "123456789012",
			LastModified:                 time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
		}},
		permissions: []Permission{
			{
				PrincipalID:  analystARN,
				ResourceKind: "table",
				DatabaseName: "analytics",
				TableName:    "orders",
				Privileges:   []string{"SELECT", "DESCRIBE"},
				LastUpdated:  time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
			},
			{
				PrincipalID:         analystARN,
				ResourceKind:        "database",
				DatabaseName:        "analytics",
				Privileges:          []string{"DESCRIBE"},
				GrantablePrivileges: []string{"DESCRIBE"},
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	settings := resourceByType(t, envelopes, awscloud.ResourceTypeLakeFormationSettings)
	settingsAttributes := attributesOf(t, settings)
	admins, ok := settingsAttributes["data_lake_admins"].([]string)
	if !ok || len(admins) != 1 || admins[0] != adminARN {
		t.Fatalf("settings data_lake_admins = %#v, want [%q]", settingsAttributes["data_lake_admins"], adminARN)
	}
	for _, forbidden := range []string{"create_database_default_permissions", "create_table_default_permissions", "authorized_session_tag_values", "default_permissions"} {
		if _, exists := settingsAttributes[forbidden]; exists {
			t.Fatalf("settings %s attribute persisted; permission bodies must stay out of facts", forbidden)
		}
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeLakeFormationResource)
	if got, want := resource.Payload["resource_id"], registeredARN; got != want {
		t.Fatalf("registered resource_id = %#v, want %q", got, want)
	}
	resourceAttributes := attributesOf(t, resource)
	if got, want := resourceAttributes["role_arn"], registerRoleARN; got != want {
		t.Fatalf("registered role_arn = %#v, want %q", got, want)
	}
	if got, want := resourceAttributes["hybrid_access_enabled"], true; got != want {
		t.Fatalf("registered hybrid_access_enabled = %#v, want %v", got, want)
	}

	permission := resourceByType(t, envelopes, awscloud.ResourceTypeLakeFormationPermission)
	permissionAttributes := attributesOf(t, permission)
	if got, want := permissionAttributes["principal_id"], analystARN; got != want {
		t.Fatalf("permission principal_id = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"condition", "condition_expression", "lf_tag_values", "policy", "policy_body", "additional_details"} {
		if _, exists := permissionAttributes[forbidden]; exists {
			t.Fatalf("permission %s attribute persisted; condition/LF-Tag/policy bodies must stay out of facts", forbidden)
		}
	}
	privileges, ok := permissionAttributes["privileges"].([]string)
	if !ok || len(privileges) != 2 {
		t.Fatalf("permission privileges = %#v, want 2 sorted enum names", permissionAttributes["privileges"])
	}

	// Registered resource -> S3 bucket.
	resourceS3 := relationshipByType(t, envelopes, awscloud.RelationshipLakeFormationResourceAtS3Bucket)
	if got, want := resourceS3.Payload["source_resource_id"], registeredARN; got != want {
		t.Fatalf("resource->s3 source_resource_id = %#v, want %q", got, want)
	}
	if got, want := resourceS3.Payload["target_resource_id"], "arn:aws:s3:::analytics-lake"; got != want {
		t.Fatalf("resource->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := resourceS3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("resource->s3 target_type = %#v, want %q", got, want)
	}

	// Registered resource -> IAM role.
	resourceRole := relationshipByType(t, envelopes, awscloud.RelationshipLakeFormationResourceUsesIAMRole)
	if got, want := resourceRole.Payload["target_arn"], registerRoleARN; got != want {
		t.Fatalf("resource->role target_arn = %#v, want %q", got, want)
	}
	if got, want := resourceRole.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("resource->role target_type = %#v, want %q", got, want)
	}

	// Permission -> Glue table.
	permTable := relationshipByType(t, envelopes, awscloud.RelationshipLakeFormationPermissionOnGlueTable)
	if got, want := permTable.Payload["target_resource_id"], "analytics/orders"; got != want {
		t.Fatalf("permission->table target_resource_id = %#v, want %q", got, want)
	}
	if got, want := permTable.Payload["target_type"], awscloud.ResourceTypeGlueTable; got != want {
		t.Fatalf("permission->table target_type = %#v, want %q", got, want)
	}

	// Permission -> Glue database.
	permDB := relationshipByType(t, envelopes, awscloud.RelationshipLakeFormationPermissionOnGlueDatabase)
	if got, want := permDB.Payload["target_resource_id"], "analytics"; got != want {
		t.Fatalf("permission->database target_resource_id = %#v, want %q", got, want)
	}
	if got, want := permDB.Payload["target_type"], awscloud.ResourceTypeGlueDatabase; got != want {
		t.Fatalf("permission->database target_type = %#v, want %q", got, want)
	}

	// Permission -> principal (IAM role).
	permPrincipal := relationshipByType(t, envelopes, awscloud.RelationshipLakeFormationPermissionGrantedToPrincipal)
	if got, want := permPrincipal.Payload["target_resource_id"], analystARN; got != want {
		t.Fatalf("permission->principal target_resource_id = %#v, want %q", got, want)
	}
	if got, want := permPrincipal.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("permission->principal target_type = %#v, want %q", got, want)
	}

	relguard.AssertObservations(t, collectRelationshipObservations(t, envelopes)...)
}

func TestScannerDerivesS3BucketPartitionFromRegisteredARN(t *testing.T) {
	cases := []struct {
		name      string
		region    string
		sourceARN string
		want      string
	}{
		{name: "commercial", region: "us-east-1", sourceARN: "arn:aws:s3:::lakehouse/data", want: "arn:aws:s3:::lakehouse"},
		{name: "govcloud", region: "us-gov-west-1", sourceARN: "arn:aws-us-gov:s3:::lakehouse/data", want: "arn:aws-us-gov:s3:::lakehouse"},
		{name: "china", region: "cn-north-1", sourceARN: "arn:aws-cn:s3:::lakehouse/data", want: "arn:aws-cn:s3:::lakehouse"},
		// Blank partition segment in the source ARN falls back to the boundary
		// region's partition via the local partition(boundary) helper.
		{name: "blank-partition fallback govcloud", region: "us-gov-west-1", sourceARN: "arn::s3:::lakehouse/data", want: "arn:aws-us-gov:s3:::lakehouse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := awscloud.Boundary{Region: tc.region}
			obs := resourceS3BucketRelationship(boundary, RegisteredResource{ResourceARN: tc.sourceARN})
			if obs == nil {
				t.Fatalf("resourceS3BucketRelationship returned nil for a valid s3 location ARN")
			}
			if obs.TargetResourceID != tc.want {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.want)
			}
			if obs.TargetARN != tc.want {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.want)
			}
			if obs.TargetType != awscloud.ResourceTypeS3Bucket {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeS3Bucket)
			}
		})
	}
}

func TestScannerOmitsS3RelationshipWhenRegisteredARNIsNotS3(t *testing.T) {
	client := fakeClient{resources: []RegisteredResource{{
		ResourceARN: "arn:aws:glue:us-east-1:123456789012:database/analytics",
		RoleARN:     "arn:aws:iam::123456789012:role/r",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipLakeFormationResourceAtS3Bucket); got != 0 {
		t.Fatalf("resource->s3 relationship count = %d, want 0 for a non-S3 registered ARN", got)
	}
}

func TestScannerOmitsPrincipalEdgeWhenPrincipalIsNotRoleARN(t *testing.T) {
	client := fakeClient{permissions: []Permission{{
		PrincipalID:  "IAM_ALLOWED_PRINCIPALS",
		ResourceKind: "database",
		DatabaseName: "analytics",
		Privileges:   []string{"ALL"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipLakeFormationPermissionGrantedToPrincipal); got != 0 {
		t.Fatalf("permission->principal count = %d, want 0 for a non-role-ARN principal", got)
	}
	// The database edge still resolves for the special principal grant.
	if got := countRelationships(envelopes, awscloud.RelationshipLakeFormationPermissionOnGlueDatabase); got != 1 {
		t.Fatalf("permission->database count = %d, want 1", got)
	}
}

func TestScannerEmitsDatabaseEdgeForTableWildcardGrant(t *testing.T) {
	client := fakeClient{permissions: []Permission{{
		PrincipalID:   "arn:aws:iam::123456789012:role/Analyst",
		ResourceKind:  "table",
		DatabaseName:  "analytics",
		TableWildcard: true,
		Privileges:    []string{"SELECT"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	// A table-wildcard grant has no single Glue table to join (the Glue table
	// resource_id is "database/table"), so it must route to the Glue database
	// node keyed by the bare database name, not emit an aws_glue_table edge with
	// a database-shaped id that would dangle.
	if got := countRelationships(envelopes, awscloud.RelationshipLakeFormationPermissionOnGlueTable); got != 0 {
		t.Fatalf("wildcard grant must not emit a glue-table edge, got %d", got)
	}
	permDB := relationshipByType(t, envelopes, awscloud.RelationshipLakeFormationPermissionOnGlueDatabase)
	if got, want := permDB.Payload["target_resource_id"], "analytics"; got != want {
		t.Fatalf("wildcard permission->database target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceGlue

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func TestScannerEmitsSettingsResourceWithEmptyState(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	// The data-lake settings resource is always emitted, even with no admins,
	// no registered resources, and no permissions.
	if _, exists := firstResource(envelopes, awscloud.ResourceTypeLakeFormationSettings); !exists {
		t.Fatalf("settings resource missing for empty Lake Formation state")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceLakeFormation,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:lakeformation:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	settings    Settings
	resources   []RegisteredResource
	permissions []Permission
}

func (c fakeClient) GetDataLakeSettings(context.Context) (Settings, error) {
	return c.settings, nil
}

func (c fakeClient) ListResources(context.Context) ([]RegisteredResource, error) {
	return c.resources, nil
}

func (c fakeClient) ListPermissions(context.Context) ([]Permission, error) {
	return c.permissions, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	if envelope, ok := firstResource(envelopes, resourceType); ok {
		return envelope
	}
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
	return facts.Envelope{}
}

func firstResource(envelopes []facts.Envelope, resourceType string) (facts.Envelope, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope, true
		}
	}
	return facts.Envelope{}, false
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
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// collectRelationshipObservations reconstructs the relationship observations the
// scanner emitted from the fact payloads so the relguard runtime contract can be
// asserted against the live graph-join data.
func collectRelationshipObservations(t *testing.T, envelopes []facts.Envelope) []awscloud.RelationshipObservation {
	t.Helper()
	var observations []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			RelationshipType: stringField(envelope, "relationship_type"),
			SourceResourceID: stringField(envelope, "source_resource_id"),
			TargetResourceID: stringField(envelope, "target_resource_id"),
			TargetARN:        stringField(envelope, "target_arn"),
			TargetType:       stringField(envelope, "target_type"),
		})
	}
	if len(observations) == 0 {
		t.Fatalf("no relationship observations collected from %d envelopes", len(envelopes))
	}
	return observations
}

func stringField(envelope facts.Envelope, key string) string {
	value, _ := envelope.Payload[key].(string)
	return strings.TrimSpace(value)
}
