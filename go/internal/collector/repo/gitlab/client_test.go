// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitlab_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/gitlab"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type recordedRequest struct {
	escapedPath string
	query       string
	token       string
}

type scriptedResponse struct {
	status     int
	retryAfter string
	body       string
}

type listingFixture struct {
	t        *testing.T
	server   *httptest.Server
	requests []recordedRequest
	mu       sync.Mutex
	// pages holds raw JSON bodies per page number (1-based). A missing page
	// returns an empty array. linkNext, when set, is the Link header of the
	// empty page. script, when non-empty, answers each request in order with
	// the next entry, ahead of the page logic.
	pages    map[int]string
	linkNext string
	status   int
	script   []scriptedResponse
	// resolveBody answers group lookups (paths without /projects).
	// resolveStatus overrides its status; script answers first when set.
	resolveBody   string
	resolveStatus int
}

// requestCount returns the number of recorded requests. The test server
// handler appends from its own goroutines, so tests read under the same
// mutex.
func (f *listingFixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// firstRequest returns the first recorded request.
func (f *listingFixture) firstRequest() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[0]
}

// allRequests returns a copy of every recorded request.
func (f *listingFixture) allRequests() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func newListingFixture(t *testing.T) *listingFixture {
	t.Helper()
	f := &listingFixture{t: t, pages: map[int]string{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{escapedPath: r.URL.EscapedPath(), query: r.URL.RawQuery, token: r.Header.Get("PRIVATE-TOKEN")})
		var entry *scriptedResponse
		if len(f.script) > 0 {
			entry = &f.script[0]
			f.script = f.script[1:]
		}
		f.mu.Unlock()
		if entry != nil {
			if entry.retryAfter != "" {
				w.Header().Set("Retry-After", entry.retryAfter)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(entry.status)
			fmt.Fprint(w, entry.body)
			return
		}
		if !strings.Contains(r.URL.EscapedPath(), "/projects") {
			status := f.resolveStatus
			if status == 0 {
				status = http.StatusOK
			}
			body := f.resolveBody
			if body == "" {
				body = `{"id":42,"full_path":"acme/platform"}`
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, body)
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		page := 0
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		body, ok := f.pages[page]
		if !ok {
			body = "[]"
		}
		if body == "[]" && f.linkNext != "" {
			w.Header().Set("Link", f.linkNext)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func projectJSON(id int, path string, archived bool) string {
	return fmt.Sprintf(`{"id":%d,"path_with_namespace":%q,"archived":%v,"namespace":{"full_path":%q}}`, id, path, archived, path[:strings.LastIndex(path, "/")])
}

func pageJSON(items ...string) string {
	return "[" + strings.Join(items, ",") + "]"
}

type clientHarness struct {
	client *gitlab.Client
	logs   *bytes.Buffer
	reader *sdkmetric.ManualReader
	sleeps []time.Duration
}

func newClientHarness(t *testing.T, server *httptest.Server) *clientHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("gitlab-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logs := &bytes.Buffer{}
	h := &clientHarness{logs: logs, reader: reader}
	client, err := gitlab.NewClient(server.Client(), server.URL, "glpat-test", inst, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.Sleep = func(_ context.Context, d time.Duration) error {
		h.sleeps = append(h.sleeps, d)
		return nil
	}
	h.client = client
	return h
}

func (h *clientHarness) listingOutcomeCount(t *testing.T, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_collector_repository_listing_requests_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				got, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				provider, _ := dp.Attributes.Value(telemetry.MetricDimensionProvider)
				if got.AsString() == outcome && provider.AsString() == "gitlab" {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestListGroupProjectsRequestShape proves the request contract:
// include_subgroups=true, with_shared=false, order_by=id, sort=asc,
// per_page=100, no archived or min_access_level, PRIVATE-TOKEN auth, and
// the group path escaped with / as %2F.
func TestListGroupProjectsRequestShape(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.pages[1] = pageJSON(projectJSON(101, "acme/platform/a", false))
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if len(listing.Projects) != 1 || listing.Projects[0].PathWithNamespace != "acme/platform/a" {
		t.Fatalf("projects = %+v, want the one listed project", listing.Projects)
	}
	if f.requestCount() < 1 {
		t.Fatal("no requests recorded")
	}
	first := f.firstRequest()
	if !strings.Contains(first.escapedPath, "acme%2Fplatform") {
		t.Fatalf("escaped path = %q, want acme%%2Fplatform", first.escapedPath)
	}
	for _, want := range []string{"include_subgroups=true", "with_shared=false", "order_by=id", "sort=asc", "per_page=100", "page=1"} {
		if !strings.Contains(first.query, want) {
			t.Fatalf("query = %q, want %q", first.query, want)
		}
	}
	for _, banned := range []string{"archived", "min_access_level", "simple="} {
		if strings.Contains(first.query, banned) {
			t.Fatalf("query = %q, must not contain %q", first.query, banned)
		}
	}
	if first.token != "glpat-test" {
		t.Fatalf("PRIVATE-TOKEN = %q, want the configured token", first.token)
	}
}

// TestListGroupProjectsPages proves offset paging over three full pages:
// 250 projects then an empty page with no Link is complete with ascending
// IDs, and every page keeps per_page=100.
func TestListGroupProjectsPages(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	items := func(from, to int) string {
		entries := make([]string, 0, to-from)
		for id := from; id < to; id++ {
			entries = append(entries, projectJSON(id, fmt.Sprintf("acme/platform/svc-%d", id), false))
		}
		return pageJSON(entries...)
	}
	f.pages[1] = items(1, 101)
	f.pages[2] = items(101, 201)
	f.pages[3] = items(201, 251)
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if !listing.Complete {
		t.Fatal("Complete = false, want true after an empty page with no Link")
	}
	if len(listing.Projects) != 250 {
		t.Fatalf("projects = %d, want 250", len(listing.Projects))
	}
	for i := 1; i < len(listing.Projects); i++ {
		if listing.Projects[i].ID <= listing.Projects[i-1].ID {
			t.Fatalf("project %d out of order at index %d", listing.Projects[i].ID, i)
		}
	}
	if listing.Pages != 4 {
		t.Fatalf("pages = %d, want 4 requests", listing.Pages)
	}
	for _, req := range f.allRequests() {
		if !strings.Contains(req.query, "per_page=100") {
			t.Fatalf("query = %q, page size must stay fixed at 100", req.query)
		}
	}
	if got := h.listingOutcomeCount(t, "success"); got != 4 {
		t.Fatalf("success count = %d, want 4 page requests", got)
	}
}

// TestListGroupProjectsShortPageDoesNotEndListing proves a short page
// mid-listing does not end it, mirroring the GitHub listing.
func TestListGroupProjectsShortPageDoesNotEndListing(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	mk := func(from, to int) string {
		entries := make([]string, 0, to-from)
		for id := from; id < to; id++ {
			entries = append(entries, projectJSON(id, fmt.Sprintf("acme/platform/svc-%d", id), false))
		}
		return pageJSON(entries...)
	}
	f.pages[1] = mk(1, 101)
	f.pages[2] = mk(101, 131)
	f.pages[3] = mk(131, 151)
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if !listing.Complete || len(listing.Projects) != 150 {
		t.Fatalf("complete = %v projects = %d, want true and 150", listing.Complete, len(listing.Projects))
	}
}

// TestListGroupProjectsEmptyPageWithNextIsIncomplete proves an empty page
// that still carries rel="next" is incomplete.
func TestListGroupProjectsEmptyPageWithNextIsIncomplete(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.pages[1] = pageJSON(projectJSON(1, "acme/platform/a", false))
	f.linkNext = `<https://gitlab.example/api/v4/groups/1/projects?page=3>; rel="next"`
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if listing.Complete {
		t.Fatal("Complete = true, want false when the empty page carries rel=next")
	}
	if len(listing.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(listing.Projects))
	}
}

// TestListGroupProjectsTruncation proves reaching the limit is always
// incomplete and trims to the limit, including when the total equals it.
func TestListGroupProjectsTruncation(t *testing.T) {
	t.Parallel()

	mk := func(from, to int) string {
		entries := make([]string, 0, to-from)
		for id := from; id < to; id++ {
			entries = append(entries, projectJSON(id, fmt.Sprintf("acme/platform/svc-%d", id), false))
		}
		return pageJSON(entries...)
	}
	for _, limit := range []int{150, 250} {
		f := newListingFixture(t)
		f.pages[1] = mk(1, 101)
		f.pages[2] = mk(101, 201)
		f.pages[3] = mk(201, 251)
		h := newClientHarness(t, f.server)

		listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", limit)
		if err != nil {
			t.Fatalf("limit %d: ListGroupProjects() error = %v", limit, err)
		}
		if listing.Complete {
			t.Fatalf("limit %d: Complete = true, want false at the limit", limit)
		}
		if len(listing.Projects) != limit {
			t.Fatalf("limit %d: projects = %d, want the trimmed limit", limit, len(listing.Projects))
		}
	}
}

// TestListGroupProjectsKeepsSubgroupDepth proves a deeply nested project
// keeps its full path, and TestListGroupProjectsDropsOutsideGroup proves a
// project outside the group is dropped and counted.
func TestListGroupProjectsKeepsSubgroupDepth(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.pages[1] = pageJSON(projectJSON(7, "acme/platform/a/b/c/svc", false))
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if len(listing.Projects) != 1 || listing.Projects[0].PathWithNamespace != "acme/platform/a/b/c/svc" {
		t.Fatalf("projects = %+v, want the nested path intact", listing.Projects)
	}
}

func TestListGroupProjectsDropsOutsideGroup(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.pages[1] = pageJSON(
		projectJSON(1, "acme/platform/a", false),
		projectJSON(2, "other/x", false),
	)
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if len(listing.Projects) != 1 || listing.Projects[0].PathWithNamespace != "acme/platform/a" {
		t.Fatalf("projects = %+v, want only the in-group project", listing.Projects)
	}
	if listing.OutsideGroup != 1 {
		t.Fatalf("OutsideGroup = %d, want 1", listing.OutsideGroup)
	}
}

// TestListGroupProjectsDecodesArchived proves the archived flag is decoded
// for the archive policy to classify.
func TestListGroupProjectsDecodesArchived(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.pages[1] = pageJSON(projectJSON(1, "acme/platform/old", true))
	h := newClientHarness(t, f.server)

	listing, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000)
	if err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	if len(listing.Projects) != 1 || !listing.Projects[0].Archived || listing.Projects[0].ID != 1 {
		t.Fatalf("projects = %+v, want the archived project with its numeric id", listing.Projects)
	}
}

// TestListGroupProjectsLogsCompletion proves the finished listing logs its
// group path, page count, listed and outside counts, and completeness.
func TestListGroupProjectsLogsCompletion(t *testing.T) {
	t.Parallel()

	f := newListingFixture(t)
	f.pages[1] = pageJSON(
		projectJSON(1, "acme/platform/a", false),
		projectJSON(2, "other/x", false),
	)
	h := newClientHarness(t, f.server)

	if _, err := h.client.ListGroupProjects(context.Background(), "acme/platform", 4000); err != nil {
		t.Fatalf("ListGroupProjects() error = %v", err)
	}
	var record map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if decoded["msg"] == "git_gitlab_group_listing_completed" {
			record = decoded
		}
	}
	if record == nil {
		t.Fatalf("no git_gitlab_group_listing_completed log in:\n%s", h.logs.String())
	}
	for key, value := range map[string]any{
		"group_path": "acme/platform", "pages": float64(2), "listed_count": float64(1),
		"outside_group_count": float64(1), "listing_complete": true,
	} {
		if record[key] != value {
			t.Fatalf("completed log %s = %v, want %v (log %v)", key, record[key], value, record)
		}
	}
	if _, ok := record["duration_seconds"].(float64); !ok {
		t.Fatalf("completed log has no duration_seconds: %v", record)
	}
}

// TestNewClientRejectsBadBase proves the API base needs a scheme and host
// with no credentials.
func TestNewClientRejectsBadBase(t *testing.T) {
	t.Parallel()

	for _, base := range []string{"", "gitlab.com/api/v4", "https://user@example.com/api/v4"} {
		if _, err := gitlab.NewClient(nil, base, "token", nil, nil); err == nil {
			t.Fatalf("NewClient(%q) error = nil, want non-nil", base)
		}
	}
}
