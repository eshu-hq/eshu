// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"strings"
)

func isJavaMetadataPath(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if strings.HasPrefix(normalized, "meta-inf/services/") ||
		strings.Contains(normalized, "/meta-inf/services/") {
		_, serviceName, ok := strings.Cut(normalized, "meta-inf/services/")
		return ok && strings.TrimSpace(serviceName) != ""
	}
	return normalized == "meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports" ||
		strings.HasSuffix(normalized, "/meta-inf/spring/org.springframework.boot.autoconfigure.autoconfiguration.imports") ||
		normalized == "meta-inf/spring.factories" ||
		strings.HasSuffix(normalized, "/meta-inf/spring.factories")
}

// isSbtBuildPropertiesPath accepts only sbt's launcher pin path.
func isSbtBuildPropertiesPath(path string) bool {
	if filepath.Base(path) != "build.properties" {
		return false
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	return normalized == "project/build.properties" || strings.HasSuffix(normalized, "/project/build.properties")
}
