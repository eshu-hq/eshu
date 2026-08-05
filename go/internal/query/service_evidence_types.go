// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// serviceEvidenceReader is the content-store surface service evidence
// extraction reads through: the bounded repository file listing every evidence
// field below is derived from, and the single-file read that both hydrates a
// listing row with no inline content and resolves an OpenAPI `$ref`.
type serviceEvidenceReader interface {
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]FileContent, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*FileContent, error)
}

const serviceEvidenceFileLimit = 5000

// listServiceEvidenceFiles reads the repository file list every service
// evidence field is extracted from, probing one row past
// serviceEvidenceFileLimit so a full page can be told apart from a repository
// holding exactly that many indexed files. ListRepoFiles is a real SQL
// `ORDER BY relative_path LIMIT $2` (ContentReader.ListRepoFiles), so a file
// past the cut is never read, no hostname inside it is ever extracted, and a
// consumer repository reachable only through that hostname never enters the
// merged consumer set -- source 0 of the enumeration on
// loadConsumerRepositoryEnrichmentFromCandidates. The +1 mirrors
// repositoryTreeFileLimit+1 in repository_tree.go.
func listServiceEvidenceFiles(ctx context.Context, reader serviceEvidenceReader, repoID string) ([]FileContent, bool, error) {
	files, err := reader.ListRepoFiles(ctx, repoID, serviceEvidenceFileLimit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list service evidence files: %w", err)
	}
	if len(files) > serviceEvidenceFileLimit {
		return files[:serviceEvidenceFileLimit], true, nil
	}
	return files, false, nil
}

// specFileResolver resolves a relative `$ref` path from a base spec file and
// returns the raw content of the referenced file. An empty string with a nil
// error means the reference resolved to nothing the repository holds; a read
// failure is returned as an error and never collapsed into that same empty
// string (#5720 round 10).
type specFileResolver func(baseRelativePath, ref string) (string, error)

// buildSpecFileResolver creates a specFileResolver closure that reads
// referenced files through the serviceEvidenceReader.
func buildSpecFileResolver(ctx context.Context, reader serviceEvidenceReader, repoID string) specFileResolver {
	return func(baseRelativePath, ref string) (string, error) {
		if reader == nil || ref == "" {
			return "", nil
		}
		// Resolve relative path against the base spec file's directory.
		resolved := openAPIRefFilePath(baseRelativePath, ref)

		fc, err := reader.GetFileContent(ctx, repoID, resolved)
		if err != nil {
			return "", fmt.Errorf("get referenced spec file %q: %w", resolved, err)
		}
		if fc == nil {
			return "", nil
		}
		return fc.Content, nil
	}
}

