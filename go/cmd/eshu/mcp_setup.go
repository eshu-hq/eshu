// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strings"
)

// mcpServerKey is the server name Eshu registers in every MCP client config.
const mcpServerKey = "eshu"

// apiKeyEnvVar is the environment variable name an MCP client should reference
// for the legacy shared admin/dev bearer token (shared-key posture). Setup
// emits this reference instead of the raw secret whenever the target client
// supports env-var interpolation.
const apiKeyEnvVar = "ESHU_API_KEY" // #nosec G101 -- apiKeyEnvVar holds an env-var name, not a secret value

// mcpTokenEnvVar is the per-user bearer token env var emitted in token
// posture (the default): a personal token issued from the console (Profile ->
// API tokens, issue #5164), scoped to that engineer's own grants rather than
// the shared admin/dev key. See docs/public/mcp/index.md for the documented
// client-side contract this name already establishes (issue #5169, F-8).
const mcpTokenEnvVar = "ESHU_MCP_TOKEN" // #nosec G101 -- mcpTokenEnvVar holds an env-var name, not a secret value

// mcpSetupMode selects between local stdio launch and hosted HTTP transport.
type mcpSetupMode int

const (
	// modeLocalStdio configures the client to spawn `eshu mcp start` over stdio.
	modeLocalStdio mcpSetupMode = iota
	// modeHostedHTTP configures the client to reach a hosted HTTP MCP endpoint
	// using an env-var reference for the bearer token when supported.
	modeHostedHTTP
)

// mcpSetupRequest carries the resolved inputs for snippet generation.
type mcpSetupRequest struct {
	// Mode picks local stdio or hosted HTTP transport.
	Mode mcpSetupMode
	// ServiceURL is the hosted MCP endpoint base, used only in hosted mode.
	ServiceURL string
	// APIKey is the resolved legacy shared bearer token (shared-key posture
	// only). It is never embedded when the platform supports env-var
	// references; it is only used to decide whether a token is present and,
	// for clients without env-var support, to mask it. Token and SSO posture
	// never read this field: neither carries a raw secret through the CLI.
	APIKey string
	// Posture selects token (default), SSO, or shared-key credential wiring
	// for hosted snippets. The zero value is postureToken, deliberately: any
	// call site that forgets to set it (including old tests) gets the new
	// safe default, not the shared key.
	Posture mcpAuthPosture
	// Issuers names the IdP(s) advertised by the discovery probe
	// (authorization_servers). Feeds SSO-posture guidance notes only.
	Issuers []string
	// PreregisteredClientID is the eshu_preregistered_client_id advertised by
	// the discovery probe, when the deployment has one configured. Feeds
	// SSO-posture guidance notes only.
	PreregisteredClientID string
}

// mcpPlatform describes how one MCP client is configured.
type mcpPlatform struct {
	// Name is the canonical platform identifier accepted by --platform.
	Name string
	// Aliases are alternate accepted spellings for Name.
	Aliases []string
	// DisplayName is the human-facing client name.
	DisplayName string
	// TargetFile describes where the snippet belongs (shown to the user). It is
	// a path hint, not necessarily a path this tool writes to.
	TargetFile string
	// SupportsEnvVarToken reports whether the client interpolates ${ENV} style
	// references inside its config. When false, hosted setup must not embed the
	// raw token and instead instructs the operator to inject it out of band.
	SupportsEnvVarToken bool
	// Writable reports whether --write has a safe, implemented merge target for
	// this platform.
	Writable bool
	// snippet renders the platform-specific config text for a request.
	snippet func(req mcpSetupRequest) (string, error)
	// notes are short post-snippet guidance lines.
	notes []string
}

