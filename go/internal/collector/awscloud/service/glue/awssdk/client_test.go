// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	awsgluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListDatabasesReadsSafeDatabaseAndTableMetadata(t *testing.T) {
	client := &fakeGlueAPI{
		databasePages: []*awsglue.GetDatabasesOutput{{
			DatabaseList: []awsgluetypes.Database{{
				Name:        aws.String("analytics"),
				CatalogId:   aws.String("123456789012"),
				Description: aws.String("analytics db"),
				LocationUri: aws.String("s3://analytics-warehouse/"),
				CreateTime:  aws.Time(time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC)),
				Parameters:  map[string]string{"classification": "warehouse"},
			}},
		}},
		tablePages: []*awsglue.GetTablesOutput{{
			TableList: []awsgluetypes.Table{{
				Name:         aws.String("orders"),
				CatalogId:    aws.String("123456789012"),
				DatabaseName: aws.String("analytics"),
				Owner:        aws.String("analytics"),
				TableType:    aws.String("EXTERNAL_TABLE"),
				CreateTime:   aws.Time(time.Date(2026, 5, 20, 16, 5, 0, 0, time.UTC)),
				UpdateTime:   aws.Time(time.Date(2026, 5, 20, 16, 10, 0, 0, time.UTC)),
				Retention:    7,
				StorageDescriptor: &awsgluetypes.StorageDescriptor{
					Location:     aws.String("s3://analytics-warehouse/orders/"),
					InputFormat:  aws.String("ParquetInputFormat"),
					OutputFormat: aws.String("ParquetOutputFormat"),
					Compressed:   true,
					Columns: []awsgluetypes.Column{
						{Name: aws.String("order_id")},
						{Name: aws.String("customer_id")},
					},
					SerdeInfo: &awsgluetypes.SerDeInfo{
						Name:                 aws.String("ParquetHiveSerDe"),
						SerializationLibrary: aws.String("org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe"),
					},
				},
				PartitionKeys: []awsgluetypes.Column{{Name: aws.String("event_date")}},
				Parameters:    map[string]string{"classification": "parquet"},
			}},
		}},
	}
	adapter := &Client{
		client:   client,
		boundary: testBoundary(),
	}

	databases, err := adapter.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases() error = %v, want nil", err)
	}
	if got, want := len(databases), 1; got != want {
		t.Fatalf("len(databases) = %d, want %d", got, want)
	}
	database := databases[0]
	if database.Name != "analytics" {
		t.Fatalf("database.Name = %q, want analytics", database.Name)
	}
	if database.LocationURI != "s3://analytics-warehouse/" {
		t.Fatalf("database.LocationURI = %q, want s3://analytics-warehouse/", database.LocationURI)
	}
	if got, want := len(database.Tables), 1; got != want {
		t.Fatalf("len(database.Tables) = %d, want %d", got, want)
	}
	table := database.Tables[0]
	if table.StorageLocation != "s3://analytics-warehouse/orders/" {
		t.Fatalf("table.StorageLocation = %q, want s3://analytics-warehouse/orders/", table.StorageLocation)
	}
	if got, want := len(table.Columns), 2; got != want {
		t.Fatalf("len(table.Columns) = %d, want %d", got, want)
	}
	if table.SerdeLibrary != "org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe" {
		t.Fatalf("table.SerdeLibrary = %q", table.SerdeLibrary)
	}
}

