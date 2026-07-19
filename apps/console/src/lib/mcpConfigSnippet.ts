// lib/mcpConfigSnippet.ts — builds a ready-to-paste MCP client config for a
// freshly created API token (issue #5164). Mirrors the shape
// `eshu mcp setup --hosted` already prints
// (go/cmd/eshu/mcp_setup_snippet.go hostedServerEntry/mcpServersJSONSnippet):
// the standard { "mcpServers": { "eshu": {...} } } object, an "http" entry
// pointed at /mcp/message, and — critically — the Authorization header
// references the ${ESHU_API_KEY} env var rather than embedding the raw
// bearer token. The console never writes the token itself into this
// snippet; TokenRevealPanel shows the raw token separately, once, for the
// caller to export as that env var.
//
// mcpEndpointHost MUST NOT default to the console's own origin
// (window.location.origin): the console UI and the MCP server are separate
// endpoints (the Vite dev proxy only forwards /eshu-api, not /mcp/message,
// and in production the console origin is not guaranteed to route MCP
// traffic either), so a console-origin default would silently hand the
// caller a snippet that looks valid but resolves to the wrong service. There
// is currently no console runtime config (import.meta.env or otherwise) that
// carries the deployment's real MCP endpoint, so callers without one get an
// unmistakable fill-in placeholder instead of a concrete-but-wrong URL.
export const mcpApiKeyEnvVar = "ESHU_API_KEY";

// placeholderMcpHost is the fill-in-the-blank host used when no configured
// MCP endpoint is available. It is deliberately shaped so it can never be
// mistaken for a real, reachable host.
const placeholderMcpHost = "https://YOUR-ESHU-MCP-HOST";

// buildMcpConfigSnippet renders the generic mcpServers-shape JSON snippet for
// mcpEndpointHost (the actual configured MCP endpoint host, if the caller has
// one). A blank or unparsable mcpEndpointHost falls back to
// placeholderMcpHost so the snippet is always valid JSON, and never points at
// a host the caller did not explicitly configure.
export function buildMcpConfigSnippet(mcpEndpointHost: string): string {
  const base = normalizedMcpEndpointHost(mcpEndpointHost);
  const doc = {
    mcpServers: {
      eshu: {
        type: "http",
        url: `${base}/mcp/message`,
        headers: {
          Authorization: `Bearer \${${mcpApiKeyEnvVar}}`,
        },
      },
    },
  };
  return JSON.stringify(doc, null, 2);
}

function normalizedMcpEndpointHost(mcpEndpointHost: string): string {
  const trimmed = mcpEndpointHost.trim().replace(/\/+$/, "");
  return trimmed.length > 0 ? trimmed : placeholderMcpHost;
}
