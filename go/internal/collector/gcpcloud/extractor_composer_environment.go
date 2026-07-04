// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeComposerEnvironment is the Cloud Asset Inventory asset type for a
// Cloud Composer environment. Its edge targets reuse asset-type constants and
// helpers already declared elsewhere in this package: assetTypeGKECluster (GKE
// Cluster extractor), assetTypeComputeNetwork / assetTypeComputeSubnetwork and
// computeFullResourceNameFromSelfLink (Compute extractors), assetTypeStorageBucket,
// storageBucketResourceNamePrefixFmt, and gcsBucketFromURI (BigQuery Table
// extractor), and assetTypeKMSCryptoKey and the strict-domain
// cmekKeyFullResourceName (extractor_helpers.go).
const assetTypeComposerEnvironment = "composer.googleapis.com/Environment"

// Bounded provider relationship types for Composer Environment edges. Each is
// a stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. The
// environment's node-runtime service account is carried as a
// fingerprinted-email attribute/anchor, not an edge, because an email is not
// an exactly resolvable CAI endpoint (the same treatment as the Dataproc
// Cluster and GKE Cluster extractors' own service accounts).
const (
	relationshipTypeComposerEnvironmentUsesGKECluster    = "composer_environment_uses_gke_cluster"
	relationshipTypeComposerEnvironmentUsesNetwork       = "composer_environment_uses_network"
	relationshipTypeComposerEnvironmentUsesSubnetwork    = "composer_environment_uses_subnetwork"
	relationshipTypeComposerEnvironmentUsesDAGBucket     = "composer_environment_uses_dag_bucket"
	relationshipTypeComposerEnvironmentEncryptedByKMSKey = "composer_environment_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeComposerEnvironment, extractComposerEnvironment)
}

// composerEnvironmentData is the bounded view of a CAI
// composer.googleapis.com/Environment resource.data blob (the Composer v1
// Environment resource). Only redaction-safe control-plane metadata, posture
// flags, and resource references are decoded. Per-key Airflow configuration
// override values, environment variable values, maintenance-window
// recurrence, and private-cluster/authorized-network CIDR values are
// intentionally not decoded, since they can carry operator-supplied or
// network-locator values.
type composerEnvironmentData struct {
	State      string                  `json:"state"`
	CreateTime string                  `json:"createTime"`
	Config     *composerEnvironmentCfg `json:"config"`
	// StorageConfig.Bucket is the Composer 3+ DAG/data bucket name (no gs://
	// prefix); Composer 1/2 report the same bucket only via
	// config.dagGcsPrefix ("gs://{bucket}/dags"). Both are read defensively;
	// dagGcsPrefix takes precedence when both are present since it is the
	// long-standing field across all Composer generations.
	StorageConfig *struct {
		Bucket string `json:"bucket"`
	} `json:"storageConfig"`
}

// composerEnvironmentCfg is the bounded view of a Composer environment's
// config block.
type composerEnvironmentCfg struct {
	GKECluster      string `json:"gkeCluster"`
	DagGcsPrefix    string `json:"dagGcsPrefix"`
	EnvironmentSize string `json:"environmentSize"`
	ResilienceMode  string `json:"resilienceMode"`
	NodeConfig      *struct {
		Network        string `json:"network"`
		Subnetwork     string `json:"subnetwork"`
		ServiceAccount string `json:"serviceAccount"`
	} `json:"nodeConfig"`
	SoftwareConfig *struct {
		ImageVersion string `json:"imageVersion"`
	} `json:"softwareConfig"`
	EncryptionConfig *struct {
		KMSKeyName string `json:"kmsKeyName"`
	} `json:"encryptionConfig"`
	PrivateEnvironmentConfig *struct {
		EnablePrivateEnvironment *bool `json:"enablePrivateEnvironment"`
		PrivateClusterConfig     *struct {
			EnablePrivateEndpoint *bool `json:"enablePrivateEndpoint"`
		} `json:"privateClusterConfig"`
		NetworkingConfig *struct {
			ConnectionType string `json:"connectionType"`
		} `json:"networkingConfig"`
	} `json:"privateEnvironmentConfig"`
	// WorkloadsConfig is decoded only far enough to detect presence; its
	// per-component (scheduler/dagProcessor/triggerer/webServer/worker) CPU,
	// memory, storage, and count fields are operator-tuned resource sizing,
	// not a control-plane identity/posture value, so only a boolean presence
	// flag is kept.
	WorkloadsConfig json.RawMessage `json:"workloadsConfig"`
}

