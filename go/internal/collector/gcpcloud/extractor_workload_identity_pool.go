// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

// Family constants for this extractor now live in catalog/gcp_security.go (#6696 HK slice); uses below name catalog.X.

func init() {
	RegisterAssetExtractor(catalog.WorkloadIdentityPoolAssetType, extractWorkloadIdentityPool)
}

// workloadIdentityPoolData is the bounded view of a CAI
// iam.googleapis.com/WorkloadIdentityPool resource.data blob. A pool carries no
// external-trust configuration of its own — that lives on its child
// WorkloadIdentityPoolProvider resources (a separate CAI asset type with its own
// extractor) — so only the pool's lifecycle posture is decoded here. Disabled is
// a pointer so a present `false` (an active pool, useful posture) is
// distinguishable from an absent field.
type workloadIdentityPoolData struct {
	State    string `json:"state"`
	Disabled *bool  `json:"disabled"`
}

// extractWorkloadIdentityPool extracts bounded, redaction-safe typed depth for one
// CAI IAM Workload Identity Pool asset. It surfaces the lifecycle state and the
// disabled posture for monitoring and drift.
//
// The pool's graph value — its providers and the external AWS/OIDC trust they
// grant, and the service accounts those providers can impersonate — lives on the
// child WorkloadIdentityPoolProvider resources, which are inbound to this pool
// and own the provider -> pool edge. The pool therefore derives no outbound
// relationships or anchors from its own data.
func extractWorkloadIdentityPool(ctx ExtractContext) (AttributeExtraction, error) {
	var data workloadIdentityPoolData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode workload identity pool data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.State); v != "" {
		attrs["state"] = v
	}
	if data.Disabled != nil {
		attrs["disabled"] = *data.Disabled
	}

	return AttributeExtraction{Attributes: attrs}, nil
}
