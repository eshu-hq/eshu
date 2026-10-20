// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	awsgluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
)

// TestClientListDatabasesPaginatesAcrossPages proves ListDatabases follows
// the NextToken across multiple GetDatabases pages (client.go ListDatabases
// lines 68–94). A non-empty NextToken on the first page triggers a second call;
// the empty NextToken on the second page terminates the loop.
func TestClientListDatabasesPaginatesAcrossPages(t *testing.T) {
	client := &fakeGlueAPI{
		databasePages: []*awsglue.GetDatabasesOutput{{
			DatabaseList: []awsgluetypes.Database{{
				Name:      aws.String("db-alpha"),
				CatalogId: aws.String("123456789012"),
			}},
			NextToken: aws.String("db-next"),
		}, {
			DatabaseList: []awsgluetypes.Database{{
				Name:      aws.String("db-beta"),
				CatalogId: aws.String("123456789012"),
			}},
			// NextToken absent — pagination terminates here.
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	databases, err := adapter.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases() error = %v, want nil", err)
	}
	if got, want := len(databases), 2; got != want {
		t.Fatalf("len(databases) = %d, want %d", got, want)
	}
	if databases[0].Name != "db-alpha" || databases[1].Name != "db-beta" {
		t.Fatalf("database names = %q / %q, want db-alpha / db-beta", databases[0].Name, databases[1].Name)
	}
}

// TestClientListDatabasesEmptyNameSkipsTableFetch proves that when a Database
// entry arrives with an empty Name, listTables returns nil early (client.go
// listTables line 122–125) rather than calling GetTables with an empty
// DatabaseName. An empty DatabaseName would be invalid and could produce
// unexpected AWS API errors.
func TestClientListDatabasesEmptyNameSkipsTableFetch(t *testing.T) {
	client := &fakeGlueAPI{
		databasePages: []*awsglue.GetDatabasesOutput{{
			DatabaseList: []awsgluetypes.Database{{
				// Name intentionally absent — listTables must not call GetTables.
				CatalogId: aws.String("123456789012"),
			}},
		}},
		tablePages: []*awsglue.GetTablesOutput{{
			TableList: []awsgluetypes.Table{{
				Name: aws.String("should-not-appear"),
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	databases, err := adapter.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases() error = %v, want nil", err)
	}
	if got, want := len(databases), 1; got != want {
		t.Fatalf("len(databases) = %d, want %d", got, want)
	}
	if len(databases[0].Tables) != 0 {
		t.Fatalf("databases[0].Tables = %#v, want empty (no GetTables call for empty database name)", databases[0].Tables)
	}
	if client.tableCalls != 0 {
		t.Fatalf("GetTables called %d times, want 0 (empty database name must skip the call)", client.tableCalls)
	}
}

// TestClientListConnectionsPhysicalRequirementsPreserved proves mapConnection
// records the subnet ID and security-group IDs from
// PhysicalConnectionRequirements (client.go mapConnection lines 487–491), which
// are the VPC-placement facts used downstream by the graph to locate Glue
// connections in the network topology.
func TestClientListConnectionsPhysicalRequirementsPreserved(t *testing.T) {
	client := &fakeGlueAPI{
		connectionPages: []*awsglue.GetConnectionsOutput{{
			ConnectionList: []awsgluetypes.Connection{{
				Name:           aws.String("vpc-jdbc"),
				ConnectionType: awsgluetypes.ConnectionTypeJdbc,
				ConnectionProperties: map[string]string{
					"JDBC_CONNECTION_URL": "jdbc:postgresql://db/",
				},
				PhysicalConnectionRequirements: &awsgluetypes.PhysicalConnectionRequirements{
					AvailabilityZone:    aws.String("us-east-1a"),
					SubnetId:            aws.String("subnet-abc"),
					SecurityGroupIdList: []string{"sg-001", "sg-002"},
				},
			}},
		}},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	connections, err := adapter.ListConnections(context.Background())
	if err != nil {
		t.Fatalf("ListConnections() error = %v, want nil", err)
	}
	if got, want := len(connections), 1; got != want {
		t.Fatalf("len(connections) = %d, want %d", got, want)
	}
	conn := connections[0]
	if conn.SubnetID != "subnet-abc" {
		t.Fatalf("conn.SubnetID = %q, want subnet-abc", conn.SubnetID)
	}
	if got, want := len(conn.SecurityGroupIDs), 2; got != want {
		t.Fatalf("len(conn.SecurityGroupIDs) = %d, want %d", got, want)
	}
	if conn.PhysicalRequirementsAZ != "us-east-1a" {
		t.Fatalf("conn.PhysicalRequirementsAZ = %q, want us-east-1a", conn.PhysicalRequirementsAZ)
	}
}
