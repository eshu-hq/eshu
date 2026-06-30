// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	assetTypeDNSResourceRecordSet = "dns.googleapis.com/ResourceRecordSet"
	assetTypeRunJob               = "run.googleapis.com/Job"
	assetTypeRunService           = "run.googleapis.com/Service"
)

// AssetsListPage is the parsed, redacted result of one Cloud Asset Inventory
// assets.list response page. Resources carry only safe control-plane metadata;
// the raw provider resource data blob (which can embed network IPs, startup
// scripts, secrets, and other data-plane content) is dropped at parse time.
type AssetsListPage struct {
	// ReadTime is the response read time.
	ReadTime time.Time
	// Resources holds the safe observations parsed from the page.
	Resources []ResourceObservation
	// NextPageToken is the continuation token for the next page, empty when the
	// shard is complete.
	NextPageToken string
}

// SearchAllResourcesPage is the parsed, redacted result of one Cloud Asset
// Inventory searchAllResources response page.
type SearchAllResourcesPage = AssetsListPage

type caiIAMPolicyWire struct {
	Etag     string              `json:"etag"`
	Bindings []caiIAMBindingWire `json:"bindings"`
}

type caiIAMBindingWire struct {
	Role      string          `json:"role"`
	Members   []string        `json:"members"`
	Condition json.RawMessage `json:"condition"`
}

type caiResourceDataWire struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Email       string            `json:"email"`
	Status      string            `json:"status"`
	State       string            `json:"state"`
	Labels      map[string]string `json:"labels"`
	RecordType  string            `json:"type"`
	TTLSeconds  int64             `json:"ttl"`
	RRDatas     []string          `json:"rrdatas"`
	Template    caiTemplateWire   `json:"template"`
}

type caiTemplateWire struct {
	Containers []caiContainerWire `json:"containers"`
	Template   struct {
		Containers []caiContainerWire `json:"containers"`
	} `json:"template"`
}

type caiContainerWire struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type caiRelatedAssetWire struct {
	Asset            string `json:"asset"`
	AssetType        string `json:"assetType"`
	RelationshipType string `json:"relationshipType"`
}

