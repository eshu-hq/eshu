// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

type mockLiveARMFallbackClient struct {
	calls     []LiveARMFallbackRequest
	responses []LiveARMFallbackResponse
	errs      []error
}

func (m *mockLiveARMFallbackClient) GetResource(
	_ context.Context,
	request LiveARMFallbackRequest,
) (LiveARMFallbackResponse, error) {
	m.calls = append(m.calls, request)
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		if err != nil {
			return LiveARMFallbackResponse{}, err
		}
	}
	if len(m.responses) == 0 {
		return LiveARMFallbackResponse{}, errors.New("unexpected arm fallback query")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func TestExplicitLiveProviderARMFallbackEnrichesAllowlistedRows(t *testing.T) {
	resourceID := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1"
	resourceGraph := &mockLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 1,
			Count:        1,
			Rows: []azurecloud.ResourceRow{
				resourceRow(resourceID, "microsoft.compute/virtualmachines", "eastus"),
			},
		}}},
	}
	armFallback := &mockLiveARMFallbackClient{
		responses: []LiveARMFallbackResponse{{Extension: map[string]any{
			"powerState":              "running",
			"administratorPassword":   "must-not-persist",
			"privateIPAddress":        "must-not-persist",
			"unrequestedSafeMetadata": "must-not-persist",
		}}},
	}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules: []LiveARMFallbackRule{{
			ResourceType:    "microsoft.compute/virtualmachines",
			APIVersion:      "2024-03-01",
			ExtensionFields: []string{"powerState", "administratorPassword", "privateIPAddress"},
		}},
	}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 1 || result.WarningCount != 0 {
		t.Fatalf("counts = resources:%d warnings:%d, want 1/0", result.ResourceCount, result.WarningCount)
	}
	if len(armFallback.calls) != 1 {
		t.Fatalf("arm fallback calls = %d, want 1", len(armFallback.calls))
	}
	call := armFallback.calls[0]
	if call.ResourceID != resourceID || call.APIVersion != "2024-03-01" {
		t.Fatalf("fallback call = %+v, want resource id and API version", call)
	}

	resource := factsOfKind(result.Facts, facts.AzureCloudResourceFactKind)[0]
	extension := resource.Payload["extension"].(map[string]any)
	data := extension["data"].(map[string]any)
	fallback := data["armFallback"].(map[string]any)
	fallbackData := fallback["data"].(map[string]any)
	if fallback["schema_version"] != liveARMFallbackExtensionSchemaVersion {
		t.Fatalf("arm fallback schema = %v", fallback["schema_version"])
	}
	if fallbackData["powerState"] != "running" {
		t.Fatalf("powerState = %v, want running", fallbackData["powerState"])
	}
	for _, forbidden := range []string{"administratorPassword", "privateIPAddress", "unrequestedSafeMetadata"} {
		if _, ok := fallbackData[forbidden]; ok {
			t.Fatalf("forbidden/unrequested fallback field %q persisted: %#v", forbidden, fallbackData)
		}
	}
}

func TestExplicitLiveProviderARMFallbackRejectsIncompleteOptIn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factory LiveProviderFactory
	}{
		{
			name: "allowlist without client",
			factory: LiveProviderFactory{
				ResourceGraphClient: &mockLiveResourceGraphClient{},
				ARMFallbackRules: []LiveARMFallbackRule{{
					ResourceType:    "microsoft.compute/virtualmachines",
					APIVersion:      "2024-03-01",
					ExtensionFields: []string{"powerState"},
				}},
			},
		},
		{
			name: "client without allowlist",
			factory: LiveProviderFactory{
				ResourceGraphClient: &mockLiveResourceGraphClient{},
				ARMFallbackClient:   &mockLiveARMFallbackClient{},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget()); err == nil {
				t.Fatal("PageProvider() error = nil, want fail-closed ARM fallback configuration error")
			}
		})
	}
}

