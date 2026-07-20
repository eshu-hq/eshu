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

// hostedBaseURL returns the trimmed hosted service base URL, or a placeholder
// host when none was resolved.
func hostedBaseURL(req mcpSetupRequest) string {
	base := strings.TrimRight(strings.TrimSpace(req.ServiceURL), "/")
	if base == "" {
		base = "https://your-eshu-host"
	}
	return base
}

// hostedServerEntry builds the hosted HTTP server entry. Credential wiring
// branches on req.Posture: token and shared-key postures emit a headers
// object with an env-var bearer reference (never the raw secret); SSO posture
// omits the headers key entirely so the client hits the bare endpoint,
// receives the RFC 9728 401 challenge, and completes the OAuth flow itself.
func hostedServerEntry(p *mcpPlatform, req mcpSetupRequest) map[string]any {
	entry := map[string]any{
		"type": "http",
		"url":  hostedBaseURL(req) + "/mcp/message",
	}
	switch req.Posture {
	case postureSSO:
		// No headers: the client authenticates via OAuth against the 401
		// challenge, not a static bearer token.
	case postureSharedKey:
		entry["headers"] = map[string]any{
			"Authorization": "Bearer " + tokenReference(p, apiKeyEnvVar, req.APIKey),
		}
	default: // postureToken
		entry["headers"] = map[string]any{
			"Authorization": "Bearer " + tokenReference(p, mcpTokenEnvVar, ""),
		}
	}
	return entry
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
//
// Hosted credential wiring uses Codex's native bearer_token_env_var TOML key
// (confirmed against codex-rs/config/src/mcp_types.rs's StreamableHttp
// transport and codex-rs/codex-mcp/src/rmcp_client.rs's resolve_bearer_token,
// which reads the named env var from the process environment at connect
// time -- never from config.toml) for token and shared-key posture, matching
// the mechanism docs/public/mcp/index.md already documents
// (`codex mcp add eshu --url ... --bearer-token-env-var ESHU_MCP_TOKEN`).
// Codex stores only the env-var name in config.toml, never the value.
// SSO posture emits the URL only: Codex's streamable_http transport has no
// documented client-side OAuth flow, so the guidance block (renderSetupSnippet)
// tells the operator to fall back to --auth token if the OAuth flow cannot run.
func codexTOMLSnippet(req mcpSetupRequest) (string, error) {
	if req.Mode != modeHostedHTTP {
		var b strings.Builder
		fmt.Fprintf(&b, "[mcp_servers.%s]\n", mcpServerKey)
		b.WriteString("command = \"eshu\"\n")
		b.WriteString("args = [\"mcp\", \"start\"]\n")
		return strings.TrimRight(b.String(), "\n"), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", mcpServerKey)
	fmt.Fprintf(&b, "url = %q\n", hostedBaseURL(req)+"/mcp/message")
	switch req.Posture {
	case postureSSO:
		// URL only; no bearer_token_env_var, no http_headers.
	case postureSharedKey:
		fmt.Fprintf(&b, "bearer_token_env_var = %q\n", apiKeyEnvVar)
	default: // postureToken
		fmt.Fprintf(&b, "bearer_token_env_var = %q\n", mcpTokenEnvVar)
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
	if req.Mode == modeHostedHTTP {
		b.WriteString(postureGuidance(p, req))
	}
	if len(p.notes) > 0 {
		b.WriteString("\nNotes:\n")
		for _, n := range p.notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	return b.String(), nil
}

// postureGuidance renders the credential-specific guidance block appended
// after a hosted snippet: how to obtain and export a per-user token (token
// posture, the default), how the OAuth flow proceeds (SSO posture), or the
// admin/dev caveat (shared-key posture, always explicit and always warned).
func postureGuidance(p *mcpPlatform, req mcpSetupRequest) string {
	var b strings.Builder
	switch req.Posture {
	case postureSSO:
		writeSSOGuidance(&b, p, req)
	case postureSharedKey:
		writeSharedKeyGuidance(&b, p)
	default: // postureToken
		writeTokenGuidance(&b, p)
	}
	return b.String()
}

// writeTokenGuidance renders the default per-user token posture's guidance:
// where to issue the token and which env var to export.
func writeTokenGuidance(b *strings.Builder, p *mcpPlatform) {
	b.WriteString("\nCreate a personal API token in the Eshu console: https://<host>/profile\n")
	b.WriteString("(Profile -> API tokens), then export it before launching the client:\n")
	writeExportOrInjectNote(b, p, mcpTokenEnvVar)
	b.WriteString("The token authenticates you as yourself; it is scoped to your grants.\n")
}

// writeSSOGuidance renders the SSO posture's guidance: the OAuth flow the
// client will run, naming the issuer and any pre-registered client id the
// discovery probe advertised, plus a Codex-specific fallback note since
// Codex's streamable_http transport has no documented OAuth flow.
func writeSSOGuidance(b *strings.Builder, p *mcpPlatform, req mcpSetupRequest) {
	b.WriteString("\nYour MCP client will authenticate via OAuth against your identity provider")
	if len(req.Issuers) > 0 {
		fmt.Fprintf(b, ":\n  %s\n", req.Issuers[0])
	} else {
		b.WriteString(".\n")
	}
	b.WriteString("In Claude Code, run /mcp and choose \"eshu\" to complete the browser sign-in.\n")
	if req.PreregisteredClientID != "" {
		fmt.Fprintf(b, "Pre-registered OAuth client id advertised by the server: %s\n", req.PreregisteredClientID)
		b.WriteString("(needed by clients that cannot self-register; some identity providers offer no anonymous DCR.)\n")
	}
	if p.Name == "codex" {
		b.WriteString("If this client cannot run the OAuth flow, fall back to: eshu mcp setup --platform codex --auth token\n")
	}
}

// writeSharedKeyGuidance renders the shared-key posture's guidance: the
// admin/dev caveat, always shown because this posture is never the default
// and only ever selected by an explicit --auth shared-key or --shared-key.
func writeSharedKeyGuidance(b *strings.Builder, p *mcpPlatform) {
	b.WriteString("\nWARNING: the shared " + apiKeyEnvVar + " is an admin/dev credential: full AllScopes\n")
	b.WriteString("access with no user attribution. Use it for bootstrap and break-glass only.\n")
	b.WriteString("Per-user tokens (the default) or SSO are the supported paths for engineers.\n")
	b.WriteString("Export the token before launching the client:\n")
	writeExportOrInjectNote(b, p, apiKeyEnvVar)
}

// writeExportOrInjectNote writes the export line for platforms that support
// env-var references in their config, or the out-of-band injection note for
// platforms that do not.
func writeExportOrInjectNote(b *strings.Builder, p *mcpPlatform, envVar string) {
	if !p.SupportsEnvVarToken {
		fmt.Fprintf(b, "This client cannot reference env vars in config; inject the %s bearer token through your secret manager, not the file.\n", envVar)
		return
	}
	fmt.Fprintf(b, "  export %s=...\n", envVar)
}
