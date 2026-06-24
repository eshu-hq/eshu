// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
)

func TestLiveSDKQueryRequestMapsBoundedOptions(t *testing.T) {
	request := liveSDKQueryRequest(LiveResourceGraphRequest{
		Query:              "Resources | project id",
		Subscriptions:      []string{"sub-1"},
		ManagementGroups:   []string{"mg-1"},
		SkipToken:          "skip-2",
		PageSize:           200,
		AllowPartialScopes: true,
	})

	if request.Query == nil || *request.Query != "Resources | project id" {
		t.Fatalf("query = %#v", request.Query)
	}
	if got := derefStrings(request.Subscriptions); strings.Join(got, ",") != "sub-1" {
		t.Fatalf("subscriptions = %#v", got)
	}
	if got := derefStrings(request.ManagementGroups); strings.Join(got, ",") != "mg-1" {
		t.Fatalf("management groups = %#v", got)
	}
	if request.Options == nil {
		t.Fatal("Options = nil, want bounded options")
	}
	if request.Options.Top == nil || *request.Options.Top != 200 {
		t.Fatalf("Top = %#v, want 200", request.Options.Top)
	}
	if request.Options.SkipToken == nil || *request.Options.SkipToken != "skip-2" {
		t.Fatalf("SkipToken = %#v, want skip-2", request.Options.SkipToken)
	}
	if request.Options.ResultFormat == nil ||
		*request.Options.ResultFormat != armresourcegraph.ResultFormatObjectArray {
		t.Fatalf("ResultFormat = %#v, want objectArray", request.Options.ResultFormat)
	}
	if request.Options.AllowPartialScopes == nil || !*request.Options.AllowPartialScopes {
		t.Fatalf("AllowPartialScopes = %#v, want true", request.Options.AllowPartialScopes)
	}
}

func TestLiveResourceGraphPageFromSDKResponse(t *testing.T) {
	response := armresourcegraph.QueryResponse{
		TotalRecords:    to.Ptr[int64](2),
		Count:           to.Ptr[int64](1),
		ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedTrue),
		SkipToken:       to.Ptr("skip-2"),
		Data: []any{
			map[string]any{
				"id":             "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"name":           "vm",
				"type":           "microsoft.compute/virtualmachines",
				"tenantId":       "tenant-marker",
				"subscriptionId": "sub-1",
				"resourceGroup":  "rg",
				"location":       "eastus",
				"tags":           map[string]any{"env": "prod"},
			},
		},
	}

	page, err := liveResourceGraphPageFromSDKResponse(response)
	if err != nil {
		t.Fatalf("liveResourceGraphPageFromSDKResponse() error = %v, want nil", err)
	}
	if page.TotalRecords != 2 || page.Count != 1 || !page.ResultTruncated || page.SkipToken != "skip-2" {
		t.Fatalf("page metadata = %+v", page)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(page.Rows))
	}
	row := page.Rows[0]
	if row.ID == "" || row.Tags["env"] != "prod" {
		t.Fatalf("row = %+v", row)
	}
}

func TestClassifyAzureSDKThrottleError(t *testing.T) {
	response := &http.Response{Header: make(http.Header)}
	response.Header.Set("Retry-After", "7")
	err := &azcore.ResponseError{
		StatusCode:  http.StatusTooManyRequests,
		ErrorCode:   "RateLimiting",
		RawResponse: response,
	}

	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorThrottled {
		t.Fatalf("kind = %q, want throttled", liveErr.kind)
	}
	if liveErr.retryAfter == 0 {
		t.Fatal("retryAfter = 0, want Retry-After-derived delay")
	}
}

func TestClassifyAzureSDKExpiredAuthTokenAsStale(t *testing.T) {
	err := &azcore.ResponseError{
		StatusCode: http.StatusUnauthorized,
		ErrorCode:  "ExpiredAuthenticationToken",
	}

	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorTokenExpired {
		t.Fatalf("kind = %q, want token_expired", liveErr.kind)
	}
}

func TestClassifyAzureSDKExpiredSkipTokenAsStale(t *testing.T) {
	err := &azcore.ResponseError{
		StatusCode: http.StatusBadRequest,
		ErrorCode:  "InvalidSkipToken",
	}

	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorSkipTokenExpired {
		t.Fatalf("kind = %q, want skip_token_expired", liveErr.kind)
	}
}

func TestLiveResourceGraphPageFromSDKResponseRejectsTableData(t *testing.T) {
	_, err := liveResourceGraphPageFromSDKResponse(armresourcegraph.QueryResponse{
		Data: map[string]any{"columns": []any{}, "rows": []any{}},
	})
	if err == nil {
		t.Fatal("error = nil, want objectArray conversion error")
	}
}

func TestLiveResourceGraphPageFromSDKResponseRejectsOversizedRows(t *testing.T) {
	oversized := strings.Repeat("x", maxLiveResourceRowJSONBytes)
	_, err := liveResourceGraphPageFromSDKResponse(armresourcegraph.QueryResponse{
		Data: []any{
			map[string]any{
				"id":         "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"name":       "vm",
				"type":       "microsoft.compute/virtualmachines",
				"properties": map[string]any{"safe": oversized},
			},
		},
	})
	if err == nil {
		t.Fatal("error = nil, want oversized row rejection")
	}
}

func TestRetryAfterFromResponseIgnoresPastHTTPDate(t *testing.T) {
	response := &http.Response{Header: make(http.Header)}
	response.Header.Set("Retry-After", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat))
	if got := retryAfterFromResponse(response); got != 0 {
		t.Fatalf("retryAfterFromResponse() = %v, want 0", got)
	}
}

func derefStrings(values []*string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil {
			out = append(out, *value)
		}
	}
	return out
}

func TestLiveSDKClientRequiresConcreteClient(t *testing.T) {
	client := AzureSDKResourceGraphClient{}
	if _, err := client.QueryResources(t.Context(), LiveResourceGraphRequest{}); err == nil {
		t.Fatal("QueryResources() error = nil, want missing SDK client")
	}
}

func TestClassifyUnknownErrorStaysUnclassified(t *testing.T) {
	if _, ok := classifyLiveProviderError(errors.New("boom")); ok {
		t.Fatal("unknown error classified as live provider error")
	}
}
