// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

func TestLiveClientFetchPageUsesBoundedAssetsListRequest(t *testing.T) {
	scopeCfg := testScope().withDefaults()
	var gotAuth string
	var gotPageSize string
	var gotPageToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/projects/my-project/assets" {
			t.Fatalf("path = %s, want /v1/projects/my-project/assets", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotPageSize = r.URL.Query().Get("pageSize")
		gotPageToken = r.URL.Query().Get("pageToken")
		if got := r.URL.Query().Get("contentType"); got != "RESOURCE" {
			t.Fatalf("contentType = %q, want RESOURCE", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"readTime":"2026-06-16T12:00:00Z",
			"nextPageToken":"NEXT",
			"assets":[{"name":"//compute.googleapis.com/projects/my-project/zones/us-central1-a/instances/inst-1","assetType":"compute.googleapis.com/Instance"}]
		}`))
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource: staticTokenSource("live-token"),
		HTTPClient:  server.Client(),
		Endpoint:    server.URL,
		PageSize:    MaxLivePageSize + 50,
	}

	page, err := client.FetchPage(context.Background(), PageRequest{Scope: scopeCfg, PageToken: "PAGE"})
	if err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if gotAuth != "Bearer live-token" {
		t.Fatalf("Authorization header = %q, want bearer token", gotAuth)
	}
	if gotPageSize != "1000" {
		t.Fatalf("pageSize = %q, want bounded max", gotPageSize)
	}
	if gotPageToken != "PAGE" {
		t.Fatalf("pageToken = %q, want PAGE", gotPageToken)
	}
	if page.NextPageToken != "NEXT" {
		t.Fatalf("NextPageToken = %q, want NEXT", page.NextPageToken)
	}
	if len(page.Resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(page.Resources))
	}
}

func TestLiveClientFetchTagPageUsesBoundedResourceManagerRequest(t *testing.T) {
	var gotPath string
	var gotPageSize string
	var gotPageToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		gotPath = r.URL.Path
		gotPageSize = r.URL.Query().Get("pageSize")
		gotPageToken = r.URL.Query().Get("pageToken")
		if got := r.URL.Query().Get("parent"); got != "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1" {
			t.Fatalf("parent query = %q, want full resource name", got)
		}
		_, _ = w.Write([]byte(`{
			"tagBindings": [{
				"tagValue": "tagValues/456",
				"tagValueNamespacedName": "123/env/prod"
			}],
			"nextPageToken": "NEXT"
		}`))
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource:             staticTokenSource("live-token"),
		HTTPClient:              server.Client(),
		ResourceManagerEndpoint: server.URL,
		TagPageSize:             MaxLiveTagPageSize + 1,
	}
	page, err := client.FetchTagPage(context.Background(), TagRequest{
		Scope:            testScope().withDefaults(),
		FullResourceName: "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1",
		AssetType:        "compute.googleapis.com/Instance",
		SourceKind:       TagSourceKindDirect,
		PageToken:        "PAGE",
	})
	if err != nil {
		t.Fatalf("FetchTagPage: %v", err)
	}
	if gotPath != "/v3/tagBindings" {
		t.Fatalf("path = %q, want /v3/tagBindings", gotPath)
	}
	if gotPageSize != "300" {
		t.Fatalf("pageSize = %q, want bounded max", gotPageSize)
	}
	if gotPageToken != "PAGE" {
		t.Fatalf("pageToken = %q, want PAGE", gotPageToken)
	}
	if page.NextPageToken != "NEXT" {
		t.Fatalf("NextPageToken = %q, want NEXT", page.NextPageToken)
	}
	if got := page.Tags["123/env"]; got != "prod" {
		t.Fatalf("tags = %#v, want 123/env=prod", page.Tags)
	}
}

func TestLiveClientFetchEffectiveTagPageKeepsInheritanceState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"effectiveTags": [{
				"tagValue": "tagValues/456",
				"namespacedTagValue": "123/team/platform",
				"tagKey": "tagKeys/123",
				"namespacedTagKey": "123/team",
				"inherited": true
			}]
		}`))
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource:             staticTokenSource("live-token"),
		HTTPClient:              server.Client(),
		ResourceManagerEndpoint: server.URL,
	}
	page, err := client.FetchTagPage(context.Background(), TagRequest{
		Scope:            testScope().withDefaults(),
		FullResourceName: "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1",
		AssetType:        "compute.googleapis.com/Instance",
		SourceKind:       TagSourceKindEffective,
	})
	if err != nil {
		t.Fatalf("FetchTagPage: %v", err)
	}
	if got := page.Tags["123/team"]; got != "platform" {
		t.Fatalf("tags = %#v, want 123/team=platform", page.Tags)
	}
	if got := page.InheritanceState["123/team"]; got != "inherited" {
		t.Fatalf("InheritanceState = %#v, want inherited", page.InheritanceState)
	}
}

func TestLiveClientTagThrottleRetriesThenWarns(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "quota body must stay out of errors", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource:             staticTokenSource("live-token"),
		HTTPClient:              server.Client(),
		ResourceManagerEndpoint: server.URL,
		MaxAttempts:             2,
		RetryBackoff:            func(int) time.Duration { return 0 },
	}
	_, err := client.FetchTagPage(context.Background(), TagRequest{
		Scope:            testScope().withDefaults(),
		FullResourceName: "//compute.googleapis.com/projects/sanitized-project/zones/us-central1-a/instances/api-1",
		AssetType:        "compute.googleapis.com/Instance",
		SourceKind:       TagSourceKindDirect,
	})
	if err == nil {
		t.Fatal("FetchTagPage returned nil error, want quota warning")
	}
	var warning ProviderWarning
	if !errors.As(err, &warning) {
		t.Fatalf("err = %T, want ProviderWarning", err)
	}
	if warning.WarningKind != gcpcloud.WarningKindQuota {
		t.Fatalf("warning kind = %q, want quota", warning.WarningKind)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if strings.Contains(err.Error(), "quota body") {
		t.Fatalf("error leaked provider body: %q", err.Error())
	}
}

