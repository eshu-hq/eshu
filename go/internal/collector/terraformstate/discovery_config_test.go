package terraformstate

import (
	"context"
	"testing"
)

func TestParseDiscoveryConfigMapsCollectorJSON(t *testing.T) {
	t.Parallel()

	config, err := ParseDiscoveryConfig(`{
		"discovery": {
			"graph": true,
			"local_repos": ["platform-infra"],
			"seeds": [{
				"kind": "s3",
				"bucket": "app-tfstate-prod",
				"key": "services/api/terraform.tfstate",
				"region": "us-east-1",
				"repo_id": "platform-infra",
				"dynamodb_table": "tfstate-locks"
			}]
		}
	}`)
	if err != nil {
		t.Fatalf("ParseDiscoveryConfig() error = %v, want nil", err)
	}
	if !config.Graph {
		t.Fatal("Graph = false, want true")
	}
	if got, want := config.LocalRepos, []string{"platform-infra"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("LocalRepos = %#v, want %#v", got, want)
	}
	if got, want := config.Seeds[0].Kind, BackendS3; got != want {
		t.Fatalf("Seeds[0].Kind = %q, want %q", got, want)
	}
	if got, want := config.Seeds[0].DynamoDBTable, "tfstate-locks"; got != want {
		t.Fatalf("Seeds[0].DynamoDBTable = %q, want %q", got, want)
	}
}

func TestDiscoveryCarriesS3SeedDynamoDBTableToCandidate(t *testing.T) {
	t.Parallel()

	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Seeds: []DiscoverySeed{{
				Kind:          BackendS3,
				Bucket:        "app-tfstate-prod",
				Key:           "services/api/terraform.tfstate",
				Region:        "us-east-1",
				DynamoDBTable: "tfstate-locks-api",
			}},
		},
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := candidates[0].DynamoDBTable, "tfstate-locks-api"; got != want {
		t.Fatalf("DynamoDBTable = %q, want %q", got, want)
	}
}
