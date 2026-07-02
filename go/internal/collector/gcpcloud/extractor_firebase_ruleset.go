// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"fmt"
	"strings"
)

// firebaseRulesetAssetType is the Cloud Asset Inventory asset type for a Firebase
// Rules Ruleset. The parent project edge targets firebaseProjectAssetType and
// reuses firebaseProjectResourceNamePrefix declared elsewhere in this package.
const firebaseRulesetAssetType = "firebaserules.googleapis.com/Ruleset"

// relationshipTypeFirebaseRulesetBelongsToProject is the bounded provider
// relationship type for the edge from a ruleset to its parent Firebase project.
const relationshipTypeFirebaseRulesetBelongsToProject = "firebase_ruleset_belongs_to_project"

func init() {
	RegisterAssetExtractor(firebaseRulesetAssetType, extractFirebaseRuleset)
}

// firebaseRulesetData is the bounded view of a CAI
// firebaserules.googleapis.com/Ruleset resource.data blob. Only the source file
// count, creation time, and the bounded target-service enum are decoded — the raw
// rule source content (which can embed internal collection, path, and field
// names) is never read into a persisted field.
type firebaseRulesetData struct {
	Source *struct {
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	} `json:"source"`
	CreateTime string `json:"createTime"`
	Metadata   *struct {
		Services []string `json:"services"`
	} `json:"metadata"`
}

// extractFirebaseRuleset extracts bounded, redaction-safe typed depth for one CAI
// Firebase Rules Ruleset asset. It surfaces the source file count, creation time,
// and the bounded target-service enum (e.g. cloud.firestore, firebase.storage);
// and emits the typed edge to the parent Firebase project with the canonical
// FirebaseProject full resource name as the correlation anchor.
//
// The raw rule source content is never read into a persisted field — only the
// file count is kept. Releases that reference this ruleset live in separate
// firebaserules Release resources (a release points at a ruleset, not the
// reverse), so those edges belong to the Release asset type, not here. The parent
// project is derived from the ruleset's own full resource name; when no project
// segment is present the edge is skipped so no unresolvable target is emitted.
func extractFirebaseRuleset(ctx ExtractContext) (AttributeExtraction, error) {
	var data firebaseRulesetData
	if err := json.Unmarshal(ctx.Data, &data); err != nil {
		return AttributeExtraction{}, fmt.Errorf("decode firebase ruleset data: %w", err)
	}

	attrs := map[string]any{}
	if data.Source != nil && len(data.Source.Files) > 0 {
		attrs["source_file_count"] = len(data.Source.Files)
	}
	if v, ok := normalizeRFC3339(data.CreateTime); ok {
		attrs["creation_time"] = v
	}
	if data.Metadata != nil {
		services := make([]string, 0, len(data.Metadata.Services))
		for _, svc := range data.Metadata.Services {
			if trimmed := strings.TrimSpace(svc); trimmed != "" {
				services = append(services, trimmed)
			}
		}
		if len(services) > 0 {
			attrs["services"] = services
		}
	}

	var anchors []string
	var rels []RelationshipObservation
	if projectID := ProjectIDFromFullName(ctx.FullResourceName); projectID != "" {
		target := firebaseProjectResourceNamePrefix + projectID
		anchors = append(anchors, target)
		rels = append(rels, RelationshipObservation{
			SourceFullResourceName: ctx.FullResourceName,
			SourceAssetType:        ctx.AssetType,
			RelationshipType:       relationshipTypeFirebaseRulesetBelongsToProject,
			TargetFullResourceName: target,
			TargetAssetType:        firebaseProjectAssetType,
			SupportState:           RelationshipSupportSupported,
		})
	}

	return AttributeExtraction{
		Attributes:         attrs,
		CorrelationAnchors: anchors,
		Relationships:      rels,
	}, nil
}
