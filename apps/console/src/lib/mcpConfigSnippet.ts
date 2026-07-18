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
export const mcpApiKeyEnvVar = "ESHU_API_KEY";

// buildMcpConfigSnippet renders the generic mcpServers-shape JSON snippet for
// serviceUrl (the origin the console itself is talking to). A blank or
// unparsable serviceUrl falls back to a placeholder host so the snippet is
// always valid JSON the caller can paste and then edit.
export function buildMcpConfigSnippet(serviceUrl: string): string {
  const base = normalizedServiceUrl(serviceUrl);
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

function normalizedServiceUrl(serviceUrl: string): string {
  const trimmed = serviceUrl.trim().replace(/\/+$/, "");
  return trimmed.length > 0 ? trimmed : "https://your-eshu-host";
}
