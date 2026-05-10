package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestTerraformStateBackendFactReaderReturnsS3Candidates(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"repo-infra",
				[]byte(`[{
					"backend_kind":"s3",
					"bucket":"app-tfstate-prod",
					"bucket_is_literal":true,
					"key":"services/api/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true,
					"dynamodb_table":"tfstate-locks-api",
					"dynamodb_table_is_literal":true
				}]`),
			}},
		}},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{RepoIDs: []string{"repo-infra"}},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}

	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	candidate := candidates[0]
	if got, want := candidate.Source, terraformstate.DiscoveryCandidateSourceGraph; got != want {
		t.Fatalf("candidate.Source = %q, want %q", got, want)
	}
	if got, want := candidate.RepoID, "repo-infra"; got != want {
		t.Fatalf("candidate.RepoID = %q, want %q", got, want)
	}
	if got, want := candidate.State.BackendKind, terraformstate.BackendS3; got != want {
		t.Fatalf("candidate.State.BackendKind = %q, want %q", got, want)
	}
	if got, want := candidate.State.Locator, "s3://app-tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("candidate.State.Locator = %q, want %q", got, want)
	}
	if got, want := candidate.Region, "us-east-1"; got != want {
		t.Fatalf("candidate.Region = %q, want %q", got, want)
	}
	if got, want := candidate.DynamoDBTable, "tfstate-locks-api"; got != want {
		t.Fatalf("candidate.DynamoDBTable = %q, want %q", got, want)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"FROM fact_records",
		"terraform_backends",
		"active_generation_id",
		"generation.status = 'active'",
		"fact.source_system = 'git'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
	if strings.Contains(query, "latest_generations") {
		t.Fatalf("query contains latest generation fallback: %s", query)
	}
}

func TestTerraformStateBackendFactReaderSkipsNonExactCandidates(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"repo-infra",
				[]byte(`[
					{"backend_kind":"local","path":"terraform.tfstate"},
					{"backend_kind":"s3","bucket":"app-tfstate-prod","key":"services/${workspace}/terraform.tfstate","region":"us-east-1"},
					{"backend_kind":"s3","bucket":"app-tfstate-prod","key":"services/api/terraform.tfstate"},
					{"backend_kind":"s3","bucket":"app-tfstate-prod","bucket_is_literal":true,"key":"services/api/terraform.tfstate","key_is_literal":false,"region":"us-east-1","region_is_literal":true}
				]`),
			}},
		}},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{RepoIDs: []string{"repo-infra"}},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %#v, want none for non-exact backend facts", candidates)
	}
}

func TestTerraformStateBackendFactReaderDoesNotCarryDynamicDynamoDBTable(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				"repo-infra",
				[]byte(`[{
					"backend_kind":"s3",
					"bucket":"app-tfstate-prod",
					"bucket_is_literal":true,
					"key":"services/api/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true,
					"dynamodb_table":"var.lock_table",
					"dynamodb_table_is_literal":false
				}]`),
			}},
		}},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{RepoIDs: []string{"repo-infra"}},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got := candidates[0].DynamoDBTable; got != "" {
		t.Fatalf("DynamoDBTable = %q, want blank for dynamic table expression", got)
	}
}

func TestTerraformStatePriorSnapshotReaderReturnsETagByLocatorHash(t *testing.T) {
	t.Parallel()

	stateKey := terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://app-tfstate-prod/services/api/terraform.tfstate",
	}
	locatorHash := terraformstate.LocatorHash(stateKey)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{
			rows: [][]any{{
				locatorHash,
				`"etag-123"`,
				"terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17",
			}},
		}},
	}
	reader := TerraformStatePriorSnapshotReader{DB: db}

	metadata, err := reader.TerraformStatePriorSnapshotMetadata(
		context.Background(),
		[]terraformstate.StateKey{stateKey},
	)
	if err != nil {
		t.Fatalf("TerraformStatePriorSnapshotMetadata() error = %v, want nil", err)
	}
	if got, want := metadata[stateKey].ETag, `"etag-123"`; got != want {
		t.Fatalf("ETag = %q, want %q", got, want)
	}
	if got, want := metadata[stateKey].GenerationID, "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"terraform_state_snapshot",
		"locator_hash",
		"active_generation_id",
		"generation.status = 'active'",
		"etag",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestTerraformStateBackendFactReaderPropagatesQueryErrors(t *testing.T) {
	t.Parallel()

	reader := TerraformStateBackendFactReader{
		DB: &fakeExecQueryer{
			queryResponses: []queueFakeRows{{err: errors.New("boom")}},
		},
	}

	_, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{RepoIDs: []string{"repo-infra"}},
	)
	if err == nil {
		t.Fatal("TerraformStateCandidates() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "terraform state backend facts") {
		t.Fatalf("TerraformStateCandidates() error = %v, want backend fact context", err)
	}
}

func TestTerraformStateGitReadinessCheckerReportsActiveGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	checker := TerraformStateGitReadinessChecker{DB: db}

	ready, err := checker.GitGenerationCommitted(context.Background(), "repo-infra")
	if err != nil {
		t.Fatalf("GitGenerationCommitted() error = %v, want nil", err)
	}
	if !ready {
		t.Fatal("GitGenerationCommitted() = false, want true")
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{"active_generation_id", "fact_kind = 'repository'", "status = 'active'"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q: %s", want, query)
		}
	}
}

func TestTerraformStateGitReadinessCheckerReturnsFalseWithoutActiveGeneration(t *testing.T) {
	t.Parallel()

	checker := TerraformStateGitReadinessChecker{
		DB: &fakeExecQueryer{
			queryResponses: []queueFakeRows{{rows: [][]any{{false}}}},
		},
	}

	ready, err := checker.GitGenerationCommitted(context.Background(), "repo-infra")
	if err != nil {
		t.Fatalf("GitGenerationCommitted() error = %v, want nil", err)
	}
	if ready {
		t.Fatal("GitGenerationCommitted() = true, want false")
	}
}
