// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"path/filepath"
	"testing"
)

// localBackendFixture returns one parsed `backend "local" {}` row shaped like
// the parser's real output (go/internal/parser/hcl/terraform_backend.go):
// row["path"] is always the absolute source .tf file path, and the backend's
// own "path" attribute (when present) is carried separately as
// row["local_path"].
func localBackendFixture(backendFilePath, localPath string) map[string]any {
	row := map[string]any{
		"backend_kind": "local",
		"name":         "local",
		"path":         backendFilePath,
	}
	if localPath != "" {
		row["local_path"] = localPath
		row["local_path_is_literal"] = true
	}
	return row
}

// TestEvaluateBackendConfigDefaultsBareLocalBackendPath is the direct
// regression test for issue #5594: a bare `backend "local" {}` (no path
// attribute) must resolve to Terraform's own default —
// "terraform.tfstate" relative to the directory containing the backend
// block — not zero candidates.
func TestEvaluateBackendConfigDefaultsBareLocalBackendPath(t *testing.T) {
	t.Parallel()

	repoLocalPath := filepath.FromSlash("/repos/platform-infra")
	backendFilePath := filepath.Join(repoLocalPath, "env", "prod", "main.tf")
	wantLocator := filepath.Join(repoLocalPath, "env", "prod", "terraform.tfstate")

	result := EvaluateBackendConfig("platform-infra", BackendConfigContext{
		Backends:      []map[string]any{localBackendFixture(backendFilePath, "")},
		RepoLocalPath: repoLocalPath,
	})

	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none for the default-path case", result.Warnings)
	}
	if got, want := len(result.Candidates), 1; got != want {
		t.Fatalf("len(Candidates) = %d, want %d", got, want)
	}
	candidate := result.Candidates[0]
	if got, want := candidate.State.BackendKind, BackendLocal; got != want {
		t.Fatalf("BackendKind = %q, want %q", got, want)
	}
	if got, want := candidate.State.Locator, wantLocator; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
	if got, want := candidate.RepoID, "platform-infra"; got != want {
		t.Fatalf("RepoID = %q, want %q", got, want)
	}
}

// TestEvaluateBackendConfigHonorsExplicitLocalBackendPath proves an explicit
// non-default path still resolves, at a directory other than repo root, using
// the same default-application code path.
func TestEvaluateBackendConfigHonorsExplicitLocalBackendPath(t *testing.T) {
	t.Parallel()

	repoLocalPath := filepath.FromSlash("/repos/platform-infra")
	backendFilePath := filepath.Join(repoLocalPath, "env", "prod", "main.tf")
	wantLocator := filepath.Join(repoLocalPath, "env", "prod", "custom.tfstate")

	result := EvaluateBackendConfig("platform-infra", BackendConfigContext{
		Backends:      []map[string]any{localBackendFixture(backendFilePath, "custom.tfstate")},
		RepoLocalPath: repoLocalPath,
	})

	if got, want := len(result.Candidates), 1; got != want {
		t.Fatalf("len(Candidates) = %d, want %d", got, want)
	}
	if got, want := result.Candidates[0].State.Locator, wantLocator; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
}

// TestEvaluateBackendConfigLocalBackendAtRepoRootUsesDefaultOnly proves the
// directory-join logic degrades correctly when the backend block's .tf file
// lives at the repo root (no subdirectory to join).
func TestEvaluateBackendConfigLocalBackendAtRepoRootUsesDefaultOnly(t *testing.T) {
	t.Parallel()

	repoLocalPath := filepath.FromSlash("/repos/platform-infra")
	backendFilePath := filepath.Join(repoLocalPath, "main.tf")
	wantLocator := filepath.Join(repoLocalPath, "terraform.tfstate")

	result := EvaluateBackendConfig("platform-infra", BackendConfigContext{
		Backends:      []map[string]any{localBackendFixture(backendFilePath, "")},
		RepoLocalPath: repoLocalPath,
	})

	if got, want := len(result.Candidates), 1; got != want {
		t.Fatalf("len(Candidates) = %d, want %d", got, want)
	}
	if got, want := result.Candidates[0].State.Locator, wantLocator; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
}

// TestEvaluateBackendConfigSkipsLocalBackendWithoutRepoLocalPath proves the
// resolver records ambiguity honestly rather than guessing: without a
// repository checkout root there is no way to build a locator that matches
// the state side's absolute path, so no candidate is produced (and,
// correctly, no warning either — a missing RepoLocalPath is an ingestion
// context gap, not a fact about the HCL content).
func TestEvaluateBackendConfigSkipsLocalBackendWithoutRepoLocalPath(t *testing.T) {
	t.Parallel()

	result := EvaluateBackendConfig("platform-infra", BackendConfigContext{
		Backends: []map[string]any{localBackendFixture("/repos/platform-infra/main.tf", "")},
	})

	if len(result.Candidates) != 0 {
		t.Fatalf("Candidates = %#v, want none without RepoLocalPath", result.Candidates)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none; a missing RepoLocalPath is not an HCL-content fact", result.Warnings)
	}
}

// TestEvaluateBackendConfigWarnsOnUnresolvedLocalBackendPath proves a dynamic
// (non-literal) path attribute is reported as a warning under the true
// Terraform attribute name "path", even though the parser stores it as
// row["local_path"] internally.
func TestEvaluateBackendConfigWarnsOnUnresolvedLocalBackendPath(t *testing.T) {
	t.Parallel()

	backend := localBackendFixture("/repos/platform-infra/main.tf", "")
	backend["local_path"] = "${terraform.workspace}.tfstate"
	backend["local_path_is_literal"] = false

	result := EvaluateBackendConfig("platform-infra", BackendConfigContext{
		Backends:      []map[string]any{backend},
		RepoLocalPath: "/repos/platform-infra",
	})

	if len(result.Candidates) != 0 {
		t.Fatalf("Candidates = %#v, want none for an unresolved path expression", result.Candidates)
	}
	if got, want := len(result.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	warning := result.Warnings[0]
	if got, want := warning.AttributeName, "path"; got != want {
		t.Fatalf("AttributeName = %q, want %q (the true Terraform HCL attribute name)", got, want)
	}
	if got, want := warning.BackendKind, "local"; got != want {
		t.Fatalf("BackendKind = %q, want %q", got, want)
	}
	if got, want := warning.Reason, backendWarningReasonWorkspaceInterpolation; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

// TestEvaluateBackendConfigOtherBackendKindsProduceNoCandidateOrWarning is the
// audit-completion guard from issue #5594: every Terraform backend kind Eshu
// does not model (gcs, azurerm, remote, http) must fall through to the
// pre-existing silent-zero behavior — no candidate, no warning — the same
// way BackendLocal used to before this fix, rather than crash or
// misclassify.
func TestEvaluateBackendConfigOtherBackendKindsProduceNoCandidateOrWarning(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"gcs", "azurerm", "remote", "http"} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			result := EvaluateBackendConfig("platform-infra", BackendConfigContext{
				Backends: []map[string]any{{
					"backend_kind": kind,
					"name":         kind,
					"path":         "/repos/platform-infra/main.tf",
				}},
				RepoLocalPath: "/repos/platform-infra",
			})
			if len(result.Candidates) != 0 {
				t.Fatalf("Candidates = %#v, want none for unmodeled backend kind %q", result.Candidates, kind)
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("Warnings = %#v, want none for unmodeled backend kind %q", result.Warnings, kind)
			}
		})
	}
}
