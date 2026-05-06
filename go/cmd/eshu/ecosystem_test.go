package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunEcosystemOverviewGetsEcosystemOverview(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"repo_count":1,"workload_count":2,"platform_count":3,"instance_count":4}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	if err := runEcosystemOverview(cmd, nil); err != nil {
		t.Fatalf("runEcosystemOverview() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/ecosystem/overview"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
}

func TestRunEcosystemIndexReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	err := runEcosystemIndex(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runEcosystemIndex() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runEcosystemIndex() error = %q, want removed guidance", err.Error())
	}
}

func TestRunEcosystemStatusReturnsRemovedContractError(t *testing.T) {
	t.Parallel()

	err := runEcosystemStatus(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runEcosystemStatus() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "removed") {
		t.Fatalf("runEcosystemStatus() error = %q, want removed guidance", err.Error())
	}
}

func TestRunEcosystemOverviewDecodesJSON(t *testing.T) {
	t.Parallel()

	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		if err := json.NewEncoder(w).Encode(map[string]int{
			"repo_count":     1,
			"workload_count": 2,
			"platform_count": 3,
			"instance_count": 4,
		}); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	if err := runEcosystemOverview(cmd, nil); err != nil {
		t.Fatalf("runEcosystemOverview() error = %v, want nil", err)
	}
	if got, want := gotMethod, http.MethodGet; got != want {
		t.Fatalf("request method = %q, want %q", got, want)
	}
}
