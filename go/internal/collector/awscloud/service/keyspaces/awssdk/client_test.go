// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskeyspaces "github.com/aws/aws-sdk-go-v2/service/keyspaces"
	awskeyspacestypes "github.com/aws/aws-sdk-go-v2/service/keyspaces/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAdapterInterfaceExcludesDataPlaneAndMutationAPIs is the exclusion test
// required first by the metadata-only contract. It reflects over the apiClient
// interface the adapter depends on and fails if any CQL data-plane read or any
// keyspace/table mutation method is reachable. This guarantees the adapter
// cannot read table rows/cells or mutate resources even if a future edit adds a
// call, because the method would not exist on the constrained interface.
func TestAdapterInterfaceExcludesDataPlaneAndMutationAPIs(t *testing.T) {
	allowed := map[string]struct{}{
		"ListKeyspaces":       {},
		"GetKeyspace":         {},
		"ListTables":          {},
		"GetTable":            {},
		"ListTagsForResource": {},
	}
	forbiddenSubstrings := []string{
		"execute", // ExecuteStatement
		"batch",   // BatchStatement
		"select",  // CQL Select / row reads
		"query",   // any row query
		"create",  // CreateKeyspace / CreateTable / CreateType
		"delete",  // DeleteKeyspace / DeleteTable / DeleteType
		"update",  // UpdateKeyspace / UpdateTable
		"restore", // RestoreTable
		"put",     // any write
		"tagresource",
		"untag",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		method := ifaceType.Method(i)
		if _, ok := allowed[method.Name]; !ok {
			t.Fatalf("apiClient exposes unexpected method %q; the metadata-only adapter interface must stay limited to %v", method.Name, keys(allowed))
		}
		lower := strings.ToLower(method.Name)
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("apiClient method %q matches forbidden data-plane/mutation pattern %q", method.Name, forbidden)
			}
		}
	}
}

