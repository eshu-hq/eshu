// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

// fixtureLocalBackendFact builds a `backend "local" {}` parser-fact row.
// backendFilePath is the absolute path of the .tf file the block was parsed
// from (row["path"], always absolute — see
// go/internal/parser/hcl/terraform_backend.go); statePath is the raw "path"
// ATTRIBUTE value the parser stores under row["state_path"] to avoid
// colliding with the file-path field (and with the durable `repository`
// fact's own, differently-scoped "local_path" field), and may be "" to
// exercise Terraform's own default (issue #5594).
func fixtureLocalBackendFact(backendFilePath, statePath string) []byte {
	statePathFields := ""
	if statePath != "" {
		statePathFields = `,
		"state_path":"` + statePath + `",
		"state_path_is_literal":true`
	}
	return []byte(`[{
		"backend_kind":"local",
		"path":"` + backendFilePath + `"` + statePathFields + `
	}]`)
}

// TestPostgresTerraformBackendQueryResolvesBareLocalBackendDefaultPath is the
// regression guard for issue #5594: a `backend "local" {}` block with no
// `path` attribute must resolve ownership against Terraform's own default
// location ("terraform.tfstate" relative to the root module directory), not
// silently produce zero candidates. The state side hashes the absolute path
// terraformstate.LocalStateSource actually opens
// (repoLocalPath + moduleDir + "terraform.tfstate"); this test proves the
// config-side adapter reproduces the identical hash from the parsed backend
// block plus the repository's local_path fact.
func TestPostgresTerraformBackendQueryResolvesBareLocalBackendDefaultPath(t *testing.T) {
	t.Parallel()

	repoLocalPath := filepath.FromSlash("/repos/platform-infra")
	backendFilePath := filepath.Join(repoLocalPath, "env", "prod", "main.tf")
	wantLocator := filepath.Join(repoLocalPath, "env", "prod", "terraform.tfstate")
	hash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, wantLocator)
	observed := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			"platform-infra",
			"repository:platform-infra",
			"gen-local-1",
			observed,
			repoLocalPath,
			fixtureLocalBackendFact(backendFilePath, ""),
		}}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "local", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d; bare `backend \"local\" {}` must resolve "+
			"against Terraform's own default path (issue #5594)", got, want)
	}
	if got, want := rows[0].RepoID, "platform-infra"; got != want {
		t.Fatalf("rows[0].RepoID = %q, want %q", got, want)
	}
	if got, want := rows[0].BackendKind, "local"; got != want {
		t.Fatalf("rows[0].BackendKind = %q, want %q", got, want)
	}
}

// TestPostgresTerraformBackendQueryResolvesExplicitLocalBackendPath proves an
// explicit non-default `path` attribute still resolves, using the same
// absolute-locator join as the default case.
func TestPostgresTerraformBackendQueryResolvesExplicitLocalBackendPath(t *testing.T) {
	t.Parallel()

	repoLocalPath := filepath.FromSlash("/repos/platform-infra")
	backendFilePath := filepath.Join(repoLocalPath, "env", "prod", "main.tf")
	wantLocator := filepath.Join(repoLocalPath, "env", "prod", "custom.tfstate")
	hash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, wantLocator)
	observed := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			"platform-infra",
			"repository:platform-infra",
			"gen-local-2",
			observed,
			repoLocalPath,
			fixtureLocalBackendFact(backendFilePath, "custom.tfstate"),
		}}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "local", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
}

// TestPostgresTerraformBackendQuerySkipsLocalBackendWithoutRepoLocalPath
// proves the adapter records ambiguity honestly rather than guessing: without
// a repository local_path fact there is no way to build a matching absolute
// locator, so the local backend produces no candidate (and therefore no
// spurious match) instead of joining against an invented path.
func TestPostgresTerraformBackendQuerySkipsLocalBackendWithoutRepoLocalPath(t *testing.T) {
	t.Parallel()

	backendFilePath := "/repos/platform-infra/env/prod/main.tf"
	wantLocator := "/repos/platform-infra/env/prod/terraform.tfstate"
	hash := terraformstate.ScopeLocatorHash(terraformstate.BackendLocal, wantLocator)
	observed := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{
			"platform-infra",
			"repository:platform-infra",
			"gen-local-3",
			observed,
			"",
			fixtureLocalBackendFact(backendFilePath, ""),
		}}}},
	}
	query := PostgresTerraformBackendQuery{DB: db}

	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "local", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 without a repository local_path fact", len(rows))
	}
}
