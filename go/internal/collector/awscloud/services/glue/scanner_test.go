package glue

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsGlueMetadataResourcesAndRelationships(t *testing.T) {
	databaseName := "analytics"
	tableName := "orders"
	crawlerName := "orders-crawler"
	jobName := "orders-etl"
	triggerName := "orders-nightly"
	workflowName := "orders-pipeline"
	connectionName := "orders-warehouse"
	roleARN := "arn:aws:iam::123456789012:role/glue-orders-etl"
	crawlerRoleARN := "arn:aws:iam::123456789012:role/glue-orders-crawler"
	client := fakeClient{
		databases: []Database{{
			CatalogID:   "123456789012",
			Name:        databaseName,
			Description: "analytics warehouse",
			LocationURI: "s3://analytics-warehouse/",
			CreateTime:  time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
			Parameters:  map[string]string{"classification": "warehouse"},
			Tables: []Table{{
				CatalogID:        "123456789012",
				DatabaseName:     databaseName,
				Name:             tableName,
				Owner:            "analytics",
				TableType:        "EXTERNAL_TABLE",
				Description:      "orders table",
				CreateTime:       time.Date(2026, 5, 20, 16, 5, 0, 0, time.UTC),
				UpdateTime:       time.Date(2026, 5, 20, 16, 10, 0, 0, time.UTC),
				LastAccessTime:   time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
				LastAnalyzedTime: time.Date(2026, 5, 20, 16, 15, 0, 0, time.UTC),
				Retention:        7,
				StorageLocation:  "s3://analytics-warehouse/orders/",
				InputFormat:      "org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat",
				OutputFormat:     "org.apache.hadoop.hive.ql.io.parquet.MapredParquetOutputFormat",
				Compressed:       true,
				SerdeName:        "ParquetHiveSerDe",
				SerdeLibrary:     "org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe",
				Parameters:       map[string]string{"classification": "parquet"},
				PartitionKeys:    []string{"event_date"},
				Columns:          []string{"order_id", "customer_id", "amount"},
			}},
		}},
		crawlers: []Crawler{{
			Name:                "orders-crawler",
			Description:         "orders catalog crawler",
			RoleARN:             crawlerRoleARN,
			DatabaseName:        databaseName,
			TablePrefix:         "raw_",
			State:               "READY",
			CreationTime:        time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
			LastUpdated:         time.Date(2026, 5, 20, 13, 0, 0, 0, time.UTC),
			Schedule:            "cron(0 1 * * ? *)",
			RecrawlBehavior:     "CRAWL_EVERYTHING",
			S3TargetCount:       2,
			JDBCTargetCount:     1,
			DynamoDBTargetCount: 0,
		}},
		jobs: []Job{{
			Name:                  jobName,
			Description:           "orders ETL",
			RoleARN:               roleARN,
			GlueVersion:           "4.0",
			WorkerType:            "G.1X",
			NumberOfWorkers:       10,
			MaxRetries:            1,
			Timeout:               2880,
			ScriptLanguage:        "python",
			ScriptLocation:        "s3://analytics-scripts/orders.py",
			CommandName:           "glueetl",
			CreatedOn:             time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
			LastModifiedOn:        time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
			SecurityConfiguration: "default-encryption",
			DefaultArgKeys:        []string{"--job-language", "--TempDir", "--connection-password"},
			NonOverridableArgKeys: []string{"--enable-metrics"},
		}},
		triggers: []Trigger{{
			Name:         triggerName,
			Type:         "SCHEDULED",
			State:        "ACTIVATED",
			Description:  "orders nightly trigger",
			Schedule:     "cron(0 4 * * ? *)",
			WorkflowName: workflowName,
			ActionJobs:   []string{jobName},
		}},
		workflows: []Workflow{{
			Name:             workflowName,
			Description:      "orders pipeline",
			CreatedOn:        time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
			LastModifiedOn:   time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
			DefaultRunKeys:   []string{"region", "tenant"},
			MaxConcurrentRun: 1,
		}},
		connections: []Connection{{
			Name:             connectionName,
			Description:      "analytics warehouse JDBC",
			ConnectionType:   "JDBC",
			CreationTime:     time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
			LastUpdatedTime:  time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
			LastUpdatedBy:    "platform-bot",
			SubnetID:         "subnet-abc",
			SecurityGroupIDs: []string{"sg-1", "sg-2"},
			PropertyKeys:     []string{"JDBC_CONNECTION_URL", "USERNAME", "PASSWORD"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	database := resourceByType(t, envelopes, awscloud.ResourceTypeGlueDatabase)
	if got, want := database.Payload["name"], databaseName; got != want {
		t.Fatalf("database name = %#v, want %q", got, want)
	}
	dbAttributes := attributesOf(t, database)
	if got, want := dbAttributes["location_uri"], "s3://analytics-warehouse/"; got != want {
		t.Fatalf("database location_uri = %#v, want %q", got, want)
	}

	table := resourceByType(t, envelopes, awscloud.ResourceTypeGlueTable)
	if got, want := table.Payload["name"], tableName; got != want {
		t.Fatalf("table name = %#v, want %q", got, want)
	}
	tableAttributes := attributesOf(t, table)
	if got, want := tableAttributes["storage_location"], "s3://analytics-warehouse/orders/"; got != want {
		t.Fatalf("table storage_location = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"column_statistics", "column_sample_values", "partition_values"} {
		if _, exists := tableAttributes[forbidden]; exists {
			t.Fatalf("table %s attribute persisted; scanner must not store column statistics or sample values", forbidden)
		}
	}

	crawler := resourceByType(t, envelopes, awscloud.ResourceTypeGlueCrawler)
	crawlerAttributes := attributesOf(t, crawler)
	if got, want := crawlerAttributes["table_prefix"], "raw_"; got != want {
		t.Fatalf("crawler table_prefix = %#v, want %q", got, want)
	}
	if _, exists := crawlerAttributes["s3_targets"]; exists {
		t.Fatalf("crawler s3_targets attribute persisted; raw S3 paths must stay out of facts")
	}
	if _, exists := crawlerAttributes["classifier_patterns"]; exists {
		t.Fatalf("crawler classifier_patterns attribute persisted; custom classifier patterns must stay out of facts")
	}

	job := resourceByType(t, envelopes, awscloud.ResourceTypeGlueJob)
	jobAttributes := attributesOf(t, job)
	if _, exists := jobAttributes["script_body"]; exists {
		t.Fatalf("job script_body attribute persisted; job script bodies must never be stored")
	}
	if _, exists := jobAttributes["default_arguments"]; exists {
		t.Fatalf("job default_arguments value map persisted; default-argument values must never be stored")
	}
	defaultArgKeys, ok := jobAttributes["default_argument_keys"].([]string)
	if !ok {
		t.Fatalf("job default_argument_keys = %#v, want []string", jobAttributes["default_argument_keys"])
	}
	for _, key := range defaultArgKeys {
		if key == "--connection-password" {
			t.Fatalf("job default_argument_keys leaked secret-shaped key %q; scanner must drop secret-like keys", key)
		}
	}
	if got, want := jobAttributes["script_location"], "s3://analytics-scripts/orders.py"; got != want {
		t.Fatalf("job script_location = %#v, want %q", got, want)
	}

	trigger := resourceByType(t, envelopes, awscloud.ResourceTypeGlueTrigger)
	triggerAttributes := attributesOf(t, trigger)
	if got, want := triggerAttributes["workflow_name"], workflowName; got != want {
		t.Fatalf("trigger workflow_name = %#v, want %q", got, want)
	}

	workflow := resourceByType(t, envelopes, awscloud.ResourceTypeGlueWorkflow)
	if got, want := workflow.Payload["name"], workflowName; got != want {
		t.Fatalf("workflow name = %#v, want %q", got, want)
	}

	connection := resourceByType(t, envelopes, awscloud.ResourceTypeGlueConnection)
	connectionAttributes := attributesOf(t, connection)
	if _, exists := connectionAttributes["connection_properties"]; exists {
		t.Fatalf("connection connection_properties persisted; property values are credential-bearing")
	}
	propertyKeys, ok := connectionAttributes["property_keys"].([]string)
	if !ok {
		t.Fatalf("connection property_keys = %#v, want []string", connectionAttributes["property_keys"])
	}
	for _, key := range propertyKeys {
		switch key {
		case "PASSWORD", "ENCRYPTED_PASSWORD":
			t.Fatalf("connection property_keys leaked secret-shaped key %q; scanner must drop secret-like keys", key)
		}
	}

	tableInDatabase := relationshipByType(t, envelopes, awscloud.RelationshipGlueTableInDatabase)
	if got, want := tableInDatabase.Payload["target_resource_id"], databaseName; got != want {
		t.Fatalf("table_in_database target_resource_id = %#v, want %q", got, want)
	}

	tableS3 := relationshipByType(t, envelopes, awscloud.RelationshipGlueTableStoredAtS3Location)
	if got, want := tableS3.Payload["target_resource_id"], "arn:aws:s3:::analytics-warehouse"; got != want {
		t.Fatalf("table->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := tableS3.Payload["target_arn"], "arn:aws:s3:::analytics-warehouse"; got != want {
		t.Fatalf("table->s3 target_arn = %#v, want %q", got, want)
	}
	if got, want := tableS3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("table->s3 target_type = %#v, want %q", got, want)
	}
	tableS3Attributes := attributesOf(t, tableS3)
	if got, want := tableS3Attributes["storage_location"], "s3://analytics-warehouse/orders/"; got != want {
		t.Fatalf("table->s3 storage_location attribute = %#v, want %q", got, want)
	}
	if got, want := tableS3Attributes["bucket"], "analytics-warehouse"; got != want {
		t.Fatalf("table->s3 bucket attribute = %#v, want %q", got, want)
	}
	if got, want := tableS3Attributes["object_key_prefix"], "orders/"; got != want {
		t.Fatalf("table->s3 object_key_prefix attribute = %#v, want %q", got, want)
	}

	crawlerDB := relationshipByType(t, envelopes, awscloud.RelationshipGlueCrawlerTargetsDatabase)
	if got, want := crawlerDB.Payload["source_resource_id"], crawlerName; got != want {
		t.Fatalf("crawler->db source_resource_id = %#v, want %q", got, want)
	}
	if got, want := crawlerDB.Payload["target_resource_id"], databaseName; got != want {
		t.Fatalf("crawler->db target_resource_id = %#v, want %q", got, want)
	}

	crawlerRole := relationshipByType(t, envelopes, awscloud.RelationshipGlueCrawlerUsesIAMRole)
	if got, want := crawlerRole.Payload["target_arn"], crawlerRoleARN; got != want {
		t.Fatalf("crawler->role target_arn = %#v, want %q", got, want)
	}
	if got, want := crawlerRole.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("crawler->role target_type = %#v, want %q", got, want)
	}

	jobRole := relationshipByType(t, envelopes, awscloud.RelationshipGlueJobUsesIAMRole)
	if got, want := jobRole.Payload["source_resource_id"], jobName; got != want {
		t.Fatalf("job->role source_resource_id = %#v, want %q", got, want)
	}
	if got, want := jobRole.Payload["target_arn"], roleARN; got != want {
		t.Fatalf("job->role target_arn = %#v, want %q", got, want)
	}

	triggerJob := relationshipByType(t, envelopes, awscloud.RelationshipGlueTriggerInvokesJob)
	if got, want := triggerJob.Payload["source_resource_id"], triggerName; got != want {
		t.Fatalf("trigger->job source_resource_id = %#v, want %q", got, want)
	}
	if got, want := triggerJob.Payload["target_resource_id"], jobName; got != want {
		t.Fatalf("trigger->job target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerOmitsTableS3RelationshipWhenLocationIsNotS3(t *testing.T) {
	client := fakeClient{databases: []Database{{
		Name: "analytics",
		Tables: []Table{{
			Name:            "orders",
			DatabaseName:    "analytics",
			StorageLocation: "hdfs://nn/orders",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipGlueTableStoredAtS3Location); got != 0 {
		t.Fatalf("table->s3 relationship count = %d, want 0 for non-s3 storage location", got)
	}
}

func TestScannerTableS3RelationshipTargetsBucketARNWithoutObjectKeyPrefix(t *testing.T) {
	client := fakeClient{databases: []Database{{
		Name: "analytics",
		Tables: []Table{{
			Name:            "no-prefix-table",
			DatabaseName:    "analytics",
			StorageLocation: "s3://lakehouse",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	tableS3 := relationshipByType(t, envelopes, awscloud.RelationshipGlueTableStoredAtS3Location)
	if got, want := tableS3.Payload["target_resource_id"], "arn:aws:s3:::lakehouse"; got != want {
		t.Fatalf("table->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := tableS3.Payload["target_arn"], "arn:aws:s3:::lakehouse"; got != want {
		t.Fatalf("table->s3 target_arn = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, tableS3)
	if _, exists := attributes["object_key_prefix"]; exists {
		t.Fatalf("table->s3 attributes leaked object_key_prefix for bucket-root location")
	}
	if got, want := attributes["bucket"], "lakehouse"; got != want {
		t.Fatalf("table->s3 bucket attribute = %#v, want %q", got, want)
	}
}

func TestScannerOmitsTableS3RelationshipWhenLocationHasNoBucket(t *testing.T) {
	client := fakeClient{databases: []Database{{
		Name: "analytics",
		Tables: []Table{{
			Name:            "no-bucket",
			DatabaseName:    "analytics",
			StorageLocation: "s3:///orphan/",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipGlueTableStoredAtS3Location); got != 0 {
		t.Fatalf("table->s3 relationship count = %d, want 0 for bucketless s3:// uri", got)
	}
}

func TestScannerOmitsRoleRelationshipsWhenRoleIsNotARN(t *testing.T) {
	client := fakeClient{
		crawlers: []Crawler{{Name: "c", RoleARN: "glue-crawler-role"}},
		jobs:     []Job{{Name: "j", RoleARN: "glue-job-role"}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipGlueJobUsesIAMRole); got != 0 {
		t.Fatalf("job->role relationship count = %d, want 0 for non-ARN role", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipGlueCrawlerUsesIAMRole); got != 0 {
		t.Fatalf("crawler->role relationship count = %d, want 0 for non-ARN role", got)
	}
}

func TestScannerEmitsOneRelationshipPerTriggerAction(t *testing.T) {
	client := fakeClient{triggers: []Trigger{{
		Name:       "t",
		ActionJobs: []string{"job-a", "job-b"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipGlueTriggerInvokesJob); got != 2 {
		t.Fatalf("trigger->job relationship count = %d, want 2", got)
	}
}

func TestScannerDropsSecretShapedDefaultArgumentKeys(t *testing.T) {
	client := fakeClient{jobs: []Job{{
		Name: "j",
		DefaultArgKeys: []string{
			"--TempDir",
			"--connection-password",
			"--AwsSecretAccessKey",
			"--secret",
			"--db-token",
			"--regular-arg",
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	job := resourceByType(t, envelopes, awscloud.ResourceTypeGlueJob)
	keys, ok := attributesOf(t, job)["default_argument_keys"].([]string)
	if !ok {
		t.Fatalf("default_argument_keys = %#v, want []string", attributesOf(t, job)["default_argument_keys"])
	}
	for _, key := range keys {
		switch key {
		case "--connection-password", "--AwsSecretAccessKey", "--secret", "--db-token":
			t.Fatalf("default_argument_keys leaked secret-shaped key %q", key)
		}
	}
	wantPresent := map[string]bool{"--TempDir": false, "--regular-arg": false}
	for _, key := range keys {
		if _, ok := wantPresent[key]; ok {
			wantPresent[key] = true
		}
	}
	for key, present := range wantPresent {
		if !present {
			t.Fatalf("default_argument_keys missing safe key %q", key)
		}
	}
}

func TestScannerDropsSecretShapedConnectionPropertyKeys(t *testing.T) {
	client := fakeClient{connections: []Connection{{
		Name:         "c",
		PropertyKeys: []string{"JDBC_CONNECTION_URL", "USERNAME", "PASSWORD", "ENCRYPTED_PASSWORD", "KAFKA_SASL_PASSWORD"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	connection := resourceByType(t, envelopes, awscloud.ResourceTypeGlueConnection)
	keys, ok := attributesOf(t, connection)["property_keys"].([]string)
	if !ok {
		t.Fatalf("property_keys = %#v, want []string", attributesOf(t, connection)["property_keys"])
	}
	for _, key := range keys {
		switch key {
		case "PASSWORD", "ENCRYPTED_PASSWORD", "KAFKA_SASL_PASSWORD":
			t.Fatalf("property_keys leaked secret-shaped key %q", key)
		}
	}
	wantPresent := map[string]bool{"JDBC_CONNECTION_URL": false, "USERNAME": false}
	for _, key := range keys {
		if _, ok := wantPresent[key]; ok {
			wantPresent[key] = true
		}
	}
	for key, present := range wantPresent {
		if !present {
			t.Fatalf("property_keys missing safe key %q", key)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceGlue,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:glue:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	databases   []Database
	crawlers    []Crawler
	jobs        []Job
	triggers    []Trigger
	workflows   []Workflow
	connections []Connection
}

func (c fakeClient) ListDatabases(context.Context) ([]Database, error) { return c.databases, nil }

func (c fakeClient) ListCrawlers(context.Context) ([]Crawler, error) { return c.crawlers, nil }
func (c fakeClient) ListJobs(context.Context) ([]Job, error)         { return c.jobs, nil }
func (c fakeClient) ListTriggers(context.Context) ([]Trigger, error) { return c.triggers, nil }

func (c fakeClient) ListWorkflows(context.Context) ([]Workflow, error) { return c.workflows, nil }

func (c fakeClient) ListConnections(context.Context) ([]Connection, error) { return c.connections, nil }

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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
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