// mcpPlatformRegistry returns the supported platform table keyed by canonical
// name. The registry is the single source of truth for snippet shape, target
// file, and token-handling capability.
func mcpPlatformRegistry() map[string]*mcpPlatform {
	platforms := []*mcpPlatform{
		{
			Name:                "claude",
			Aliases:             []string{"claude-code", "claudecode"},
			DisplayName:         "Claude Code",
			TargetFile:          ".mcp.json (project) or ~/.claude.json (user)",
			SupportsEnvVarToken: true,
			Writable:            true,
			snippet:             mcpServersJSONSnippet,
			notes: []string{
				"Project scope: commit .mcp.json at the repository root.",
				"User scope: merge under \"mcpServers\" in ~/.claude.json.",
			},
		},
		{
			Name:                "cursor",
			DisplayName:         "Cursor",
			TargetFile:          ".cursor/mcp.json (project) or ~/.cursor/mcp.json (global)",
			SupportsEnvVarToken: true,
			Writable:            true,
			snippet:             mcpServersJSONSnippet,
			notes: []string{
				"Project scope: .cursor/mcp.json. Global scope: ~/.cursor/mcp.json.",
			},
		},
		{
			Name:                "vscode",
			Aliases:             []string{"vs-code", "code"},
			DisplayName:         "VS Code",
			TargetFile:          ".vscode/mcp.json (workspace)",
			SupportsEnvVarToken: true,
			Writable:            true,
			snippet:             vscodeJSONSnippet,
			notes: []string{
				"VS Code expects servers under a top-level \"servers\" key in .vscode/mcp.json.",
				"Use ${input:...} or ${env:...} for secrets; never commit a raw token.",
			},
		},
		{
			Name:                "codex",
			Aliases:             []string{"codex-cli"},
			DisplayName:         "Codex CLI",
			TargetFile:          "~/.codex/config.toml",
			SupportsEnvVarToken: true,
			Writable:            false,
			snippet:             codexTOMLSnippet,
			notes: []string{
				"Add the block under [mcp_servers.eshu] in ~/.codex/config.toml.",
				"Repo-local .mcp.json is not enough for Codex; configure the active Codex MCP config.",
			},
		},
		{
			Name:                "generic",
			Aliases:             []string{"json", "mcp"},
			DisplayName:         "Generic MCP JSON",
			TargetFile:          "your MCP client's mcpServers configuration",
			SupportsEnvVarToken: true,
			Writable:            false,
			snippet:             mcpServersJSONSnippet,
			notes: []string{
				"Most MCP clients accept this \"mcpServers\" object; place it where your client expects.",
			},
		},
	}

	registry := make(map[string]*mcpPlatform, len(platforms))
	for _, p := range platforms {
		registry[p.Name] = p
		for _, alias := range p.Aliases {
			registry[alias] = p
		}
	}
	return registry
}

// supportedPlatformNames returns the sorted canonical platform names for error
// messages and help output.
func supportedPlatformNames() []string {
	seen := make(map[string]struct{})
	for _, p := range mcpPlatformRegistry() {
		seen[p.Name] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolvePlatform looks up a platform by canonical name or alias. Lookup is
// case-insensitive. An unknown platform yields an error listing supported
// names.
func resolvePlatform(raw string) (*mcpPlatform, error) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		key = "generic"
	}
	registry := mcpPlatformRegistry()
	if p, ok := registry[key]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("unsupported platform %q: supported platforms are %s",
		raw, strings.Join(supportedPlatformNames(), ", "))
}

// redactToken masks a bearer token for display. It never returns the raw token.
// A short token is fully masked; a longer token keeps a 4-character prefix so an
// operator can recognize which key is configured without exposing it.
func redactToken(token string) string {
	t := strings.TrimSpace(token)
	if t == "" {
		return ""
	}
	if len(t) <= 8 {
		return strings.Repeat("*", len(t))
	}
	return t[:4] + strings.Repeat("*", len(t)-4)
}

// tokenReference returns the value to embed for a hosted bearer token
// referenced by envVar (apiKeyEnvVar for shared-key posture, mcpTokenEnvVar
// for token posture). When the platform supports env-var references it
// returns the ${envVar} reference and never the secret. Otherwise it returns
// a masked placeholder built from secret (when known) and the caller emits
// out-of-band injection guidance. secret is empty for token posture, which
// never resolves a raw personal token through the CLI in the first place.
func tokenReference(p *mcpPlatform, envVar, secret string) string {
	if p.SupportsEnvVarToken {
		return "${" + envVar + "}"
	}
	if strings.TrimSpace(secret) == "" {
		return "<set " + envVar + " out of band>"
	}
	return redactToken(secret)
}
