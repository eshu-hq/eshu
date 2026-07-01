// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeDataprocCluster is the CAI asset type for a Dataproc cluster. Its edge
// targets reuse asset-type constants and helpers already declared elsewhere in
// this package: assetTypeComputeNetwork / assetTypeComputeSubnetwork and
// computeFullResourceNameFromSelfLink (Compute extractors), assetTypeStorageBucket
// and storageBucketResourceNamePrefixFmt (BigQuery Table extractor), and
// assetTypeKMSCryptoKey / cloudKMSResourceNamePrefix (BigQuery Table extractor).
const assetTypeDataprocCluster = "dataproc.googleapis.com/Cluster"

// Bounded provider relationship types for Dataproc Cluster edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. The cluster's
// service account is carried as a fingerprinted-email anchor, not an edge,
// because an email is not an exactly resolvable CAI endpoint.
const (
	relationshipTypeClusterUsesNetwork       = "dataproc_cluster_uses_network"
	relationshipTypeClusterUsesSubnetwork    = "dataproc_cluster_uses_subnetwork"
	relationshipTypeClusterEncryptedByKMSKey = "dataproc_cluster_encrypted_by_kms_key"
	relationshipTypeClusterUsesStagingBucket = "dataproc_cluster_uses_staging_bucket"
)

func init() {
	RegisterAssetExtractor(assetTypeDataprocCluster, extractDataprocCluster)
}

// dataprocClusterData is the bounded view of a CAI dataproc.googleapis.com/Cluster
// resource.data blob. Only redaction-safe control-plane metadata and resource
// references are decoded. Software properties, initialization actions, and Kerberos
// or metastore config are intentionally not decoded because they can carry
// operator-supplied values.
type dataprocClusterData struct {
	Status *struct {
		State string `json:"state"`
	} `json:"status"`
	Config *dataprocClusterConfig `json:"config"`
}

// dataprocClusterConfig is the bounded view of a Dataproc cluster's config block.
type dataprocClusterConfig struct {
	ConfigBucket     string                    `json:"configBucket"`
	GceClusterConfig *dataprocGceClusterConfig `json:"gceClusterConfig"`
	MasterConfig     *dataprocInstanceGroup    `json:"masterConfig"`
	WorkerConfig     *dataprocInstanceGroup    `json:"workerConfig"`
	SoftwareConfig   *struct {
		ImageVersion string `json:"imageVersion"`
	} `json:"softwareConfig"`
	EncryptionConfig *struct {
		GcePdKMSKeyName string `json:"gcePdKmsKeyName"`
	} `json:"encryptionConfig"`
	AutoscalingConfig *struct {
		PolicyURI string `json:"policyUri"`
	} `json:"autoscalingConfig"`
}

// dataprocGceClusterConfig is the bounded view of the shared GCE cluster config.
type dataprocGceClusterConfig struct {
	NetworkURI     string `json:"networkUri"`
	SubnetworkURI  string `json:"subnetworkUri"`
	ServiceAccount string `json:"serviceAccount"`
	InternalIPOnly *bool  `json:"internalIpOnly"`
}

// dataprocInstanceGroup is the bounded view of a master or worker instance group.
type dataprocInstanceGroup struct {
	NumInstances   int    `json:"numInstances"`
	MachineTypeURI string `json:"machineTypeUri"`
}

