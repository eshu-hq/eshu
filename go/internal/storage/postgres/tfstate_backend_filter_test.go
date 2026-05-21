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
	if got, want := db.queries[0].args[0], "s3"; got != want {
		t.Fatalf("backend kind arg = %#v, want %q", got, want)
	}
	if got, want := db.queries[0].args[1], "bg-ops-qa-terraform-state"; got != want {
		t.Fatalf("bucket arg = %#v, want %q", got, want)
	}
	if got, want := db.queries[0].args[2], "us-east-1"; got != want {
		t.Fatalf("region arg = %#v, want %q", got, want)
	}
}

func TestTerraformStateBackendFactReaderFiltersRepoScopedS3Candidates(t *testing.T) {
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
			{rows: [][]any{{
				"repo-infra",
				"/repos/repo-infra",
				[]byte(`[{
					"backend_kind":"s3",
					"bucket":"bg-ops-qa-terraform-state",
					"bucket_is_literal":true,
					"key":"ops-qa/terragrunt.tfstate",
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

	if got, want := len(candidates), 2; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	for _, candidate := range candidates {
		if got, want := candidate.TargetScopeID, "ops-qa-aws"; got != want {
			t.Fatalf("TargetScopeID = %q, want %q", got, want)
		}
		if !strings.HasPrefix(candidate.State.Locator, "s3://bg-ops-qa-terraform-state/") {
			t.Fatalf("Locator = %q, want ops-qa bucket only", candidate.State.Locator)
		}
	}
}
