// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsathena "github.com/aws/aws-sdk-go-v2/service/athena"
	awsathenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListWorkGroupsReadsSafeMetadataAndDiscardsForbiddenFields(t *testing.T) {
	wgArn := "arn:aws:athena:us-east-1:123456789012:workgroup/primary"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	creation := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	api := &fakeAthenaAPI{
		workGroupPages: []*awsathena.ListWorkGroupsOutput{{
			WorkGroups: []awsathenatypes.WorkGroupSummary{{
				Name:        aws.String("primary"),
				Description: aws.String("default"),
				State:       awsathenatypes.WorkGroupStateEnabled,
			}},
		}},
		workGroupDetails: map[string]*awsathena.GetWorkGroupOutput{
			"primary": {
				WorkGroup: &awsathenatypes.WorkGroup{
					Name:         aws.String("primary"),
					Description:  aws.String("default workgroup"),
					State:        awsathenatypes.WorkGroupStateEnabled,
					CreationTime: aws.Time(creation),
					Configuration: &awsathenatypes.WorkGroupConfiguration{
						BytesScannedCutoffPerQuery:      aws.Int64(10737418240),
						EnforceWorkGroupConfiguration:   aws.Bool(true),
						PublishCloudWatchMetricsEnabled: aws.Bool(true),
						RequesterPaysEnabled:            aws.Bool(false),
						EngineVersion: &awsathenatypes.EngineVersion{
							EffectiveEngineVersion: aws.String("Athena engine version 3"),
							SelectedEngineVersion:  aws.String("AUTO"),
						},
						ResultConfiguration: &awsathenatypes.ResultConfiguration{
							OutputLocation:      aws.String("s3://athena-results-orders/queries/"),
							ExpectedBucketOwner: aws.String("123456789012"),
							EncryptionConfiguration: &awsathenatypes.EncryptionConfiguration{
								EncryptionOption: awsathenatypes.EncryptionOptionSseKms,
								KmsKey:           aws.String(kmsARN),
							},
						},
					},
				},
			},
		},
		workGroupTags: map[string][]awsathenatypes.Tag{
			wgArn: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceAthena,
		},
		workGroupARN: func(_ awscloud.Boundary, name string) string {
			if name == "primary" {
				return wgArn
			}
			return ""
		},
	}

	groups, err := adapter.ListWorkGroups(context.Background())
	if err != nil {
		t.Fatalf("ListWorkGroups() error = %v, want nil", err)
	}
	if got, want := len(groups), 1; got != want {
		t.Fatalf("len(groups) = %d, want %d", got, want)
	}
	group := groups[0]
	if group.Name != "primary" {
		t.Fatalf("group.Name = %q, want primary", group.Name)
	}
	if group.OutputLocation != "s3://athena-results-orders/queries/" {
		t.Fatalf("group.OutputLocation = %q", group.OutputLocation)
	}
	if group.EncryptionOption != "SSE_KMS" {
		t.Fatalf("group.EncryptionOption = %q", group.EncryptionOption)
	}
	if group.KMSKey != kmsARN {
		t.Fatalf("group.KMSKey = %q", group.KMSKey)
	}
	if !group.EnforceWorkGroupConfiguration {
		t.Fatalf("group.EnforceWorkGroupConfiguration = false, want true")
	}
	if !group.PublishCloudWatchMetricsEnabled {
		t.Fatalf("group.PublishCloudWatchMetricsEnabled = false, want true")
	}
	if group.RequesterPaysEnabled {
		t.Fatalf("group.RequesterPaysEnabled = true, want false")
	}
	if group.EngineVersion != "AUTO" {
		t.Fatalf("group.EngineVersion = %q, want AUTO", group.EngineVersion)
	}
	if group.EffectiveEngineVersion != "Athena engine version 3" {
		t.Fatalf("group.EffectiveEngineVersion = %q", group.EffectiveEngineVersion)
	}
	if group.BytesScannedCutoffPerQuery != 10737418240 {
		t.Fatalf("group.BytesScannedCutoffPerQuery = %d", group.BytesScannedCutoffPerQuery)
	}
	if group.ExpectedBucketOwner != "123456789012" {
		t.Fatalf("group.ExpectedBucketOwner = %q", group.ExpectedBucketOwner)
	}
	if group.Tags["Environment"] != "prod" {
		t.Fatalf("group.Tags = %#v, want Environment=prod", group.Tags)
	}
}

