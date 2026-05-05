package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunQueryPostsLanguageQueryRequest(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("json.Decode() error = %v, want nil", err)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	if err := runQuery(cmd, []string{"Service"}); err != nil {
		t.Fatalf("runQuery() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/code/language-query"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotBody["query"], "Service"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
}

func TestRunStatsPreservesRepositorySelector(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"repository":{}}`))
	}))
	defer server.Close()

	t.Setenv("ESHU_SERVICE_URL", server.URL)

	if err := runStats(&cobra.Command{}, []string{"acme/payments"}); err != nil {
		t.Fatalf("runStats() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/repositories/"+url.PathEscape("acme/payments")+"/stats"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
}

func TestRunIndexPassesDiscoveryReportEnvToBootstrap(t *testing.T) {
	originalLookPath := indexLookPath
	originalExec := indexExec
	t.Cleanup(func() {
		indexLookPath = originalLookPath
		indexExec = originalExec
	})

	indexLookPath = func(file string) (string, error) {
		if file != "eshu-bootstrap-index" {
			t.Fatalf("indexLookPath(%q), want eshu-bootstrap-index", file)
		}
		return "/bin/eshu-bootstrap-index", nil
	}

	repoPath := t.TempDir()
	reportPath := filepath.Join(t.TempDir(), "reports", "advisory.json")
	var gotArgs []string
	var gotEnv []string
	indexExec = func(binary string, args []string, env []string) error {
		if binary != "/bin/eshu-bootstrap-index" {
			t.Fatalf("binary = %q, want /bin/eshu-bootstrap-index", binary)
		}
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("force", false, "")
	cmd.Flags().String("discovery-report", "", "")
	if err := cmd.Flags().Set("discovery-report", reportPath); err != nil {
		t.Fatalf("Set(discovery-report) error = %v, want nil", err)
	}

	if err := runIndex(cmd, []string{repoPath}); err != nil {
		t.Fatalf("runIndex() error = %v, want nil", err)
	}

	if got, want := strings.Join(gotArgs, " "), "eshu-bootstrap-index --path "+repoPath; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	wantReportPath, err := filepath.Abs(reportPath)
	if err != nil {
		t.Fatalf("Abs(reportPath) error = %v, want nil", err)
	}
	if !envContains(gotEnv, "ESHU_DISCOVERY_REPORT="+wantReportPath) {
		t.Fatalf("env missing ESHU_DISCOVERY_REPORT=%q; env=%v", wantReportPath, gotEnv)
	}
}

func envContains(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func TestRunStatsCanonicalizesExistingPathSelector(t *testing.T) {
	absolutePath, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v, want nil", err)
	}

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"repository":{}}`))
	}))
	defer server.Close()

	t.Setenv("ESHU_SERVICE_URL", server.URL)

	if err := runStats(&cobra.Command{}, []string{absolutePath}); err != nil {
		t.Fatalf("runStats() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/repositories/"+url.PathEscape(absolutePath)+"/stats"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
}

func TestRunDeleteAllReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().Bool("all", false, "")
	if err := cmd.Flags().Set("all", "true"); err != nil {
		t.Fatalf("Set(all) error = %v, want nil", err)
	}

	err := runDelete(cmd, nil)
	if err == nil {
		t.Fatal("runDelete(--all) error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runDelete(--all) error = %q, want removed guidance", err.Error())
	}
}

func TestRunCleanReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	err := runClean(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runClean() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runClean() error = %q, want removed guidance", err.Error())
	}
}

func TestRunUnwatchReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	err := runUnwatch(&cobra.Command{}, []string{"/tmp/repo"})
	if err == nil {
		t.Fatal("runUnwatch() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runUnwatch() error = %q, want removed guidance", err.Error())
	}
}

func TestRunWatchingReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	err := runWatching(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runWatching() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runWatching() error = %q, want removed guidance", err.Error())
	}
}

func TestRunAddPackageReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	err := runAddPackage(&cobra.Command{}, []string{"demo", "go"})
	if err == nil {
		t.Fatal("runAddPackage() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runAddPackage() error = %q, want removed guidance", err.Error())
	}
}
