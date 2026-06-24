// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"net/url"
	"strings"
)

func swiftPackageResolvedDependencyVariables(document map[string]any, lang string) []map[string]any {
	if !isSwiftPackageResolvedV2(document["version"]) {
		return nil
	}
	pins, ok := document["pins"].([]any)
	if !ok {
		return nil
	}
	rows := make([]map[string]any, 0, len(pins))
	for _, rawPin := range pins {
		pin, ok := rawPin.(map[string]any)
		if !ok {
			continue
		}
		identity, _ := pin["identity"].(string)
		identity = strings.ToLower(strings.TrimSpace(identity))
		location, _ := pin["location"].(string)
		location = strings.TrimSpace(location)
		kind, _ := pin["kind"].(string)
		if identity == "" || location == "" || !isRemoteSwiftPin(kind) {
			continue
		}
		sourceLocation := sanitizeSwiftRemoteLocation(location)
		if sourceLocation == "" {
			continue
		}
		state, _ := pin["state"].(map[string]any)
		version, _ := state["version"].(string)
		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}
		namespace, ok := swiftSourceNamespace(sourceLocation)
		if !ok {
			continue
		}
		rows = append(rows, map[string]any{
			"lang":             lang,
			"name":             namespace + "/" + identity,
			"value":            version,
			"section":          "Package.resolved",
			"config_kind":      "dependency",
			"package_manager":  "swift",
			"package_identity": identity,
			"source_location":  sourceLocation,
			"source_namespace": namespace,
			"lockfile":         true,
			"lockfile_format":  "swift-package-resolved",
		})
	}
	return rows
}

func isSwiftPackageResolvedV2(rawVersion any) bool {
	version, ok := rawVersion.(float64)
	return ok && version == 2
}

func isRemoteSwiftPin(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "remotesourcecontrol":
		return true
	default:
		return false
	}
}

func sanitizeSwiftRemoteLocation(location string) string {
	trimmed := strings.TrimSpace(location)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "git@") {
		cleaned := trimmed
		if index := strings.IndexAny(cleaned, "?#"); index >= 0 {
			cleaned = cleaned[:index]
		}
		hostAndPath := strings.TrimPrefix(cleaned, "git@")
		host, repoPath, ok := strings.Cut(hostAndPath, ":")
		if ok && strings.TrimSpace(host) != "" && strings.TrimSpace(repoPath) != "" {
			return cleaned
		}
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || parsed.Path == "" {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http", "ssh", "git":
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	default:
		return ""
	}
}

func swiftSourceNamespace(location string) (string, bool) {
	host, repoPath, ok := splitSwiftRemoteLocation(location)
	if !ok {
		return "", false
	}
	repoPath = strings.TrimSuffix(strings.Trim(repoPath, "/"), ".git")
	segments := strings.Split(repoPath, "/")
	if len(segments) < 2 {
		return "", false
	}
	namespacePath := strings.Join(segments[:len(segments)-1], "/")
	if namespacePath == "" {
		return "", false
	}
	return strings.ToLower(host + "/" + namespacePath), true
}

func splitSwiftRemoteLocation(location string) (string, string, bool) {
	trimmed := strings.TrimSpace(location)
	if strings.HasPrefix(trimmed, "git@") {
		hostAndPath := strings.TrimPrefix(trimmed, "git@")
		host, repoPath, ok := strings.Cut(hostAndPath, ":")
		return strings.ToLower(strings.TrimSpace(host)), strings.TrimSpace(repoPath), ok && host != "" && repoPath != ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || parsed.Path == "" {
		return "", "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http", "ssh", "git":
		return strings.ToLower(parsed.Host), parsed.Path, true
	default:
		return "", "", false
	}
}