func keys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func TestClientSnapshotMapsKeyspacesAndTablesMetadataOnly(t *testing.T) {
	keyspaceARN := "arn:aws:cassandra:us-east-1:123456789012:/keyspace/orders/"
	tableARN := "arn:aws:cassandra:us-east-1:123456789012:/keyspace/orders/table/events"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeKeyspacesAPI{
		keyspacePages: []*awskeyspaces.ListKeyspacesOutput{{
			Keyspaces: []awskeyspacestypes.KeyspaceSummary{{
				KeyspaceName:        aws.String("orders"),
				ResourceArn:         aws.String(keyspaceARN),
				ReplicationStrategy: awskeyspacestypes.RsSingleRegion,
			}},
		}},
		keyspaces: map[string]*awskeyspaces.GetKeyspaceOutput{
			"orders": {
				KeyspaceName:        aws.String("orders"),
				ResourceArn:         aws.String(keyspaceARN),
				ReplicationStrategy: awskeyspacestypes.RsSingleRegion,
			},
		},
		tablePages: map[string][]*awskeyspaces.ListTablesOutput{
			"orders": {{
				Tables: []awskeyspacestypes.TableSummary{{
					KeyspaceName: aws.String("orders"),
					ResourceArn:  aws.String(tableARN),
				}},
			}},
		},
		tables: map[string]*awskeyspaces.GetTableOutput{
			"events": {
				KeyspaceName:      aws.String("orders"),
				TableName:         aws.String("events"),
				ResourceArn:       aws.String(tableARN),
				Status:            awskeyspacestypes.TableStatusActive,
				CreationTimestamp: aws.Time(createdAt),
				DefaultTimeToLive: aws.Int32(3600),
				CapacitySpecification: &awskeyspacestypes.CapacitySpecificationSummary{
					ThroughputMode:     awskeyspacestypes.ThroughputModeProvisioned,
					ReadCapacityUnits:  aws.Int64(5),
					WriteCapacityUnits: aws.Int64(10),
				},
				EncryptionSpecification: &awskeyspacestypes.EncryptionSpecification{
					Type:             awskeyspacestypes.EncryptionTypeCustomerManagedKmsKey,
					KmsKeyIdentifier: aws.String(kmsARN),
				},
				PointInTimeRecovery: &awskeyspacestypes.PointInTimeRecoverySummary{
					Status: awskeyspacestypes.PointInTimeRecoveryStatusEnabled,
				},
				SchemaDefinition: &awskeyspacestypes.SchemaDefinition{
					AllColumns: []awskeyspacestypes.ColumnDefinition{
						{Name: aws.String("tenant_id"), Type: aws.String("uuid")},
						{Name: aws.String("payload"), Type: aws.String("text")},
					},
					PartitionKeys: []awskeyspacestypes.PartitionKey{{Name: aws.String("tenant_id")}},
					ClusteringKeys: []awskeyspacestypes.ClusteringKey{{
						Name:    aws.String("event_id"),
						OrderBy: awskeyspacestypes.SortOrderAsc,
					}},
					StaticColumns: []awskeyspacestypes.StaticColumn{{Name: aws.String("tenant_name")}},
				},
			},
		},
		tags: map[string][]*awskeyspaces.ListTagsForResourceOutput{
			tableARN: {{
				Tags: []awskeyspacestypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
			}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	snapshot, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if got, want := len(snapshot.Keyspaces), 1; got != want {
		t.Fatalf("len(Keyspaces) = %d, want %d", got, want)
	}
	if got := snapshot.Keyspaces[0].ARN; got != keyspaceARN {
		t.Fatalf("keyspace ARN = %q, want %q", got, keyspaceARN)
	}
	if got, want := len(snapshot.Tables), 1; got != want {
		t.Fatalf("len(Tables) = %d, want %d", got, want)
	}
	table := snapshot.Tables[0]
	if table.ARN != tableARN || table.Name != "events" || table.KeyspaceName != "orders" {
		t.Fatalf("table identity = %#v, want ARN/name/keyspace", table)
	}
	if table.KeyspaceARN != keyspaceARN {
		t.Fatalf("table KeyspaceARN = %q, want %q (joins keyspace node)", table.KeyspaceARN, keyspaceARN)
	}
	if table.CapacityMode != "PROVISIONED" || table.ReadCapacityUnits != 5 {
		t.Fatalf("table capacity = %#v, want PROVISIONED/5", table)
	}
	if table.Encryption.KMSKeyIdentifier != kmsARN {
		t.Fatalf("table KMS identifier = %q, want %q", table.Encryption.KMSKeyIdentifier, kmsARN)
	}
	if table.PointInTimeRecovery.Status != "ENABLED" {
		t.Fatalf("table PITR = %q, want ENABLED", table.PointInTimeRecovery.Status)
	}
	if len(table.Schema.Columns) != 2 || table.Schema.Columns[0].Name != "tenant_id" {
		t.Fatalf("schema columns = %#v, want structural column names", table.Schema.Columns)
	}
	if table.Tags["Environment"] != "prod" {
		t.Fatalf("tags = %#v, want prod environment tag", table.Tags)
	}
	if got, want := api.getTableNames, []string{"events"}; !stringSlicesEqual(got, want) {
		t.Fatalf("GetTable names = %#v, want %#v", got, want)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceKeyspaces,
	}
}

type fakeKeyspacesAPI struct {
	keyspacePages []*awskeyspaces.ListKeyspacesOutput
	keyspaceCalls int
	keyspaces     map[string]*awskeyspaces.GetKeyspaceOutput
	tablePages    map[string][]*awskeyspaces.ListTablesOutput
	tableCalls    map[string]int
	tables        map[string]*awskeyspaces.GetTableOutput
	getTableNames []string
	tags          map[string][]*awskeyspaces.ListTagsForResourceOutput
	tagCalls      map[string]int
}

func (f *fakeKeyspacesAPI) ListKeyspaces(
	_ context.Context,
	_ *awskeyspaces.ListKeyspacesInput,
	_ ...func(*awskeyspaces.Options),
) (*awskeyspaces.ListKeyspacesOutput, error) {
	if f.keyspaceCalls >= len(f.keyspacePages) {
		return &awskeyspaces.ListKeyspacesOutput{}, nil
	}
	page := f.keyspacePages[f.keyspaceCalls]
	f.keyspaceCalls++
	return page, nil
}

func (f *fakeKeyspacesAPI) GetKeyspace(
	_ context.Context,
	input *awskeyspaces.GetKeyspaceInput,
	_ ...func(*awskeyspaces.Options),
) (*awskeyspaces.GetKeyspaceOutput, error) {
	if output := f.keyspaces[aws.ToString(input.KeyspaceName)]; output != nil {
		return output, nil
	}
	return &awskeyspaces.GetKeyspaceOutput{}, nil
}

func (f *fakeKeyspacesAPI) ListTables(
	_ context.Context,
	input *awskeyspaces.ListTablesInput,
	_ ...func(*awskeyspaces.Options),
) (*awskeyspaces.ListTablesOutput, error) {
	keyspaceName := aws.ToString(input.KeyspaceName)
	if f.tableCalls == nil {
		f.tableCalls = map[string]int{}
	}
	pages := f.tablePages[keyspaceName]
	if f.tableCalls[keyspaceName] >= len(pages) {
		return &awskeyspaces.ListTablesOutput{}, nil
	}
	page := pages[f.tableCalls[keyspaceName]]
	f.tableCalls[keyspaceName]++
	return page, nil
}

func (f *fakeKeyspacesAPI) GetTable(
	_ context.Context,
	input *awskeyspaces.GetTableInput,
	_ ...func(*awskeyspaces.Options),
) (*awskeyspaces.GetTableOutput, error) {
	tableName := aws.ToString(input.TableName)
	f.getTableNames = append(f.getTableNames, tableName)
	if output := f.tables[tableName]; output != nil {
		return output, nil
	}
	return &awskeyspaces.GetTableOutput{}, nil
}

func (f *fakeKeyspacesAPI) ListTagsForResource(
	_ context.Context,
	input *awskeyspaces.ListTagsForResourceInput,
	_ ...func(*awskeyspaces.Options),
) (*awskeyspaces.ListTagsForResourceOutput, error) {
	resourceARN := aws.ToString(input.ResourceArn)
	if f.tagCalls == nil {
		f.tagCalls = map[string]int{}
	}
	pages := f.tags[resourceARN]
	if f.tagCalls[resourceARN] >= len(pages) {
		return &awskeyspaces.ListTagsForResourceOutput{}, nil
	}
	page := pages[f.tagCalls[resourceARN]]
	f.tagCalls[resourceARN]++
	return page, nil
}

var _ apiClient = (*fakeKeyspacesAPI)(nil)

func stringSlicesEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
