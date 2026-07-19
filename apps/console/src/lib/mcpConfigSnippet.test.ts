// lib/mcpConfigSnippet.test.ts
// Regression guard for the codex P1: the MCP client config snippet must never
// silently point at a wrong-but-plausible host. buildMcpConfigSnippet takes
// only the actual configured MCP endpoint host — callers (TokenRevealPanel)
// must never derive that argument from window.location.origin, since the
// console UI and the MCP server are separate endpoints. When no configured
// host is supplied, the snippet must fall back to an unmistakable
// placeholder rather than emit a concrete-but-wrong URL.
import { describe, expect, it } from "vitest";

import { buildMcpConfigSnippet, mcpApiKeyEnvVar } from "./mcpConfigSnippet";

interface McpConfigDoc {
  readonly mcpServers: {
    readonly eshu: {
      readonly type: string;
      readonly url: string;
      readonly headers: {
        readonly Authorization: string;
      };
    };
  };
}

function parseSnippet(mcpEndpointHost: string): McpConfigDoc {
  return JSON.parse(buildMcpConfigSnippet(mcpEndpointHost)) as McpConfigDoc;
}

describe("buildMcpConfigSnippet", () => {
  it("falls back to an unmistakable placeholder host when no endpoint is configured", () => {
    const snippet = parseSnippet("");
    expect(snippet.mcpServers.eshu.url).toBe("https://YOUR-ESHU-MCP-HOST/mcp/message");
  });

  it("never falls back to a console-origin-shaped host such as localhost:5174", () => {
    // A console dev-server origin is exactly the wrong default (see the P1):
    // the snippet must not resemble it even by coincidence.
    const snippet = parseSnippet("");
    expect(snippet.mcpServers.eshu.url).not.toContain("localhost");
    expect(snippet.mcpServers.eshu.url).not.toContain("5174");
  });

  it("uses an explicitly configured endpoint host when one is supplied", () => {
    const snippet = parseSnippet("https://eshu.example.com");
    expect(snippet.mcpServers.eshu.url).toBe("https://eshu.example.com/mcp/message");
  });

  it("trims trailing slashes from a supplied host", () => {
    const snippet = parseSnippet("https://eshu.example.com/");
    expect(snippet.mcpServers.eshu.url).toBe("https://eshu.example.com/mcp/message");
  });

  it("references the API key env var instead of embedding a raw token", () => {
    const snippet = parseSnippet("https://eshu.example.com");
    expect(snippet.mcpServers.eshu.headers.Authorization).toBe(`Bearer \${${mcpApiKeyEnvVar}}`);
  });
});