func TestExplicitLiveProviderARMFallbackSkippedForUnallowlistedRows(t *testing.T) {
	resourceGraph := &mockLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 1,
			Count:        1,
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Network/publicIPAddresses/pip-1",
					"microsoft.network/publicipaddresses",
					"eastus",
				),
			},
		}}},
	}
	armFallback := &mockLiveARMFallbackClient{}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules: []LiveARMFallbackRule{{
			ResourceType:    "microsoft.compute/virtualmachines",
			APIVersion:      "2024-03-01",
			ExtensionFields: []string{"powerState"},
		}},
	}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(armFallback.calls) != 0 {
		t.Fatalf("arm fallback calls = %d, want 0", len(armFallback.calls))
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	if got := warnings[0].Payload["warning_kind"]; got != azurecloud.WarningFallbackSkipped {
		t.Fatalf("warning_kind = %v, want fallback_skipped", got)
	}
}

func TestExplicitLiveProviderARMFallbackFailuresOutrankSkippedRows(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		response  LiveARMFallbackResponse
		want      string
		wantSaved bool
	}{
		{
			name: "throttled",
			err:  liveProviderError{kind: liveProviderErrorThrottled},
			want: azurecloud.WarningThrottled,
		},
		{
			name: "timeout_stale",
			err:  context.DeadlineExceeded,
			want: azurecloud.WarningStale,
		},
		{
			name: "permission_hidden",
			err:  liveProviderError{kind: liveProviderErrorPermissionHidden},
			want: azurecloud.WarningPermissionHidden,
		},
		{
			name: "oversized_redaction",
			response: LiveARMFallbackResponse{Extension: map[string]any{
				"powerState": strings.Repeat("x", maxLiveARMFallbackExtensionJSONBytes),
			}},
			want: azurecloud.WarningRedaction,
		},
		{
			name: "successful_fallback_preserves_skip",
			response: LiveARMFallbackResponse{Extension: map[string]any{
				"powerState": "running",
			}},
			want:      azurecloud.WarningFallbackSkipped,
			wantSaved: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resourceGraph := &mockLiveResourceGraphClient{
				responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
					TotalRecords: 2,
					Count:        2,
					Rows: []azurecloud.ResourceRow{
						resourceRow(
							"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Network/publicIPAddresses/pip-1",
							"microsoft.network/publicipaddresses",
							"eastus",
						),
						resourceRow(
							"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1",
							"microsoft.compute/virtualmachines",
							"eastus",
						),
					},
				}}},
			}
			armFallback := &mockLiveARMFallbackClient{
				responses: []LiveARMFallbackResponse{tc.response},
				errs:      []error{tc.err},
			}
			factory := LiveProviderFactory{
				ResourceGraphClient: resourceGraph,
				ARMFallbackClient:   armFallback,
				ARMFallbackRules: []LiveARMFallbackRule{{
					ResourceType:    "microsoft.compute/virtualmachines",
					APIVersion:      "2024-03-01",
					ExtensionFields: []string{"powerState"},
				}},
			}
			provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
			if err != nil {
				t.Fatalf("PageProvider() error = %v, want nil", err)
			}

			result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
			if err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
			if len(warnings) != 1 {
				t.Fatalf("warnings = %d, want 1", len(warnings))
			}
			if got := warnings[0].Payload["warning_kind"]; got != tc.want {
				t.Fatalf("warning_kind = %v, want %s", got, tc.want)
			}
			if message := warnings[0].Payload["message"].(string); strings.Contains(message, "11111111") ||
				strings.Contains(message, "rg-app") ||
				strings.Contains(message, "vm-1") ||
				strings.Contains(message, "pip-1") {
				t.Fatalf("warning message carried raw provider identity: %q", message)
			}
			if len(armFallback.calls) != 1 {
				t.Fatalf("arm fallback calls = %d, want 1", len(armFallback.calls))
			}
			resource := factsOfKind(result.Facts, facts.AzureCloudResourceFactKind)[1]
			extension := resource.Payload["extension"].(map[string]any)
			data := extension["data"].(map[string]any)
			_, saved := data["armFallback"]
			if saved != tc.wantSaved {
				t.Fatalf("armFallback saved = %v, want %v", saved, tc.wantSaved)
			}
		})
	}
}

