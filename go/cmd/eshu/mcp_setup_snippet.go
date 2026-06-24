// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// marshalIndentNoHTML renders v as 2-space-indented JSON without HTML escaping
// so generated snippets contain literal ${ESHU_API_KEY} references and URLs.
func marshalIndentNoHTML(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("marshal snippet: %w", err)
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// localStdioServerEntry builds the stdio server entry used by every
// mcpServers-style client. It spawns `eshu mcp start` over stdio and embeds no
// secrets.
func localStdioServerEntry() map[string]any {
	return map[string]any{
		"command": "eshu",
		"args":    []string{"mcp", "start"},
	}
}

// hostedServerEntry builds the hosted HTTP server entry. The bearer token is
// emitted as an env-var reference for clients that support it, or a masked
// placeholder otherwise; the raw secret is never written into the snippet.
func hostedServerEntry(p *mcpPlatform, req mcpSetupRequest) map[string]any {
	base := strings.TrimRight(strings.TrimSpace(req.ServiceURL), "/")
	if base == "" {
		base = "https://your-eshu-host"
	}
	return map[string]any{
		"type": "http",
		"url":  base + "/mcp/message",
		"headers": map[string]any{
			"Authorization": "Bearer " + tokenReference(p, req.APIKey),
		},
	}
}

// serverEntry selects the stdio or hosted entry for the request mode.
func serverEntry(p *mcpPlatform, req mcpSetupRequest) map[string]any {
	if req.Mode == modeHostedHTTP {
		return hostedServerEntry(p, req)
	}
	return localStdioServerEntry()
}

// mcpServersJSONSnippet renders the common { "mcpServers": { "eshu": {...} } }
// shape used by Claude Code, Cursor, and generic MCP clients.
func mcpServersJSONSnippet(req mcpSetupRequest) (string, error) {
	p, err := resolvePlatform("generic")
	if err != nil {
		return "", err
	}
	doc := map[string]any{
		"mcpServers": map[string]any{
			mcpServerKey: serverEntry(p, req),
		},
	}
	return marshalIndentNoHTML(doc)
}

// vscodeJSONSnippet renders the VS Code .vscode/mcp.json shape, which nests
// servers under a top-level "servers" key.
func vscodeJSONSnippet(req mcpSetupRequest) (string, error) {
	p, err := resolvePlatform("vscode")
	if err != nil {
		return "", err
	}
	doc := map[string]any{
		"servers": map[string]any{
			mcpServerKey: serverEntry(p, req),
		},
	}
	return marshalIndentNoHTML(doc)
}

// codexTOMLSnippet renders the Codex CLI ~/.codex/config.toml block. Codex uses
// TOML, so this is generated as text rather than JSON.
func codexTOMLSnippet(req mcpSetupRequest) (string, error) {
	p, err := resolvePlatform("codex")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("[mcp_servers.")
	b.WriteString(mcpServerKey)
	b.WriteString("]\n")
	if req.Mode == modeHostedHTTP {
		base := strings.TrimRight(strings.TrimSpace(req.ServiceURL), "/")
		if base == "" {
			base = "https://your-eshu-host"
		}
		fmt.Fprintf(&b, "url = %q\n", base+"/mcp/message")
		fmt.Fprintf(&b, "http_headers = { Authorization = %q }\n", "Bearer "+tokenReference(p, req.APIKey))
	} else {
		b.WriteString("command = \"eshu\"\n")
		b.WriteString("args = [\"mcp\", \"start\"]\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// renderSetupSnippet builds the full guidance block for a platform: snippet plus
// target-file and notes. It never prints a raw token.
func renderSetupSnippet(p *mcpPlatform, req mcpSetupRequest) (string, error) {
	snippet, err := p.snippet(req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "MCP setup for %s\n", p.DisplayName)
	fmt.Fprintf(&b, "Add to: %s\n\n", p.TargetFile)
	b.WriteString(snippet)
	b.WriteString("\n")
	if req.Mode == modeHostedHTTP && !p.SupportsEnvVarToken {
		fmt.Fprintf(&b, "\nThis client cannot reference env vars in config; inject the %s bearer token through your secret manager, not the file.\n", apiKeyEnvVar)
	} else if req.Mode == modeHostedHTTP {
		fmt.Fprintf(&b, "\nExport the token before launching the client: export %s=...\n", apiKeyEnvVar)
	}
	if len(p.notes) > 0 {
		b.WriteString("\nNotes:\n")
		for _, n := range p.notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	return b.String(), nil
}
