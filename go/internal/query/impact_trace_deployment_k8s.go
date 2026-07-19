// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

func buildK8sRelationships(k8sResources []map[string]any) []map[string]any {
	relationships := make([]map[string]any, 0, len(k8sResources)*2)
	seen := make(map[string]struct{}, len(k8sResources)*2)

	for _, source := range k8sResources {
		sourceKind := safeStr(source, "kind")
		sourceID := safeStr(source, "entity_id")
		sourceName := safeStr(source, "entity_name")
		if sourceID == "" || sourceName == "" {
			continue
		}

		if strings.EqualFold(sourceKind, "Service") {
			serviceInput := k8sSelectMatchInputFromRow(source)
			for _, target := range k8sResources {
				targetID := safeStr(target, "entity_id")
				targetName := safeStr(target, "entity_name")
				if targetID == "" || targetName == "" || targetID == sourceID {
					continue
				}
				matched, reason, _ := k8sSelectMatch(serviceInput, k8sSelectMatchInputFromRow(target))
				if !matched {
					continue
				}
				key := "SELECTS:" + sourceID + ":" + targetID
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				relationships = append(relationships, map[string]any{
					"type":        "SELECTS",
					"source_id":   sourceID,
					"source_name": sourceName,
					"target_id":   targetID,
					"target_name": targetName,
					"reason":      reason,
				})
			}
		}

		for _, imageRef := range metadataStringSlice(source, "container_images") {
			key := "USES_IMAGE:" + sourceID + ":" + imageRef
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			relationships = append(relationships, map[string]any{
				"type":        "USES_IMAGE",
				"source_id":   sourceID,
				"source_name": sourceName,
				"target_name": imageRef,
				"reason":      "k8s_container_image",
			})
		}
	}

	return relationships
}
