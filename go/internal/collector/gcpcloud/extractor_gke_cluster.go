// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeGKECluster is the Cloud Asset Inventory asset type for a GKE
// cluster. Its edge targets reuse asset-type constants and helpers already
// declared elsewhere in this package: assetTypeComputeNetwork /
// assetTypeComputeSubnetwork and computeFullResourceNameFromSelfLink (Compute
// extractors).
const assetTypeGKECluster = "container.googleapis.com/Cluster"

// Bounded provider relationship types for GKE Cluster edges. Each is a stable
// string carried on a gcp_cloud_relationship fact; the reducer materializes an
// edge only when both endpoints resolve exactly. A node pool's own service
// account is carried per-pool as a fingerprinted-email attribute/anchor, not an
// edge, because an email is not an exactly resolvable CAI endpoint (mirrors the
// Dataproc Cluster extractor's cluster-service-account treatment).
const (
	relationshipTypeGKEClusterUsesNetwork    = "gke_cluster_uses_network"
	relationshipTypeGKEClusterUsesSubnetwork = "gke_cluster_uses_subnetwork"
)

// defaultServiceAccountSentinel is the GKE API's sentinel value for "use the
// project's default Compute Engine service account". It is not a resolvable
// service-account email, so it must never be fingerprinted or anchored.
const defaultServiceAccountSentinel = "default"

func init() {
	RegisterAssetExtractor(assetTypeGKECluster, extractGKECluster)
}

// gkeClusterData is the bounded view of a CAI container.googleapis.com/Cluster
// resource.data blob. Only redaction-safe control-plane metadata, posture
// flags, and resource references are decoded. Master authorized-network CIDR
// values and display names are intentionally not decoded (only a bounded count
// is kept); node-pool OAuth scope values are reduced to a count. No field here
// ever carries a public/private IP, an IAM policy, or a data-plane value.
type gkeClusterData struct {
	Location             string `json:"location"`
	Status               string `json:"status"`
	CurrentMasterVersion string `json:"currentMasterVersion"`
	CurrentNodeVersion   string `json:"currentNodeVersion"`
	Network              string `json:"network"`
	Subnetwork           string `json:"subnetwork"`
	CreateTime           string `json:"createTime"`
	ReleaseChannel       *struct {
		Channel string `json:"channel"`
	} `json:"releaseChannel"`
	PrivateClusterConfig *struct {
		EnablePrivateNodes    *bool `json:"enablePrivateNodes"`
		EnablePrivateEndpoint *bool `json:"enablePrivateEndpoint"`
	} `json:"privateClusterConfig"`
	MasterAuthorizedNetworksConfig *struct {
		Enabled    *bool             `json:"enabled"`
		CidrBlocks []json.RawMessage `json:"cidrBlocks"`
	} `json:"masterAuthorizedNetworksConfig"`
	WorkloadIdentityConfig *struct {
		WorkloadPool string `json:"workloadPool"`
	} `json:"workloadIdentityConfig"`
	AddonsConfig *struct {
		HTTPLoadBalancing *struct {
			Disabled *bool `json:"disabled"`
		} `json:"httpLoadBalancing"`
		HorizontalPodAutoscaling *struct {
			Disabled *bool `json:"disabled"`
		} `json:"horizontalPodAutoscaling"`
		NetworkPolicyConfig *struct {
			Disabled *bool `json:"disabled"`
		} `json:"networkPolicyConfig"`
	} `json:"addonsConfig"`
	NodePools []gkeNodePoolData `json:"nodePools"`
}

// gkeNodePoolData is the bounded view of one entry in a GKE cluster's
// nodePools array.
type gkeNodePoolData struct {
	Name   string `json:"name"`
	Config *struct {
		MachineType    string   `json:"machineType"`
		ServiceAccount string   `json:"serviceAccount"`
		OauthScopes    []string `json:"oauthScopes"`
	} `json:"config"`
	Autoscaling *struct {
		Enabled      *bool `json:"enabled"`
		MinNodeCount int   `json:"minNodeCount"`
		MaxNodeCount int   `json:"maxNodeCount"`
	} `json:"autoscaling"`
	InitialNodeCount int `json:"initialNodeCount"`
}

