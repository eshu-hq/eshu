package terraformstate

import (
	"testing"
)

func TestTerragruntRemoteStateCandidateS3Backend(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":              "s3",
		"bucket":                    "app-tfstate-prod",
		"bucket_is_literal":         true,
		"key":                       "services/api/terraform.tfstate",
		"key_is_literal":            true,
		"region":                    "us-east-1",
		"region_is_literal":         true,
		"dynamodb_table":            "tfstate-locks-api",
		"dynamodb_table_is_literal": true,
		"resolved_from":             "self",
	}

	candidate, ok := TerragruntRemoteStateCandidate("platform-infra", "/repos/platform-infra", row)
	if !ok {
		t.Fatalf("TerragruntRemoteStateCandidate ok = false, want true; row=%#v", row)
	}
	if got, want := candidate.State.BackendKind, BackendS3; got != want {
		t.Fatalf("BackendKind = %q, want %q (must NEVER be terragrunt)", got, want)
	}
	if got, want := candidate.State.Locator, "s3://app-tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
	if got, want := candidate.Region, "us-east-1"; got != want {
		t.Fatalf("Region = %q, want %q", got, want)
	}
	if got, want := candidate.DynamoDBTable, "tfstate-locks-api"; got != want {
		t.Fatalf("DynamoDBTable = %q, want %q", got, want)
	}
	if got, want := candidate.RepoID, "platform-infra"; got != want {
		t.Fatalf("RepoID = %q, want %q", got, want)
	}
	if got, want := candidate.Source, DiscoveryCandidateSourceGraph; got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
	if candidate.State.BackendKind == BackendTerragrunt {
		t.Fatal("BackendKind = terragrunt; resolver MUST emit underlying backend")
	}
}

func TestTerragruntRemoteStateCandidateLocalBackend(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":    "local",
		"path":            "/repos/platform-infra/env/prod/terraform.tfstate",
		"path_is_literal": true,
		"resolved_from":   "self",
	}

	candidate, ok := TerragruntRemoteStateCandidate("platform-infra", "/repos/platform-infra", row)
	if !ok {
		t.Fatalf("TerragruntRemoteStateCandidate ok = false, want true; row=%#v", row)
	}
	if got, want := candidate.State.BackendKind, BackendLocal; got != want {
		t.Fatalf("BackendKind = %q, want %q (must NEVER be terragrunt)", got, want)
	}
	if got, want := candidate.State.Locator, "/repos/platform-infra/env/prod/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
	if got, want := candidate.Source, DiscoveryCandidateSourceGitLocalFile; got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
	if got, want := candidate.RelativePath, "env/prod/terraform.tfstate"; got != want {
		// RelativePath must be repo-relative so the approval matcher and
		// downstream policy lookups operate on the same shape used by the
		// existing local-state candidate path.
		t.Fatalf("RelativePath = %q, want %q (repo-relative, not basename)", got, want)
	}
}

// TestTerragruntRemoteStateCandidateLocalBackendRejectsOutsideRepo guards
// against emitting a git-local candidate for a path that lives outside the
// repository checkout. Approval keys on the repo-relative path; an absolute
// backend path that escapes the repo cannot be expressed as repo-relative
// and must not produce a git-local candidate.
func TestTerragruntRemoteStateCandidateLocalBackendRejectsOutsideRepo(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":    "local",
		"path":            "/var/lib/state/terraform.tfstate",
		"path_is_literal": true,
	}

	if _, ok := TerragruntRemoteStateCandidate("platform-infra", "/repos/platform-infra", row); ok {
		t.Fatal("TerragruntRemoteStateCandidate ok = true, want false for path outside repo root")
	}
}

// TestTerragruntRemoteStateCandidateLocalBackendRejectsBlankRepoLocalPath
// guards the storage-adapter contract: when the repository fact has no
// recorded local_path, the resolver cannot compute a repo-relative path and
// must reject the candidate rather than emit a basename-only RelativePath.
func TestTerragruntRemoteStateCandidateLocalBackendRejectsBlankRepoLocalPath(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":    "local",
		"path":            "/repos/platform-infra/env/prod/terraform.tfstate",
		"path_is_literal": true,
	}

	if _, ok := TerragruntRemoteStateCandidate("platform-infra", "", row); ok {
		t.Fatal("TerragruntRemoteStateCandidate ok = true, want false for blank repoLocalPath")
	}
}

func TestTerragruntRemoteStateCandidateRejectsDynamicAttributes(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":      "s3",
		"bucket":            "app-${local.env}-tfstate",
		"bucket_is_literal": false,
		"key":               "services/api/terraform.tfstate",
		"key_is_literal":    true,
		"region":            "us-east-1",
		"region_is_literal": true,
	}

	if _, ok := TerragruntRemoteStateCandidate("platform-infra", "/repos/platform-infra", row); ok {
		t.Fatal("TerragruntRemoteStateCandidate ok = true, want false for dynamic bucket")
	}
}

func TestTerragruntRemoteStateCandidateRejectsRelativeLocalPath(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":    "local",
		"path":            "terraform.tfstate",
		"path_is_literal": true,
	}

	if _, ok := TerragruntRemoteStateCandidate("platform-infra", "/repos/platform-infra", row); ok {
		t.Fatal("TerragruntRemoteStateCandidate ok = true, want false for relative local path")
	}
}

func TestTerragruntRemoteStateCandidateRejectsBlankRepoID(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":      "s3",
		"bucket":            "app-tfstate-prod",
		"bucket_is_literal": true,
		"key":               "services/api/terraform.tfstate",
		"key_is_literal":    true,
		"region":            "us-east-1",
		"region_is_literal": true,
	}

	if _, ok := TerragruntRemoteStateCandidate("", "/repos/platform-infra", row); ok {
		t.Fatal("TerragruntRemoteStateCandidate ok = true, want false for blank repoID")
	}
}

func TestTerragruntRemoteStateCandidatePreservesIncludeChainResolution(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"backend_kind":      "s3",
		"bucket":            "parent-bucket",
		"bucket_is_literal": true,
		"key":               "services/api/terraform.tfstate",
		"key_is_literal":    true,
		"region":            "us-east-1",
		"region_is_literal": true,
		"resolved_from":     "include_chain",
	}

	candidate, ok := TerragruntRemoteStateCandidate("platform-infra", "/repos/platform-infra", row)
	if !ok {
		t.Fatalf("TerragruntRemoteStateCandidate ok = false, want true")
	}
	if got, want := candidate.State.BackendKind, BackendS3; got != want {
		t.Fatalf("BackendKind = %q, want %q (must NEVER be terragrunt)", got, want)
	}
	if got, want := candidate.State.Locator, "s3://parent-bucket/services/api/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
}
