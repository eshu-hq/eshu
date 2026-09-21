// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package athena

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsAthenaMetadataOnlyFactsAndRelationships(t *testing.T) {
	resultBucketARN := "arn:aws:s3:::athena-results-orders"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	client := fakeClient{
		workGroups: []WorkGroup{{
			Name:                            "primary",
			State:                           "ENABLED",
			Description:                     "default workgroup",
			CreationTime:                    time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			OutputLocation:                  "s3://athena-results-orders/queries/",
			EncryptionOption:                "SSE_KMS",
			KMSKey:                          kmsARN,
			EnforceWorkGroupConfiguration:   true,
			PublishCloudWatchMetricsEnabled: true,
			RequesterPaysEnabled:            false,
			EngineVersion:                   "Athena engine version 3",
			EffectiveEngineVersion:          "Athena engine version 3",
			BytesScannedCutoffPerQuery:      10737418240,
			ExpectedBucketOwner:             "123456789012",
			Tags:                            map[string]string{"Environment": "prod"},
		}},
		dataCatalogs: []DataCatalog{{
			Name:        "AwsDataCatalog",
			Type:        "GLUE",
			Description: "default data catalog",
			Tags:        map[string]string{"Owner": "platform"},
		}, {
			Name:        "external_orders",
			Type:        "LAMBDA",
			Description: "external orders catalog",
		}},
		preparedStatements: map[string][]PreparedStatement{
			"primary": {{
				WorkGroupName:    "primary",
				StatementName:    "orders_by_day",
				LastModifiedTime: time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC),
			}},
		},
		namedQueries: map[string][]NamedQuery{
			"primary": {{
				NamedQueryID:  "11111111-2222-3333-4444-555555555555",
				Name:          "daily-orders",
				Description:   "daily orders summary",
				Database:      "orders",
				WorkGroupName: "primary",
			}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	workGroup := resourceByType(t, envelopes, awscloud.ResourceTypeAthenaWorkGroup)
	if got, want := workGroup.Payload["resource_id"], "primary"; got != want {
		t.Fatalf("workgroup resource_id = %#v, want %q", got, want)
	}
	if got, want := workGroup.Payload["state"], "ENABLED"; got != want {
		t.Fatalf("workgroup state = %#v, want %q", got, want)
	}
	wgAttrs := attributesOf(t, workGroup)
	assertAttribute(t, wgAttrs, "description", "default workgroup")
	assertAttribute(t, wgAttrs, "output_location", "s3://athena-results-orders/queries/")
	assertAttribute(t, wgAttrs, "encryption_option", "SSE_KMS")
	assertAttribute(t, wgAttrs, "kms_key", kmsARN)
	assertAttribute(t, wgAttrs, "enforce_workgroup_configuration", true)
	assertAttribute(t, wgAttrs, "publish_cloudwatch_metrics_enabled", true)
	assertAttribute(t, wgAttrs, "requester_pays_enabled", false)
	assertAttribute(t, wgAttrs, "engine_version", "Athena engine version 3")
	assertAttribute(t, wgAttrs, "effective_engine_version", "Athena engine version 3")
	assertAttribute(t, wgAttrs, "bytes_scanned_cutoff_per_query", int64(10737418240))
	assertAttribute(t, wgAttrs, "expected_bucket_owner", "123456789012")
	for _, forbidden := range []string{
		"query_results",
		"query_history",
		"query_string",
		"query_statement",
		"sql",
		"output",
		"result_set",
		"rows",
	} {
		if _, exists := wgAttrs[forbidden]; exists {
			t.Fatalf("%s attribute persisted; Athena scanner must stay metadata-only", forbidden)
		}
	}

	catalogs := resourcesByType(envelopes, awscloud.ResourceTypeAthenaDataCatalog)
	if got, want := len(catalogs), 2; got != want {
		t.Fatalf("len(data catalogs) = %d, want %d", got, want)
	}
	catalogNames := make([]string, 0, len(catalogs))
	for _, catalog := range catalogs {
		name, _ := catalog.Payload["name"].(string)
		catalogNames = append(catalogNames, name)
	}
	sort.Strings(catalogNames)
	if want := []string{"AwsDataCatalog", "external_orders"}; !reflect.DeepEqual(catalogNames, want) {
		t.Fatalf("catalog names = %#v, want %#v", catalogNames, want)
	}

	preparedStatements := resourcesByType(envelopes, awscloud.ResourceTypeAthenaPreparedStatement)
	if got, want := len(preparedStatements), 1; got != want {
		t.Fatalf("len(prepared statements) = %d, want %d", got, want)
	}
	statement := preparedStatements[0]
	if got, want := statement.Payload["resource_id"], "primary/orders_by_day"; got != want {
		t.Fatalf("prepared statement resource_id = %#v, want %q", got, want)
	}
	stmtAttrs := attributesOf(t, statement)
	assertAttribute(t, stmtAttrs, "statement_name", "orders_by_day")
	assertAttribute(t, stmtAttrs, "workgroup_name", "primary")
	for _, forbidden := range []string{
		"query_statement",
		"query_string",
		"sql",
		"statement",
		"body",
	} {
		if _, exists := stmtAttrs[forbidden]; exists {
			t.Fatalf(
				"%s attribute persisted; Athena prepared statement scanner must never store the SQL body",
				forbidden,
			)
		}
	}

	statementInWorkGroup := relationshipByType(
		t,
		envelopes,
		awscloud.RelationshipAthenaPreparedStatementInWorkGroup,
	)
	if got, want := statementInWorkGroup.Payload["target_resource_id"], "primary"; got != want {
		t.Fatalf("prepared statement->workgroup target_resource_id = %#v, want %q", got, want)
	}

	namedQueries := resourcesByType(envelopes, awscloud.ResourceTypeAthenaNamedQuery)
	if got, want := len(namedQueries), 1; got != want {
		t.Fatalf("len(named queries) = %d, want %d", got, want)
	}
	namedQuery := namedQueries[0]
	if got, want := namedQuery.Payload["resource_id"], "11111111-2222-3333-4444-555555555555"; got != want {
		t.Fatalf("named query resource_id = %#v, want %q", got, want)
	}
	nqAttrs := attributesOf(t, namedQuery)
	assertAttribute(t, nqAttrs, "named_query_id", "11111111-2222-3333-4444-555555555555")
	assertAttribute(t, nqAttrs, "query_name", "daily-orders")
	assertAttribute(t, nqAttrs, "database", "orders")
	assertAttribute(t, nqAttrs, "workgroup_name", "primary")
	assertAttribute(t, nqAttrs, "description", "daily orders summary")
	for _, forbidden := range []string{
		"query_string",
		"query_statement",
		"sql",
		"statement",
		"body",
		"query",
		"query_history",
		"result_location",
	} {
		if _, exists := nqAttrs[forbidden]; exists {
			t.Fatalf(
				"%s attribute persisted; Athena named query scanner must never store the SQL body",
				forbidden,
			)
		}
	}

	namedQueryInWorkGroup := relationshipByType(
		t,
		envelopes,
		awscloud.RelationshipAthenaNamedQueryInWorkGroup,
	)
	if got, want := namedQueryInWorkGroup.Payload["target_resource_id"], "primary"; got != want {
		t.Fatalf("named query->workgroup target_resource_id = %#v, want %q", got, want)
	}

	resultBucketRelationship := relationshipByType(
		t,
		envelopes,
		awscloud.RelationshipAthenaWorkGroupUsesResultBucket,
	)
	if got, want := resultBucketRelationship.Payload["target_resource_id"], resultBucketARN; got != want {
		t.Fatalf("result bucket target_resource_id = %#v, want %q", got, want)
	}
	if got, want := resultBucketRelationship.Payload["target_arn"], resultBucketARN; got != want {
		t.Fatalf("result bucket target_arn = %#v, want %q", got, want)
	}
	resultAttrs, _ := resultBucketRelationship.Payload["attributes"].(map[string]any)
	for _, forbidden := range []string{
		"output_location",
		"object_key",
		"prefix",
		"object_prefix",
		"result_location",
	} {
		if _, exists := resultAttrs[forbidden]; exists {
			t.Fatalf(
				"%s attribute persisted on result-bucket relationship; payload must stay bucket-only",
				forbidden,
			)
		}
	}

	kmsRelationship := relationshipByType(t, envelopes, awscloud.RelationshipAthenaWorkGroupUsesKMSKey)
	if got, want := kmsRelationship.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerOmitsBucketRelationshipForNonS3OutputLocation(t *testing.T) {
	client := fakeClient{workGroups: []WorkGroup{{
		Name:           "primary",
		State:          "ENABLED",
		OutputLocation: "  ",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipAthenaWorkGroupUsesResultBucket); got != 0 {
		t.Fatalf("result bucket relationships = %d, want 0 for empty output location", got)
	}
}

func TestScannerEmitsBucketRelationshipForBareBucketName(t *testing.T) {
	client := fakeClient{workGroups: []WorkGroup{{
		Name:           "primary",
		State:          "ENABLED",
		OutputLocation: "s3://athena-results-orders",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipAthenaWorkGroupUsesResultBucket)
	want := "arn:aws:s3:::athena-results-orders"
	if got := relationship.Payload["target_resource_id"]; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != want {
		t.Fatalf("target_arn = %#v, want %q", got, want)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{workGroups: []WorkGroup{{
		Name:             "primary",
		State:            "ENABLED",
		EncryptionOption: "SSE_KMS",
		KMSKey:           "alias/athena",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipAthenaWorkGroupUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/athena"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerRequestsPreparedStatementsAndNamedQueriesForEveryDiscoveredWorkGroup(t *testing.T) {
	client := &recordingClient{
		workGroups: []WorkGroup{
			{Name: "primary", State: "ENABLED"},
			{Name: "analytics", State: "ENABLED"},
		},
	}

	_, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	want := []string{"analytics", "primary"}
	sort.Strings(client.preparedRequests)
	if !reflect.DeepEqual(client.preparedRequests, want) {
		t.Fatalf("ListPreparedStatements requests = %#v, want %#v", client.preparedRequests, want)
	}
	sort.Strings(client.namedRequests)
	if !reflect.DeepEqual(client.namedRequests, want) {
		t.Fatalf("ListNamedQueries requests = %#v, want %#v", client.namedRequests, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
	if !strings.Contains(err.Error(), `athena scanner received service_kind`) {
		t.Fatalf("Scan() error = %q, want service_kind mismatch text", err)
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil || !strings.Contains(err.Error(), "athena scanner client is required") {
		t.Fatalf("Scan() error = %v, want client-required", err)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAthena,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:athena:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	workGroups         []WorkGroup
	dataCatalogs       []DataCatalog
	preparedStatements map[string][]PreparedStatement
	namedQueries       map[string][]NamedQuery
}

func (c fakeClient) ListWorkGroups(context.Context) ([]WorkGroup, error) {
	return c.workGroups, nil
}

func (c fakeClient) ListDataCatalogs(context.Context) ([]DataCatalog, error) {
	return c.dataCatalogs, nil
}

func (c fakeClient) ListPreparedStatements(_ context.Context, workGroupNames []string) ([]PreparedStatement, error) {
	var statements []PreparedStatement
	for _, name := range workGroupNames {
		statements = append(statements, c.preparedStatements[name]...)
	}
	return statements, nil
}

func (c fakeClient) ListNamedQueries(_ context.Context, workGroupNames []string) ([]NamedQuery, error) {
	var queries []NamedQuery
	for _, name := range workGroupNames {
		queries = append(queries, c.namedQueries[name]...)
	}
	return queries, nil
}

type recordingClient struct {
	workGroups       []WorkGroup
	preparedRequests []string
	namedRequests    []string
}

func (c *recordingClient) ListWorkGroups(context.Context) ([]WorkGroup, error) {
	return c.workGroups, nil
}

func (c *recordingClient) ListDataCatalogs(context.Context) ([]DataCatalog, error) {
	return nil, nil
}

func (c *recordingClient) ListPreparedStatements(_ context.Context, workGroupNames []string) ([]PreparedStatement, error) {
	c.preparedRequests = append(c.preparedRequests, workGroupNames...)
	return nil, nil
}

func (c *recordingClient) ListNamedQueries(_ context.Context, workGroupNames []string) ([]NamedQuery, error) {
	c.namedRequests = append(c.namedRequests, workGroupNames...)
	return nil, nil
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

func resourcesByType(envelopes []facts.Envelope, resourceType string) []facts.Envelope {
	var matches []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			matches = append(matches, envelope)
		}
	}
	return matches
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
