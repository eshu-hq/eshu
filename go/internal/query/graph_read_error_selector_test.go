// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests cover the repository-selector graph read itself
// (resolveRepositorySelectorExactForAccess, invoked via
// resolveRepositorySelectorForRequestWithAccess or directly), not a handler's
// downstream graph read. A canonical-looking selector such as "repo-1" short
// circuits looksCanonicalRepositoryID and never reaches the graph, so every
// case here uses a non-canonical, name-like selector ("my-repo-name") to force
// the real MATCH (r:Repository) ... lookup and exercise the guard.

// TestPackageRegistryDependencyChainsSelectorMapsGraphReadAvailabilityErrors
// covers listDependencyChains's resolveRepositorySelectorForRequestWithAccess
// call (package_registry_dependency_chains_handler.go), a direct caller of the
// writing variant.
func TestPackageRegistryDependencyChainsSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &PackageRegistryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependency-chains?repository_id=my-repo-name&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestSupplyChainAdvisoryEvidenceSelectorMapsGraphReadAvailabilityErrors covers
// listAdvisoryEvidence's resolveRepositorySelectorForRequestWithAccess call
// (supply_chain_advisory_evidence_handler.go), a direct caller of the writing
// variant.
func TestSupplyChainAdvisoryEvidenceSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &SupplyChainHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/advisories/evidence?repository_id=my-repo-name&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestContainerImageIdentitiesSelectorMapsGraphReadAvailabilityErrors covers
// listContainerImageIdentities's resolveContainerImageSourceRepositorySelector
// call (container_image_identity_scope.go), the *_scope.go thin-wrapper family
// that threads a capability through resolveRepositorySelectorForRequestWithAccess.
func TestContainerImageIdentitiesSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &SupplyChainHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities?source_repository_id=my-repo-name&limit=10", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestGetRepositoryContentSelectorMapsGraphReadAvailabilityErrors covers
// getRepositoryContent's resolveRepositoryPathSelector call
// (repository_content.go via repository_selectors.go), a repository route
// using the non-writing resolveRepositorySelectorExactForAccess variant that
// the caller itself must guard.
func TestGetRepositoryContentSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &RepositoryHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/my-repo-name/content?path=main.go", nil)
			req.SetPathValue("repo_id", "my-repo-name")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.getRepositoryContent(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestResolveEntitySelectorMapsGraphReadAvailabilityErrors covers
// resolveEntity's repo_id-anchored resolveRepositorySelectorExactForAccess
// call (entity.go), distinct from the sibling
// TestResolveEntityMapsGraphReadAvailabilityErrors in
// graph_read_error_repository_entity_test.go, which uses the canonical-looking
// "repo-1" selector and so only reaches the later main graph query.
func TestResolveEntitySelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &EntityHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/entities/resolve", strings.NewReader(`{"name":"foo","repo_id":"my-repo-name"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.resolveEntity(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleRelationshipStorySelectorMapsGraphReadAvailabilityErrors covers
// handleRelationshipStory's applyRepositorySelectorForCapability call at the
// top of the handler (code_relationship_story.go / code_repository_selector.go).
func TestHandleRelationshipStorySelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships/story", strings.NewReader(`{"target":"foo","repo_id":"my-repo-name"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleRelationshipStory(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleSymbolSearchSelectorMapsGraphReadAvailabilityErrors covers
// handleSymbolSearch's applyRepositorySelectorForCapability call
// (code_symbol.go).
func TestHandleSymbolSearchSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/symbols/search", strings.NewReader(`{"symbol":"foo","repo_id":"my-repo-name"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleSymbolSearch(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleComplexitySelectorMapsGraphReadAvailabilityErrors covers
// handleComplexity's applyRepositorySelectorForCapability call (code.go).
func TestHandleComplexitySelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/complexity", strings.NewReader(`{"repo_id":"my-repo-name"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleComplexity(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleRelationshipsSelectorMapsGraphReadAvailabilityErrors covers
// handleRelationships's applyRepositorySelectorForCapability call
// (code_relationships.go).
func TestHandleRelationshipsSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", strings.NewReader(`{"entity_id":"foo","repo_id":"my-repo-name"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleRelationships(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// TestHandleDeadCodeSelectorMapsGraphReadAvailabilityErrors covers
// handleDeadCode's applyRepositorySelectorForCapability call
// (code_dead_code.go).
func TestHandleDeadCodeSelectorMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			handler := &CodeHandler{Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, test.err
			}}}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", strings.NewReader(`{"repo_id":"my-repo-name"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleDeadCode(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