func TestExplicitLiveProviderARMFallbackSkipsInvalidResourceID(t *testing.T) {
	resourceGraph := &mockLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 1,
			Count:        1,
			Rows: []azurecloud.ResourceRow{
				resourceRow("not-an-arm-id", "microsoft.compute/virtualmachines", "eastus"),
			},
		}}},
	}
	armFallback := &mockLiveARMFallbackClient{}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules: []LiveARMFallbackRule{{
			ResourceType:    "microsoft.compute/virtualmachines",
			APIVersion:      "2024-03-01",
			ExtensionFields: []string{"powerState"},
		}},
	}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(armFallback.calls) != 0 {
		t.Fatalf("arm fallback calls = %d, want 0", len(armFallback.calls))
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	if got := warnings[0].Payload["warning_kind"]; got != azurecloud.WarningUnsupported {
		t.Fatalf("warning_kind = %v, want unsupported", got)
	}
}

func TestExplicitLiveProviderARMFallbackThrottleAndTimeoutAreWarnings(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "throttled",
			err:  liveProviderError{kind: liveProviderErrorThrottled},
			want: azurecloud.WarningThrottled,
		},
		{
			name: "timeout",
			err:  context.DeadlineExceeded,
			want: azurecloud.WarningStale,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resourceGraph := &mockLiveResourceGraphClient{
				responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
					TotalRecords: 1,
					Count:        1,
					Rows: []azurecloud.ResourceRow{
						resourceRow(
							"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1",
							"microsoft.compute/virtualmachines",
							"eastus",
						),
					},
				}}},
			}
			armFallback := &mockLiveARMFallbackClient{errs: []error{tc.err}}
			factory := LiveProviderFactory{
				ResourceGraphClient: resourceGraph,
				ARMFallbackClient:   armFallback,
				ARMFallbackRules: []LiveARMFallbackRule{{
					ResourceType:    "microsoft.compute/virtualmachines",
					APIVersion:      "2024-03-01",
					ExtensionFields: []string{"powerState"},
				}},
			}
			provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
			if err != nil {
				t.Fatalf("PageProvider() error = %v, want nil", err)
			}

			result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
			if err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
			if len(warnings) != 1 {
				t.Fatalf("warnings = %d, want 1", len(warnings))
			}
			if got := warnings[0].Payload["warning_kind"]; got != tc.want {
				t.Fatalf("warning_kind = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestExplicitLiveProviderARMFallbackRejectsOversizedExtension(t *testing.T) {
	resourceGraph := &mockLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 1,
			Count:        1,
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1",
					"microsoft.compute/virtualmachines",
					"eastus",
				),
			},
		}}},
	}
	armFallback := &mockLiveARMFallbackClient{
		responses: []LiveARMFallbackResponse{{Extension: map[string]any{
			"powerState": strings.Repeat("x", maxLiveARMFallbackExtensionJSONBytes),
		}}},
	}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules: []LiveARMFallbackRule{{
			ResourceType:    "microsoft.compute/virtualmachines",
			APIVersion:      "2024-03-01",
			ExtensionFields: []string{"powerState"},
		}},
	}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	resource := factsOfKind(result.Facts, facts.AzureCloudResourceFactKind)[0]
	extension := resource.Payload["extension"].(map[string]any)
	data := extension["data"].(map[string]any)
	if _, ok := data["armFallback"]; ok {
		t.Fatalf("oversized arm fallback extension persisted: %#v", data["armFallback"])
	}
	warning := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)[0]
	if got := warning.Payload["warning_kind"]; got != azurecloud.WarningRedaction {
		t.Fatalf("warning_kind = %v, want redaction", got)
	}
}