// extractGKECluster extracts bounded, redaction-safe typed depth for one GKE
// Cluster CAI asset. It returns the Terraform/drift/monitoring attribute set
// (location, status, master/node version, release channel, create time,
// private-cluster and master-authorized-networks posture, workload identity
// pool, addon posture, and a per-node-pool summary with a fingerprinted
// service-account email), cross-source correlation anchors (network,
// subnetwork, and node-pool service-account fingerprints), and the typed
// network/subnetwork edges. Master-authorized-network CIDR values and node-pool
// OAuth scope values are never decoded into the output; the GKE API's "default"
// service-account sentinel is never fingerprinted or anchored, since it does
// not identify a specific service account.
func extractGKECluster(ctx ExtractContext) (AttributeExtraction, error) {
	var data gkeClusterData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode gke cluster data: %w", err)
	}

	attrs := gkeClusterAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if net := gkeNetworkFullName(data.Network, ctx.ProjectID); net != "" {
		anchors = append(anchors, net)
		rels = append(rels, gkeClusterEdge(ctx, relationshipTypeGKEClusterUsesNetwork, net, assetTypeComputeNetwork))
	}
	if subnet := gkeSubnetworkFullName(data.Subnetwork, ctx.ProjectID, data.Location); subnet != "" {
		anchors = append(anchors, subnet)
		rels = append(rels, gkeClusterEdge(ctx, relationshipTypeGKEClusterUsesSubnetwork, subnet, assetTypeComputeSubnetwork))
	}
	if pools, poolAnchors := gkeNodePoolsAttribute(data.NodePools); len(pools) > 0 {
		attrs["node_pools"] = pools
		anchors = append(anchors, poolAnchors...)
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// gkeClusterAttributes assembles the bounded cluster-level attribute map.
// Empty, absent, or unset fields are omitted rather than written as zero
// values so a partial CAI page does not fabricate a posture (for example a
// false "disabled" addon flag that was simply not reported).
func gkeClusterAttributes(data gkeClusterData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Location); v != "" {
		attrs["location"] = v
	}
	if v := strings.TrimSpace(data.Status); v != "" {
		attrs["status"] = v
	}
	if v := strings.TrimSpace(data.CurrentMasterVersion); v != "" {
		attrs["current_master_version"] = v
	}
	if v := strings.TrimSpace(data.CurrentNodeVersion); v != "" {
		attrs["current_node_version"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["create_time"] = v
	}
	if data.ReleaseChannel != nil {
		if v := strings.TrimSpace(data.ReleaseChannel.Channel); v != "" {
			attrs["release_channel"] = v
		}
	}
	if pcc := data.PrivateClusterConfig; pcc != nil {
		if pcc.EnablePrivateNodes != nil {
			attrs["private_nodes_enabled"] = *pcc.EnablePrivateNodes
		}
		if pcc.EnablePrivateEndpoint != nil {
			attrs["private_endpoint_enabled"] = *pcc.EnablePrivateEndpoint
		}
	}
	if man := data.MasterAuthorizedNetworksConfig; man != nil {
		if man.Enabled != nil {
			attrs["master_authorized_networks_enabled"] = *man.Enabled
		}
		if n := len(man.CidrBlocks); n > 0 {
			attrs["master_authorized_networks_count"] = n
		}
	}
	if wic := data.WorkloadIdentityConfig; wic != nil {
		if v := strings.TrimSpace(wic.WorkloadPool); v != "" {
			attrs["workload_identity_pool"] = v
		}
	}
	if addons := data.AddonsConfig; addons != nil {
		if addons.HTTPLoadBalancing != nil && addons.HTTPLoadBalancing.Disabled != nil {
			attrs["http_load_balancing_disabled"] = *addons.HTTPLoadBalancing.Disabled
		}
		if addons.HorizontalPodAutoscaling != nil && addons.HorizontalPodAutoscaling.Disabled != nil {
			attrs["horizontal_pod_autoscaling_disabled"] = *addons.HorizontalPodAutoscaling.Disabled
		}
		if addons.NetworkPolicyConfig != nil && addons.NetworkPolicyConfig.Disabled != nil {
			attrs["network_policy_config_disabled"] = *addons.NetworkPolicyConfig.Disabled
		}
	}
	if n := len(data.NodePools); n > 0 {
		attrs["node_pool_count"] = n
	}
	return attrs
}

// gkeNodePoolsAttribute assembles the bounded per-node-pool summary list and
// the node-pool service-account fingerprint anchors. A node pool using the GKE
// "default" service-account sentinel contributes no fingerprint or anchor,
// since the sentinel does not identify a specific service account.
func gkeNodePoolsAttribute(pools []gkeNodePoolData) ([]map[string]any, []string) {
	if len(pools) == 0 {
		return nil, nil
	}
	summaries := make([]map[string]any, 0, len(pools))
	var anchors []string
	for _, pool := range pools {
		summary := map[string]any{}
		if v := strings.TrimSpace(pool.Name); v != "" {
			summary["name"] = v
		}
		if pool.Config != nil {
			if v := computeMachineTypeName(pool.Config.MachineType); v != "" {
				summary["machine_type"] = v
			} else if v := strings.TrimSpace(pool.Config.MachineType); v != "" {
				summary["machine_type"] = v
			}
			if sa := strings.TrimSpace(pool.Config.ServiceAccount); sa != "" && !strings.EqualFold(sa, defaultServiceAccountSentinel) {
				if fp := secretsiam.GCPServiceAccountEmailDigest(sa); fp != "" {
					summary["service_account_fingerprint"] = fp
					anchors = append(anchors, fp)
				}
			}
			if n := len(pool.Config.OauthScopes); n > 0 {
				summary["oauth_scope_count"] = n
			}
		}
		if pool.Autoscaling != nil {
			if pool.Autoscaling.Enabled != nil {
				summary["autoscaling_enabled"] = *pool.Autoscaling.Enabled
			}
			if pool.Autoscaling.MinNodeCount > 0 {
				summary["autoscaling_min_node_count"] = pool.Autoscaling.MinNodeCount
			}
			if pool.Autoscaling.MaxNodeCount > 0 {
				summary["autoscaling_max_node_count"] = pool.Autoscaling.MaxNodeCount
			}
		}
		if pool.InitialNodeCount > 0 {
			summary["initial_node_count"] = pool.InitialNodeCount
		}
		summaries = append(summaries, summary)
	}
	return summaries, anchors
}

// gkeNetworkFullName resolves a GKE cluster's network reference to its CAI
// full resource name. GKE reports the network as a bare short name (e.g.
// "default"), a project-qualified partial (projects/p/global/networks/n), or
// an already-normalized CAI full resource name. A bare name is promoted to the
// project-less global partial before resolution against the cluster's project,
// mirroring the Dataproc Cluster extractor's networkUri handling.
func gkeNetworkFullName(ref, projectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") {
		trimmed = "global/networks/" + trimmed
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// gkeSubnetworkFullName resolves a GKE cluster's subnetwork reference to its
// CAI full resource name. A bare subnetwork short name is regional, so it is
// promoted to the project-less regional partial using the cluster's own
// location; a bare name with no known location cannot be resolved and yields ""
// so no edge is fabricated.
func gkeSubnetworkFullName(ref, projectID, location string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") {
		region := strings.TrimSpace(location)
		if region == "" {
			return ""
		}
		trimmed = "regions/" + region + "/subnetworks/" + trimmed
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// gkeClusterEdge builds one typed provider relationship observation anchored
// on the cluster's CAI full resource name.
func gkeClusterEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