func TestClientListDataCatalogsReadsSafeMetadata(t *testing.T) {
	api := &fakeAthenaAPI{
		dataCatalogPages: []*awsathena.ListDataCatalogsOutput{{
			DataCatalogsSummary: []awsathenatypes.DataCatalogSummary{
				{CatalogName: aws.String("AwsDataCatalog"), Type: awsathenatypes.DataCatalogTypeGlue},
				{CatalogName: aws.String("external_orders"), Type: awsathenatypes.DataCatalogTypeLambda},
			},
		}},
		dataCatalogDetails: map[string]*awsathena.GetDataCatalogOutput{
			"AwsDataCatalog": {
				DataCatalog: &awsathenatypes.DataCatalog{
					Name:        aws.String("AwsDataCatalog"),
					Type:        awsathenatypes.DataCatalogTypeGlue,
					Description: aws.String("Glue catalog"),
				},
			},
			"external_orders": {
				DataCatalog: &awsathenatypes.DataCatalog{
					Name:        aws.String("external_orders"),
					Type:        awsathenatypes.DataCatalogTypeLambda,
					Description: aws.String("external orders catalog"),
				},
			},
		},
		dataCatalogTags: map[string][]awsathenatypes.Tag{
			"arn:aws:athena:us-east-1:123456789012:datacatalog/AwsDataCatalog": {
				{Key: aws.String("Owner"), Value: aws.String("platform")},
			},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceAthena,
		},
		dataCatalogARN: func(_ awscloud.Boundary, name string) string {
			return "arn:aws:athena:us-east-1:123456789012:datacatalog/" + name
		},
	}

	catalogs, err := adapter.ListDataCatalogs(context.Background())
	if err != nil {
		t.Fatalf("ListDataCatalogs() error = %v, want nil", err)
	}
	if got, want := len(catalogs), 2; got != want {
		t.Fatalf("len(catalogs) = %d, want %d", got, want)
	}
	names := make([]string, 0, len(catalogs))
	for _, catalog := range catalogs {
		names = append(names, catalog.Name)
	}
	sort.Strings(names)
	if want := []string{"AwsDataCatalog", "external_orders"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
	for _, catalog := range catalogs {
		if catalog.Name == "AwsDataCatalog" {
			if catalog.Type != "GLUE" {
				t.Fatalf("AwsDataCatalog Type = %q, want GLUE", catalog.Type)
			}
			if catalog.Tags["Owner"] != "platform" {
				t.Fatalf("AwsDataCatalog Tags = %#v", catalog.Tags)
			}
		}
		if catalog.Name == "external_orders" && catalog.Type != "LAMBDA" {
			t.Fatalf("external_orders Type = %q, want LAMBDA", catalog.Type)
		}
	}
}

func TestClientListPreparedStatementsReadsNamesAndNeverCallsGetPreparedStatement(t *testing.T) {
	modified := time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC)
	api := &fakeAthenaAPI{
		preparedStatementPages: map[string][]*awsathena.ListPreparedStatementsOutput{
			"primary": {{
				PreparedStatements: []awsathenatypes.PreparedStatementSummary{{
					StatementName:    aws.String("orders_by_day"),
					LastModifiedTime: aws.Time(modified),
				}},
			}},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceAthena,
		},
	}

	statements, err := adapter.ListPreparedStatements(context.Background(), []string{"primary"})
	if err != nil {
		t.Fatalf("ListPreparedStatements() error = %v, want nil", err)
	}
	if got, want := len(statements), 1; got != want {
		t.Fatalf("len(statements) = %d, want %d", got, want)
	}
	statement := statements[0]
	if statement.StatementName != "orders_by_day" {
		t.Fatalf("statement.StatementName = %q", statement.StatementName)
	}
	if statement.WorkGroupName != "primary" {
		t.Fatalf("statement.WorkGroupName = %q", statement.WorkGroupName)
	}
	if !statement.LastModifiedTime.Equal(modified) {
		t.Fatalf("statement.LastModifiedTime = %s, want %s", statement.LastModifiedTime, modified)
	}
	if api.getPreparedStatementCalls != 0 {
		t.Fatalf(
			"GetPreparedStatement called %d times; SDK adapter must never fetch prepared statement SQL bodies",
			api.getPreparedStatementCalls,
		)
	}
}

