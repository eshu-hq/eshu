// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
)

// ARMIdentity is the normalized identity parsed from an Azure Resource Manager
// resource ID. The raw ARM resource ID stays the durable provider identity;
// these fields are normalized source evidence the reducer uses to resolve the
// shared cloud_resource_uid keyspace. Normalized lowercases the ARM ID so case
// variation does not split a stable fact key.
type ARMIdentity struct {
	// SubscriptionID is the Azure subscription GUID segment.
	SubscriptionID string
	// ResourceGroup is the resource group segment.
	ResourceGroup string
	// ProviderNamespace is the lowercased provider namespace, for example
	// "microsoft.compute".
	ProviderNamespace string
	// ResourceType is the lowercased fully qualified resource type, for example
	// "microsoft.network/virtualnetworks/subnets" for nested resources.
	ResourceType string
	// ResourceName is the leaf resource name.
	ResourceName string
	// Normalized is the lowercased ARM resource ID used for stable key
	// derivation.
	Normalized string
}

// ParseARMIdentity parses a subscription-scoped ARM resource ID into normalized
// identity fields. It accepts nested resource types (type/subtype paths) and
// rejects IDs that are not absolute or lack a provider/type/name triple.
//
// It does not accept management-group or tenant-scoped IDs; those scopes carry
// their own identity shape and are out of this slice. Callers normalize the
// raw ARM ID once here so every emitted fact shares one deterministic identity.
func ParseARMIdentity(armResourceID string) (ARMIdentity, error) {
	trimmed := strings.TrimSpace(armResourceID)
	if trimmed == "" {
		return ARMIdentity{}, fmt.Errorf("arm resource id is empty")
	}
	if !strings.HasPrefix(trimmed, "/") {
		return ARMIdentity{}, fmt.Errorf("arm resource id must be absolute: %q", armResourceID)
	}

	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	lower := make([]string, len(segments))
	for i, segment := range segments {
		lower[i] = strings.ToLower(segment)
	}

	subscriptionID, err := segmentValue(segments, lower, "subscriptions")
	if err != nil {
		return ARMIdentity{}, err
	}
	resourceGroup, err := segmentValue(segments, lower, "resourcegroups")
	if err != nil {
		return ARMIdentity{}, err
	}

	providerIndex := indexOf(lower, "providers")
	if providerIndex < 0 || providerIndex+1 >= len(segments) {
		return ARMIdentity{}, fmt.Errorf("arm resource id missing providers segment: %q", armResourceID)
	}

	providerNamespace := lower[providerIndex+1]
	typeAndName := segments[providerIndex+2:]
	// A valid resource path after the provider namespace is type/name pairs:
	// type, name[, subtype, subname...]. It must have an even, non-zero length.
	if len(typeAndName) < 2 || len(typeAndName)%2 != 0 {
		return ARMIdentity{}, fmt.Errorf("arm resource id provider path is not type/name balanced: %q", armResourceID)
	}

	typeParts := []string{providerNamespace}
	for i := 0; i < len(typeAndName); i += 2 {
		typeParts = append(typeParts, strings.ToLower(typeAndName[i]))
	}
	resourceName := typeAndName[len(typeAndName)-1]

	return ARMIdentity{
		SubscriptionID:    subscriptionID,
		ResourceGroup:     resourceGroup,
		ProviderNamespace: providerNamespace,
		ResourceType:      strings.Join(typeParts, "/"),
		ResourceName:      resourceName,
		Normalized:        "/" + strings.Join(lower, "/"),
	}, nil
}

func segmentValue(segments, lower []string, key string) (string, error) {
	index := indexOf(lower, key)
	if index < 0 || index+1 >= len(segments) {
		return "", fmt.Errorf("arm resource id missing %s segment", key)
	}
	value := strings.TrimSpace(segments[index+1])
	if value == "" {
		return "", fmt.Errorf("arm resource id has empty %s value", key)
	}
	return value, nil
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
