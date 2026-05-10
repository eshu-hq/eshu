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
	if got := config.Seeds[0].PreviousETag; got != "" {
		t.Fatalf("Seeds[0].PreviousETag = %q, want blank because config is not durable metadata", got)
	}
}

func TestDiscoveryCarriesDurablePriorETagToSeedCandidate(t *testing.T) {
	t.Parallel()

	stateKey := StateKey{
		BackendKind: BackendS3,
		Locator:     "s3://app-tfstate-prod/services/api/terraform.tfstate",
	}
	resolver := DiscoveryResolver{
		Config: DiscoveryConfig{
			Seeds: []DiscoverySeed{{
				Kind:   BackendS3,
				Bucket: "app-tfstate-prod",
				Key:    "services/api/terraform.tfstate",
				Region: "us-east-1",
			}},
		},
		PriorSnapshots: fakePriorSnapshotReader{
			metadata: map[StateKey]PriorSnapshotMetadata{
				stateKey: {
					ETag:         `"etag-123"`,
					GenerationID: "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17",
				},
			},
		},
	}

	candidates, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if got, want := candidates[0].PreviousETag, `"etag-123"`; got != want {
		t.Fatalf("PreviousETag = %q, want durable ETag %q", got, want)
	}
	if got, want := candidates[0].PriorGenerationID, "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17"; got != want {
		t.Fatalf("PriorGenerationID = %q, want durable generation ID %q", got, want)
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

type fakePriorSnapshotReader struct {
	metadata map[StateKey]PriorSnapshotMetadata
}

func (r fakePriorSnapshotReader) TerraformStatePriorSnapshotMetadata(
	_ context.Context,
	states []StateKey,
) (map[StateKey]PriorSnapshotMetadata, error) {
	out := map[StateKey]PriorSnapshotMetadata{}
	for _, state := range states {
		if metadata, ok := r.metadata[state]; ok {
			out[state] = metadata
		}
	}
	return out, nil
}
