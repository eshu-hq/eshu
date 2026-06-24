// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package yaml

import "strings"

func appendFolderFromGrafanaResource(payload map[string]any, document map[string]any, ctx grafanaSourceContext) {
	spec := nestedMap(document, "spec")
	appendFolderRow(
		payload,
		firstNonEmpty(cleanString(spec["uid"]), cleanString(spec["folderUID"]), ctx.resourceName),
		firstNonEmpty(cleanString(spec["title"]), cleanString(spec["name"])),
		ctx,
	)
}

func appendFoldersFromProvisioning(payload map[string]any, object map[string]any, ctx grafanaSourceContext) {
	if providers, ok := object["providers"].([]any); ok {
		for _, item := range providers {
			provider, ok := item.(map[string]any)
			if !ok {
				continue
			}
			uid := firstNonEmpty(cleanString(provider["folderUid"]), cleanString(provider["folderUID"]))
			title := cleanString(provider["folder"])
			if uid == "" && title == "" {
				continue
			}
			appendFolderRow(payload, uid, title, ctx)
		}
	}
	if folders, ok := object["folders"].([]any); ok {
		for _, item := range folders {
			folder, ok := item.(map[string]any)
			if !ok {
				continue
			}
			uid := firstNonEmpty(cleanString(folder["uid"]), cleanString(folder["folderUid"]), cleanString(folder["folderUID"]))
			title := firstNonEmpty(cleanString(folder["title"]), cleanString(folder["name"]), cleanString(folder["folder"]))
			if uid == "" && title == "" {
				continue
			}
			appendFolderRow(payload, uid, title, ctx)
		}
	}
}

func appendFolderRow(payload map[string]any, uid string, title string, ctx grafanaSourceContext) {
	titleFingerprint := fingerprintValue(title)
	row := baseObservabilityRow(ctx, "folder."+firstNonEmpty(uid, titleFingerprint, ctx.resourceName, ctx.configKey))
	row["declaration_kind"] = firstNonEmpty(ctx.declarationKind, "grafana_folder")
	if uid != "" {
		row["folder_uid"] = uid
	}
	if titleFingerprint != "" {
		row["folder_title_fingerprint"] = titleFingerprint
	}
	if uid != "" || titleFingerprint != "" {
		row["outcome"] = "exact"
	} else {
		row["outcome"] = "derived"
	}
	appendBucketRow(payload, observabilityFolderBucket, row)
}

func isGrafanaFolderResource(apiVersion string, kind string) bool {
	lowerAPI := strings.ToLower(apiVersion)
	return strings.EqualFold(kind, "GrafanaFolder") ||
		(strings.Contains(lowerAPI, "grafana") && strings.EqualFold(kind, "Folder"))
}
