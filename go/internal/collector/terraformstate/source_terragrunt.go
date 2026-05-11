package terraformstate

import (
	"path/filepath"
	"strings"
)

// TerragruntRemoteStateCandidate translates one parsed Terragrunt
// remote_state row into a DiscoveryCandidate with the underlying backend kind.
// The Terragrunt indirection is config-time only; downstream discovery,
// candidate validation, source readers, and fact persistence all see the
// resolved backend (s3 or local) and never the synthetic "terragrunt" kind.
//
// repoLocalPath is the absolute filesystem root the repository is checked
// out under, sourced from the repository fact's local_path. It is required
// for local-backend resolution because the candidate's RelativePath must be
// repo-relative for the approval matcher to recognise it; an unset
// repoLocalPath therefore rejects local-backend rows. S3-backend rows do not
// depend on repoLocalPath.
//
// The function returns ok=false when:
//   - repoID is blank
//   - the row's backend_kind is unsupported (anything other than s3 or local)
//   - a required attribute is missing or marked non-literal
//   - the resulting locator would violate the discovery exact-source contract
//     (relative local paths, prefix-only S3 keys, dynamic expressions)
//   - a local backend path lies outside the repoLocalPath tree, which would
//     leave the candidate without a meaningful repo-relative key.
//
// Returning ok=false is the safe default: an unparseable row yields no
// candidate and no fact rather than an under-constrained read of the wrong
// state object.
func TerragruntRemoteStateCandidate(repoID, repoLocalPath string, row map[string]any) (DiscoveryCandidate, bool) {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return DiscoveryCandidate{}, false
	}
	backendKind := strings.TrimSpace(stringAttr(row, "backend_kind", "name"))
	switch backendKind {
	case string(BackendS3):
		return terragruntRemoteStateS3Candidate(repoID, row)
	case string(BackendLocal):
		return terragruntRemoteStateLocalCandidate(repoID, repoLocalPath, row)
	default:
		return DiscoveryCandidate{}, false
	}
}

// terragruntRemoteStateS3Candidate handles the S3 backend resolution path. It
// requires literal bucket, key, and region values, rejects workspace-prefixed
// layouts (the discovery contract treats those as ambiguous), and rejects
// prefix-only keys. The optional dynamodb_table attribute is recorded only
// when itself a literal value.
func terragruntRemoteStateS3Candidate(repoID string, row map[string]any) (DiscoveryCandidate, bool) {
	if strings.TrimSpace(stringAttr(row, "workspace_key_prefix")) != "" {
		return DiscoveryCandidate{}, false
	}
	bucket := strings.TrimSpace(stringAttr(row, "bucket"))
	key := strings.TrimSpace(stringAttr(row, "key"))
	region := strings.TrimSpace(stringAttr(row, "region"))
	dynamoDBTable := exactOptionalRow(row, "dynamodb_table")
	if !isExactRowAttribute(row, "bucket", bucket) ||
		!isExactRowAttribute(row, "key", key) ||
		!isExactRowAttribute(row, "region", region) {
		return DiscoveryCandidate{}, false
	}
	if strings.HasSuffix(key, "/") {
		return DiscoveryCandidate{}, false
	}
	return DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendS3,
			Locator:     "s3://" + bucket + "/" + key,
		},
		Source:        DiscoveryCandidateSourceGraph,
		RepoID:        repoID,
		Region:        region,
		DynamoDBTable: dynamoDBTable,
	}, true
}

// terragruntRemoteStateLocalCandidate handles the local backend resolution
// path. The local backend points at a file path; the discovery contract
// requires that path to be absolute and operator-approved. We surface it as a
// DiscoveryCandidateSourceGitLocalFile so the downstream Git-local approval
// gates still apply, which means the path must live inside repoLocalPath so
// it can be expressed as a repo-relative path the approval matcher
// recognises. A missing, relative, or out-of-repo path is rejected.
func terragruntRemoteStateLocalCandidate(repoID, repoLocalPath string, row map[string]any) (DiscoveryCandidate, bool) {
	path := strings.TrimSpace(stringAttr(row, "path"))
	if !isExactRowAttribute(row, "path", path) {
		return DiscoveryCandidate{}, false
	}
	if !filepath.IsAbs(path) {
		return DiscoveryCandidate{}, false
	}
	repoLocalPath = strings.TrimSpace(repoLocalPath)
	if repoLocalPath == "" {
		// Without the repo root we cannot compute a repo-relative path; the
		// candidate would default to filepath.Base(path), which the approval
		// matcher rejects. Drop the candidate rather than emit an
		// unmatchable shape.
		return DiscoveryCandidate{}, false
	}
	cleanedPath := filepath.Clean(path)
	cleanedRoot := filepath.Clean(repoLocalPath)
	relative, err := filepath.Rel(cleanedRoot, cleanedPath)
	if err != nil || relative == "" || relative == "." || strings.HasPrefix(relative, "..") {
		// filepath.Rel returns a "../" prefix when cleanedPath is outside
		// cleanedRoot. Such paths are not git-local from this repository's
		// perspective and must not produce a git-local candidate.
		return DiscoveryCandidate{}, false
	}
	return DiscoveryCandidate{
		State: StateKey{
			BackendKind: BackendLocal,
			Locator:     cleanedPath,
		},
		Source:       DiscoveryCandidateSourceGitLocalFile,
		RepoID:       repoID,
		RelativePath: filepath.ToSlash(relative),
		StateInVCS:   true,
	}, true
}

// stringAttr returns the first non-empty string value in the row matching one
// of the supplied keys. The variadic shape lets callers tolerate the legacy
// "name" alias used by parseTerraformBackends without writing two lookups.
func stringAttr(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key].(string); ok {
			return value
		}
	}
	return ""
}

// exactOptionalRow returns the trimmed value of an optional row attribute when
// it is both present and marked as a literal. Empty or non-literal values
// produce "" so callers can ignore them without further checks.
func exactOptionalRow(row map[string]any, name string) string {
	value := strings.TrimSpace(stringAttr(row, name))
	if value == "" || !isExactRowAttribute(row, name, value) {
		return ""
	}
	return value
}

// isExactRowAttribute reports whether the named attribute on the row is a
// literal value safe to use as exact backend evidence. The check enforces the
// `<name>_is_literal` companion flag set by the parser and rejects values that
// still embed unresolved Terraform-style interpolation (`${...}`) or
// references like `var.`/`local.`.
func isExactRowAttribute(row map[string]any, name string, value string) bool {
	literalKey := name + "_is_literal"
	switch literal := row[literalKey].(type) {
	case bool:
		return literal && isExactRowValue(value)
	case string:
		return strings.EqualFold(strings.TrimSpace(literal), "true") && isExactRowValue(value)
	default:
		return false
	}
}

// isExactRowValue mirrors isExactBackendValue from the postgres adapter; it
// rejects empty values and values containing dynamic expression markers. Kept
// in this package so the resolver does not depend on storage internals.
func isExactRowValue(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "${") || strings.Contains(value, "(") {
		return false
	}
	for _, dynamicPrefix := range []string{"var.", "local.", "path.", "terraform."} {
		if strings.HasPrefix(value, dynamicPrefix) {
			return false
		}
	}
	return true
}
