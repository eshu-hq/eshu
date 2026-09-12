// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/service/evidence"
)

// EvidenceReader is the content-store surface service evidence
// extraction reads through: the bounded repository file listing every evidence
// field below is derived from, and the single-file read that both hydrates a
// listing row with no inline content and resolves an OpenAPI `$ref`.
type EvidenceReader interface {
	ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error)
	GetFileContent(ctx context.Context, repoID, relativePath string) (*querycontract.FileContent, error)
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
// repositoryTreeFileLimit+1 in repository/tree.go.
func listServiceEvidenceFiles(ctx context.Context, reader EvidenceReader, repoID string) ([]querycontract.FileContent, bool, error) {
	files, err := reader.ListRepoFiles(ctx, repoID, serviceEvidenceFileLimit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list service evidence files: %w", err)
	}
	if len(files) > serviceEvidenceFileLimit {
		return files[:serviceEvidenceFileLimit], true, nil
	}
	return files, false, nil
}

// specFileResolver resolves a relative `$ref` path from a base spec file.
// Alias onto querycontract so the moved repository handler family can name
// the resolver type from outside this package (#6060, lane B B3).
type specFileResolver = querycontract.SpecFileResolver

// buildSpecFileResolver creates a specFileResolver closure that reads
// referenced files through the EvidenceReader.
func buildSpecFileResolver(ctx context.Context, reader EvidenceReader, repoID string) specFileResolver {
	return func(baseRelativePath, ref string) (string, error) {
		if reader == nil || ref == "" {
			return "", nil
		}
		// Resolve relative path against the base spec file's directory.
		resolved := evidence.OpenAPIRefFilePath(baseRelativePath, ref)
		if resolved == "" {
			// PR #5933 review fix (Copilot): a fragment-only $ref (e.g.
			// "#/components/schemas/Widget") resolves to no external file.
			// Reading through to the store with an empty relative path is a
			// needless query and, on a backend that rejects an empty path,
			// produces a confusing `get referenced spec file ""` error.
			return "", nil
		}

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

// QueryEvidence groups content-derived service evidence before it is
// shaped into service context, service story, or deployment trace responses.
type QueryEvidence struct {
	Hostnames            []HostnameEvidence            `json:"hostnames,omitempty"`
	Environments         []EnvironmentEvidence         `json:"environments,omitempty"`
	DocsRoutes           []DocsRouteEvidence           `json:"docs_routes,omitempty"`
	APISpecs             []APISpecEvidence             `json:"api_specs,omitempty"`
	FrameworkRoutes      []FrameworkRouteEvidence      `json:"framework_routes,omitempty"`
	EntrypointCandidates []EntrypointCandidateEvidence `json:"entrypoint_candidates,omitempty"`

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

// HostnameEvidence is exact hostname evidence that may become a public
// service entrypoint.
type HostnameEvidence struct {
	Hostname     string `json:"hostname"`
	Environment  string `json:"environment,omitempty"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// EnvironmentEvidence captures an environment signal from service
// content paths, file bodies, or exact hostname evidence.
type EnvironmentEvidence struct {
	Environment  string `json:"environment"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// DocsRouteEvidence captures documented internal docs/spec routes.
type DocsRouteEvidence struct {
	Route        string `json:"route"`
	RelativePath string `json:"relative_path"`
	Reason       string `json:"reason"`
}

// EntrypointCandidateEvidence preserves hostname-shaped candidates that
// are rejected or ambiguous and therefore must not become public entrypoints.
type EntrypointCandidateEvidence struct {
	Candidate      string `json:"candidate"`
	Classification string `json:"classification"`
	RelativePath   string `json:"relative_path"`
	Reason         string `json:"reason"`
}

// APISpecEvidence summarizes one API spec file and its parsed routes,
// server hostnames, and operation IDs when available. It is an alias onto
// querycontract so the moved repository handler family can name it from
// outside this package (#6060, lane B B3).
type APISpecEvidence = querycontract.ServiceAPISpecEvidence

// APIEndpointEvidence captures one API endpoint path from an API spec.
// Alias onto querycontract; see APISpecEvidence.
type APIEndpointEvidence = querycontract.ServiceAPIEndpointEvidence

// FrameworkRouteEvidence captures one framework route's handler evidence.
type FrameworkRouteEvidence = querycontract.FrameworkRouteEvidence

// FrameworkRouteEntryEvidence captures one route entrypoint binding.
type FrameworkRouteEntryEvidence = querycontract.FrameworkRouteEntryEvidence