// ParseAssetsListPage parses one assets.list response page into safe resource
// observations. Only the full resource name, asset type, location, labels,
// IAM binding shape, relationship shape, DNS record shape, image-reference
// shape, ancestors, state, and update time are kept. The raw resource data blob
// and raw IAM policy JSON are never carried into the observation.
func ParseAssetsListPage(raw []byte) (AssetsListPage, error) {
	var wire struct {
		ReadTime      string `json:"readTime"`
		NextPageToken string `json:"nextPageToken"`
		Assets        []struct {
			Name       string              `json:"name"`
			AssetType  string              `json:"assetType"`
			UpdateTime string              `json:"updateTime"`
			Ancestors  []string            `json:"ancestors"`
			IAMPolicy  caiIAMPolicyWire    `json:"iamPolicy"`
			Related    caiRelatedAssetWire `json:"relatedAsset"`
			Resource   struct {
				Location string `json:"location"`
				// Data is captured raw so the registered per-asset-type extractor can
				// pull bounded typed depth from it. It is decoded into the bounded
				// caiResourceDataWire for the base observation and handed to the
				// extractor transiently; the raw blob is never persisted.
				Data json.RawMessage `json:"data"`
			} `json:"resource"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return AssetsListPage{}, fmt.Errorf("parse assets.list page: %w", err)
	}

	page := AssetsListPage{
		ReadTime:      parseTime(wire.ReadTime),
		NextPageToken: strings.TrimSpace(wire.NextPageToken),
		Resources:     make([]ResourceObservation, 0, len(wire.Assets)),
	}
	for _, asset := range wire.Assets {
		var data caiResourceDataWire
		if len(asset.Resource.Data) > 0 {
			if err := json.Unmarshal(asset.Resource.Data, &data); err != nil {
				return AssetsListPage{}, fmt.Errorf("parse assets.list resource data: %w", err)
			}
		}
		obs := ResourceObservation{
			Name:                strings.TrimSpace(asset.Name),
			AssetType:           strings.TrimSpace(asset.AssetType),
			DisplayName:         displayNameForAsset(asset.AssetType, data.DisplayName, data.Name),
			State:               firstNonEmpty(data.State, data.Status),
			Location:            strings.TrimSpace(asset.Resource.Location),
			Ancestors:           cloneStrings(asset.Ancestors),
			Labels:              cloneStringMap(data.Labels),
			IAMPolicyBindings:   parseIAMPolicyBindings(asset.IAMPolicy),
			Relationships:       parseRelatedAsset(asset.Name, asset.AssetType, asset.Related),
			DNSRecords:          parseDNSRecords(asset.Name, asset.AssetType, data),
			ImageReferences:     parseImageReferences(asset.Name, asset.AssetType, data),
			ServiceAccountEmail: serviceAccountEmail(asset.AssetType, data),
			UpdateTime:          parseTime(asset.UpdateTime),
		}
		if err := applyTypedDepth(&obs, asset.Resource.Data); err != nil {
			return AssetsListPage{}, err
		}
		page.Resources = append(page.Resources, obs)
	}
	return page, nil
}

// applyTypedDepth dispatches a resource's raw CAI data blob to the registered
// per-asset-type extractor and attaches the bounded attributes, correlation
// anchors, and typed relationships it produces. The raw blob is never persisted:
// only the extractor's redaction-safe output reaches the observation, and asset
// types with no registered extractor keep the bounded base observation
// unchanged. The extractor's relationships are appended to any provider
// relatedAsset relationships already parsed for the resource.
func applyTypedDepth(obs *ResourceObservation, data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	extraction, handled, err := extractAssetAttributes(ExtractContext{
		FullResourceName: obs.Name,
		AssetType:        obs.AssetType,
		ProjectID:        ProjectIDFromFullName(obs.Name),
		Data:             data,
	})
	if err != nil {
		return err
	}
	if !handled {
		return nil
	}
	obs.Attributes = extraction.Attributes
	obs.CorrelationAnchors = extraction.CorrelationAnchors
	obs.Relationships = append(obs.Relationships, extraction.Relationships...)
	return nil
}

// ParseSearchAllResourcesPage parses one searchAllResources response page into
// safe resource observations. As with ParseAssetsListPage, only bounded
// control-plane metadata is retained; the additionalAttributes blob is dropped
// because it can carry network ranges and other data-plane-adjacent content not
// in scope for the first slice.
func ParseSearchAllResourcesPage(raw []byte) (SearchAllResourcesPage, error) {
	var wire struct {
		ReadTime      string `json:"readTime"`
		NextPageToken string `json:"nextPageToken"`
		Results       []struct {
			Name         string            `json:"name"`
			AssetType    string            `json:"assetType"`
			DisplayName  string            `json:"displayName"`
			Location     string            `json:"location"`
			State        string            `json:"state"`
			UpdateTime   string            `json:"updateTime"`
			Labels       map[string]string `json:"labels"`
			Folders      []string          `json:"folders"`
			Organization string            `json:"organization"`
			Project      string            `json:"project"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return SearchAllResourcesPage{}, fmt.Errorf("parse searchAllResources page: %w", err)
	}

	page := SearchAllResourcesPage{
		ReadTime:      parseTime(wire.ReadTime),
		NextPageToken: strings.TrimSpace(wire.NextPageToken),
		Resources:     make([]ResourceObservation, 0, len(wire.Results)),
	}
	for _, result := range wire.Results {
		page.Resources = append(page.Resources, ResourceObservation{
			Name:        strings.TrimSpace(result.Name),
			AssetType:   strings.TrimSpace(result.AssetType),
			DisplayName: strings.TrimSpace(result.DisplayName),
			State:       strings.TrimSpace(result.State),
			Location:    strings.TrimSpace(result.Location),
			Ancestors:   searchAncestors(result.Project, result.Folders, result.Organization),
			Labels:      cloneStringMap(result.Labels),
			UpdateTime:  parseTime(result.UpdateTime),
		})
	}
	return page, nil
}

// searchAncestors assembles an ordered ancestor chain from the discrete project,
// folder, and organization fields a searchAllResources result carries, matching
// the most-specific-first ordering assets.list uses in its ancestors array.
func searchAncestors(project string, folders []string, organization string) []string {
	ancestors := make([]string, 0, len(folders)+2)
	if proj := strings.TrimSpace(project); proj != "" {
		ancestors = append(ancestors, proj)
	}
	ancestors = append(ancestors, cloneStrings(folders)...)
	if org := strings.TrimSpace(organization); org != "" {
		ancestors = append(ancestors, org)
	}
	if len(ancestors) == 0 {
		return nil
	}
	return ancestors
}

func parseTime(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func displayNameForAsset(assetType, displayName, dataName string) string {
	if strings.TrimSpace(assetType) == assetTypeDNSResourceRecordSet {
		return ""
	}
	return firstNonEmpty(displayName, dataName)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func parseIAMPolicyBindings(policy caiIAMPolicyWire) []IAMPolicyBindingObservation {
	if len(policy.Bindings) == 0 {
		return nil
	}
	out := make([]IAMPolicyBindingObservation, 0, len(policy.Bindings))
	for _, binding := range policy.Bindings {
		out = append(out, IAMPolicyBindingObservation{
			Role:                      strings.TrimSpace(binding.Role),
			Members:                   cloneStrings(binding.Members),
			ConditionPresent:          rawJSONPresent(binding.Condition),
			ConditionFingerprintInput: compactRawJSON(binding.Condition),
			Etag:                      strings.TrimSpace(policy.Etag),
		})
	}
	return out
}

func serviceAccountEmail(assetType string, data caiResourceDataWire) string {
	if strings.TrimSpace(assetType) != serviceAccountAssetType {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(data.Email))
}

func parseRelatedAsset(assetName, assetType string, related caiRelatedAssetWire) []RelationshipObservation {
	sourceName := strings.TrimSpace(assetName)
	relationshipType := strings.TrimSpace(related.RelationshipType)
	targetName := strings.TrimSpace(related.Asset)
	if sourceName == "" || relationshipType == "" || targetName == "" {
		return nil
	}
	return []RelationshipObservation{{
		SourceFullResourceName: sourceName,
		SourceAssetType:        strings.TrimSpace(assetType),
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        strings.TrimSpace(related.AssetType),
		SupportState:           RelationshipSupportSupported,
	}}
}

func rawJSONPresent(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func compactRawJSON(raw json.RawMessage) string {
	if !rawJSONPresent(raw) {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(encoded)
}

func parseDNSRecords(assetName, assetType string, data caiResourceDataWire) []DNSRecordObservation {
	if strings.TrimSpace(assetType) != assetTypeDNSResourceRecordSet {
		return nil
	}
	record := DNSRecordObservation{
		ManagedZoneFullResourceName: dnsManagedZoneName(assetName),
		RecordType:                  strings.TrimSpace(data.RecordType),
		RecordName:                  strings.TrimSpace(data.Name),
		Targets:                     cloneStrings(data.RRDatas),
		TTLSeconds:                  data.TTLSeconds,
	}
	if !hasUsableDNSRecordObservation(record) {
		return nil
	}
	return []DNSRecordObservation{record}
}

func parseImageReferences(assetName, assetType string, data caiResourceDataWire) []ImageReferenceObservation {
	if !isCloudRunRuntimeAsset(assetType) {
		return nil
	}
	containers := make([]caiContainerWire, 0, len(data.Template.Containers)+len(data.Template.Template.Containers))
	containers = append(containers, data.Template.Containers...)
	containers = append(containers, data.Template.Template.Containers...)
	if len(containers) == 0 {
		return nil
	}
	owner := strings.TrimSpace(assetName)
	out := make([]ImageReferenceObservation, 0, len(containers))
	for _, container := range containers {
		image := strings.TrimSpace(container.Image)
		if image == "" {
			continue
		}
		out = append(out, ImageReferenceObservation{
			OwningFullResourceName: owner,
			ImageReference:         image,
			ImageDigest:            imageDigestFromReference(image),
			ContainerName:          strings.TrimSpace(container.Name),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isCloudRunRuntimeAsset(assetType string) bool {
	switch strings.TrimSpace(assetType) {
	case assetTypeRunService, assetTypeRunJob:
		return true
	default:
		return false
	}
}

func imageDigestFromReference(imageReference string) string {
	_, digest, ok := strings.Cut(strings.TrimSpace(imageReference), "@")
	if !ok {
		return ""
	}
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return ""
}

func dnsManagedZoneName(assetName string) string {
	assetName = strings.TrimSpace(assetName)
	const rrsetsSegment = "/rrsets/"
	idx := strings.Index(assetName, rrsetsSegment)
	if idx <= 0 {
		return ""
	}
	return assetName[:idx]
}
