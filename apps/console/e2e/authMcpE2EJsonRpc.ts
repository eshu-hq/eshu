// authMcpE2EJsonRpc.ts — minimal JSON-RPC 2.0 client for the MCP HTTP
// transport (POST /mcp/message), shared by every shape module in the F-9
// (issue #5170) MCP-identity E2E suite. No SSE session is ever established:
// POST /mcp/message without a sessionId query param works standalone
// (go/internal/mcp/server_sse.go's handleHTTPMessage doc comment), which is
// all this suite's request/response-shaped probes need — a real client would
// pair this with GET /sse for server-initiated notifications, but this suite
// tests transport auth/authorization, not the notification channel.
export interface McpJsonRpcResult {
  readonly httpStatus: number;
  readonly wwwAuthenticate: string | null;
  readonly bodyText: string;
  readonly json: Record<string, unknown> | null;
}

// callMcpMessage POSTs one JSON-RPC request to `${mcpBase}/mcp/message`,
// optionally with a bearer credential. It never throws on a non-2xx HTTP
// status — every shape/negative assertion in this suite needs to inspect the
// exact status code and WWW-Authenticate header of a 401, so those are
// returned as data, not raised as errors.
export async function callMcpMessage(
  mcpBase: string,
  method: string,
  params: Record<string, unknown> | undefined,
  credential: string | undefined,
  id = 1,
): Promise<McpJsonRpcResult> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (credential !== undefined) {
    headers.Authorization = `Bearer ${credential}`;
  }
  const body: Record<string, unknown> = { jsonrpc: "2.0", id, method };
  if (params !== undefined) {
    body.params = params;
  }
  const res = await fetch(`${mcpBase}/mcp/message`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
  const bodyText = await res.text();
  let json: Record<string, unknown> | null = null;
  try {
    json = bodyText.length > 0 ? (JSON.parse(bodyText) as Record<string, unknown>) : null;
  } catch {
    json = null;
  }
  return {
    httpStatus: res.status,
    wwwAuthenticate: res.headers.get("WWW-Authenticate"),
    bodyText,
    json,
  };
}

// mcpInitialize/mcpToolsList/mcpToolsCall are thin, named wrappers so call
// sites read like the MCP method they exercise rather than a bare string.
export const mcpInitialize = (mcpBase: string, credential?: string): Promise<McpJsonRpcResult> =>
  callMcpMessage(mcpBase, "initialize", {}, credential, 1);

export const mcpPing = (mcpBase: string, credential?: string): Promise<McpJsonRpcResult> =>
  callMcpMessage(mcpBase, "ping", undefined, credential, 1);

export const mcpToolsList = (mcpBase: string, credential?: string): Promise<McpJsonRpcResult> =>
  callMcpMessage(mcpBase, "tools/list", {}, credential, 2);

export const mcpToolsCall = (
  mcpBase: string,
  toolName: string,
  toolArgs: Record<string, unknown>,
  credential?: string,
): Promise<McpJsonRpcResult> =>
  callMcpMessage(mcpBase, "tools/call", { name: toolName, arguments: toolArgs }, credential, 3);

// extractToolCallText pulls the first text content block out of a
// tools/call JSON-RPC success result (the MCP content[] envelope every tool
// in this codebase returns — see go/internal/mcp/dispatch_*.go call sites),
// throwing with the raw body attached when the shape does not match so a
// parsing regression fails loudly instead of silently returning "". This is
// a human-readable SUMMARY line (server.go's summarizePlainToolText /
// summarizeToolText), NOT the tool's JSON payload — use
// extractToolCallStructuredContent for the actual data.
export function extractToolCallText(result: McpJsonRpcResult): string {
  const resultObj = result.json?.result as { content?: readonly { type?: string; text?: string }[] } | undefined;
  const first = resultObj?.content?.find((block) => block.type === "text");
  if (!first || typeof first.text !== "string") {
    throw new Error(`tools/call result missing text content block: ${result.bodyText}`);
  }
  return first.text;
}

// extractToolCallStructuredContent returns a tools/call result's
// StructuredContent field — the tool's actual JSON payload, already decoded
// (go/internal/mcp/server.go's "tools/call" case sets this on every success
// response alongside the human-readable text summary and the "resource"
// content block carrying the same value as a JSON string). Throws with the
// raw body attached when the field is absent, so a parsing regression fails
// loudly instead of silently returning undefined.
export function extractToolCallStructuredContent(result: McpJsonRpcResult): unknown {
  const resultObj = result.json?.result as { structuredContent?: unknown } | undefined;
  if (resultObj === undefined || !("structuredContent" in resultObj)) {
    throw new Error(`tools/call result missing structuredContent: ${result.bodyText}`);
  }
  return resultObj.structuredContent;
}
