// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// Bounded provider relationship types for the compute Engine Instance edges
// carried on gcp_cloud_relationship facts. The reducer materializes each edge
// only when both endpoints resolve exactly. Only edges whose target is an exactly
// resolvable CAI resource are emitted: attached disks, and the interface network
// and subnetwork. The instance's service account is surfaced as a fingerprinted
// correlation anchor rather than an edge, because a service account email is not
// an exactly resolvable CAI resource endpoint; the secrets/IAM trust and
// image-identity layers own the "runs as" join through the same email digest.
// The asset type constants (assetTypeComputeInstance, assetTypeComputeDisk) and
// the compute name-derivation helpers are shared with the sibling compute
// extractors in this package.
const (
	relationshipTypeInstanceUsesDisk     = "instance_uses_disk"
	relationshipTypeInstanceInNetwork    = "instance_in_network"
	relationshipTypeInstanceInSubnetwork = "instance_in_subnetwork"
)

func init() {
	RegisterAssetExtractor(assetTypeComputeInstance, extractInstance)
}

// instanceData is the bounded view of a CAI compute.googleapis.com/Instance
// resource.data blob. Only redaction-safe control-plane metadata and resource
// references are decoded. The external-IP access configs (accessConfigs[].natIP
// for IPv4 and ipv6AccessConfigs for IPv6) are decoded only to derive a
// non-address exposure signal and count; their raw address values never leave
// this extractor, and the private networkIP is never decoded at all. Metadata
// item values (which may hold startup scripts, SSH keys, or env values) and the
// raw service-account email are never persisted — the metadata keys and a
// fingerprinted email are kept instead, per the GCP collector contract Payload
// Boundaries.
type instanceData struct {
	MachineType        string `json:"machineType"`
	Status             string `json:"status"`
	Zone               string `json:"zone"`
	CanIPForward       *bool  `json:"canIpForward"`
	DeletionProtection *bool  `json:"deletionProtection"`
	CreationTimestamp  string `json:"creationTimestamp"`
	Scheduling         *struct {
		Preemptible       *bool  `json:"preemptible"`
		AutomaticRestart  *bool  `json:"automaticRestart"`
		OnHostMaintenance string `json:"onHostMaintenance"`
		ProvisioningModel string `json:"provisioningModel"`
	} `json:"scheduling"`
	ShieldedInstanceConfig *struct {
		EnableSecureBoot          *bool `json:"enableSecureBoot"`
		EnableVtpm                *bool `json:"enableVtpm"`
		EnableIntegrityMonitoring *bool `json:"enableIntegrityMonitoring"`
	} `json:"shieldedInstanceConfig"`
	ServiceAccounts []struct {
		Email  string   `json:"email"`
		Scopes []string `json:"scopes"`
	} `json:"serviceAccounts"`
	NetworkInterfaces []struct {
		Network       string `json:"network"`
		Subnetwork    string `json:"subnetwork"`
		AccessConfigs []struct {
			Type string `json:"type"`
			// NatIP is decoded only to detect an external-IP allocation; its value
			// is never persisted.
			NatIP string `json:"natIP"`
		} `json:"accessConfigs"`
		// IPv6AccessConfigs signal an external IPv6 allocation (type DIRECT_IPV6);
		// the address itself is never decoded, only the presence is counted.
		IPv6AccessConfigs []struct {
			Type string `json:"type"`
		} `json:"ipv6AccessConfigs"`
	} `json:"networkInterfaces"`
	Disks []struct {
		Source string `json:"source"`
		Boot   bool   `json:"boot"`
	} `json:"disks"`
	Metadata *struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	} `json:"metadata"`
	Tags *struct {
		Items []string `json:"items"`
	} `json:"tags"`
}

