// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPackageRegistryDependencyChainsHandlerReportsPublishersTruncatedOverCap
// is the regression test for the #5461/#5816 finding that
// loadPackagePublishers (package_registry_dependency_chains.go) issued its
// own batched ListPackageRegistryCorrelations with Limit: packageRegistryMaxLimit
// and never read the returned Truncated flag: when a package has more
// publisher/ownership facts than that cap, publishers beyond it silently
// vanished from every dependency chain with no signal to the caller.
//
// It uses rawFactPackageRegistryCorrelationStore (not a hand-built
// pre-decoded-rows fake) so the publisher page is built by the REAL
// buildPackageRegistryCorrelationPage, exercising the actual "+1 lookahead"
// pagination contract end-to-end through the HTTP handler: 201 publisher
// facts for one package (one more than packageRegistryMaxLimit=200) must
// report publishers_truncated=true, while still returning a complete,
// correctly joined chain for the consumption leg.
func TestPackageRegistryDependencyChainsHandlerReportsPublishersTruncatedOverCap(t *testing.T) {
	t.Parallel()

	const overCapPublisherCount = packageRegistryMaxLimit + 1

	publisherFacts := make([]packageRegistryCorrelationFactRow, 0, overCapPublisherCount)
	for i := 0; i < overCapPublisherCount; i++ {
		publisherFacts = append(publisherFacts, packageRegistryCorrelationFactRow{
			FactID:        fmt.Sprintf("publisher-fact-%03d", i),
			FactKind:      packagePublicationCorrelationFactKind,
			SchemaVersion: "1.0.0",
			Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
				"package_id":        "pkg:npm://registry.example/team-api",
				"relationship_kind": "publication",
				// Every publisher repo id must differ from the consumer
				// repository so loadPackagePublishers's self-reference filter
				// never drops one, keeping the fetched-vs-visible count exact.
				"repository_id": fmt.Sprintf("repo-publisher-%03d", i),
			}),
		})
	}

	store := &rawFactPackageRegistryCorrelationStore{
		consumptionFacts: []packageRegistryCorrelationFactRow{
			{
				FactID:        "consume-1",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/team-api",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
		},
		publisherFacts: publisherFacts,
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Chains []struct {
			PackageID  string           `json:"package_id"`
			Ambiguous  bool             `json:"ambiguous"`
			Publishers []map[string]any `json:"publishers"`
		} `json:"chains"`
		Count               int  `json:"count"`
		Truncated           bool `json:"truncated"`
		PublishersTruncated bool `json:"publishers_truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}

	if resp.Truncated {
		t.Fatal("truncated = true, want false: the single consumption fact fits well under the requested limit")
	}
	if !resp.PublishersTruncated {
		t.Fatalf("publishers_truncated = false, want true: %d publisher facts exceed packageRegistryMaxLimit=%d", overCapPublisherCount, packageRegistryMaxLimit)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	chain := resp.Chains[0]
	if !chain.Ambiguous {
		t.Fatal("ambiguous = false, want true: more than one candidate publisher remains")
	}
	if got, want := len(chain.Publishers), packageRegistryMaxLimit; got != want {
		t.Fatalf("len(publishers) = %d, want %d (the visible window is bounded at the cap even though publishers_truncated signals more exist)", got, want)
	}
}

// TestPackageRegistryDependencyChainsHandlerReportsPublishersNotTruncatedUnderCap
// is the below-cap sibling of the test above: when a package's publisher
// evidence fits well inside packageRegistryMaxLimit, publishers_truncated
// must be false and every publisher fact must be present on the chain.
func TestPackageRegistryDependencyChainsHandlerReportsPublishersNotTruncatedUnderCap(t *testing.T) {
	t.Parallel()

	store := &rawFactPackageRegistryCorrelationStore{
		consumptionFacts: []packageRegistryCorrelationFactRow{
			{
				FactID:        "consume-1",
				FactKind:      packageConsumptionCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/team-api",
					"relationship_kind": "consumption",
					"repository_id":     "repo-consumer",
				}),
			},
		},
		publisherFacts: []packageRegistryCorrelationFactRow{
			{
				FactID:        "publisher-fact-1",
				FactKind:      packagePublicationCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/team-api",
					"relationship_kind": "publication",
					"repository_id":     "repo-publisher-1",
				}),
			},
			{
				FactID:        "publisher-fact-2",
				FactKind:      packageOwnershipCorrelationFactKind,
				SchemaVersion: "1.0.0",
				Payload: mustMarshalPackageRegistryCorrelationPayload(t, map[string]any{
					"package_id":        "pkg:npm://registry.example/team-api",
					"relationship_kind": "ownership",
					"repository_id":     "repo-publisher-2",
				}),
			},
		},
	}
	handler := &PackageRegistryHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/dependency-chains?repository_id=repo-consumer&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Chains []struct {
			Publishers []map[string]any `json:"publishers"`
		} `json:"chains"`
		Count               int  `json:"count"`
		PublishersTruncated bool `json:"publishers_truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}

	if resp.PublishersTruncated {
		t.Fatal("publishers_truncated = true, want false: only 2 publisher facts, well under packageRegistryMaxLimit")
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := len(resp.Chains[0].Publishers), 2; got != want {
		t.Fatalf("len(publishers) = %d, want %d", got, want)
	}
}
