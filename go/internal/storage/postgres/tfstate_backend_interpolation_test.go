// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestTerraformStateBackendFactReaderResolvesVariableDefaultCandidate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"repo-infra",
				[]byte(`{
					"terraform_variables":[{
						"name":"state_bucket",
						"default":"app-tfstate-prod",
						"path":"variables.tf"
					}],
					"terraform_backends":[{
						"backend_kind":"s3",
						"bucket":"var.state_bucket",
						"bucket_is_literal":false,
						"key":"services/api/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true,
						"path":"backend.tf"
					}]
				}`),
			}}},
			{},
		},
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
	if got, want := candidates[0].State.Locator, "s3://app-tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
	if !strings.Contains(db.queries[0].query, "terraform_variables") {
		t.Fatalf("backend fact query must include variable context, got: %s", db.queries[0].query)
	}
}

func TestTerraformStateBackendFactReaderResolvesVariableDefaultAcrossModuleFiles(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{
					"repo-infra",
					[]byte(`{
						"terraform_backends":[{
							"backend_kind":"s3",
							"bucket":"var.state_bucket",
							"bucket_is_literal":false,
							"key":"services/api/terraform.tfstate",
							"key_is_literal":true,
							"region":"us-east-1",
							"region_is_literal":true,
							"path":"backend.tf"
						}]
					}`),
				},
				{
					"repo-infra",
					[]byte(`{
						"terraform_variables":[{
							"name":"state_bucket",
							"default":"app-tfstate-prod",
							"path":"variables.tf"
						}]
					}`),
				},
			}},
			{},
		},
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
	if got, want := candidates[0].State.Locator, "s3://app-tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
	if !strings.Contains(db.queries[0].query, "terraform_variables") {
		t.Fatalf("backend fact query must include variable context, got: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "terraform_locals") {
		t.Fatalf("backend fact query must include local context, got: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_variables') = 'array'") {
		t.Fatalf("backend fact query must return variable-only files for module context, got: %s", db.queries[0].query)
	}
}

func TestTerraformStateBackendFactReaderResolvesLocalTemplateCandidate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"repo-infra",
				[]byte(`{
					"terraform_variables":[{
						"name":"service_name",
						"default":"api",
						"path":"env/prod/variables.tf"
					}],
					"terraform_locals":[{
						"name":"state_prefix",
						"value":"services/${var.service_name}",
						"path":"env/prod/locals.tf"
					}],
					"terraform_backends":[{
						"backend_kind":"s3",
						"bucket":"app-tfstate-prod",
						"bucket_is_literal":true,
						"key":"${local.state_prefix}/terraform.tfstate",
						"key_is_literal":false,
						"region":"us-east-1",
						"region_is_literal":true,
						"path":"env/prod/backend.tf"
					}]
				}`),
			}}},
			{},
		},
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
	if got, want := candidates[0].State.Locator, "s3://app-tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
}

