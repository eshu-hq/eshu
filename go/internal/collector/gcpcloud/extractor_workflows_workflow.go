// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// assetTypeWorkflowsWorkflow is the Cloud Asset Inventory asset type for a
// Workflows Workflow. assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix are
// shared constants declared by the BigQuery Table extractor and reused here for
// the CMEK edge.
const assetTypeWorkflowsWorkflow = "workflows.googleapis.com/Workflow"

// relationshipTypeWorkflowsWorkflowEncryptedByKMSKey is the bounded provider
// relationship type for a Workflow's CMEK edge, carried on a
// gcp_cloud_relationship fact; the reducer materializes it only when both
// endpoints resolve exactly. The workflow's runtime service account is carried
// as a fingerprinted-email attribute/anchor, not an edge, because an email is
// not an exactly resolvable CAI endpoint (mirrors the Dataflow Job, Dataproc
// Cluster, and GKE Cluster extractors' own service-account treatment).
const relationshipTypeWorkflowsWorkflowEncryptedByKMSKey = "workflow_encrypted_by_kms_key"

func init() {
	RegisterAssetExtractor(assetTypeWorkflowsWorkflow, extractWorkflowsWorkflow)
}

// workflowsWorkflowData is the bounded view of a CAI
// workflows.googleapis.com/Workflow resource.data blob, matching the
// Workflows v1 REST Workflow resource. sourceContents is decoded only to
// detect presence; the workflow definition body (which can carry step
// arguments, headers, and other operator-supplied values) is never retained
// or otherwise inspected. userEnvVars, tags, and labels are omitted here
// because the collector's shared label/tag path already captures and
// fingerprints those; this extractor does not re-copy them into typed depth.
type workflowsWorkflowData struct {
	State                 string `json:"state"`
	RevisionID            string `json:"revisionId"`
	CreateTime            string `json:"createTime"`
	UpdateTime            string `json:"updateTime"`
	RevisionCreateTime    string `json:"revisionCreateTime"`
	ServiceAccount        string `json:"serviceAccount"`
	CallLogLevel          string `json:"callLogLevel"`
	ExecutionHistoryLevel string `json:"executionHistoryLevel"`
	CryptoKeyName         string `json:"cryptoKeyName"`
	SourceContents        string `json:"sourceContents"`
}

// extractWorkflowsWorkflow extracts bounded, redaction-safe typed depth for one
// Workflows Workflow CAI asset. It returns the Terraform/drift/monitoring
// attribute set (deployment state, revision id, call-log level, execution
// history level, create/update/revision-create time, a source-contents
// presence flag, the CMEK key name, and the fingerprinted runtime service
// account), cross-source correlation anchors (the CMEK key name and the
// service-account fingerprint), and the typed CMEK encryption edge when the
// workflow is configured with a customer-managed key. The workflow's YAML/JSON
// source body (`sourceContents`) is decoded only far enough to detect
// presence; no step, argument, header, or embedded credential value from that
// body is ever read into an attribute, anchor, or edge, so any called
// service (Cloud Run, Cloud Functions, or an arbitrary HTTP endpoint)
// referenced only inside the workflow definition is out of reach of this
// safe-metadata extractor and is not modeled as an edge.
func extractWorkflowsWorkflow(ctx ExtractContext) (AttributeExtraction, error) {
	var data workflowsWorkflowData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode workflows workflow data: %w", err)
	}

	attrs := workflowsWorkflowAttributes(data)
	var anchors []string
	var rels []RelationshipObservation

	if fp := secretsiam.GCPServiceAccountEmailDigest(workflowsWorkflowServiceAccountEmail(data.ServiceAccount)); fp != "" {
		attrs["service_account_fingerprint"] = fp
		anchors = append(anchors, fp)
	}

	if kms := strings.TrimSpace(data.CryptoKeyName); kms != "" {
		attrs["crypto_key_name"] = kms
		if kmsName := workflowsWorkflowKMSKeyFullName(kms); kmsName != "" {
			anchors = append(anchors, kmsName)
			rels = append(rels, workflowsWorkflowEdge(ctx, relationshipTypeWorkflowsWorkflowEncryptedByKMSKey, kmsName, assetTypeKMSCryptoKey))
		}
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: dedupeNonEmpty(anchors),
		Relationships:      rels,
	}, nil
}

