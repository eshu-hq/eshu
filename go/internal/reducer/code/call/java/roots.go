// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// MetadataFileRootCallerID returns a file-root caller identity for Java
// service_loader/spring_autoconfiguration provider references.
func MetadataFileRootCallerID(repositoryID string, callerFilePath string, call map[string]any) string {
	if repositoryID == "" || callerFilePath == "" {
		return ""
	}
	switch strings.TrimSpace(payloadcore.AnyToString(call["call_kind"])) {
	case "java.service_loader_provider", "java.spring_autoconfiguration_class":
		return repositoryID + ":" + shared.NormalizePath(callerFilePath)
	default:
		return ""
	}
}
