// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

// PageRequest identifies one Cloud Asset Inventory page fetch for a bounded
// scope. PageToken is empty for the first page and carries the continuation
// token returned by the previous page for resumed fetches.
//
// PageRequest never carries credential material. The PageProvider implementation
// resolves the read-only credential named by ScopeConfig.CredentialRef out of
// band; the runtime only references credentials by name.
type PageRequest struct {
	// Scope is the bounded parent scope the page belongs to.
	Scope ScopeConfig
	// PageToken is the continuation token, empty for the first page of a scope.
	PageToken string
}

// PageProvider is the seam between the GCP collector runtime and the Cloud Asset
// Inventory transport. It returns one parsed, redacted assets.list page per call
// and a continuation token in the page for the next call.
//
// The live gRPC/REST Cloud Asset Inventory client is a PageProvider
// implementation. Tests use FixturePageProvider so no test ever performs a live
// Google Cloud call. Implementations MUST already have dropped the raw resource
// data blob (the gcpcloud parser is the single redaction choke point).
type PageProvider interface {
	// FetchPage returns the next assets.list page for the request. The returned
	// page's NextPageToken is empty when the scope is fully drained.
	FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error)
}

// FixturePageProvider serves Cloud Asset Inventory pages from in-memory or
// file-backed fixtures keyed by scope id. It is the default PageProvider for
// fixture tests and the collector-gcp-cloud binary's offline smoke path; it
// performs no network or Google Cloud calls.
//
// Pages for a scope are served in order. The first FetchPage for a scope returns
// pages[0]; each subsequent call returns the next page only when the supplied
// PageToken matches the previous page's NextPageToken, so a caller that resumes
// from a continuation token reads the same deterministic sequence.
type FixturePageProvider struct {
	// pagesByScope holds the ordered pages to serve for each scope id.
	pagesByScope map[string][]gcpcloud.AssetsListPage
}

// NewFixturePageProvider builds a fixture provider from already-parsed pages
// grouped by scope id. The pages are served in slice order. The provider copies
// the outer map so later caller mutation cannot change served pages.
func NewFixturePageProvider(pagesByScope map[string][]gcpcloud.AssetsListPage) *FixturePageProvider {
	copied := make(map[string][]gcpcloud.AssetsListPage, len(pagesByScope))
	for scopeID, pages := range pagesByScope {
		cloned := make([]gcpcloud.AssetsListPage, len(pages))
		copy(cloned, pages)
		copied[scopeID] = cloned
	}
	return &FixturePageProvider{pagesByScope: copied}
}

// NewFixturePageProviderFromFiles parses assets.list fixture files grouped by
// scope id into a FixturePageProvider. It is the offline file-backed seam for
// the binary's smoke path and for tests that want to exercise parsing. Each
// scope's files are parsed in slice order. It returns an error when a fixture
// cannot be read or parsed so a misconfigured offline fixture fails fast instead
// of silently serving an empty scope.
func NewFixturePageProviderFromFiles(filesByScope map[string][]string) (*FixturePageProvider, error) {
	pagesByScope := make(map[string][]gcpcloud.AssetsListPage, len(filesByScope))
	for scopeID, files := range filesByScope {
		pages := make([]gcpcloud.AssetsListPage, 0, len(files))
		for _, file := range files {
			raw, err := os.ReadFile(filepath.Clean(file))
			if err != nil {
				return nil, fmt.Errorf("read gcp fixture page %q for scope %q: %w", file, scopeID, err)
			}
			page, err := gcpcloud.ParseAssetsListPage(raw)
			if err != nil {
				return nil, fmt.Errorf("parse gcp fixture page %q for scope %q: %w", file, scopeID, err)
			}
			pages = append(pages, page)
		}
		pagesByScope[scopeID] = pages
	}
	return NewFixturePageProvider(pagesByScope), nil
}

// errPageTokenNotFound reports that a resumed page token did not match any known
// page boundary for the scope. The source classifies it as a page-token-expired
// warning rather than a hard failure.
var errPageTokenNotFound = errors.New("gcp fixture page token does not match a known page boundary")

// FetchPage returns the next fixture page for the request. The first page of a
// scope is served for an empty token. A non-empty token is matched against the
// NextPageToken of a previously served page, so resuming from a continuation
// token returns the deterministic next page. A token that matches no page
// boundary returns errPageTokenNotFound.
func (p *FixturePageProvider) FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error) {
	if err := ctx.Err(); err != nil {
		return gcpcloud.AssetsListPage{}, err
	}
	pages, ok := p.pagesByScope[req.Scope.ScopeID]
	if !ok || len(pages) == 0 {
		return gcpcloud.AssetsListPage{}, nil
	}
	token := strings.TrimSpace(req.PageToken)
	if token == "" {
		return pages[0], nil
	}
	for i := 0; i < len(pages)-1; i++ {
		if strings.TrimSpace(pages[i].NextPageToken) == token {
			return pages[i+1], nil
		}
	}
	// A token matching only the final page's NextPageToken points at a page that
	// does not exist in the fixture: the token is expired/dangling. The source
	// converts this into a page_token_expired warning rather than truncating.
	return gcpcloud.AssetsListPage{}, errPageTokenNotFound
}

var _ PageProvider = (*FixturePageProvider)(nil)
