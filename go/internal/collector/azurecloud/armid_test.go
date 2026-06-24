// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import "testing"

func TestParseARMIdentitySubscriptionResource(t *testing.T) {
	const id = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01"
	got, err := ParseARMIdentity(id)
	if err != nil {
		t.Fatalf("ParseARMIdentity returned error: %v", err)
	}
	want := ARMIdentity{
		SubscriptionID:    "11111111-1111-1111-1111-111111111111",
		ResourceGroup:     "rg-app",
		ProviderNamespace: "microsoft.compute",
		ResourceType:      "microsoft.compute/virtualmachines",
		ResourceName:      "vm-web-01",
		Normalized:        "/subscriptions/11111111-1111-1111-1111-111111111111/resourcegroups/rg-app/providers/microsoft.compute/virtualmachines/vm-web-01",
	}
	if got != want {
		t.Fatalf("ParseARMIdentity =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseARMIdentityNestedResourceType(t *testing.T) {
	const id = "/subscriptions/22222222-2222-2222-2222-222222222222/resourceGroups/rg-net/providers/Microsoft.Network/virtualNetworks/vnet-core/subnets/web"
	got, err := ParseARMIdentity(id)
	if err != nil {
		t.Fatalf("ParseARMIdentity returned error: %v", err)
	}
	if got.ResourceType != "microsoft.network/virtualnetworks/subnets" {
		t.Fatalf("ResourceType = %q, want microsoft.network/virtualnetworks/subnets", got.ResourceType)
	}
	if got.ResourceName != "web" {
		t.Fatalf("ResourceName = %q, want web", got.ResourceName)
	}
	if got.ResourceGroup != "rg-net" {
		t.Fatalf("ResourceGroup = %q, want rg-net", got.ResourceGroup)
	}
}

func TestParseARMIdentityErrors(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"not absolute":       "subscriptions/x/resourceGroups/rg/providers/p/t/n",
		"missing provider":   "/subscriptions/sub/resourceGroups/rg",
		"missing name":       "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines",
		"missing resource g": "/subscriptions/sub/providers/Microsoft.Compute/virtualMachines/vm",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseARMIdentity(id); err == nil {
				t.Fatalf("ParseARMIdentity(%q) error = nil, want error", id)
			}
		})
	}
}

func TestParseARMIdentityIsDeterministic(t *testing.T) {
	const id = "/subscriptions/AAAA/resourceGroups/RG/providers/Microsoft.Storage/storageAccounts/Acct"
	first, err := ParseARMIdentity(id)
	if err != nil {
		t.Fatalf("first parse error: %v", err)
	}
	second, err := ParseARMIdentity(id)
	if err != nil {
		t.Fatalf("second parse error: %v", err)
	}
	if first != second {
		t.Fatalf("ParseARMIdentity not deterministic: %+v vs %+v", first, second)
	}
}