// extractDataprocCluster extracts bounded, redaction-safe typed depth for one
// Dataproc Cluster CAI asset. It returns the Terraform/drift/monitoring attribute
// set (lifecycle state, internal-IP posture, master/worker machine type and
// instance counts, image version, CMEK and autoscaling posture, and the
// fingerprinted runtime service-account email) and the typed network,
// subnetwork, CMEK, and staging-bucket edges.
func extractDataprocCluster(ctx ExtractContext) (AttributeExtraction, error) {
	var data dataprocClusterData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode dataproc cluster data: %w", err)
	}

	attrs := dataprocClusterAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	if cfg := data.Config; cfg != nil {
		if gce := cfg.GceClusterConfig; gce != nil {
			if net := computeFullResourceNameFromSelfLink(gce.NetworkURI, ctx.ProjectID); net != "" {
				anchors = append(anchors, net)
				rels = append(rels, dataprocClusterEdge(ctx, relationshipTypeClusterUsesNetwork, net, assetTypeComputeNetwork))
			}
			if subnet := computeFullResourceNameFromSelfLink(gce.SubnetworkURI, ctx.ProjectID); subnet != "" {
				anchors = append(anchors, subnet)
				rels = append(rels, dataprocClusterEdge(ctx, relationshipTypeClusterUsesSubnetwork, subnet, assetTypeComputeSubnetwork))
			}
			if fp := secretsiam.GCPServiceAccountEmailDigest(gce.ServiceAccount); fp != "" {
				anchors = append(anchors, fp)
			}
		}
		if enc := cfg.EncryptionConfig; enc != nil {
			if kms := dataprocKMSKeyFullName(enc.GcePdKMSKeyName); kms != "" {
				anchors = append(anchors, kms)
				rels = append(rels, dataprocClusterEdge(ctx, relationshipTypeClusterEncryptedByKMSKey, kms, assetTypeKMSCryptoKey))
			}
		}
		if bucket := strings.TrimSpace(cfg.ConfigBucket); bucket != "" {
			name := storageBucketResourceNamePrefixFmt + bucket
			anchors = append(anchors, name)
			rels = append(rels, dataprocClusterEdge(ctx, relationshipTypeClusterUsesStagingBucket, name, assetTypeStorageBucket))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// dataprocClusterAttributes assembles the bounded attribute map. Empty, absent,
// or default-valued fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture.
func dataprocClusterAttributes(data dataprocClusterData) map[string]any {
	attrs := map[string]any{}

	if data.Status != nil {
		if v := strings.TrimSpace(data.Status.State); v != "" {
			attrs["status_state"] = v
		}
	}
	cfg := data.Config
	if cfg == nil {
		return attrs
	}
	if gce := cfg.GceClusterConfig; gce != nil {
		if gce.InternalIPOnly != nil {
			attrs["internal_ip_only"] = *gce.InternalIPOnly
		}
		if fp := secretsiam.GCPServiceAccountEmailDigest(gce.ServiceAccount); fp != "" {
			attrs["service_account_fingerprint"] = fp
		}
	}
	if m := cfg.MasterConfig; m != nil {
		if v := computeMachineTypeName(m.MachineTypeURI); v != "" {
			attrs["master_machine_type"] = v
		}
		if m.NumInstances > 0 {
			attrs["master_num_instances"] = m.NumInstances
		}
	}
	if w := cfg.WorkerConfig; w != nil {
		if v := computeMachineTypeName(w.MachineTypeURI); v != "" {
			attrs["worker_machine_type"] = v
		}
		if w.NumInstances > 0 {
			attrs["worker_num_instances"] = w.NumInstances
		}
	}
	if s := cfg.SoftwareConfig; s != nil {
		if v := strings.TrimSpace(s.ImageVersion); v != "" {
			attrs["image_version"] = v
		}
	}
	if enc := cfg.EncryptionConfig; enc != nil && strings.TrimSpace(enc.GcePdKMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if a := cfg.AutoscalingConfig; a != nil && strings.TrimSpace(a.PolicyURI) != "" {
		attrs["autoscaling_enabled"] = true
	}
	return attrs
}

// dataprocKMSKeyFullName builds the CAI CryptoKey full resource name from a
// relative KMS key name. An already-normalized CAI full resource name is
// returned unchanged. It returns "" for a blank reference.
func dataprocKMSKeyFullName(kmsKeyName string) string {
	trimmed := strings.TrimSpace(kmsKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return cloudKMSResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// dataprocClusterEdge builds one typed provider relationship observation anchored
// on the cluster's CAI full resource name.
func dataprocClusterEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
