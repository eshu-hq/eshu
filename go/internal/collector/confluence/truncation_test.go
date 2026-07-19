// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestHTTPClientListSpacePagesTruncatesWhenMoreDataExistsPastMaxTotalPages(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		next := ""
		if requestCount < 3 {
			next = fmt.Sprintf("/api/v2/spaces/100/pages?cursor=%d", requestCount)
		}
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: []Page{confluencePage(fmt.Sprintf("page-%d", requestCount), "Title", 1, "<p>body</p>")},
			Links:   Links{Next: next},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	pages, truncated, err := client.ListSpacePages(context.Background(), "100", 25, 2)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if got, want := len(pages), 2; got != want {
		t.Fatalf("len(pages) = %d, want %d", got, want)
	}
	if !truncated {
		t.Fatal("truncated = false, want true (more data existed past max_total_pages)")
	}
}

func TestHTTPClientListSpacePagesNoTruncationAtExactMaxTotalPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: []Page{
				confluencePage("page-1", "One", 1, "<p>1</p>"),
				confluencePage("page-2", "Two", 1, "<p>2</p>"),
			},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	pages, truncated, err := client.ListSpacePages(context.Background(), "100", 25, 2)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if got, want := len(pages), 2; got != want {
		t.Fatalf("len(pages) = %d, want %d", got, want)
	}
	if truncated {
		t.Fatal("truncated = true, want false when the provider's own final page lands exactly at max_total_pages")
	}
}

func TestHTTPClientListSpacePagesNoTruncationUnderMaxTotalPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: []Page{confluencePage("page-1", "One", 1, "<p>1</p>")},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	pages, truncated, err := client.ListSpacePages(context.Background(), "100", 25, 50)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if got, want := len(pages), 1; got != want {
		t.Fatalf("len(pages) = %d, want %d", got, want)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestHTTPClientListSpacePagesTreatsRepeatedCursorAsTerminal(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: []Page{confluencePage(fmt.Sprintf("page-%d", requestCount), "Title", 1, "<p>body</p>")},
			// Always points back at the exact same cursor, simulating a
			// provider bug that never terminates the chain.
			Links: Links{Next: "/api/v2/spaces/100/pages?cursor=stuck"},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	pages, truncated, err := client.ListSpacePages(context.Background(), "100", 25, 1000000)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true (repeated cursor must terminate the walk)")
	}
	// The loop must stop quickly rather than spin forever: it fetches the
	// first cursor, follows to the repeated one, detects the repeat, and
	// stops -- at most a small, bounded number of requests.
	if requestCount > 3 {
		t.Fatalf("requestCount = %d, want a small bounded number of requests", requestCount)
	}
	if len(pages) == 0 {
		t.Fatal("pages is empty, want at least the first fetched page retained")
	}
}

func TestHTTPClientListSpacePagesStopsAtMaxCursorPages(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		// Each page returns zero records but a distinct next cursor, so the
		// max-total-pages bound never trips; only the defensive
		// max-cursor-pages backstop can stop this walk.
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: nil,
			Links:   Links{Next: fmt.Sprintf("/api/v2/spaces/100/pages?cursor=%d", requestCount)},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	_, truncated, err := client.ListSpacePages(context.Background(), "100", 25, 1000000)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true (defensive max-cursor-pages bound must stop the walk)")
	}
	if requestCount > maxCursorPages {
		t.Fatalf("requestCount = %d, want at most maxCursorPages (%d)", requestCount, maxCursorPages)
	}
}

func TestHTTPClientListPageTreeTruncatesWhenMoreDataExistsPastMaxTotalPages(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		next := ""
		if requestCount < 3 {
			next = fmt.Sprintf("/api/v2/pages/root/descendants?cursor=%d", requestCount)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{{"id": fmt.Sprintf("child-%d", requestCount), "type": "page"}},
			"_links":  map[string]string{"next": next},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	// max_total_pages=2 includes the root ID itself, so only one descendant
	// fetch loop iteration's worth of records fits before the cap trips.
	ids, truncated, err := client.ListPageTree(context.Background(), "root", 25, 2)
	if err != nil {
		t.Fatalf("ListPageTree() error = %v, want nil", err)
	}
	if got, want := len(ids), 2; got != want {
		t.Fatalf("len(ids) = %d, want %d", got, want)
	}
	if !truncated {
		t.Fatal("truncated = false, want true (more descendants existed past max_total_pages)")
	}
}

func TestHTTPClientListPageTreeNoTruncationUnderMaxTotalPages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{{"id": "child-page", "type": "page"}},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	ids, truncated, err := client.ListPageTree(context.Background(), "root", 25, 50)
	if err != nil {
		t.Fatalf("ListPageTree() error = %v, want nil", err)
	}
	if got, want := ids, []string{"root", "child-page"}; !equalStrings(got, want) {
		t.Fatalf("ids = %#v, want %#v", got, want)
	}
	if truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestSourceEmitsTruncatedCoverageWarningWhenCursorWalkIsBounded(t *testing.T) {
	t.Parallel()

	if CoverageWarningTruncated != "truncated" {
		t.Fatalf("CoverageWarningTruncated = %q, want %q", CoverageWarningTruncated, "truncated")
	}

	client := &fakeClient{
		space:      Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{confluencePage("123", "Runbook", 1, "<p>body</p>")},
		truncated:  true,
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL:       "https://example.atlassian.net/wiki",
			SpaceID:       "100",
			MaxTotalPages: 2500,
			Now:           fixedNow,
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}

	envelopes := drainFacts(t, collected.Facts)
	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	metadata := payloadMap(sourceFact.Payload, "source_metadata")
	if got, want := payloadString(metadata, "coverage_warning"), CoverageWarningTruncated; got != want {
		t.Fatalf("coverage_warning = %q, want %q", got, want)
	}
	if got, want := payloadInt(metadata, "max_total_pages"), 2500; got != want {
		t.Fatalf("max_total_pages = %d, want %d", got, want)
	}
}

func TestSourceEmitsCompleteCoverageWarningWhenCursorWalkIsNotBounded(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		space:      Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{confluencePage("123", "Runbook", 1, "<p>body</p>")},
		truncated:  false,
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     fixedNow,
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}

	envelopes := drainFacts(t, collected.Facts)
	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	metadata := payloadMap(sourceFact.Payload, "source_metadata")
	if got, want := payloadString(metadata, "coverage_warning"), CoverageWarningComplete; got != want {
		t.Fatalf("coverage_warning = %q, want %q", got, want)
	}
}

func TestHTTPClientListPageTreeTreatsRepeatedCursorAsTerminal(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{{"id": fmt.Sprintf("child-%d", requestCount), "type": "page"}},
			"_links":  map[string]string{"next": "/api/v2/pages/root/descendants?cursor=stuck"},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, BearerToken: "token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	_, truncated, err := client.ListPageTree(context.Background(), "root", 25, 1000000)
	if err != nil {
		t.Fatalf("ListPageTree() error = %v, want nil", err)
	}
	if !truncated {
		t.Fatal("truncated = false, want true (repeated cursor must terminate the walk)")
	}
	if requestCount > 3 {
		t.Fatalf("requestCount = %d, want a small bounded number of requests", requestCount)
	}
}