// workflowsWorkflowAttributes assembles the bounded attribute map. Empty or
// absent fields are omitted rather than written as zero values so a partial
// CAI page does not fabricate a state or posture that was simply not
// reported. source_contents_present is written only when sourceContents is
// non-blank; the value itself is discarded immediately after the presence
// check.
func workflowsWorkflowAttributes(data workflowsWorkflowData) map[string]any {
	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.RevisionID); v != "" {
		attrs["revision_id"] = v
	}
	if v := strings.TrimSpace(data.CallLogLevel); v != "" {
		attrs["call_log_level"] = v
	}
	if v := strings.TrimSpace(data.ExecutionHistoryLevel); v != "" {
		attrs["execution_history_level"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if v, ok := normalizeRFC3339(data.UpdateTime); ok {
		attrs["update_time"] = v
	}
	if v, ok := normalizeRFC3339(data.RevisionCreateTime); ok {
		attrs["revision_create_time"] = v
	}
	if strings.TrimSpace(data.SourceContents) != "" {
		attrs["source_contents_present"] = true
	}
	return attrs
}

// workflowsWorkflowServiceAccountEmail extracts the bare email address from a
// Workflow's serviceAccount field, which the Workflows API reports as either a
// bare email or a
// "projects/<p>/serviceAccounts/<email>" relative resource name. An
// unrecognized shape is passed through unchanged so a plain email still
// fingerprints correctly; a blank input yields "".
func workflowsWorkflowServiceAccountEmail(serviceAccount string) string {
	trimmed := strings.TrimSpace(serviceAccount)
	if trimmed == "" {
		return ""
	}
	if idx := strings.LastIndex(trimmed, "/serviceAccounts/"); idx >= 0 {
		return trimmed[idx+len("/serviceAccounts/"):]
	}
	return trimmed
}

// workflowsWorkflowKMSKeyFullName derives the Cloud KMS CryptoKey CAI full
// resource name from a Workflow's cryptoKeyName, which the Workflows API
// reports as a bare
// "projects/.../locations/.../keyRings/.../cryptoKeys/..." relative name or as
// an already CAI-prefixed "//cloudkms.googleapis.com/..." full resource name.
// An already-prefixed value is returned unchanged so the prefix is never
// doubled; a bare value is prefixed only when it matches the expected
// CryptoKey path shape. It returns "" for a blank reference or an
// unrecognized shape, so a malformed key name never becomes an edge endpoint
// or anchor.
func workflowsWorkflowKMSKeyFullName(cryptoKeyName string) string {
	trimmed := strings.TrimSpace(cryptoKeyName)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		if strings.HasPrefix(trimmed, cloudKMSResourceNamePrefix) {
			return trimmed
		}
		return ""
	}
	if strings.HasPrefix(trimmed, "projects/") && strings.Contains(trimmed, "/locations/") &&
		strings.Contains(trimmed, "/keyRings/") && strings.Contains(trimmed, "/cryptoKeys/") {
		return cloudKMSResourceNamePrefix + trimmed
	}
	return ""
}

// workflowsWorkflowEdge builds one typed provider relationship observation
// anchored on the Workflow's CAI full resource name.
func workflowsWorkflowEdge(ctx ExtractContext, relationshipType, targetName, targetType string) RelationshipObservation {
	return RelationshipObservation{
		SourceFullResourceName: ctx.FullResourceName,
		SourceAssetType:        ctx.AssetType,
		RelationshipType:       relationshipType,
		TargetFullResourceName: targetName,
		TargetAssetType:        targetType,
		SupportState:           RelationshipSupportSupported,
	}
}