func TestNewADCLiveClientUsesAssetsListOAuthScope(t *testing.T) {
	if CloudAssetInventoryOAuthScope != "https://www.googleapis.com/auth/cloud-platform" {
		t.Fatalf("CloudAssetInventoryOAuthScope = %q, want assets.list OAuth scope", CloudAssetInventoryOAuthScope)
	}
}

func TestLiveClientMapsBareAssetFamilyToBoundedFilter(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.AssetTypeFamily = "compute"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		got := r.URL.Query()["assetTypes"]
		if len(got) != 1 || got[0] != "compute.googleapis.com.*" {
			t.Fatalf("assetTypes = %#v, want bounded compute regex", got)
		}
		_, _ = w.Write([]byte(`{"readTime":"2026-06-16T12:00:00Z","assets":[]}`))
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource: staticTokenSource("live-token"),
		HTTPClient:  server.Client(),
		Endpoint:    server.URL,
	}
	if _, err := client.FetchPage(context.Background(), PageRequest{Scope: scopeCfg.withDefaults()}); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestLiveClientRejectsUnsupportedAssetFamilyBeforeRequest(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.AssetTypeFamily = "compute/instances"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource: staticTokenSource("live-token"),
		HTTPClient:  server.Client(),
		Endpoint:    server.URL,
	}
	_, err := client.FetchPage(context.Background(), PageRequest{Scope: scopeCfg.withDefaults()})
	if err == nil {
		t.Fatal("FetchPage returned nil error, want unsupported warning")
	}
	var warning ProviderWarning
	if !errors.As(err, &warning) {
		t.Fatalf("err = %T, want ProviderWarning", err)
	}
	if warning.WarningKind != gcpcloud.WarningKindUnsupported {
		t.Fatalf("warning kind = %q, want unsupported", warning.WarningKind)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestLiveClientRejectsMissingTokenSourceBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)

	client := LiveClient{HTTPClient: server.Client(), Endpoint: server.URL}
	_, err := client.FetchPage(context.Background(), PageRequest{Scope: testScope().withDefaults()})
	if err == nil {
		t.Fatal("FetchPage returned nil error, want missing token source")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestLiveClientRetriesThrottleThenParsesPage(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "quota body must stay out of errors", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"readTime":"2026-06-16T12:00:00Z","assets":[]}`))
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource:  staticTokenSource("live-token"),
		HTTPClient:   server.Client(),
		Endpoint:     server.URL,
		MaxAttempts:  2,
		RetryBackoff: func(int) time.Duration { return 0 },
	}
	if _, err := client.FetchPage(context.Background(), PageRequest{Scope: testScope().withDefaults()}); err != nil {
		t.Fatalf("FetchPage: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestLiveClientClassifiesPermissionWithoutResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "response-body-canary", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource: staticTokenSource("live-token"),
		HTTPClient:  server.Client(),
		Endpoint:    server.URL,
	}
	_, err := client.FetchPage(context.Background(), PageRequest{Scope: testScope().withDefaults()})
	if err == nil {
		t.Fatal("FetchPage returned nil error, want warning error")
	}
	var warning ProviderWarning
	if !errors.As(err, &warning) {
		t.Fatalf("err = %T, want ProviderWarning", err)
	}
	if warning.WarningKind != gcpcloud.WarningKindPartialPermission {
		t.Fatalf("warning kind = %q, want partial permission", warning.WarningKind)
	}
	if strings.Contains(err.Error(), "response-body-canary") {
		t.Fatalf("error leaked provider body: %q", err.Error())
	}
}

func TestLiveClientRejectsParentPathInjection(t *testing.T) {
	scopeCfg := testScope()
	scopeCfg.ParentScopeID = "project/with/slash"
	client := LiveClient{TokenSource: staticTokenSource("live-token")}

	_, err := client.FetchPage(context.Background(), PageRequest{Scope: scopeCfg.withDefaults()})
	if err == nil {
		t.Fatal("FetchPage returned nil error, want parent path validation error")
	}
	if strings.Contains(err.Error(), "project/with/slash") {
		t.Fatalf("error leaked parent id: %q", err.Error())
	}
}

func TestLiveClientClassifiesUnavailableSeparatelyFromQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "transient provider body", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client := LiveClient{
		TokenSource:  staticTokenSource("live-token"),
		HTTPClient:   server.Client(),
		Endpoint:     server.URL,
		MaxAttempts:  1,
		RetryBackoff: func(int) time.Duration { return 0 },
	}
	_, err := client.FetchPage(context.Background(), PageRequest{Scope: testScope().withDefaults()})
	if err == nil {
		t.Fatal("FetchPage returned nil error, want provider warning")
	}
	var warning ProviderWarning
	if !errors.As(err, &warning) {
		t.Fatalf("err = %T, want ProviderWarning", err)
	}
	if warning.WarningKind != gcpcloud.WarningKindUnavailable {
		t.Fatalf("warning kind = %q, want unavailable", warning.WarningKind)
	}
	if strings.Contains(err.Error(), "transient provider body") {
		t.Fatalf("error leaked provider body: %q", err.Error())
	}
}

type staticTokenSource string

func (s staticTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: string(s), TokenType: "Bearer"}, nil
}
