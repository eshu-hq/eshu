// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import "strings"

// dockerfileInstallEntityID distinguishes repeated install declarations on
// one source line while retaining the readable package name in the entity.
func dockerfileInstallEntityID(repoID, path, entityType, entityName string, line int, metadata map[string]any) string {
	if !strings.EqualFold(strings.TrimSpace(entityType), "DockerfileInstall") {
		return ""
	}
	ordinal := metadataStringValue(metadata, "install_ordinal")
	if ordinal == "" {
		return ""
	}
	return CanonicalEntityID(repoID, path, entityType, entityName+"\x1f"+ordinal, line)
}
