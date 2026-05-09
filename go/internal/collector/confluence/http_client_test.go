package confluence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientUsesReadOnlyRequests(t *testing.T) {
	t.Parallel()

	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if got := r.Header.Get("Authorization"); got == "" {
			t.Fatal("Authorization header is empty")
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		_ = json.NewEncoder(w).Encode(pageListResponse{
			Results: []Page{confluencePage("123", "Payment", 1, "<p>body</p>")},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:  server.URL,
		Email:    "bot@example.com",
		APIToken: "token",
		Client:   server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	pages, err := client.ListSpacePages(context.Background(), "100", 25)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if len(pages) != 1 {
		t.Fatalf("len(pages) = %d, want 1", len(pages))
	}
	if got, want := len(methods), 1; got != want {
		t.Fatalf("request count = %d, want %d", got, want)
	}
}

func TestHTTPClientDecodesNestedPageLabels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		response := map[string]any{
			"id":     "123",
			"status": "current",
			"title":  "Payment Service Deployment",
			"labels": map[string]any{
				"results": []map[string]string{
					{"name": "payments"},
					{"name": "deployment"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:     server.URL,
		BearerToken: "token",
		Client:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	page, err := client.GetPage(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetPage() error = %v, want nil", err)
	}
	if got, want := labelNames(pageLabels(page)), []string{"payments", "deployment"}; !equalStrings(got, want) {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
}

func TestHTTPClientFollowsRelativeNextLinkWithQuery(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch requestCount {
		case 1:
			if got, want := r.URL.Query().Get("limit"), "25"; got != want {
				t.Fatalf("first limit = %q, want %q", got, want)
			}
			_ = json.NewEncoder(w).Encode(pageListResponse{
				Results: []Page{confluencePage("123", "First", 1, "<p>first</p>")},
				Links:   Links{Next: "/api/v2/spaces/100/pages?cursor=abc"},
			})
		case 2:
			if got, want := r.URL.Path, "/api/v2/spaces/100/pages"; got != want {
				t.Fatalf("second path = %q, want %q", got, want)
			}
			if got, want := r.URL.Query().Get("cursor"), "abc"; got != want {
				t.Fatalf("second cursor = %q, want %q", got, want)
			}
			_ = json.NewEncoder(w).Encode(pageListResponse{
				Results: []Page{confluencePage("124", "Second", 1, "<p>second</p>")},
			})
		default:
			t.Fatalf("unexpected request count %d", requestCount)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:     server.URL,
		BearerToken: "token",
		Client:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	pages, err := client.ListSpacePages(context.Background(), "100", 25)
	if err != nil {
		t.Fatalf("ListSpacePages() error = %v, want nil", err)
	}
	if got, want := len(pages), 2; got != want {
		t.Fatalf("len(pages) = %d, want %d", got, want)
	}
}

func TestHTTPClientListPageTreeKeepsOnlyPageDescendants(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{
				{"id": "child-page", "type": "page"},
				{"id": "child-whiteboard", "type": "whiteboard"},
				{"id": "child-folder", "type": "folder"},
			},
		})
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:     server.URL,
		BearerToken: "token",
		Client:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	ids, err := client.ListPageTree(context.Background(), "root", 25)
	if err != nil {
		t.Fatalf("ListPageTree() error = %v, want nil", err)
	}
	if got, want := ids, []string{"root", "child-page"}; !equalStrings(got, want) {
		t.Fatalf("ids = %#v, want %#v", got, want)
	}
}

func TestNewHTTPClientRejectsUnsupportedBaseURLScheme(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:     "ftp://example.atlassian.net/wiki",
		BearerToken: "token",
	})
	if err == nil {
		t.Fatal("NewHTTPClient() error = nil, want unsupported scheme error")
	}
}

func TestNewHTTPClientRequiresCredentials(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPClient(HTTPClientConfig{BaseURL: "https://example.atlassian.net/wiki"})
	if err == nil {
		t.Fatal("NewHTTPClient() error = nil, want credential error")
	}
}

func TestNewHTTPClientUsesDefaultTimeout(t *testing.T) {
	t.Parallel()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL:     "https://example.atlassian.net/wiki",
		BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	if got, want := client.client.Timeout, 30*time.Second; got != want {
		t.Fatalf("default Timeout = %v, want %v", got, want)
	}
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