func TestTerraformStateBackendFactReaderRejectsUnresolvedExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		context string
	}{
		{
			name: "missing variable default",
			context: `{
				"terraform_variables":[{"name":"state_bucket","path":"variables.tf"}],
				"terraform_backends":[{
					"backend_kind":"s3",
					"bucket":"var.state_bucket",
					"bucket_is_literal":false,
					"key":"services/api/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true,
					"path":"backend.tf"
				}]
			}`,
		},
		{
			name: "duplicate variable definitions",
			context: `{
				"terraform_variables":[
					{"name":"state_bucket","default":"first-tfstate","path":"variables.tf"},
					{"name":"state_bucket","default":"second-tfstate","path":"extra.tf"}
				],
				"terraform_backends":[{
					"backend_kind":"s3",
					"bucket":"var.state_bucket",
					"bucket_is_literal":false,
					"key":"services/api/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true,
					"path":"backend.tf"
				}]
			}`,
		},
		{
			name: "module output reference",
			context: `{
				"terraform_backends":[{
					"backend_kind":"s3",
					"bucket":"module.state.bucket",
					"bucket_is_literal":false,
					"key":"services/api/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true,
					"path":"backend.tf"
				}]
			}`,
		},
		{
			name: "workspace key interpolation",
			context: `{
				"terraform_backends":[{
					"backend_kind":"s3",
					"bucket":"app-tfstate-prod",
					"bucket_is_literal":true,
					"key":"services/${terraform.workspace}/terraform.tfstate",
					"key_is_literal":false,
					"region":"us-east-1",
					"region_is_literal":true,
					"path":"backend.tf"
				}]
			}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{
				queryResponses: []queueFakeRows{
					{rows: [][]any{{"repo-infra", []byte(tt.context)}}},
					{},
				},
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
				t.Fatalf("candidates = %#v, want none for unresolved expression", candidates)
			}
		})
	}
}

func TestTerraformStateBackendFactReaderFiltersResolvedVariableCandidate(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"repo-infra",
				[]byte(`{
					"terraform_variables":[{
						"name":"state_bucket",
						"default":"app-tfstate-prod",
						"path":"variables.tf"
					}],
					"terraform_backends":[{
						"backend_kind":"s3",
						"bucket":"var.state_bucket",
						"bucket_is_literal":false,
						"key":"services/api/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true,
						"path":"backend.tf"
					}]
				}`),
			}}},
			{},
		},
	}
	reader := TerraformStateBackendFactReader{DB: db}

	candidates, err := reader.TerraformStateCandidates(
		context.Background(),
		terraformstate.DiscoveryQuery{BackendFilters: []terraformstate.DiscoveryBackendFilter{{
			TargetScopeID: "aws-prod",
			BackendKind:   terraformstate.BackendS3,
			Bucket:        "app-tfstate-prod",
			Key:           "services/api/terraform.tfstate",
			Region:        "us-east-1",
		}}},
	)
	if err != nil {
		t.Fatalf("TerraformStateCandidates() error = %v, want nil", err)
	}

	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(candidates) = %d, want %d", got, want)
	}
	if got, want := candidates[0].TargetScopeID, "aws-prod"; got != want {
		t.Fatalf("TargetScopeID = %q, want %q", got, want)
	}
	if !strings.Contains(db.queries[0].query, "LIKE 'var.%'") {
		t.Fatalf("filtered query must allow Go-side variable resolution, got: %s", db.queries[0].query)
	}
	if !strings.Contains(db.queries[0].query, "jsonb_array_elements(\n              CASE") {
		t.Fatalf("filtered query must guard jsonb_array_elements with CASE, got: %s", db.queries[0].query)
	}
}

func TestPostgresTerraformBackendQueryResolvesVariableDefaultForLocatorHash(t *testing.T) {
	t.Parallel()

	bucket := "app-tfstate-prod"
	key := "services/api/terraform.tfstate"
	hash := expectedLocatorHash(bucket, key)
	observed := time.Date(2026, time.June, 13, 15, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			"repo-infra",
			"repository:repo-infra",
			"gen-abc",
			observed,
			[]byte(`{
				"terraform_variables":[{
					"name":"state_bucket",
					"default":"app-tfstate-prod",
					"path":"variables.tf"
				}],
				"terraform_backends":[{
					"backend_kind":"s3",
					"bucket":"var.state_bucket",
					"bucket_is_literal":false,
					"key":"services/api/terraform.tfstate",
					"key_is_literal":true,
					"region":"us-east-1",
					"region_is_literal":true,
					"path":"backend.tf"
				}]
			}`),
		}}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].RepoID, "repo-infra"; got != want {
		t.Fatalf("RepoID = %q, want %q", got, want)
	}
	if got, want := rows[0].LocatorHash, hash; got != want {
		t.Fatalf("LocatorHash = %q, want %q", got, want)
	}
	if !strings.Contains(db.queries[0].query, "terraform_variables") {
		t.Fatalf("canonical query must include variable context, got: %s", db.queries[0].query)
	}
}

func TestPostgresTerraformBackendQueryResolvesVariableDefaultAcrossModuleFiles(t *testing.T) {
	t.Parallel()

	bucket := "app-tfstate-prod"
	key := "services/api/terraform.tfstate"
	hash := expectedLocatorHash(bucket, key)
	observed := time.Date(2026, time.June, 13, 16, 10, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			{
				"repo-infra",
				"repository:repo-infra",
				"gen-abc",
				observed,
				[]byte(`{
					"terraform_backends":[{
						"backend_kind":"s3",
						"bucket":"var.state_bucket",
						"bucket_is_literal":false,
						"key":"services/api/terraform.tfstate",
						"key_is_literal":true,
						"region":"us-east-1",
						"region_is_literal":true,
						"path":"backend.tf"
					}]
				}`),
			},
			{
				"repo-infra",
				"repository:repo-infra",
				"gen-abc",
				observed,
				[]byte(`{
					"terraform_variables":[{
						"name":"state_bucket",
						"default":"app-tfstate-prod",
						"path":"variables.tf"
					}]
				}`),
			},
		}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].RepoID, "repo-infra"; got != want {
		t.Fatalf("RepoID = %q, want %q", got, want)
	}
	if got, want := rows[0].LocatorHash, hash; got != want {
		t.Fatalf("LocatorHash = %q, want %q", got, want)
	}
	if !strings.Contains(db.queries[0].query, "backend_generations") {
		t.Fatalf("canonical query must group context by backend generations, got: %s", db.queries[0].query)
	}
}
