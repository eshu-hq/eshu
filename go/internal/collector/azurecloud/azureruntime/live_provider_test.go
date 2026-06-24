// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

type mockLiveResourceGraphClient struct {
	calls     []LiveResourceGraphRequest
	responses []LiveResourceGraphResponse
	errs      []error
}

func (m *mockLiveResourceGraphClient) QueryResources(
	_ context.Context,
	request LiveResourceGraphRequest,
) (LiveResourceGraphResponse, error) {
	m.calls = append(m.calls, request)
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		if err != nil {
			return LiveResourceGraphResponse{}, err
		}
	}
	if len(m.responses) == 0 {
		return LiveResourceGraphResponse{}, errors.New("unexpected live query")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func TestExplicitLiveProviderPaginatesBySkipToken(t *testing.T) {
	client := &mockLiveResourceGraphClient{responses: []LiveResourceGraphResponse{
		{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 2,
			Count:        1,
			SkipToken:    fixtureSkipToken,
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1",
					"microsoft.compute/virtualmachines",
					"eastus",
				),
			},
		}},
		{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 2,
			Count:        1,
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-2",
					"microsoft.compute/virtualmachines",
					"eastus",
				),
			},
		}},
	}}
	factory := LiveProviderFactory{
		ResourceGraphClient: client,
		PageSize:            200,
	}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	boundary := testBoundary()
	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 2 {
		t.Fatalf("ResourceCount = %d, want 2", result.ResourceCount)
	}
	if len(client.calls) != 2 {
		t.Fatalf("live calls = %d, want 2", len(client.calls))
	}
	if client.calls[0].SkipToken != "" || client.calls[1].SkipToken != fixtureSkipToken {
		t.Fatalf(
			"skip token calls = [%q %q], want [\"\" %q]",
			client.calls[0].SkipToken,
			client.calls[1].SkipToken,
			fixtureSkipToken,
		)
	}
	for _, call := range client.calls {
		if call.PageSize != 200 {
			t.Fatalf("PageSize = %d, want 200", call.PageSize)
		}
		if len(call.Subscriptions) != 1 || call.Subscriptions[0] != testTarget().ProviderScopeID {
			t.Fatalf("subscriptions = %#v, want target provider scope", call.Subscriptions)
		}
		if call.Query == "" {
			t.Fatal("live query is blank")
		}
	}
}

func TestExplicitLiveProviderThrottleBackoffIsBounded(t *testing.T) {
	client := &mockLiveResourceGraphClient{
		errs: []error{
			liveProviderError{kind: liveProviderErrorThrottled, retryAfter: 10 * time.Second},
			nil,
		},
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
	var slept []time.Duration
	factory := LiveProviderFactory{
		ResourceGraphClient: client,
		MaxRetries:          1,
		BackoffCap:          2 * time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 1 || result.WarningCount != 1 {
		t.Fatalf("counts = resources:%d warnings:%d, want 1/1", result.ResourceCount, result.WarningCount)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("sleep calls = %v, want capped 2s backoff", slept)
	}
	warning := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)[0]
	if got := warning.Payload["warning_kind"]; got != azurecloud.WarningThrottled {
		t.Fatalf("warning_kind = %v, want throttled", got)
	}
	if warning.Payload["retryable"] != true {
		t.Fatalf("retryable = %v, want true", warning.Payload["retryable"])
	}
}

func TestExplicitLiveProviderExpiredSkipTokenProducesPartialWarning(t *testing.T) {
	client := &mockLiveResourceGraphClient{
		errs: []error{
			nil,
			liveProviderError{kind: liveProviderErrorSkipTokenExpired},
		},
		responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
			TotalRecords: 2,
			Count:        1,
			SkipToken:    "expired-token",
			Rows: []azurecloud.ResourceRow{
				resourceRow(
					"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1",
					"microsoft.compute/virtualmachines",
					"eastus",
				),
			},
		}}},
	}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 1 || result.WarningCount != 1 {
		t.Fatalf("counts = resources:%d warnings:%d, want 1/1", result.ResourceCount, result.WarningCount)
	}
	warning := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)[0]
	if got := warning.Payload["warning_kind"]; got != azurecloud.WarningStale {
		t.Fatalf("warning_kind = %v, want stale token warning", got)
	}
	if warning.Payload["retryable"] != true {
		t.Fatalf("retryable = %v, want true", warning.Payload["retryable"])
	}
}

func TestExplicitLiveProviderPermissionHiddenReportsScopeAccess(t *testing.T) {
	client := &mockLiveResourceGraphClient{responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{
		TotalRecords: 0,
		Count:        0,
	}, Access: azurecloud.ScopeAccess{
		Partial:             true,
		HiddenResourceCount: 2,
		Reason:              azurecloud.WarningPermissionHidden,
		Message:             "some configured resources were hidden from the read-only identity",
	}}}}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}

	result, err := azurecloud.NewCollector(provider, nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if !result.Partial || result.WarningCount != 1 {
		t.Fatalf("partial=%v warnings=%d, want partial warning", result.Partial, result.WarningCount)
	}
	warning := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)[0]
	if got := warning.Payload["warning_kind"]; got != azurecloud.WarningPermissionHidden {
		t.Fatalf("warning_kind = %v, want permission_hidden", got)
	}
	if got := warning.Payload["hidden_resource_count"]; got != 2 {
		t.Fatalf("hidden_resource_count = %#v, want 2", got)
	}
}

func TestExplicitLiveProviderRejectsMissingCredentialRef(t *testing.T) {
	target := testTarget()
	target.CredentialRef = ""
	factory := LiveProviderFactory{ResourceGraphClient: &mockLiveResourceGraphClient{}}
	if _, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, target); err == nil {
		t.Fatal("PageProvider() error = nil, want missing credential ref error")
	}
}

func TestExplicitLiveProviderDefaultQueryExcludesPropertiesBag(t *testing.T) {
	client := &mockLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: azurecloud.ResourceGraphPage{}}},
	}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), azurecloud.Boundary{}, testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	if _, err := provider.NextPage(context.Background(), ""); err != nil {
		t.Fatalf("NextPage() error = %v, want nil", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("live calls = %d, want 1", len(client.calls))
	}
	if strings.Contains(strings.ToLower(client.calls[0].Query), "properties") {
		t.Fatalf("default live query projects properties bag: %q", client.calls[0].Query)
	}
}

func testBoundary() azurecloud.Boundary {
	return azurecloud.Boundary{
		CollectorInstanceID: "azure-collector-1",
		TenantID:            testTarget().TenantID,
		ScopeKind:           testTarget().ScopeKind,
		ProviderScopeID:     testTarget().ProviderScopeID,
		ResourceTypeFamily:  testTarget().ResourceTypeFamily,
		LocationBucket:      testTarget().LocationBucket,
		SourceLane:          azurecloud.SourceLaneResourceGraph,
		ScopeID:             "azure:tenant-abc:subscription:11111111-1111-1111-1111-111111111111:microsoft.compute:eastus:resource_graph",
		GenerationID:        "generation-1",
		FencingToken:        7,
		ObservedAt:          fixedClock()(),
	}
}