func TestClientListNamedQueriesStripsSQLBodyBeforeReturningToScanner(t *testing.T) {
	api := &fakeAthenaAPI{
		namedQueryPages: map[string][]*awsathena.ListNamedQueriesOutput{
			"primary": {{
				NamedQueryIds: []string{"11111111-2222-3333-4444-555555555555"},
			}},
		},
		batchGetNamedQueryOutput: &awsathena.BatchGetNamedQueryOutput{
			NamedQueries: []awsathenatypes.NamedQuery{{
				NamedQueryId: aws.String("11111111-2222-3333-4444-555555555555"),
				Name:         aws.String("daily-orders"),
				Database:     aws.String("orders"),
				Description:  aws.String("daily orders summary"),
				WorkGroup:    aws.String("primary"),
				QueryString:  aws.String("SELECT customer_email, ssn FROM orders WHERE ds = current_date"),
			}},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceAthena,
		},
	}

	queries, err := adapter.ListNamedQueries(context.Background(), []string{"primary"})
	if err != nil {
		t.Fatalf("ListNamedQueries() error = %v, want nil", err)
	}
	if got, want := len(queries), 1; got != want {
		t.Fatalf("len(queries) = %d, want %d", got, want)
	}
	query := queries[0]
	if query.NamedQueryID != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("query.NamedQueryID = %q", query.NamedQueryID)
	}
	if query.Name != "daily-orders" {
		t.Fatalf("query.Name = %q", query.Name)
	}
	if query.Database != "orders" {
		t.Fatalf("query.Database = %q", query.Database)
	}
	if query.WorkGroupName != "primary" {
		t.Fatalf("query.WorkGroupName = %q", query.WorkGroupName)
	}
	if query.Description != "daily orders summary" {
		t.Fatalf("query.Description = %q", query.Description)
	}
	value := reflect.ValueOf(query)
	for index := 0; index < value.NumField(); index++ {
		field := value.Type().Field(index)
		if got, ok := value.Field(index).Interface().(string); ok {
			if containsSQLBody(got) {
				t.Fatalf(
					"named query field %q leaked SQL body: %q; SDK adapter must discard QueryString",
					field.Name, got,
				)
			}
		}
	}
}

func TestClientNeverCallsGetNamedQuery(t *testing.T) {
	api := &fakeAthenaAPI{
		namedQueryPages: map[string][]*awsathena.ListNamedQueriesOutput{
			"primary": {{NamedQueryIds: []string{"id-1"}}},
		},
		batchGetNamedQueryOutput: &awsathena.BatchGetNamedQueryOutput{
			NamedQueries: []awsathenatypes.NamedQuery{{
				NamedQueryId: aws.String("id-1"),
				Name:         aws.String("daily-orders"),
				Database:     aws.String("orders"),
				WorkGroup:    aws.String("primary"),
				QueryString:  aws.String("SELECT 1"),
			}},
		},
	}
	adapter := &Client{
		client: api,
		boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceAthena,
		},
	}

	_, err := adapter.ListNamedQueries(context.Background(), []string{"primary"})
	if err != nil {
		t.Fatalf("ListNamedQueries() error = %v, want nil", err)
	}
	if api.getNamedQueryCalls != 0 {
		t.Fatalf(
			"GetNamedQuery called %d times; SDK adapter must never call per-query Get APIs",
			api.getNamedQueryCalls,
		)
	}
}

func containsSQLBody(value string) bool {
	for _, marker := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP"} {
		if len(value) == 0 {
			continue
		}
		if len(value) >= len(marker) && (value[:len(marker)] == marker) {
			return true
		}
	}
	return false
}

type fakeAthenaAPI struct {
	workGroupPages   []*awsathena.ListWorkGroupsOutput
	workGroupCalls   int
	workGroupDetails map[string]*awsathena.GetWorkGroupOutput
	workGroupTags    map[string][]awsathenatypes.Tag

	dataCatalogPages   []*awsathena.ListDataCatalogsOutput
	dataCatalogCalls   int
	dataCatalogDetails map[string]*awsathena.GetDataCatalogOutput
	dataCatalogTags    map[string][]awsathenatypes.Tag

	preparedStatementPages    map[string][]*awsathena.ListPreparedStatementsOutput
	preparedStatementCalls    map[string]int
	getPreparedStatementCalls int

	namedQueryPages          map[string][]*awsathena.ListNamedQueriesOutput
	namedQueryCalls          map[string]int
	batchGetNamedQueryOutput *awsathena.BatchGetNamedQueryOutput
	getNamedQueryCalls       int
}

func (f *fakeAthenaAPI) ListWorkGroups(
	_ context.Context,
	_ *awsathena.ListWorkGroupsInput,
	_ ...func(*awsathena.Options),
) (*awsathena.ListWorkGroupsOutput, error) {
	if f.workGroupCalls >= len(f.workGroupPages) {
		return &awsathena.ListWorkGroupsOutput{}, nil
	}
	page := f.workGroupPages[f.workGroupCalls]
	f.workGroupCalls++
	return page, nil
}

