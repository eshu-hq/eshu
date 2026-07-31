// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package distribution

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientListTagsFollowsShortPageNextLink(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got, want := r.URL.Query().Get("n"), "3"; got != want {
			t.Fatalf("n = %q, want %q", got, want)
		}
		switch r.URL.Query().Get("last") {
		case "":
			w.Header().Set("Link", `</v2/team/api/tags/list?n=3&last=v1>; rel="next"`)
			_, _ = w.Write([]byte(`{"name":"team/api","tags":["v1"]}`))
		case "v1":
			_, _ = w.Write([]byte(`{"name":"team/api","tags":["v2"]}`))
		default:
			t.Fatalf("unexpected query %q", r.URL.RawQuery)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	response, err := client.ListTags(context.Background(), "team/api", 2)
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if got, want := fmt.Sprint(response.Tags), "[v1 v2]"; got != want {
		t.Fatalf("ListTags().Tags = %s, want %s", got, want)
	}
	if !response.Complete {
		t.Fatal("ListTags().Complete = false, want true")
	}
	if got, want := requests.Load(), int64(2); got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

func TestClientListTagsMarksExactlyLimitWithNextIncomplete(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Link", `</v2/team/api/tags/list?n=3&last=v2>; rel="next"`)
		_, _ = w.Write([]byte(`{"name":"team/api","tags":["v1","v2"]}`))
	}))
	defer server.Close()

	response, err := newTestClient(t, server).ListTags(context.Background(), "team/api", 2)
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if got, want := fmt.Sprint(response.Tags), "[v1 v2]"; got != want {
		t.Fatalf("ListTags().Tags = %s, want %s", got, want)
	}
	if response.Complete {
		t.Fatal("ListTags().Complete = true, want false")
	}
	if got, want := requests.Load(), int64(1); got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

func TestClientListTagsNoLinkAtOrBelowLimitIsComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "short", body: `{"tags":["v1"]}`},
		{name: "exact", body: `{"tags":["v1","v2"]}`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			response, err := newTestClient(t, server).ListTags(context.Background(), "team/api", 2)
			if err != nil {
				t.Fatalf("ListTags() error = %v", err)
			}
			if !response.Complete {
				t.Fatal("ListTags().Complete = false, want true")
			}
			if got, want := requests.Load(), int64(1); got != want {
				t.Fatalf("requests = %d, want %d", got, want)
			}
		})
	}
}

