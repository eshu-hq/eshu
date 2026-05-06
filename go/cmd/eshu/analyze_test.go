package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunAnalyzeChainPostsCanonicalRequest(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("json.Decode() error = %v, want nil", err)
		}
		_, _ = w.Write([]byte(`{"chains":[]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	addAnalyzeRepoSelectorFlags(cmd)
	cmd.Flags().Int("depth", 5, "Maximum traversal depth")
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("depth", "7"); err != nil {
		t.Fatalf("Set(depth) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("repo-id", "repo-1"); err != nil {
		t.Fatalf("Set(repo-id) error = %v, want nil", err)
	}

	if err := runAnalyzeChain(cmd, []string{"wrapper", "helper"}); err != nil {
		t.Fatalf("runAnalyzeChain() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/code/call-chain"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotBody["start"], "wrapper"; got != want {
		t.Fatalf("body[start] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["end"], "helper"; got != want {
		t.Fatalf("body[end] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["max_depth"], float64(7); got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}

func TestRunAnalyzeCallersPostsTransitiveRequest(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("json.Decode() error = %v, want nil", err)
		}
		_, _ = w.Write([]byte(`{"incoming":[]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	addAnalyzeRepoSelectorFlags(cmd)
	cmd.Flags().Bool("transitive", false, "Include transitive callers")
	cmd.Flags().Int("depth", 5, "Maximum traversal depth")
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("transitive", "true"); err != nil {
		t.Fatalf("Set(transitive) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("depth", "8"); err != nil {
		t.Fatalf("Set(depth) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("repo-id", "repo-1"); err != nil {
		t.Fatalf("Set(repo-id) error = %v, want nil", err)
	}

	if err := runAnalyzeCallers(cmd, []string{"helper"}); err != nil {
		t.Fatalf("runAnalyzeCallers() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/code/relationships"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotBody["name"], "helper"; got != want {
		t.Fatalf("body[name] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["transitive"], true; got != want {
		t.Fatalf("body[transitive] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["max_depth"], float64(8); got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}

func TestRunAnalyzeCallsResolvesRepoSelectorAlias(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repository:r_payments","name":"payments","path":"/src/payments","local_path":"/src/payments","remote_url":"","repo_slug":"acme/payments","has_remote":false}]}`))
		case "/api/v0/code/relationships":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("json.Decode() error = %v, want nil", err)
			}
			_, _ = w.Write([]byte(`{"outgoing":[]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	addAnalyzeRepoSelectorFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("repo", "payments"); err != nil {
		t.Fatalf("Set(repo) error = %v, want nil", err)
	}

	if err := runAnalyzeCalls(cmd, []string{"helper"}); err != nil {
		t.Fatalf("runAnalyzeCalls() error = %v, want nil", err)
	}
	if got, want := gotBody["repo_id"], "repository:r_payments"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}

func TestRunAnalyzeDeadCodePostsExclusions(t *testing.T) {
	t.Parallel()

	var gotMethods []string
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethods = append(gotMethods, r.Method+" "+r.URL.Path)
		gotPath = r.URL.Path
		if r.URL.Path == "/api/v0/repositories" {
			_, _ = w.Write([]byte(`{"count":1,"repositories":[{"id":"repo-1","name":"repo-1","path":"","local_path":"","remote_url":"","repo_slug":"","has_remote":false}]}`))
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("json.Decode() error = %v, want nil", err)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	cmd.Flags().String("repo", "", "Optional repository selector")
	cmd.Flags().String("repo-id", "", "Optional repository ID")
	cmd.Flags().Int("limit", 100, "Maximum dead-code candidates to return")
	cmd.Flags().StringSlice("exclude", nil, "Decorator exclusions")
	cmd.Flags().Bool("fail-on-found", false, "Exit non-zero when results are found")
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("repo-id", "repo-1"); err != nil {
		t.Fatalf("Set(repo-id) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("exclude", "@route,@app.route"); err != nil {
		t.Fatalf("Set(exclude) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("limit", "25"); err != nil {
		t.Fatalf("Set(limit) error = %v, want nil", err)
	}

	if err := runAnalyzeDeadCode(cmd, nil); err != nil {
		t.Fatalf("runAnalyzeDeadCode() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/code/dead-code"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotMethods, []string{"POST /api/v0/code/dead-code"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("request sequence = %#v, want %#v", got, want)
	}
	if got, want := gotBody["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := gotBody["limit"], float64(25); got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	exclusions, ok := gotBody["exclude_decorated_with"].([]any)
	if !ok {
		t.Fatalf("body[exclude_decorated_with] type = %T, want []any", gotBody["exclude_decorated_with"])
	}
	if got, want := len(exclusions), 2; got != want {
		t.Fatalf("len(body[exclude_decorated_with]) = %d, want %d", got, want)
	}
}

func TestRunAnalyzeDeadCodeResolvesRepoSelectorAlias(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/repositories":
			_, _ = w.Write([]byte(`{"count":2,"repositories":[{"id":"repository:r_payments","name":"payments","path":"/src/payments","local_path":"/src/payments","remote_url":"","repo_slug":"acme/payments","has_remote":false},{"id":"repository:r_billing","name":"billing","path":"/src/billing","local_path":"/src/billing","remote_url":"","repo_slug":"acme/billing","has_remote":false}]}`))
		case "/api/v0/code/dead-code":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("json.Decode() error = %v, want nil", err)
			}
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	cmd.Flags().String("repo", "", "Optional repository selector")
	cmd.Flags().String("repo-id", "", "Optional repository ID")
	cmd.Flags().StringSlice("exclude", nil, "Decorator exclusions")
	cmd.Flags().Bool("fail-on-found", false, "Exit non-zero when results are found")
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("repo", "payments"); err != nil {
		t.Fatalf("Set(repo) error = %v, want nil", err)
	}

	if err := runAnalyzeDeadCode(cmd, nil); err != nil {
		t.Fatalf("runAnalyzeDeadCode() error = %v, want nil", err)
	}
	if got, want := gotBody["repo_id"], "repository:r_payments"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}

func TestRunAnalyzeDeadCodeFailsOnAmbiguousRepoSelector(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/repositories" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"count":2,"repositories":[{"id":"repository:r_one","name":"payments","path":"/src/payments-one","local_path":"/src/payments-one","remote_url":"","repo_slug":"acme/payments-one","has_remote":false},{"id":"repository:r_two","name":"payments","path":"/src/payments-two","local_path":"/src/payments-two","remote_url":"","repo_slug":"acme/payments-two","has_remote":false}]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	cmd.Flags().String("repo", "", "Optional repository selector")
	cmd.Flags().String("repo-id", "", "Optional repository ID")
	cmd.Flags().StringSlice("exclude", nil, "Decorator exclusions")
	cmd.Flags().Bool("fail-on-found", false, "Exit non-zero when results are found")
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if err := cmd.Flags().Set("repo", "payments"); err != nil {
		t.Fatalf("Set(repo) error = %v, want nil", err)
	}

	err := runAnalyzeDeadCode(cmd, nil)
	if err == nil {
		t.Fatal("runAnalyzeDeadCode() error = nil, want non-nil")
	}
	if got, want := err.Error(), "resolve repo selector \"payments\": multiple repositories match: repository:r_one, repository:r_two"; got != want {
		t.Fatalf("runAnalyzeDeadCode() error = %q, want %q", got, want)
	}
}
