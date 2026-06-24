// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

// AzureSDKARMFallbackClient adapts the official Azure Resource Manager SDK
// resources client to the narrow LiveARMFallbackClient interface. The wrapper
// exposes only Client.GetByID so operator-owned wiring cannot reach mutating
// ARM operations through the fallback seam.
type AzureSDKARMFallbackClient struct {
	client *armresources.Client
}

// NewAzureSDKARMFallbackClient wraps an existing ARM resources SDK client.
func NewAzureSDKARMFallbackClient(client *armresources.Client) AzureSDKARMFallbackClient {
	return AzureSDKARMFallbackClient{client: client}
}

// NewAzureSDKARMFallbackClientFromCredential builds an ARM resources SDK client
// from an Azure credential supplied by operator-owned wiring. This helper does
// not read credential material or wire command defaults; callers must inject the
// resulting client explicitly into LiveProviderFactory.
func NewAzureSDKARMFallbackClientFromCredential(
	subscriptionID string,
	credential azcore.TokenCredential,
	options *arm.ClientOptions,
) (AzureSDKARMFallbackClient, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return AzureSDKARMFallbackClient{}, fmt.Errorf("azure ARM fallback subscription id is required")
	}
	if credential == nil {
		return AzureSDKARMFallbackClient{}, fmt.Errorf("azure ARM fallback credential is required")
	}
	client, err := armresources.NewClient(subscriptionID, credential, options)
	if err != nil {
		return AzureSDKARMFallbackClient{}, fmt.Errorf("create azure ARM resources client: %w", err)
	}
	return AzureSDKARMFallbackClient{client: client}, nil
}

// GetResource executes one read-only ARM GET-by-ID call and returns only the
// provider properties object for later allowlist filtering and redaction.
func (c AzureSDKARMFallbackClient) GetResource(
	ctx context.Context,
	request LiveARMFallbackRequest,
) (LiveARMFallbackResponse, error) {
	if c.client == nil {
		return LiveARMFallbackResponse{}, fmt.Errorf("azure ARM resources SDK client is required")
	}
	response, err := c.client.GetByID(ctx, request.ResourceID, request.APIVersion, nil)
	if err != nil {
		return LiveARMFallbackResponse{}, err
	}
	extension, err := liveARMFallbackPropertiesMap(response.Properties)
	if err != nil {
		return LiveARMFallbackResponse{}, err
	}
	return LiveARMFallbackResponse{Extension: extension}, nil
}

func liveARMFallbackPropertiesMap(properties any) (map[string]any, error) {
	if properties == nil {
		return nil, nil
	}
	if extension, ok := properties.(map[string]any); ok {
		return extension, nil
	}
	raw, err := json.Marshal(properties)
	if err != nil {
		return nil, fmt.Errorf("marshal azure ARM fallback properties: %w", err)
	}
	var extension map[string]any
	if err := json.Unmarshal(raw, &extension); err != nil {
		return nil, fmt.Errorf("decode azure ARM fallback properties object: %w", err)
	}
	return extension, nil
}
