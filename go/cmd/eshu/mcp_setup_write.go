// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// mergeMCPServerConfig merges the eshu server entry into an existing JSON config
// document, preserving every unrelated key and every other server entry.
//
// serversKey selects the container key the target client uses ("mcpServers" for
// Claude Code/Cursor/generic, "servers" for VS Code). entry is the eshu server
// object. existing is the raw bytes of the current config file (may be empty).
// The returned bytes are pretty-printed JSON with no HTML escaping so embedded
// ${ESHU_API_KEY} references and URLs survive round-tripping.
func mergeMCPServerConfig(existing []byte, serversKey string, entry map[string]any) ([]byte, error) {
	doc := map[string]any{}
	trimmed := bytes.TrimSpace(existing)
	if len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &doc); err != nil {
			return nil, fmt.Errorf("parse existing config (refusing to clobber): %w", err)
		}
	}

	servers, ok := doc[serversKey].(map[string]any)
	if !ok {
		// Either absent or the wrong type. If it is present with a wrong type we
		// must not silently drop it; surface an error instead of clobbering.
		if _, present := doc[serversKey]; present {
			return nil, fmt.Errorf("existing %q key is not an object; refusing to overwrite", serversKey)
		}
		servers = map[string]any{}
	}
	servers[mcpServerKey] = entry
	doc[serversKey] = servers

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode merged config: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// serversKeyForPlatform returns the JSON container key a platform uses for its
// server map.
func serversKeyForPlatform(p *mcpPlatform) string {
	if p.Name == "vscode" {
		return "servers"
	}
	return "mcpServers"
}

// writeMCPServerConfig safely installs the eshu server entry into targetPath,
// merging with any existing config and preserving unrelated content. It creates
// parent directories as needed and writes atomically via a temp file rename.
func writeMCPServerConfig(p *mcpPlatform, req mcpSetupRequest, targetPath string) error {
	if !p.Writable {
		return fmt.Errorf("platform %q has no safe --write target; copy the snippet into %s manually", p.Name, p.TargetFile)
	}

	existing, err := os.ReadFile(targetPath) // #nosec G304 -- targetPath is an operator-provided MCP config file path, not an HTTP request param //nolint:gosec
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing config %s: %w", targetPath, err)
	}

	merged, err := mergeMCPServerConfig(existing, serversKeyForPlatform(p), serverEntry(p, req))
	if err != nil {
		return err
	}
	merged = append(merged, '\n')

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".eshu-mcp-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(merged); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("install config %s: %w", targetPath, err)
	}
	return nil
}

// defaultWriteTarget resolves the default config file path for a writable
// platform. It honors HOME via os.UserHomeDir for user-scoped targets and uses
// the current working directory for project-scoped targets.
func defaultWriteTarget(p *mcpPlatform) (string, error) {
	switch p.Name {
	case "claude":
		// Project-scoped .mcp.json is the safest default to merge.
		return ".mcp.json", nil
	case "cursor":
		return filepath.Join(".cursor", "mcp.json"), nil
	case "vscode":
		return filepath.Join(".vscode", "mcp.json"), nil
	default:
		return "", fmt.Errorf("platform %q has no default --write target", p.Name)
	}
}

// describeWriteTarget returns a human label for where a write landed, trimming a
// home prefix when present for readability.
func describeWriteTarget(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}
