// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeDataflowJob is the Cloud Asset Inventory asset type for a Dataflow
// Job. Its edge targets reuse asset-type constants and helpers already
// declared elsewhere in this package: assetTypeComputeNetwork /
// assetTypeComputeSubnetwork and computeFullResourceNameFromSelfLink (Compute
// Network extractor), assetTypeStorageBucket and
// storageBucketResourceNamePrefixFmt / gcsBucketFromURI (BigQuery Table
// extractor), and assetTypeKMSCryptoKey / cloudKMSResourceNamePrefix (BigQuery
// Table extractor) for the CMEK edge.
const assetTypeDataflowJob = "dataflow.googleapis.com/Job"

// Bounded provider relationship types for Dataflow Job edges. Each is a
// stable string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly. The job's
// runtime service account is carried as a fingerprinted-email attribute/
// anchor, not an edge, because an email is not an exactly resolvable CAI
// endpoint (mirrors the Dataproc Cluster and GKE Cluster extractors' own
// service-account treatment).
const (
	relationshipTypeDataflowJobUsesNetwork       = "dataflow_job_uses_network"
	relationshipTypeDataflowJobUsesSubnetwork    = "dataflow_job_uses_subnetwork"
	relationshipTypeDataflowJobUsesStagingBucket = "dataflow_job_uses_staging_bucket"
	relationshipTypeDataflowJobEncryptedByKMSKey = "dataflow_job_encrypted_by_kms_key"
)

func init() {
	RegisterAssetExtractor(assetTypeDataflowJob, extractDataflowJob)
}

// dataflowJobData is the bounded view of a CAI dataflow.googleapis.com/Job
// resource.data blob. Only control-plane metadata, posture flags, and
// resource references are decoded. Pipeline parameter values (`environment.
// userAgent`, `environment.sdkPipelineOptions` option values, and any step
// graph content) are never decoded, since they can carry operator-supplied
// values, secrets, or data-plane locators unrelated to resource identity.
// Only the network/subnetwork/service-account/staging-location references
// needed for edges and the bounded posture fields below cross the redaction
// boundary.
type dataflowJobData struct {
	Type         string                  `json:"type"`
	CurrentState string                  `json:"currentState"`
	Location     string                  `json:"location"`
	CreateTime   string                  `json:"createTime"`
	StartTime    string                  `json:"startTime"`
	JobMetadata  *dataflowJobMetadata    `json:"jobMetadata"`
	Environment  *dataflowJobEnvironment `json:"environment"`
}

// dataflowJobMetadata is the bounded view of a Dataflow Job's jobMetadata
// block, reporting the SDK used to submit the pipeline. sdkSupportStatus is a
// bounded lifecycle enum (UNKNOWN/SUPPORTED/STALE/DEPRECATED/UNSUPPORTED) an
// operator can alert on; the output-only known-bug list is not decoded.
type dataflowJobMetadata struct {
	SdkVersion *struct {
		Version          string `json:"version"`
		SdkSupportStatus string `json:"sdkSupportStatus"`
	} `json:"sdkVersion"`
}

// dataflowJobEnvironment is the bounded view of a Dataflow Job's environment
// block. workerPools carries the network/subnetwork/zone the job's workers
// run in; tempStoragePrefix is the gs:// staging location for pipeline
// artifacts. serviceAccountEmail is the runtime worker service account.
// serviceKmsKeyName is the optional CMEK key protecting the job's state.
type dataflowJobEnvironment struct {
	ServiceAccountEmail string                  `json:"serviceAccountEmail"`
	ServiceKmsKeyName   string                  `json:"serviceKmsKeyName"`
	TempStoragePrefix   string                  `json:"tempStoragePrefix"`
	WorkerPools         []dataflowJobWorkerPool `json:"workerPools"`
}

// dataflowJobWorkerPool is the bounded view of one entry in a Dataflow Job's
// environment.workerPools array. Only the network/subnetwork/zone resource
// references are decoded; machine type, disk, autoscaling, and IP-allocation
// tuning fields are dropped by omission.
type dataflowJobWorkerPool struct {
	Network    string `json:"network"`
	Subnetwork string `json:"subnetwork"`
	Zone       string `json:"zone"`
}

