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
					"bucket":"bg-ops-qa-terraform-state",
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
					"bucket":"bg-ops-qa-terraform-state",
					"bucket_is_literal":true,
					"key":"ops-qa/us-east-1/ops-qa.network-us-east-1/services/ops-qa-eks/terraform.tfstate",
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
				TargetScopeID: "ops-qa-aws",
				BackendKind:   terraformstate.BackendS3,
				Bucket:        "bg-ops-qa-terraform-state",
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
		if got, want := candidate.TargetScopeID, "ops-qa-aws"; got != want {
			t.Fatalf("TargetScopeID = %q, want %q", got, want)
		}
		if !strings.HasPrefix(candidate.State.Locator, "s3://bg-ops-qa-terraform-state/") {
			t.Fatalf("Locator = %q, want ops-qa bucket", candidate.State.Locator)
		}
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if strings.Contains(db.queries[0].query, "requested_repos") {
		t.Fatalf("filtered query should not require repo scope: %s", db.queries[0].query)
	}
	filterArg := db.queries[0].args[0]
	for _, want := range []string{`"backend_kind":"s3"`, `"bucket":"bg-ops-qa-terraform-state"`, `"region":"us-east-1"`} {
		if !strings.Contains(filterArg.(string), want) {
			t.Fatalf("filter arg = %#v, want JSON containing %q", filterArg, want)
		}
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
						"bucket":"bg-ops-qa-terraform-state",
						"bucket_is_literal":true,
						"key":"ops-qa/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true
					},
					{
						"backend_kind":"s3",
						"bucket":"bg-ops-prod-terraform-state",
						"bucket_is_literal":true,
						"key":"ops-prod/terraform.tfstate",
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
					"bucket":"bg-ops-qa-terraform-state",
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
				TargetScopeID: "ops-qa-aws",
				BackendKind:   terraformstate.BackendS3,
				Bucket:        "bg-ops-qa-terraform-state",
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
	if _, ok := locators["s3://bg-ops-prod-terraform-state/ops-prod/terraform.tfstate"]; !ok {
		t.Fatalf("repo-scoped prod locator missing from hybrid union: %#v", locators)
	}
	repoScopedFiltered, ok := locators["s3://bg-ops-qa-terraform-state/ops-qa/terraform.tfstate"]
	if !ok {
		t.Fatalf("repo-scoped filtered locator missing from hybrid union: %#v", locators)
	}
	if got, want := repoScopedFiltered.TargetScopeID, "ops-qa-aws"; got != want {
		t.Fatalf("repo-scoped filtered TargetScopeID = %q, want %q", got, want)
	}
	filtered, ok := locators["s3://bg-ops-qa-terraform-state/helm-charts/terraform.tfstate"]
	if !ok {
		t.Fatalf("filtered global locator missing from hybrid union: %#v", locators)
	}
	if got, want := filtered.TargetScopeID, "ops-qa-aws"; got != want {
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
					Bucket:      "bg-ops-qa-terraform-state",
					Region:      "us-east-1",
				},
				{
					BackendKind: terraformstate.BackendS3,
					Bucket:      "bg-ops-prod-terraform-state",
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
