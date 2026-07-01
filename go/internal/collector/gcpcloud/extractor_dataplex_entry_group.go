// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// assetTypeDataplexEntryGroup is the CAI asset type for a Dataplex entry group.
// An entry group is a container: its contained entries reference it from their
// own assets (inbound), and its project is base-observation placement, so the
// extractor derives no outbound edges or correlation anchors — only bounded
// posture attributes.
const assetTypeDataplexEntryGroup = "dataplex.googleapis.com/EntryGroup"

func init() {
	RegisterAssetExtractor(assetTypeDataplexEntryGroup, extractDataplexEntryGroup)
}

// dataplexEntryGroupData is the bounded view of a CAI
// dataplex.googleapis.com/EntryGroup resource.data blob (Dataplex Catalog
// EntryGroup resource). Only the redaction-safe catalog transfer status and
// creation time are decoded; the free-text description is not decoded. The
// EntryGroup resource has no lifecycle state field.
type dataplexEntryGroupData struct {
	TransferStatus string `json:"transferStatus"`
	CreateTime     string `json:"createTime"`
}

// extractDataplexEntryGroup extracts bounded, redaction-safe typed depth for one
// Dataplex Entry Group CAI asset. It returns the monitoring attribute set
// (catalog transfer status and creation time) and no edges or anchors, since an
// entry group owns no outbound references from its own resource.data.
func extractDataplexEntryGroup(ctx ExtractContext) (AttributeExtraction, error) {
	var data dataplexEntryGroupData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode dataplex entry group data: %w", err)
	}

	attrs := map[string]any{}
	if v := strings.TrimSpace(data.TransferStatus); v != "" {
		attrs["transfer_status"] = v
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}

	return AttributeExtraction{Attributes: attrs}, nil
}