// serviceSliceValue, serviceMapValue, and serviceStringValue read one field out
// of a loosely-parsed YAML/JSON spec document. A spec file is caller content,
// so any node can be any type; each accessor returns the zero value rather than
// panicking on a shape the repository happened to commit.
func serviceSliceValue(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

func serviceMapValue(raw any) map[string]any {
	typed, _ := raw.(map[string]any)
	return typed
}

func serviceStringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

// openAPIRefFilePath resolves a `$ref` against the directory of the spec file
// that carried it, dropping any `#/...` JSON-pointer fragment. It returns a
// repository-relative path, or an empty string when the ref is only a fragment.
func openAPIRefFilePath(baseRelativePath, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if fragmentIndex := strings.Index(ref, "#"); fragmentIndex >= 0 {
		ref = ref[:fragmentIndex]
	}
	if ref == "" {
		return ""
	}
	baseDir := filepath.Dir(baseRelativePath)
	return filepath.Clean(filepath.Join(baseDir, ref))
}

// ServiceQueryEvidence groups content-derived service evidence before it is
// shaped into service context, service story, or deployment trace responses.
type ServiceQueryEvidence struct {
	Hostnames            []ServiceHostnameEvidence            `json:"hostnames,omitempty"`
	Environments         []ServiceEnvironmentEvidence         `json:"environments,omitempty"`
	DocsRoutes           []ServiceDocsRouteEvidence           `json:"docs_routes,omitempty"`
	APISpecs             []ServiceAPISpecEvidence             `json:"api_specs,omitempty"`
	FrameworkRoutes      []FrameworkRouteEvidence             `json:"framework_routes,omitempty"`
	EntrypointCandidates []ServiceEntrypointCandidateEvidence `json:"entrypoint_candidates,omitempty"`

	// filesTruncated reports that the repository file list this evidence was
	// extracted from came back full at serviceEvidenceFileLimit, so every field
	// above describes a bounded slice of the repository rather than all of it.
	// Its consumer-facing consequence is source 0 of the enumeration on
	// loadConsumerRepositoryEnrichmentFromCandidates: hostnames are derived
	// from these files, and a hostname living only in a file past the cut is
	// never searched for. Unexported on purpose: it is plumbing for the
	// disclosure flags this package derives, and every field above carries a
	// json tag, so an exported one would join the struct's serialized shape
	// without any caller asking for it.
	filesTruncated bool
}

// ServiceHostnameEvidence is exact hostname evidence that may become a public
// service entrypoint.
type ServiceHostnameEvidence struct {
	Hostname     string `json:"hostname"`
	Environment  string `json:"environment,omitempty"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// ServiceEnvironmentEvidence captures an environment signal from service
// content paths, file bodies, or exact hostname evidence.
type ServiceEnvironmentEvidence struct {
	Environment  string `json:"environment"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// ServiceDocsRouteEvidence captures documented internal docs/spec routes.
type ServiceDocsRouteEvidence struct {
	Route        string `json:"route"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// ServiceEntrypointCandidateEvidence preserves hostname-shaped candidates that
// are rejected or ambiguous and therefore must not become public entrypoints.
type ServiceEntrypointCandidateEvidence struct {
	Candidate      string `json:"candidate"`
	Classification string `json:"classification"`
	RelativePath   string `json:"relative_path"`
	Reason         string `json:"reason"`
}

// ServiceAPISpecEvidence summarizes one API spec file and its parsed routes,
// server hostnames, and operation IDs when available.
type ServiceAPISpecEvidence struct {
	RelativePath     string                       `json:"relative_path"`
	Format           string                       `json:"format"`
	Parsed           bool                         `json:"parsed"`
	SpecVersion      string                       `json:"spec_version,omitempty"`
	APIVersion       string                       `json:"api_version,omitempty"`
	EndpointCount    int                          `json:"endpoint_count,omitempty"`
	MethodCount      int                          `json:"method_count,omitempty"`
	OperationIDCount int                          `json:"operation_id_count,omitempty"`
	DocsRoutes       []string                     `json:"docs_routes,omitempty"`
	Hostnames        []string                     `json:"hostnames,omitempty"`
	Endpoints        []ServiceAPIEndpointEvidence `json:"endpoints,omitempty"`
}

// ServiceAPIEndpointEvidence captures one API endpoint path from an API spec.
type ServiceAPIEndpointEvidence struct {
	Path         string   `json:"path"`
	Methods      []string `json:"methods,omitempty"`
	OperationIDs []string `json:"operation_ids,omitempty"`
}

// FrameworkRouteEvidence captures routes detected by parser framework_semantics
// from fact_records.
type FrameworkRouteEvidence struct {
	Framework    string                        `json:"framework"`
	RelativePath string                        `json:"relative_path"`
	RoutePaths   []string                      `json:"route_paths"`
	RouteMethods []string                      `json:"route_methods"`
	RouteEntries []FrameworkRouteEntryEvidence `json:"route_entries,omitempty"`
}

// FrameworkRouteEntryEvidence preserves one parser-observed route declaration.
// Handler is the route's handler function symbol when the parser observed an
// exact binding; it is omitted for inline or middleware-wrapped routes whose
// handler is ambiguous (#2721).
type FrameworkRouteEntryEvidence struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler,omitempty"`
}