func TestClientListConnectionsRequiresHidePasswordTrue(t *testing.T) {
	client := &fakeGlueAPI{
		connectionPages: []*awsglue.GetConnectionsOutput{{
			ConnectionList: []awsgluetypes.Connection{{
				Name:           aws.String("warehouse"),
				ConnectionType: awsgluetypes.ConnectionTypeJdbc,
				ConnectionProperties: map[string]string{
					"JDBC_CONNECTION_URL": "jdbc:postgresql://db/",
					"USERNAME":            "analyst",
					"PASSWORD":            "shouldNeverEscape",
				},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	connections, err := adapter.ListConnections(context.Background())
	if err != nil {
		t.Fatalf("ListConnections() error = %v, want nil", err)
	}
	if !client.connectionsHidePassword {
		t.Fatalf("GetConnections called without HidePassword=true; password values risk leaking")
	}
	if got, want := len(connections), 1; got != want {
		t.Fatalf("len(connections) = %d, want %d", got, want)
	}
	for _, key := range connections[0].PropertyKeys {
		if key == "" {
			continue
		}
		// adapter records key names only; scanner later drops secret-shaped keys.
	}
}

func TestClientListWorkflowsCallsGetWorkflowWithoutGraph(t *testing.T) {
	client := &fakeGlueAPI{
		workflowNamePages: []*awsglue.ListWorkflowsOutput{{
			Workflows: []string{"orders-pipeline"},
		}},
		workflowDescribe: map[string]*awsglue.GetWorkflowOutput{
			"orders-pipeline": {
				Workflow: &awsgluetypes.Workflow{
					Name:              aws.String("orders-pipeline"),
					Description:       aws.String("orders pipeline"),
					CreatedOn:         aws.Time(time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)),
					LastModifiedOn:    aws.Time(time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)),
					MaxConcurrentRuns: aws.Int32(1),
					DefaultRunProperties: map[string]string{
						"region": "us-east-1",
					},
				},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	workflows, err := adapter.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("ListWorkflows() error = %v, want nil", err)
	}
	if got, want := len(workflows), 1; got != want {
		t.Fatalf("len(workflows) = %d, want %d", got, want)
	}
	if workflows[0].Name != "orders-pipeline" {
		t.Fatalf("workflows[0].Name = %q, want orders-pipeline", workflows[0].Name)
	}
	if client.workflowDescribeIncludesGraph {
		t.Fatalf("GetWorkflow called with IncludeGraph=true; graph payload must stay outside scanner contract")
	}
}

func TestClientListJobsAndTriggersMapSafeMetadata(t *testing.T) {
	client := &fakeGlueAPI{
		jobPages: []*awsglue.GetJobsOutput{{
			Jobs: []awsgluetypes.Job{{
				Name:            aws.String("orders-etl"),
				Description:     aws.String("orders ETL"),
				Role:            aws.String("arn:aws:iam::123456789012:role/glue-orders-etl"),
				GlueVersion:     aws.String("4.0"),
				WorkerType:      awsgluetypes.WorkerTypeG1x,
				NumberOfWorkers: aws.Int32(10),
				Command: &awsgluetypes.JobCommand{
					Name:           aws.String("glueetl"),
					ScriptLocation: aws.String("s3://analytics-scripts/orders.py"),
					PythonVersion:  aws.String("3"),
				},
				DefaultArguments: map[string]string{
					"--TempDir":             "s3://temp/",
					"--connection-password": "shouldNeverEscape",
				},
			}},
		}},
		triggerPages: []*awsglue.GetTriggersOutput{{
			Triggers: []awsgluetypes.Trigger{{
				Name:         aws.String("orders-nightly"),
				Type:         awsgluetypes.TriggerTypeScheduled,
				State:        awsgluetypes.TriggerStateActivated,
				Schedule:     aws.String("cron(0 4 * * ? *)"),
				WorkflowName: aws.String("orders-pipeline"),
				Actions: []awsgluetypes.Action{{
					JobName: aws.String("orders-etl"),
				}},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	jobs, err := adapter.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("ListJobs() error = %v, want nil", err)
	}
	if got, want := len(jobs), 1; got != want {
		t.Fatalf("len(jobs) = %d, want %d", got, want)
	}
	job := jobs[0]
	if job.ScriptLocation != "s3://analytics-scripts/orders.py" {
		t.Fatalf("job.ScriptLocation = %q", job.ScriptLocation)
	}
	hasTempDir := false
	for _, key := range job.DefaultArgKeys {
		if key == "--TempDir" {
			hasTempDir = true
		}
	}
	if !hasTempDir {
		t.Fatalf("job.DefaultArgKeys = %#v, want --TempDir present", job.DefaultArgKeys)
	}

	triggers, err := adapter.ListTriggers(context.Background())
	if err != nil {
		t.Fatalf("ListTriggers() error = %v, want nil", err)
	}
	if got, want := len(triggers), 1; got != want {
		t.Fatalf("len(triggers) = %d, want %d", got, want)
	}
	trigger := triggers[0]
	if got, want := len(trigger.ActionJobs), 1; got != want {
		t.Fatalf("len(trigger.ActionJobs) = %d, want %d", got, want)
	}
	if trigger.ActionJobs[0] != "orders-etl" {
		t.Fatalf("trigger.ActionJobs[0] = %q, want orders-etl", trigger.ActionJobs[0])
	}
}

func TestClientListCrawlersMapsTargetCountsAndScheduleWithoutTargetPayloads(t *testing.T) {
	client := &fakeGlueAPI{
		crawlerPages: []*awsglue.GetCrawlersOutput{{
			Crawlers: []awsgluetypes.Crawler{{
				Name:         aws.String("orders-crawler"),
				Role:         aws.String("arn:aws:iam::123456789012:role/glue-crawler"),
				DatabaseName: aws.String("analytics"),
				State:        awsgluetypes.CrawlerStateReady,
				Schedule: &awsgluetypes.Schedule{
					ScheduleExpression: aws.String("cron(0 1 * * ? *)"),
				},
				RecrawlPolicy: &awsgluetypes.RecrawlPolicy{
					RecrawlBehavior: awsgluetypes.RecrawlBehaviorCrawlEverything,
				},
				Targets: &awsgluetypes.CrawlerTargets{
					S3Targets: []awsgluetypes.S3Target{
						{Path: aws.String("s3://analytics-warehouse/orders/")},
					},
					JdbcTargets: []awsgluetypes.JdbcTarget{{
						ConnectionName: aws.String("warehouse"),
						Path:           aws.String("postgresql://db/orders"),
					}},
				},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	crawlers, err := adapter.ListCrawlers(context.Background())
	if err != nil {
		t.Fatalf("ListCrawlers() error = %v, want nil", err)
	}
	if got, want := len(crawlers), 1; got != want {
		t.Fatalf("len(crawlers) = %d, want %d", got, want)
	}
	crawler := crawlers[0]
	if got, want := crawler.S3TargetCount, 1; got != want {
		t.Fatalf("crawler.S3TargetCount = %d, want %d", got, want)
	}
	if got, want := crawler.JDBCTargetCount, 1; got != want {
		t.Fatalf("crawler.JDBCTargetCount = %d, want %d", got, want)
	}
	if crawler.Schedule != "cron(0 1 * * ? *)" {
		t.Fatalf("crawler.Schedule = %q", crawler.Schedule)
	}
}

func TestClientMapDerivedKeysAreSortedDeterministically(t *testing.T) {
	// Go map iteration is randomized; the adapter must sort the keys it
	// forwards from job default-arguments, workflow default-run-properties,
	// and connection-property maps so fact payloads stay byte-identical
	// across rescans of the same Glue state.
	client := &fakeGlueAPI{
		jobPages: []*awsglue.GetJobsOutput{{
			Jobs: []awsgluetypes.Job{{
				Name: aws.String("orders-etl"),
				DefaultArguments: map[string]string{
					"--zeta":     "z",
					"--alpha":    "a",
					"--mike":     "m",
					"--bravo":    "b",
					"--lima":     "l",
					"--charlie":  "c",
					"--november": "n",
				},
				NonOverridableArguments: map[string]string{
					"--zulu":  "z",
					"--alpha": "a",
					"--lima":  "l",
				},
			}},
		}},
		workflowNamePages: []*awsglue.ListWorkflowsOutput{{
			Workflows: []string{"orders-pipeline"},
		}},
		workflowDescribe: map[string]*awsglue.GetWorkflowOutput{
			"orders-pipeline": {
				Workflow: &awsgluetypes.Workflow{
					Name: aws.String("orders-pipeline"),
					DefaultRunProperties: map[string]string{
						"zone":   "us-east-1a",
						"region": "us-east-1",
						"tenant": "acme",
						"squad":  "data",
						"env":    "prod",
					},
				},
			},
		},
		connectionPages: []*awsglue.GetConnectionsOutput{{
			ConnectionList: []awsgluetypes.Connection{{
				Name: aws.String("warehouse"),
				ConnectionProperties: map[string]string{
					"USERNAME":            "u",
					"JDBC_CONNECTION_URL": "j",
					"PORT":                "5432",
					"HOST":                "db",
					"KAFKA_SSL_ENABLED":   "true",
				},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	jobs, err := adapter.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("ListJobs() error = %v, want nil", err)
	}
	if got, want := jobs[0].DefaultArgKeys, []string{"--alpha", "--bravo", "--charlie", "--lima", "--mike", "--november", "--zeta"}; !sliceEqual(got, want) {
		t.Fatalf("DefaultArgKeys = %#v, want sorted %#v", got, want)
	}
	if got, want := jobs[0].NonOverridableArgKeys, []string{"--alpha", "--lima", "--zulu"}; !sliceEqual(got, want) {
		t.Fatalf("NonOverridableArgKeys = %#v, want sorted %#v", got, want)
	}

	workflows, err := adapter.ListWorkflows(context.Background())
	if err != nil {
		t.Fatalf("ListWorkflows() error = %v, want nil", err)
	}
	if got, want := workflows[0].DefaultRunKeys, []string{"env", "region", "squad", "tenant", "zone"}; !sliceEqual(got, want) {
		t.Fatalf("DefaultRunKeys = %#v, want sorted %#v", got, want)
	}

	connections, err := adapter.ListConnections(context.Background())
	if err != nil {
		t.Fatalf("ListConnections() error = %v, want nil", err)
	}
	if got, want := connections[0].PropertyKeys, []string{"HOST", "JDBC_CONNECTION_URL", "KAFKA_SSL_ENABLED", "PORT", "USERNAME"}; !sliceEqual(got, want) {
		t.Fatalf("PropertyKeys = %#v, want sorted %#v", got, want)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

type fakeGlueAPI struct {
	databasePages                 []*awsglue.GetDatabasesOutput
	databaseCalls                 int
	tablePages                    []*awsglue.GetTablesOutput
	tableCalls                    int
	crawlerPages                  []*awsglue.GetCrawlersOutput
	crawlerCalls                  int
	jobPages                      []*awsglue.GetJobsOutput
	jobCalls                      int
	triggerPages                  []*awsglue.GetTriggersOutput
	triggerCalls                  int
	workflowNamePages             []*awsglue.ListWorkflowsOutput
	workflowNameCalls             int
	workflowDescribe              map[string]*awsglue.GetWorkflowOutput
	workflowDescribeIncludesGraph bool
	connectionPages               []*awsglue.GetConnectionsOutput
	connectionCalls               int
	connectionsHidePassword       bool
}

func (f *fakeGlueAPI) GetDatabases(
	_ context.Context,
	_ *awsglue.GetDatabasesInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetDatabasesOutput, error) {
	if f.databaseCalls >= len(f.databasePages) {
		return &awsglue.GetDatabasesOutput{}, nil
	}
	page := f.databasePages[f.databaseCalls]
	f.databaseCalls++
	return page, nil
}

func (f *fakeGlueAPI) GetTables(
	_ context.Context,
	input *awsglue.GetTablesInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetTablesOutput, error) {
	if aws.ToString(input.DatabaseName) == "" {
		return nil, nil
	}
	if f.tableCalls >= len(f.tablePages) {
		return &awsglue.GetTablesOutput{}, nil
	}
	page := f.tablePages[f.tableCalls]
	f.tableCalls++
	return page, nil
}

func (f *fakeGlueAPI) GetCrawlers(
	_ context.Context,
	_ *awsglue.GetCrawlersInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetCrawlersOutput, error) {
	if f.crawlerCalls >= len(f.crawlerPages) {
		return &awsglue.GetCrawlersOutput{}, nil
	}
	page := f.crawlerPages[f.crawlerCalls]
	f.crawlerCalls++
	return page, nil
}

func (f *fakeGlueAPI) GetJobs(
	_ context.Context,
	_ *awsglue.GetJobsInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetJobsOutput, error) {
	if f.jobCalls >= len(f.jobPages) {
		return &awsglue.GetJobsOutput{}, nil
	}
	page := f.jobPages[f.jobCalls]
	f.jobCalls++
	return page, nil
}

func (f *fakeGlueAPI) GetTriggers(
	_ context.Context,
	_ *awsglue.GetTriggersInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetTriggersOutput, error) {
	if f.triggerCalls >= len(f.triggerPages) {
		return &awsglue.GetTriggersOutput{}, nil
	}
	page := f.triggerPages[f.triggerCalls]
	f.triggerCalls++
	return page, nil
}

func (f *fakeGlueAPI) ListWorkflows(
	_ context.Context,
	_ *awsglue.ListWorkflowsInput,
	_ ...func(*awsglue.Options),
) (*awsglue.ListWorkflowsOutput, error) {
	if f.workflowNameCalls >= len(f.workflowNamePages) {
		return &awsglue.ListWorkflowsOutput{}, nil
	}
	page := f.workflowNamePages[f.workflowNameCalls]
	f.workflowNameCalls++
	return page, nil
}

func (f *fakeGlueAPI) GetWorkflow(
	_ context.Context,
	input *awsglue.GetWorkflowInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetWorkflowOutput, error) {
	if aws.ToBool(input.IncludeGraph) {
		f.workflowDescribeIncludesGraph = true
	}
	if f.workflowDescribe == nil {
		return &awsglue.GetWorkflowOutput{}, nil
	}
	if output, ok := f.workflowDescribe[aws.ToString(input.Name)]; ok {
		return output, nil
	}
	return &awsglue.GetWorkflowOutput{}, nil
}

func (f *fakeGlueAPI) GetConnections(
	_ context.Context,
	input *awsglue.GetConnectionsInput,
	_ ...func(*awsglue.Options),
) (*awsglue.GetConnectionsOutput, error) {
	if input.HidePassword {
		f.connectionsHidePassword = true
	}
	if f.connectionCalls >= len(f.connectionPages) {
		return &awsglue.GetConnectionsOutput{}, nil
	}
	page := f.connectionPages[f.connectionCalls]
	f.connectionCalls++
	return page, nil
}

var _ apiClient = (*fakeGlueAPI)(nil)
