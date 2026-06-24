package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestTerraformStateBackendFactReaderReturnsFilteredS3CandidatesAcrossRepos(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"helm-charts",
				[]byte(`[{
					"backend_kind":"s3",
					"bucket":"example-platform-qa-terraform-state",
					"bucket_is_literal":true,
					"key":"helm-charts/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true
				}]`),
			}}},
			{rows: [][]any{{
				"iac-terragrunt-core-infra",
				"/repos/iac-terragrunt-core-infra",
				[]byte(`[{
					"backend_kind":"s3",
					"bucket":"example-platform-qa-terraform-state",
					"bucket_is_literal":true,
					"key":"platform-qa/us-east-1/platform-qa.network-us-east-1/services/platform-qa-eks/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true
				}]`),
			}}},
		},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{
			BackendFilters: []terraformstate.DiscoveryBackendFilter{{
				TargetScopeID: "platform-qa-aws",
				BackendKind:   terraformstate.BackendS3,
				Bucket:        "example-platform-qa-terraform-state",
				Region:        "us-east-1",
			}},
		},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}

	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	for _, candidate := range candidates {
		if got, want := candidate.TargetScopeID, "platform-qa-aws"; got != want {
			t.Fatalf("TargetScopeID = %q, want %q", got, want)
		}
		if !strings.HasPrefix(candidate.State.Locator, "s3://example-platform-qa-terraform-state/") {
			t.Fatalf("Locator = %q, want platform-qa bucket", candidate.State.Locator)
		}
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if strings.Contains(db.queries[0].query, "requested_repos") {
		t.Fatalf("filtered query should not require repo scope: %s", db.queries[0].query)
	}
	filterArg := db.queries[0].args[0]
	for _, want := range []string{`"backend_kind":"s3"`, `"bucket":"example-platform-qa-terraform-state"`, `"region":"us-east-1"`} {
		if !strings.Contains(filterArg.(string), want) {
			t.Fatalf("filter arg = %#v, want JSON containing %q", filterArg, want)
		}
	}
}

func TestTerraformStateBackendFactReaderFiltersS3CandidatesByExactKey(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"api-infra",
				[]byte(`[
					{
						"backend_kind":"s3",
						"bucket":"remote-e2e-tfstate",
						"bucket_is_literal":true,
						"key":"services/api/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true
					},
					{
						"backend_kind":"s3",
						"bucket":"remote-e2e-tfstate",
						"bucket_is_literal":true,
						"key":"services/deleted/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true
					}
				]`),
			}}},
			{},
		},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{
			BackendFilters: []terraformstate.DiscoveryBackendFilter{{
				TargetScopeID: "aws-e2e",
				BackendKind:   terraformstate.BackendS3,
				Bucket:        "remote-e2e-tfstate",
				Key:           "services/api/terraform.tfstate",
				Region:        "us-east-1",
			}},
		},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d: %#v", got, want, candidates)
	}
	if got, want := candidates[0].State.Locator, "s3://remote-e2e-tfstate/services/api/terraform.tfstate"; got != want {
		t.Fatalf("candidate locator = %q, want %q", got, want)
	}
	filterArg := db.queries[0].args[0]
	if !strings.Contains(filterArg.(string), `"key":"services/api/terraform.tfstate"`) {
		t.Fatalf("filter arg = %#v, want JSON containing exact key", filterArg)
	}
}

func TestTerraformStateBackendFactReaderUnionsRepoScopedAndFilteredS3Candidates(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"repo-infra",
				[]byte(`[
					{
						"backend_kind":"s3",
						"bucket":"example-platform-qa-terraform-state",
						"bucket_is_literal":true,
						"key":"platform-qa/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true
					},
					{
						"backend_kind":"s3",
						"bucket":"example-platform-prod-terraform-state",
						"bucket_is_literal":true,
						"key":"platform-prod/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true
					}
				]`),
			}}},
			{},
			{rows: [][]any{{
				"helm-charts",
				[]byte(`[{
					"backend_kind":"s3",
					"bucket":"example-platform-qa-terraform-state",
					"bucket_is_literal":true,
					"key":"helm-charts/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true
				}]`),
			}}},
			{},
		},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{
			RepoIDs: []string{"repo-infra"},
			BackendFilters: []terraformstate.DiscoveryBackendFilter{{
				TargetScopeID: "platform-qa-aws",
				BackendKind:   terraformstate.BackendS3,
				Bucket:        "example-platform-qa-terraform-state",
				Region:        "us-east-1",
			}},
		},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}

	if got, want := len(candidates), 3; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	locators := map[string]terraformstate.DiscoveryCandidate{}
	for _, candidate := range candidates {
		locators[candidate.State.Locator] = candidate
	}
	if _, ok := locators["s3://example-platform-prod-terraform-state/platform-prod/terraform.tfstate"]; !ok {
		t.Fatalf("repo-scoped prod locator missing from hybrid union: %#v", locators)
	}
	repoScopedFiltered, ok := locators["s3://example-platform-qa-terraform-state/platform-qa/terraform.tfstate"]
	if !ok {
		t.Fatalf("repo-scoped filtered locator missing from hybrid union: %#v", locators)
	}
	if got, want := repoScopedFiltered.TargetScopeID, "platform-qa-aws"; got != want {
		t.Fatalf("repo-scoped filtered TargetScopeID = %q, want %q", got, want)
	}
	filtered, ok := locators["s3://example-platform-qa-terraform-state/helm-charts/terraform.tfstate"]
	if !ok {
		t.Fatalf("filtered global locator missing from hybrid union: %#v", locators)
	}
	if got, want := filtered.TargetScopeID, "platform-qa-aws"; got != want {
		t.Fatalf("filtered TargetScopeID = %q, want %q", got, want)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestTerraformStateBackendFactReaderBatchesMultipleBackendFilters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{},
			{},
		},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	_, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{
			BackendFilters: []terraformstate.DiscoveryBackendFilter{
				{
					BackendKind: terraformstate.BackendS3,
					Bucket:      "example-platform-qa-terraform-state",
					Region:      "us-east-1",
				},
				{
					BackendKind: terraformstate.BackendS3,
					Bucket:      "example-platform-prod-terraform-state",
					Region:      "us-east-1",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}

	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want one Terraform and one Terragrunt query", got)
	}
}
