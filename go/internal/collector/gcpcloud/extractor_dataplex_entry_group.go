// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud //nolint:dirgate // #6696 wave-1: flat extractor moves to extractor/<group> in a later slice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
)

func init() {
	RegisterAssetExtractor(catalog.AssetTypeDataplexEntryGroup, extractDataplexEntryGroup)
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
