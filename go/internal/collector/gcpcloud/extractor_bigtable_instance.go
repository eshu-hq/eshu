// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeBigtableInstance is the Cloud Asset Inventory asset type for a
// Cloud Bigtable Instance.
const assetTypeBigtableInstance = "bigtableadmin.googleapis.com/Instance"

func init() {
	RegisterAssetExtractor(assetTypeBigtableInstance, extractBigtableInstance)
}

// bigtableInstanceData is the bounded view of a CAI
// bigtableadmin.googleapis.com/Instance resource.data blob. The Bigtable Admin
// v2 Instance resource carries only instance-level control-plane metadata; it
// has no clusters, encryption, or kmsKeyName field. Clusters are a separate
// CAI asset type (bigtableadmin.googleapis.com/Cluster, handled by
// extractor_bigtable_cluster.go), and CMEK lives on that Cluster resource, not
// here. Instance labels are already carried by the shared envelope label path
// (see envelope.go), so they are not decoded again.
type bigtableInstanceData struct {
	DisplayName string `json:"displayName"`
	State       string `json:"state"`
	Type        string `json:"type"`
	Edition     string `json:"edition"`
}

// extractBigtableInstance extracts bounded, redaction-safe typed depth for one
// Cloud Bigtable Instance CAI asset. It returns the Terraform/drift/monitoring
// attribute set (display name, state, instance type PRODUCTION/DEVELOPMENT, and
// edition). It emits no outbound edges or correlation anchors: an Instance's
// clusters and their CMEK keys are owned by the sibling Bigtable Cluster
// extractor, which is where those graph relationships resolve.
func extractBigtableInstance(ctx ExtractContext) (AttributeExtraction, error) {
	var data bigtableInstanceData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode bigtable instance data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.DisplayName); v != "" {
		attrs["display_name"] = v
	}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if v := strings.TrimSpace(data.Type); v != "" {
		attrs["instance_type"] = v
	}
	if v := strings.TrimSpace(data.Edition); v != "" {
		attrs["edition"] = v
	}

	return AttributeExtraction{Attributes: attrs}, nil
}