// extractComposerEnvironment extracts bounded, redaction-safe typed depth for
// one Cloud Composer Environment CAI asset. It returns the
// Terraform/drift/monitoring attribute set (lifecycle state, creation time,
// environment size, resilience mode, Airflow image version, CMEK posture,
// private-environment and private-endpoint posture, networking connection
// type, workloads-config presence, and the fingerprinted node-runtime
// service-account email) and the typed GKE cluster, network, subnetwork, DAG
// bucket, and CMEK edges.
func extractComposerEnvironment(ctx ExtractContext) (AttributeExtraction, error) {
	var data composerEnvironmentData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode composer environment data: %w", err)
	}

	attrs := composerEnvironmentAttributes(data)

	var anchors []string
	var rels []RelationshipObservation
	cfg := data.Config
	if cfg != nil {
		if cluster := composerGKEClusterFullName(cfg.GKECluster); cluster != "" {
			anchors = append(anchors, cluster)
			rels = append(rels, composerEnvironmentEdge(ctx, relationshipTypeComposerEnvironmentUsesGKECluster, cluster, assetTypeGKECluster))
		}
		if nc := cfg.NodeConfig; nc != nil {
			if net := composerNetworkFullName(nc.Network, ctx.ProjectID); net != "" {
				anchors = append(anchors, net)
				rels = append(rels, composerEnvironmentEdge(ctx, relationshipTypeComposerEnvironmentUsesNetwork, net, assetTypeComputeNetwork))
			}
			if subnet := composerSubnetworkFullName(nc.Subnetwork, ctx.ProjectID); subnet != "" {
				anchors = append(anchors, subnet)
				rels = append(rels, composerEnvironmentEdge(ctx, relationshipTypeComposerEnvironmentUsesSubnetwork, subnet, assetTypeComputeSubnetwork))
			}
			if fp := composerServiceAccountFingerprint(nc.ServiceAccount); fp != "" {
				anchors = append(anchors, fp)
			}
		}
		if enc := cfg.EncryptionConfig; enc != nil {
			if kms := cmekKeyFullResourceName(enc.KMSKeyName); kms != "" {
				anchors = append(anchors, kms)
				rels = append(rels, composerEnvironmentEdge(ctx, relationshipTypeComposerEnvironmentEncryptedByKMSKey, kms, assetTypeKMSCryptoKey))
			}
		}
	}
	if bucket := composerDAGBucketName(data); bucket != "" {
		name := storageBucketResourceNamePrefixFmt + bucket
		anchors = append(anchors, name)
		rels = append(rels, composerEnvironmentEdge(ctx, relationshipTypeComposerEnvironmentUsesDAGBucket, name, assetTypeStorageBucket))
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// composerEnvironmentAttributes assembles the bounded attribute map. Empty,
// absent, or unset fields are omitted rather than written as zero values so a
// partial CAI page does not fabricate a posture (for example a false
// "private environment" posture that was simply not reported).
func composerEnvironmentAttributes(data composerEnvironmentData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	cfg := data.Config
	if cfg == nil {
		return attrs
	}
	if v := strings.TrimSpace(cfg.EnvironmentSize); v != "" {
		attrs["environment_size"] = v
	}
	if v := strings.TrimSpace(cfg.ResilienceMode); v != "" {
		attrs["resilience_mode"] = v
	}
	if nc := cfg.NodeConfig; nc != nil {
		if fp := composerServiceAccountFingerprint(nc.ServiceAccount); fp != "" {
			attrs["service_account_fingerprint"] = fp
		}
	}
	if sc := cfg.SoftwareConfig; sc != nil {
		if v := strings.TrimSpace(sc.ImageVersion); v != "" {
			attrs["image_version"] = v
		}
	}
	if enc := cfg.EncryptionConfig; enc != nil && strings.TrimSpace(enc.KMSKeyName) != "" {
		attrs["customer_managed_encryption"] = true
	}
	if pec := cfg.PrivateEnvironmentConfig; pec != nil {
		if pec.EnablePrivateEnvironment != nil {
			attrs["private_environment_enabled"] = *pec.EnablePrivateEnvironment
		}
		if pcc := pec.PrivateClusterConfig; pcc != nil && pcc.EnablePrivateEndpoint != nil {
			attrs["private_endpoint_enabled"] = *pcc.EnablePrivateEndpoint
		}
		if nc := pec.NetworkingConfig; nc != nil {
			if v := strings.TrimSpace(nc.ConnectionType); v != "" {
				attrs["networking_connection_type"] = v
			}
		}
	}
	if len(cfg.WorkloadsConfig) > 0 && string(cfg.WorkloadsConfig) != "null" {
		attrs["workloads_config_present"] = true
	}
	return attrs
}

// composerServiceAccountFingerprint fingerprints a Composer node-runtime
// service-account email. The GKE/Composer API's "default" sentinel (use the
// project's default Compute Engine service account) is never fingerprinted or
// anchored, since it does not identify a specific service account.
func composerServiceAccountFingerprint(email string) string {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" || strings.EqualFold(trimmed, defaultServiceAccountSentinel) {
		return ""
	}
	return secretsiam.GCPServiceAccountEmailDigest(trimmed)
}

