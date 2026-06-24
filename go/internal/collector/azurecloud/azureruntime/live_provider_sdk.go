// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
)

// maxLiveResourceRowJSONBytes bounds one live SDK row before it becomes a
// ResourceRow. The owned default query avoids provider properties; this cap
// protects explicitly overridden queries from persisting unbounded ARM metadata.
const maxLiveResourceRowJSONBytes = 64 * 1024

// AzureSDKResourceGraphClient adapts the official Azure Resource Graph SDK
// client to the narrow LiveResourceGraphClient interface. It is only used when
// explicitly injected into LiveProviderFactory; command defaults still use the
// gated zero-value factory.
type AzureSDKResourceGraphClient struct {
	client *armresourcegraph.Client
}

// NewAzureSDKResourceGraphClient wraps an existing Resource Graph SDK client.
func NewAzureSDKResourceGraphClient(client *armresourcegraph.Client) AzureSDKResourceGraphClient {
	return AzureSDKResourceGraphClient{client: client}
}

// NewAzureSDKResourceGraphClientFromCredential builds a Resource Graph SDK
// client from an Azure credential supplied by operator-owned wiring. This helper
// does not read credentials, create env-based secrets, or wire the command path;
// it only keeps the live adapter construction explicit.
func NewAzureSDKResourceGraphClientFromCredential(
	credential azcore.TokenCredential,
	options *arm.ClientOptions,
) (AzureSDKResourceGraphClient, error) {
	if credential == nil {
		return AzureSDKResourceGraphClient{}, fmt.Errorf("azure resource graph credential is required")
	}
	factory, err := armresourcegraph.NewClientFactory(credential, options)
	if err != nil {
		return AzureSDKResourceGraphClient{}, fmt.Errorf("create azure resource graph client factory: %w", err)
	}
	return AzureSDKResourceGraphClient{client: factory.NewClient()}, nil
}

// QueryResources executes one SDK Resource Graph Resources query and converts
// the objectArray response into the collector's provider-neutral page shape.
func (c AzureSDKResourceGraphClient) QueryResources(
	ctx context.Context,
	request LiveResourceGraphRequest,
) (LiveResourceGraphResponse, error) {
	if c.client == nil {
		return LiveResourceGraphResponse{}, fmt.Errorf("azure resource graph SDK client is required")
	}
	response, err := c.client.Resources(ctx, liveSDKQueryRequest(request), nil)
	if err != nil {
		return LiveResourceGraphResponse{}, err
	}
	page, err := liveResourceGraphPageFromSDKResponse(response.QueryResponse)
	if err != nil {
		return LiveResourceGraphResponse{}, err
	}
	return LiveResourceGraphResponse{Page: page}, nil
}

func liveSDKQueryRequest(request LiveResourceGraphRequest) armresourcegraph.QueryRequest {
	resultFormat := armresourcegraph.ResultFormatObjectArray
	return armresourcegraph.QueryRequest{
		Query:            to.Ptr(request.Query),
		Subscriptions:    liveSDKStringPointers(request.Subscriptions),
		ManagementGroups: liveSDKStringPointers(request.ManagementGroups),
		Options: &armresourcegraph.QueryRequestOptions{
			AllowPartialScopes: to.Ptr(request.AllowPartialScopes),
			ResultFormat:       &resultFormat,
			SkipToken:          liveSDKOptionalString(request.SkipToken),
			Top:                to.Ptr(request.PageSize),
		},
	}
}

func liveResourceGraphPageFromSDKResponse(
	response armresourcegraph.QueryResponse,
) (azurecloud.ResourceGraphPage, error) {
	rows, err := liveSDKResourceRows(response.Data)
	if err != nil {
		return azurecloud.ResourceGraphPage{}, err
	}
	return azurecloud.ResourceGraphPage{
		TotalRecords:    liveInt64(response.TotalRecords),
		Count:           liveInt64(response.Count),
		ResultTruncated: liveResultTruncated(response.ResultTruncated),
		SkipToken:       liveString(response.SkipToken),
		Rows:            rows,
	}, nil
}

func liveSDKResourceRows(data any) ([]azurecloud.ResourceRow, error) {
	if data == nil {
		return nil, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal azure resource graph data: %w", err)
	}
	var rawRows []json.RawMessage
	if err := json.Unmarshal(raw, &rawRows); err != nil {
		return nil, fmt.Errorf("decode azure resource graph objectArray data: %w", err)
	}
	rows := make([]azurecloud.ResourceRow, 0, len(rawRows))
	for _, rawRow := range rawRows {
		if len(rawRow) > maxLiveResourceRowJSONBytes {
			return nil, fmt.Errorf("azure resource graph row exceeds %d byte live bound", maxLiveResourceRowJSONBytes)
		}
		var row azurecloud.ResourceRow
		if err := json.Unmarshal(rawRow, &row); err != nil {
			return nil, fmt.Errorf("decode azure resource graph objectArray row: %w", err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func classifyAzureSDKError(err error) (liveProviderError, bool) {
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		return liveProviderError{}, false
	}
	if responseErr.StatusCode == http.StatusTooManyRequests ||
		strings.EqualFold(responseErr.ErrorCode, "RateLimiting") {
		return liveProviderError{
			kind:       liveProviderErrorThrottled,
			retryAfter: retryAfterFromResponse(responseErr.RawResponse),
			err:        err,
		}, true
	}
	if responseErr.StatusCode == http.StatusForbidden {
		return liveProviderError{kind: liveProviderErrorPermissionHidden, err: err}, true
	}
	if responseErr.StatusCode == http.StatusUnauthorized ||
		azureSDKErrorCode(responseErr.ErrorCode) == "expiredauthenticationtoken" {
		return liveProviderError{kind: liveProviderErrorTokenExpired, err: err}, true
	}
	if responseErr.StatusCode == http.StatusBadRequest &&
		isAzureSDKSkipTokenError(responseErr.ErrorCode) {
		return liveProviderError{kind: liveProviderErrorSkipTokenExpired, err: err}, true
	}
	return liveProviderError{}, false
}

func isAzureSDKSkipTokenError(errorCode string) bool {
	code := azureSDKErrorCode(errorCode)
	return strings.Contains(code, "skiptoken") &&
		(strings.Contains(code, "invalid") || strings.Contains(code, "expired"))
}

func azureSDKErrorCode(errorCode string) string {
	code := strings.ToLower(strings.TrimSpace(errorCode))
	code = strings.ReplaceAll(code, "_", "")
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func retryAfterFromResponse(response *http.Response) time.Duration {
	if response == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if delay := time.Until(at); delay > 0 {
			return delay
		}
	}
	return 0
}

func liveSDKStringPointers(values []string) []*string {
	out := make([]*string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, to.Ptr(trimmed))
		}
	}
	return out
}

func liveSDKOptionalString(value string) *string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return to.Ptr(trimmed)
	}
	return nil
}

func liveInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func liveString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func liveResultTruncated(value *armresourcegraph.ResultTruncated) bool {
	return value != nil && *value == armresourcegraph.ResultTruncatedTrue
}