// extractDataflowJob extracts bounded, redaction-safe typed depth for one
// Dataflow Job CAI asset. It returns the Terraform/drift/monitoring
// attribute set (job type BATCH/STREAMING, current state, location, create/
// start time, SDK version and support status, the fingerprinted runtime
// service-account email, and the CMEK key name), cross-source correlation
// anchors (network, subnetwork, staging bucket, CMEK key, and service-account
// fingerprint), and the typed network, subnetwork, staging-bucket, and CMEK
// encryption edges. The first worker pool that reports a network or subnetwork
// reference is used to resolve the network edges, since Dataflow jobs configure
// one effective network/subnetwork across all worker pools in practice; no
// pipeline parameter value, user agent, or SDK option value ever reaches the
// output.
func extractDataflowJob(ctx ExtractContext) (AttributeExtraction, error) {
	var data dataflowJobData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode dataflow job data: %w", err)
	}

	attrs := dataflowJobAttributes(data)

	var anchors []string
	var rels []RelationshipObservation

	if env := data.Environment; env != nil {
		if fp := secretsiam.GCPServiceAccountEmailDigest(strings.TrimSpace(env.ServiceAccountEmail)); fp != "" {
			attrs["service_account_fingerprint"] = fp
			anchors = append(anchors, fp)
		}

		if kms := cmekKeyFullResourceName(env.ServiceKmsKeyName); kms != "" {
			attrs["service_kms_key_name"] = strings.TrimPrefix(kms, cloudKMSResourceNamePrefix)
			anchors = append(anchors, kms)
			rels = append(rels, dataflowJobEdge(ctx, relationshipTypeDataflowJobEncryptedByKMSKey, kms, assetTypeKMSCryptoKey))
		}

		if net, subnet := dataflowJobWorkerPoolNetwork(env.WorkerPools, ctx.ProjectID); net != "" || subnet != "" {
			if net != "" {
				anchors = append(anchors, net)
				rels = append(rels, dataflowJobEdge(ctx, relationshipTypeDataflowJobUsesNetwork, net, assetTypeComputeNetwork))
			}
			if subnet != "" {
				anchors = append(anchors, subnet)
				rels = append(rels, dataflowJobEdge(ctx, relationshipTypeDataflowJobUsesSubnetwork, subnet, assetTypeComputeSubnetwork))
			}
		}

		if bucket := dataflowStagingBucket(env.TempStoragePrefix); bucket != "" {
			name := storageBucketResourceNamePrefixFmt + bucket
			anchors = append(anchors, name)
			rels = append(rels, dataflowJobEdge(ctx, relationshipTypeDataflowJobUsesStagingBucket, name, assetTypeStorageBucket))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// dataflowJobAttributes assembles the bounded attribute map. Empty or absent
// fields are omitted rather than written as zero values so a partial CAI page
// does not fabricate a job type or state that was simply not reported.
func dataflowJobAttributes(data dataflowJobData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["job_type"] = v
	}
	if v := strings.TrimSpace(data.CurrentState); v != "" {
		attrs["current_state"] = v
	}
	if v := strings.TrimSpace(data.Location); v != "" {
		attrs["location"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := normalizeRFC3339(data.StartTime); ok {
		attrs["start_time"] = v
	}
	if data.JobMetadata != nil && data.JobMetadata.SdkVersion != nil {
		if v := strings.TrimSpace(data.JobMetadata.SdkVersion.Version); v != "" {
			attrs["sdk_version"] = v
		}
		if v := strings.TrimSpace(data.JobMetadata.SdkVersion.SdkSupportStatus); v != "" {
			attrs["sdk_support_status"] = v
		}
	}
	return attrs
}

// dataflowJobWorkerPoolNetwork returns the resolved network and subnetwork
// full resource names from the first worker pool that reports a network or
// subnetwork reference. Both endpoints are resolved from that same single
// pool, never cross-latched across pools: a network from one pool and a
// subnetwork from a different pool never co-occurred on any real worker pool,
// so pairing them would fabricate a placement that does not exist. Later pools
// are consulted only when the first candidate pool resolves neither reference
// (for example a bare subnetwork name whose zone does not resolve to a region).
// A bare short name (no "/") is promoted to the project-less global (network)
// or regional (subnetwork, using the pool's own zone) partial before
// resolution, mirroring the GKE Cluster and Dataproc Cluster extractors' own
// network/subnetwork reference handling.
func dataflowJobWorkerPoolNetwork(pools []dataflowJobWorkerPool, projectID string) (network, subnetwork string) {
	for _, pool := range pools {
		if strings.TrimSpace(pool.Network) == "" && strings.TrimSpace(pool.Subnetwork) == "" {
			continue
		}
		network = dataflowNetworkFullName(pool.Network, projectID)
		subnetwork = dataflowSubnetworkFullName(pool.Subnetwork, projectID, dataflowRegionFromZone(pool.Zone))
		if network != "" || subnetwork != "" {
			return network, subnetwork
		}
	}
	return network, subnetwork
}

// dataflowNetworkFullName resolves a Dataflow worker pool's network reference
// to its CAI full resource name. A bare short name (e.g. "default") is
// promoted to the project-less global partial before resolution against the
// job's project.
func dataflowNetworkFullName(ref, projectID string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") {
		trimmed = "global/networks/" + trimmed
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// dataflowSubnetworkFullName resolves a Dataflow worker pool's subnetwork
// reference to its CAI full resource name. A bare subnetwork short name is
// regional, so it is promoted to the project-less regional partial using the
// worker pool's own region; a bare name with no known region cannot be
// resolved and yields "" so no edge is fabricated.
func dataflowSubnetworkFullName(ref, projectID, region string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "/") {
		if region == "" {
			return ""
		}
		trimmed = "regions/" + region + "/subnetworks/" + trimmed
	}
	return computeFullResourceNameFromSelfLink(trimmed, projectID)
}

// dataflowRegionFromZone derives the Compute region from a Dataflow worker
// pool's zone (e.g. "us-central1-a"), which is always a region name plus a
// single trailing "-<letter>" suffix. A zone with no recognizable suffix, or a
// blank zone, yields "".
func dataflowRegionFromZone(zone string) string {
	trimmed := strings.TrimSpace(zone)
	if trimmed == "" {
		return ""
	}
	idx := strings.LastIndex(trimmed, "-")
	if idx <= 0 {
		return ""
	}
	suffix := trimmed[idx+1:]
	if len(suffix) == 1 && suffix[0] >= 'a' && suffix[0] <= 'z' {
		return trimmed[:idx]
	}
	return ""
}

// dataflowStagingBucket extracts only the bucket name from a Dataflow Job's
// environment.tempStoragePrefix, dropping the object path in every form as a
// data-plane locator. The Dataflow API documents the supported resource types
// as `storage.googleapis.com/{bucket}/{object}` and
// `bucket.storage.googleapis.com/{object}`; the shared gs://-only
// gcsBucketFromURI is also accepted defensively for a `gs://bucket/object`
// value. Any other shape yields "" so no staging-bucket edge is fabricated.
func dataflowStagingBucket(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return ""
	}
	if bucket := gcsBucketFromURI(trimmed); bucket != "" {
		return bucket
	}
	// Strip an optional scheme (https:// or //) before matching the two
	// documented storage.googleapis.com host forms.
	rest := trimmed
	if after, ok := strings.CutPrefix(rest, "https://"); ok {
		rest = after
	} else if after, ok := strings.CutPrefix(rest, "//"); ok {
		rest = after
	}
	const host = "storage.googleapis.com"
	// Path-style: storage.googleapis.com/{bucket}/{object}
	if after, ok := strings.CutPrefix(rest, host+"/"); ok {
		bucket, _, _ := strings.Cut(after, "/")
		return strings.TrimSpace(bucket)
	}
	// Virtual-hosted-style: {bucket}.storage.googleapis.com/{object}
	if host, _, _ := strings.Cut(rest, "/"); strings.HasSuffix(host, ".storage.googleapis.com") {
		bucket := strings.TrimSuffix(host, ".storage.googleapis.com")
		return strings.TrimSpace(bucket)
	}
	return ""
}

// dataflowJobEdge builds one typed provider relationship observation anchored
// on the Dataflow Job's CAI full resource name.
func dataflowJobEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