// gkeClusterResourceNamePrefix is the CAI full-resource-name prefix for the
// GKE Cluster asset type's own hostname (assetTypeGKECluster is
// "container.googleapis.com/Cluster" — the asset *type*, not the CAI resource
// hostname prefix used to build a full resource name).
const gkeClusterResourceNamePrefix = "//container.googleapis.com/"

// composerGKEClusterFullName resolves a Composer environment's config.gkeCluster
// reference to its CAI GKE Cluster full resource name. The Composer API always
// reports this as a relative resource name
// ("projects/{p}/locations/{l}/clusters/{c}"); an already-normalized CAI full
// resource name is returned unchanged, and a blank reference yields "" so no
// edge is fabricated.
func composerGKEClusterFullName(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return gkeClusterResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// composerNetworkFullName resolves a Composer nodeConfig.network reference to
// its CAI full resource name. Composer reports this as a relative resource
// name or a bare network short name (e.g. "default"); a bare name is promoted
// to the project-less global partial before resolution against the
// environment's project, mirroring the Dataproc Cluster and GKE Cluster
// extractors' own network handling.
func composerNetworkFullName(ref, projectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") {
		trimmed = "global/networks/" + trimmed
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// composerSubnetworkFullName resolves a Composer nodeConfig.subnetwork
// reference to its CAI full resource name. Composer always reports the
// subnetwork as a fully relative resource name
// ("projects/{p}/regions/{r}/subnetworks/{s}") when set, since — unlike GKE or
// Dataproc — the Composer API does not accept a bare subnetwork short name for
// this field. A blank reference yields "" so no edge is fabricated.
func composerSubnetworkFullName(ref, projectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// composerDAGBucketName resolves the environment's DAG/data Cloud Storage
// bucket name. config.dagGcsPrefix ("gs://{bucket}/dags", present across all
// Composer generations) takes precedence when present; storageConfig.bucket
// (no gs:// prefix, Composer 3+ only) is used as a fallback. A dagGcsPrefix
// value without the "gs://" scheme is rejected outright rather than
// mis-parsed into a bogus bucket name, mirroring the BigQuery Table
// extractor's gcsBucketFromURI scheme guard; it falls through to the
// storageConfig.bucket fallback exactly as if dagGcsPrefix were blank. It
// returns "" when neither field carries a usable bucket name.
func composerDAGBucketName(data composerEnvironmentData) string {
	if data.Config != nil {
		if bucket := gcsBucketFromURI(data.Config.DagGcsPrefix); bucket != "" {
			return bucket
		}
	}
	if data.StorageConfig != nil {
		if v := strings.TrimSpace(data.StorageConfig.Bucket); v != "" {
			return v
		}
	}
	return ""
}

// composerEnvironmentEdge builds one typed provider relationship observation
// anchored on the environment's CAI full resource name.
func composerEnvironmentEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
