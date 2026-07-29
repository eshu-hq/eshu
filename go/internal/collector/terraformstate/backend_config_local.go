// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"path/filepath"
	"strings"
)

// defaultTerraformLocalStatePath is Terraform's own default for the local
// backend's "path" argument when the attribute is omitted from the backend
// block, relative to the directory containing the backend block (the root
// module's working directory). Source:
// https://developer.hashicorp.com/terraform/language/backend/local —
// "path - (Optional) The path to the tfstate file. This defaults to
// "terraform.tfstate" relative to the root module by default." Issue #5594:
// a bare `backend "local" {}` is the ordinary spelling of this default and
// must resolve ownership the same as an explicit `path = "terraform.tfstate"`.
const defaultTerraformLocalStatePath = "terraform.tfstate"

// backendConfigLocalCandidate derives a BackendLocal DiscoveryCandidate from
// one parsed `backend "local" {}` block, applying Terraform's own path
// default when the attribute is absent (issue #5594). It returns ok=false —
// no candidate, no guess — when:
//
//   - repoLocalPath is blank: without the repository's checkout root there is
//     no way to build an absolute locator that matches the absolute path
//     terraformstate.LocalStateSource hashes when it actually opens the same
//     state file (see BackendConfigContext.RepoLocalPath).
//   - the backend block's own source-file path is missing or, unexpectedly,
//     not absolute (the HCL parser always emits an absolute path here; see
//     go/internal/parser/hcl/terraform_backend.go).
//   - the source file resolves outside repoLocalPath (defensive; should not
//     happen for a repo-owned file).
//   - the "path" attribute is present but is a dynamic expression that did
//     not resolve to an exact literal (var./local. reference, interpolation,
//     function call, etc.) — reported as a warning instead, matching the S3
//     candidate's treatment of unresolved attributes.
func backendConfigLocalCandidate(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
	repoLocalPath string,
) (DiscoveryCandidate, bool) {
	repoLocalPath = strings.TrimSpace(repoLocalPath)
	if repoLocalPath == "" {
		return DiscoveryCandidate{}, false
	}
	statePath, ok := resolveOptionalLocalStatePathAttribute(backend, resolution)
	if !ok {
		return DiscoveryCandidate{}, false
	}
	relativeDir, ok := backendFileDirRelativeToRepo(repoLocalPath, backendStringValue(backend, "path"))
	if !ok {
		return DiscoveryCandidate{}, false
	}

	cleanedRoot := filepath.Clean(repoLocalPath)
	absoluteLocator := filepath.Clean(filepath.Join(cleanedRoot, relativeDir, filepath.FromSlash(statePath)))
	if !filepath.IsAbs(absoluteLocator) {
		return DiscoveryCandidate{}, false
	}

	return DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendLocal,
			Locator:     absoluteLocator,
		},
		Source: DiscoveryCandidateSourceGraph,
		RepoID: strings.TrimSpace(repoID),
	}, true
}

// resolveOptionalLocalStatePathAttribute returns the local backend's
// effective state-file path: the literal or resolved "path" attribute value
// when present, or Terraform's own default when the attribute is absent. It
// returns ok=false only when the attribute is present but did not resolve to
// an exact literal value.
func resolveOptionalLocalStatePathAttribute(
	backend map[string]any,
	resolution backendResolutionContext,
) (string, bool) {
	raw := strings.TrimSpace(backendStringValue(backend, "local_path"))
	if raw == "" {
		return defaultTerraformLocalStatePath, true
	}
	decision := resolveBackendConfigAttributeDecision(backend, "local_path", resolution)
	return decision.value, decision.ok
}

// backendFileDirRelativeToRepo returns the repo-relative directory of the .tf
// file that declared the backend block (absoluteBackendFile), expressed
// relative to repoLocalPath. Returns ok=false when absoluteBackendFile is
// blank, not absolute, or resolves outside repoLocalPath.
func backendFileDirRelativeToRepo(repoLocalPath string, absoluteBackendFile string) (string, bool) {
	absoluteBackendFile = strings.TrimSpace(absoluteBackendFile)
	if absoluteBackendFile == "" || !filepath.IsAbs(absoluteBackendFile) {
		return "", false
	}
	cleanedRoot := filepath.Clean(repoLocalPath)
	relativeDir, err := filepath.Rel(cleanedRoot, filepath.Dir(filepath.Clean(absoluteBackendFile)))
	if err != nil || relativeDir == ".." || strings.HasPrefix(relativeDir, ".."+string(filepath.Separator)) {
		return "", false
	}
	if relativeDir == "." {
		return "", true
	}
	return relativeDir, true
}

// backendConfigLocalExpressionWarnings reports the local backend's "path"
// attribute when present but unresolved. A blank/absent attribute is never a
// warning — it is Terraform's own default (see
// defaultTerraformLocalStatePath) — and a missing repoLocalPath is an
// ingestion-context gap, not a fact about the HCL content, so it is not
// reported as a backend-expression warning either.
func backendConfigLocalExpressionWarnings(
	repoID string,
	backend map[string]any,
	resolution backendResolutionContext,
) []BackendExpressionWarning {
	raw := strings.TrimSpace(backendStringValue(backend, "local_path"))
	if raw == "" {
		return nil
	}
	decision := resolveBackendConfigAttributeDecision(backend, "local_path", resolution)
	if decision.ok {
		return nil
	}
	return []BackendExpressionWarning{
		backendExpressionWarningForRowAttribute(repoID, backend, "path", "local_path", decision),
	}
}