func TestClientListTagsStopsAtLimitPlusOne(t *testing.T) {
	t.Parallel()

	const limit = 5
	var requests atomic.Int64
	tags := make([]string, 0, 100)
	for i := range 100 {
		tags = append(tags, strconv.Quote(fmt.Sprintf("v%03d", i)))
	}
	body := `{"tags":[` + strings.Join(tags, ",") + `]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	response, err := newTestClient(t, server).ListTags(context.Background(), "team/api", limit)
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if got, want := len(response.Tags), limit+1; got != want {
		t.Fatalf("len(ListTags().Tags) = %d, want %d", got, want)
	}
	if response.Complete {
		t.Fatal("ListTags().Complete = true, want false")
	}
	if got, want := requests.Load(), int64(1); got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

func TestClientListTagsStopsOnNoProgressAndCycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		secondTags   string
		secondLink   string
		wantTags     string
		wantRequests int64
	}{
		{
			name:         "duplicate page makes no progress",
			secondTags:   `["v1"]`,
			secondLink:   `</v2/team/api/tags/list?n=4&last=v2>; rel="next"`,
			wantTags:     "[v1]",
			wantRequests: 2,
		},
		{
			name:         "next link cycles to first page",
			secondTags:   `["v2"]`,
			secondLink:   `</v2/team/api/tags/list?n=4>; rel="next"`,
			wantTags:     "[v1 v2]",
			wantRequests: 2,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch r.URL.Query().Get("last") {
				case "":
					w.Header().Set("Link", `</v2/team/api/tags/list?n=4&last=v1>; rel="next"`)
					_, _ = w.Write([]byte(`{"tags":["v1"]}`))
				case "v1":
					w.Header().Set("Link", tt.secondLink)
					_, _ = w.Write([]byte(`{"tags":` + tt.secondTags + `}`))
				default:
					t.Fatalf("unexpected query %q", r.URL.RawQuery)
				}
			}))
			defer server.Close()

			response, err := newTestClient(t, server).ListTags(context.Background(), "team/api", 3)
			if err != nil {
				t.Fatalf("ListTags() error = %v", err)
			}
			if got := fmt.Sprint(response.Tags); got != tt.wantTags {
				t.Fatalf("ListTags().Tags = %s, want %s", got, tt.wantTags)
			}
			if response.Complete {
				t.Fatal("ListTags().Complete = true, want false")
			}
			if got := requests.Load(); got != tt.wantRequests {
				t.Fatalf("requests = %d, want %d", got, tt.wantRequests)
			}
		})
	}
}

func TestClientListTagsRejectsUnsafeNextLinksWithoutRequest(t *testing.T) {
	t.Parallel()

	var outsideRequests atomic.Int64
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outsideRequests.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("outside Authorization = %q, want empty", got)
		}
		_, _ = w.Write([]byte(`{"tags":["outside"]}`))
	}))
	defer outside.Close()

	tests := []struct {
		name string
		link string
	}{
		{name: "cross origin", link: "<" + outside.URL + `/v2/team/api/tags/list?n=3&last=v1>; rel="next"`},
		{name: "wrong path", link: `</v2/other/repository/tags/list?n=3&last=v1>; rel="next"`},
		{name: "userinfo", link: `<http://user@example.invalid/v2/team/api/tags/list?n=3&last=v1>; rel="next"`},
		{name: "fragment", link: `</v2/team/api/tags/list?n=3&last=v1#fragment>; rel="next"`},
		{name: "malformed", link: `not-a-link; rel="next"`},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Link", tt.link)
				_, _ = w.Write([]byte(`{"tags":["v1"]}`))
			}))
			defer server.Close()

			client, err := NewClient(ClientConfig{
				BaseURL:     server.URL,
				BearerToken: "synthetic-token",
				Client:      server.Client(),
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			response, listErr := client.ListTags(context.Background(), "team/api", 2)
			if listErr != nil {
				t.Fatalf("ListTags() error = %v", listErr)
			}
			if got, want := fmt.Sprint(response.Tags), "[v1]"; got != want {
				t.Fatalf("ListTags().Tags = %s, want %s", got, want)
			}
			if response.Complete {
				t.Fatal("ListTags().Complete = true, want false")
			}
			if got, want := requests.Load(), int64(1); got != want {
				t.Fatalf("registry requests = %d, want %d", got, want)
			}
		})
	}
	if got := outsideRequests.Load(); got != 0 {
		t.Fatalf("cross-origin requests = %d, want 0", got)
	}
}

func TestClientListTagsOversizedPageReturnsIncomplete(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tags":["` + strings.Repeat("x", int(maxTagListPageBytes)) + `"]}`))
	}))
	defer server.Close()

	response, err := newTestClient(t, server).ListTags(context.Background(), "team/api", 2)
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(response.Tags) != 0 {
		t.Fatalf("ListTags().Tags = %v, want empty bounded partial result", response.Tags)
	}
	if response.Complete {
		t.Fatal("ListTags().Complete = true, want false")
	}
}

func TestClientListTagsStopsAtPageBound(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		request := requests.Add(1)
		tag := fmt.Sprintf("v%02d", request)
		next := fmt.Sprintf(
			`</v2/team/api/tags/list?n=101&last=%s>; rel="next"`,
			tag,
		)
		w.Header().Set("Link", next)
		_, _ = fmt.Fprintf(w, `{"tags":[%q]}`, tag)
	}))
	defer server.Close()

	response, err := newTestClient(t, server).ListTags(context.Background(), "team/api", 100)
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if got, want := len(response.Tags), maxTagListPages; got != want {
		t.Fatalf("len(ListTags().Tags) = %d, want %d", got, want)
	}
	if response.Complete {
		t.Fatal("ListTags().Complete = true, want false")
	}
	if got, want := requests.Load(), int64(maxTagListPages); got != want {
		t.Fatalf("requests = %d, want bounded %d", got, want)
	}
}

func BenchmarkClientListTagsSinglePage(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"team/api","tags":["v1","v2"]}`))
	}))
	defer server.Close()

	client, err := NewClient(ClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		b.Fatalf("NewClient() error = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		response, listErr := client.ListTags(context.Background(), "team/api", 2)
		if listErr != nil {
			b.Fatalf("ListTags() error = %v", listErr)
		}
		if len(response.Tags) != 2 || !response.Complete {
			b.Fatalf("ListTags() = %#v, want two complete tags", response)
		}
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
