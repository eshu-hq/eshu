// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResourceGraphPage is one parsed Azure Resource Graph Resources API response
// page. It models the bounded provider response shape the collector reads:
// pagination via $skipToken, completeness via resultTruncated, and the resource
// rows themselves. It carries no provider credentials or response bodies beyond
// the control-plane row metadata.
type ResourceGraphPage struct {
	// TotalRecords is the provider-reported total matching the query.
	TotalRecords int64
	// Count is the number of rows in this page.
	Count int64
	// ResultTruncated reports whether the provider truncated the result set.
	ResultTruncated bool
	// SkipToken is the continuation token for the next page; empty means done.
	SkipToken string
	// Rows are the resource rows on this page.
	Rows []ResourceRow
}

// ResourceRow is one Azure Resource Graph resource row. Fields mirror the
// Resource Graph projection: identity, scope, location, kind, SKU, managedBy,
// identity presence, tags, and the raw properties object that becomes the
// redacted extension payload.
type ResourceRow struct {
	// ID is the raw ARM resource ID.
	ID string `json:"id"`
	// Name is the leaf resource name.
	Name string `json:"name"`
	// Type is the provider-reported lowercased resource type.
	Type string `json:"type"`
	// TenantID is the Azure tenant ID for the row.
	TenantID string `json:"tenantId"`
	// SubscriptionID is the owning subscription GUID.
	SubscriptionID string `json:"subscriptionId"`
	// ResourceGroup is the owning resource group.
	ResourceGroup string `json:"resourceGroup"`
	// Location is the Azure location.
	Location string `json:"location"`
	// Kind is the optional resource kind discriminator.
	Kind string `json:"kind"`
	// ManagedBy is the ARM ID of an owning resource when present.
	ManagedBy string `json:"managedBy"`
	// APIVersion is the provider API version when present.
	APIVersion string `json:"apiVersion"`
	// SKU is the SKU object (name, tier) when present.
	SKU map[string]any `json:"sku"`
	// Identity is the managed-identity object when present.
	Identity map[string]any `json:"identity"`
	// Tags carries the row tags.
	Tags map[string]string `json:"tags"`
	// Properties is the raw provider properties object, redacted before
	// emission.
	Properties map[string]any `json:"properties"`
	// Timestamp is the provider read/change time when present.
	Timestamp string `json:"timestamp"`
}

type resourceGraphRaw struct {
	TotalRecords    int64         `json:"totalRecords"`
	Count           int64         `json:"count"`
	ResultTruncated string        `json:"resultTruncated"`
	SkipToken       string        `json:"$skipToken"`
	Data            []ResourceRow `json:"data"`
}

// ParseResourceGraphPage parses one Resource Graph Resources API response page
// from raw JSON. It tolerates the provider's string-valued resultTruncated
// field ("true"/"false") and rejects malformed JSON.
func ParseResourceGraphPage(raw []byte) (ResourceGraphPage, error) {
	var parsed resourceGraphRaw
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ResourceGraphPage{}, fmt.Errorf("parse resource graph page: %w", err)
	}
	return ResourceGraphPage{
		TotalRecords:    parsed.TotalRecords,
		Count:           parsed.Count,
		ResultTruncated: strings.EqualFold(strings.TrimSpace(parsed.ResultTruncated), "true"),
		SkipToken:       strings.TrimSpace(parsed.SkipToken),
		Rows:            parsed.Data,
	}, nil
}

// HasIdentity reports whether the row exposes a managed identity with a type
// other than "None".
func (r ResourceRow) HasIdentity() bool {
	if len(r.Identity) == 0 {
		return false
	}
	identityType, _ := r.Identity["type"].(string)
	return strings.TrimSpace(identityType) != "" && !strings.EqualFold(identityType, "None")
}

// SKUClass returns a bounded SKU classifier, preferring tier over name. It
// never returns raw SKU capacity or pricing detail.
func (r ResourceRow) SKUClass() string {
	if tier, ok := r.SKU["tier"].(string); ok && strings.TrimSpace(tier) != "" {
		return strings.TrimSpace(tier)
	}
	if name, ok := r.SKU["name"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return ""
}

// ProviderTime parses the row timestamp into a UTC time. A blank or unparseable
// timestamp yields nil so the absence of a provider time stays explicit.
func (r ResourceRow) ProviderTime() *time.Time {
	value := strings.TrimSpace(r.Timestamp)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}