// extractInstance extracts bounded, redaction-safe typed depth for one compute
// Engine Instance CAI asset. It returns the Terraform/drift/monitoring attribute
// set (machine type, status, zone, scheduling and shielded posture, external-IP
// exposure signal, disk/interface counts, network tags, and metadata keys); the
// attached disks, interface network and subnetwork, and the fingerprinted service
// account email as cross-source correlation anchors; and the typed disk,
// network, and subnetwork edges. No IP address, metadata value, or raw service
// account email reaches the output.
func extractInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data instanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode instance data: %w", err)
	}

	attrs := instanceAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	for _, disk := range data.Disks {
		if diskName := computeFullResourceNameFromSelfLink(disk.Source, ctx.ProjectID); diskName != "" {
			anchors = append(anchors, diskName)
			rels = append(rels, instanceEdge(ctx, relationshipTypeInstanceUsesDisk, diskName, assetTypeComputeDisk))
		}
	}
	for _, ni := range data.NetworkInterfaces {
		if networkName := computeFullResourceNameFromSelfLink(ni.Network, ctx.ProjectID); networkName != "" {
			anchors = append(anchors, networkName)
			rels = append(rels, instanceEdge(ctx, relationshipTypeInstanceInNetwork, networkName, assetTypeComputeNetwork))
		}
		if subnetName := computeFullResourceNameFromSelfLink(ni.Subnetwork, ctx.ProjectID); subnetName != "" {
			anchors = append(anchors, subnetName)
			rels = append(rels, instanceEdge(ctx, relationshipTypeInstanceInSubnetwork, subnetName, assetTypeComputeSubnetwork))
		}
	}
	for _, sa := range data.ServiceAccounts {
		if digest := secretsiam.GCPServiceAccountEmailDigest(sa.Email); digest != "" {
			anchors = append(anchors, digest)
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// instanceAttributes assembles the bounded attribute map. Empty or absent fields
// are omitted rather than written as zero values so a partial CAI page does not
// fabricate a posture (for example an empty status or a false shielded-boot flag
// that was simply not reported). Counts are omitted when their list is empty.
func instanceAttributes(data instanceData) map[string]any {
	attrs := map[string]any{}
	if v := computeMachineTypeName(data.MachineType); v != "" {
		attrs["machine_type"] = v
	}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if v := computeZoneName(data.Zone); v != "" {
		attrs["zone"] = v
	}
	if data.CanIPForward != nil {
		attrs["can_ip_forward"] = *data.CanIPForward
	}
	if data.DeletionProtection != nil {
		attrs["deletion_protection"] = *data.DeletionProtection
	}
	if v, ok := normalizeRFC3339(data.CreationTimestamp); ok {
		attrs["creation_time"] = v
	}
	if s := data.Scheduling; s != nil {
		if s.Preemptible != nil {
			attrs["preemptible"] = *s.Preemptible
		}
		if s.AutomaticRestart != nil {
			attrs["automatic_restart"] = *s.AutomaticRestart
		}
		if v := strings.TrimSpace(s.OnHostMaintenance); v != "" {
			attrs["on_host_maintenance"] = v
		}
		if v := strings.TrimSpace(s.ProvisioningModel); v != "" {
			attrs["provisioning_model"] = v
		}
	}
	if c := data.ShieldedInstanceConfig; c != nil {
		if c.EnableSecureBoot != nil {
			attrs["enable_secure_boot"] = *c.EnableSecureBoot
		}
		if c.EnableVtpm != nil {
			attrs["enable_vtpm"] = *c.EnableVtpm
		}
		if c.EnableIntegrityMonitoring != nil {
			attrs["enable_integrity_monitoring"] = *c.EnableIntegrityMonitoring
		}
	}
	if n := len(data.ServiceAccounts); n > 0 {
		attrs["service_account_count"] = n
		if scopes := serviceAccountScopes(data); len(scopes) > 0 {
			attrs["service_account_scopes"] = scopes
		}
	}
	if n := len(data.NetworkInterfaces); n > 0 {
		attrs["network_interface_count"] = n
		externalConfigs := externalAccessConfigCount(data)
		attrs["has_external_ip"] = externalConfigs > 0
		if externalConfigs > 0 {
			attrs["external_access_config_count"] = externalConfigs
		}
	}
	if n := len(data.Disks); n > 0 {
		attrs["disk_count"] = n
		attrs["boot_disk_present"] = bootDiskPresent(data)
	}
	if keys := metadataKeys(data); len(keys) > 0 {
		attrs["metadata_keys"] = keys
	}
	if data.Tags != nil {
		if tags := dedupeNonEmpty(data.Tags.Items); len(tags) > 0 {
			attrs["network_tags"] = tags
		}
	}
	return attrs
}

// serviceAccountScopes returns the deduplicated OAuth scope URLs across every
// attached service account. Scopes are well-known Google control-plane URLs (not
// identities), so they are safe to keep and useful for Terraform drift.
func serviceAccountScopes(data instanceData) []string {
	var scopes []string
	for _, sa := range data.ServiceAccounts {
		scopes = append(scopes, sa.Scopes...)
	}
	return dedupeNonEmpty(scopes)
}

// externalAccessConfigCount counts the access configs that allocate an external
// IP across all interfaces, conveying external exposure without persisting any
// address. An IPv4 ONE_TO_ONE_NAT type or a present natIP signals an external
// IPv4, and any ipv6AccessConfig (type DIRECT_IPV6) signals an external IPv6, so
// an IPv6-only externally reachable instance is not a false negative.
func externalAccessConfigCount(data instanceData) int {
	count := 0
	for _, ni := range data.NetworkInterfaces {
		for _, ac := range ni.AccessConfigs {
			if strings.EqualFold(strings.TrimSpace(ac.Type), "ONE_TO_ONE_NAT") || strings.TrimSpace(ac.NatIP) != "" {
				count++
			}
		}
		count += len(ni.IPv6AccessConfigs)
	}
	return count
}

// bootDiskPresent reports whether any attached disk is the boot disk.
func bootDiskPresent(data instanceData) bool {
	for _, disk := range data.Disks {
		if disk.Boot {
			return true
		}
	}
	return false
}

// metadataKeys returns the deduplicated instance metadata key names. Metadata
// values (which may hold startup scripts, SSH keys, or env values) are never
// decoded, so only the keys — control-plane labels, not values — are kept.
func metadataKeys(data instanceData) []string {
	if data.Metadata == nil || len(data.Metadata.Items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(data.Metadata.Items))
	for _, item := range data.Metadata.Items {
		keys = append(keys, item.Key)
	}
	return dedupeNonEmpty(keys)
}

// computeMachineTypeName extracts the bare machine-type name (for example
// e2-standard-4) from a machineType reference, which CAI may report as a compute
// self-link, a partial path, or the bare name itself. A path that does not name a
// machineTypes segment yields "" so an ambiguous reference is dropped.
func computeMachineTypeName(machineTypeRef string) string {
	trimmed := strings.TrimSpace(machineTypeRef)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/machineTypes/"); idx >= 0 {
		return strings.TrimSpace(trimmed[idx+len("/machineTypes/"):])
	}
	if strings.Contains(trimmed, "/") {
		return ""
	}
	return trimmed
}

// instanceEdge builds a supported typed relationship observation rooted at the
// instance.
func instanceEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