func (f *fakeAthenaAPI) GetWorkGroup(
	_ context.Context,
	input *awsathena.GetWorkGroupInput,
	_ ...func(*awsathena.Options),
) (*awsathena.GetWorkGroupOutput, error) {
	if input == nil {
		return &awsathena.GetWorkGroupOutput{}, nil
	}
	if output, ok := f.workGroupDetails[aws.ToString(input.WorkGroup)]; ok {
		return output, nil
	}
	return &awsathena.GetWorkGroupOutput{}, nil
}

func (f *fakeAthenaAPI) ListDataCatalogs(
	_ context.Context,
	_ *awsathena.ListDataCatalogsInput,
	_ ...func(*awsathena.Options),
) (*awsathena.ListDataCatalogsOutput, error) {
	if f.dataCatalogCalls >= len(f.dataCatalogPages) {
		return &awsathena.ListDataCatalogsOutput{}, nil
	}
	page := f.dataCatalogPages[f.dataCatalogCalls]
	f.dataCatalogCalls++
	return page, nil
}

func (f *fakeAthenaAPI) GetDataCatalog(
	_ context.Context,
	input *awsathena.GetDataCatalogInput,
	_ ...func(*awsathena.Options),
) (*awsathena.GetDataCatalogOutput, error) {
	if input == nil {
		return &awsathena.GetDataCatalogOutput{}, nil
	}
	if output, ok := f.dataCatalogDetails[aws.ToString(input.Name)]; ok {
		return output, nil
	}
	return &awsathena.GetDataCatalogOutput{}, nil
}

func (f *fakeAthenaAPI) ListPreparedStatements(
	_ context.Context,
	input *awsathena.ListPreparedStatementsInput,
	_ ...func(*awsathena.Options),
) (*awsathena.ListPreparedStatementsOutput, error) {
	if input == nil {
		return &awsathena.ListPreparedStatementsOutput{}, nil
	}
	workGroup := aws.ToString(input.WorkGroup)
	if f.preparedStatementCalls == nil {
		f.preparedStatementCalls = make(map[string]int)
	}
	pages := f.preparedStatementPages[workGroup]
	index := f.preparedStatementCalls[workGroup]
	if index >= len(pages) {
		return &awsathena.ListPreparedStatementsOutput{}, nil
	}
	page := pages[index]
	f.preparedStatementCalls[workGroup] = index + 1
	return page, nil
}

func (f *fakeAthenaAPI) ListNamedQueries(
	_ context.Context,
	input *awsathena.ListNamedQueriesInput,
	_ ...func(*awsathena.Options),
) (*awsathena.ListNamedQueriesOutput, error) {
	if input == nil {
		return &awsathena.ListNamedQueriesOutput{}, nil
	}
	workGroup := aws.ToString(input.WorkGroup)
	if f.namedQueryCalls == nil {
		f.namedQueryCalls = make(map[string]int)
	}
	pages := f.namedQueryPages[workGroup]
	index := f.namedQueryCalls[workGroup]
	if index >= len(pages) {
		return &awsathena.ListNamedQueriesOutput{}, nil
	}
	page := pages[index]
	f.namedQueryCalls[workGroup] = index + 1
	return page, nil
}

func (f *fakeAthenaAPI) BatchGetNamedQuery(
	_ context.Context,
	_ *awsathena.BatchGetNamedQueryInput,
	_ ...func(*awsathena.Options),
) (*awsathena.BatchGetNamedQueryOutput, error) {
	if f.batchGetNamedQueryOutput == nil {
		return &awsathena.BatchGetNamedQueryOutput{}, nil
	}
	return f.batchGetNamedQueryOutput, nil
}

func (f *fakeAthenaAPI) ListTagsForResource(
	_ context.Context,
	input *awsathena.ListTagsForResourceInput,
	_ ...func(*awsathena.Options),
) (*awsathena.ListTagsForResourceOutput, error) {
	if input == nil {
		return &awsathena.ListTagsForResourceOutput{}, nil
	}
	resource := aws.ToString(input.ResourceARN)
	if tags, ok := f.workGroupTags[resource]; ok {
		return &awsathena.ListTagsForResourceOutput{Tags: tags}, nil
	}
	if tags, ok := f.dataCatalogTags[resource]; ok {
		return &awsathena.ListTagsForResourceOutput{Tags: tags}, nil
	}
	return &awsathena.ListTagsForResourceOutput{}, nil
}

var _ apiClient = (*fakeAthenaAPI)(nil)
