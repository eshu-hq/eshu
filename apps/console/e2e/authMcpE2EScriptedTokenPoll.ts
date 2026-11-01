// authMcpE2EScriptedTokenPoll.ts — the fail-closed, convergence-tolerant proof
// that the scripted-OAuth-minted JWT is a genuinely accepted MCP credential
// (shape B step shapeB_scripted_oauth_chain_tools_call).
//
// Why a poll: core's oidcbearer provider snapshot is TTL-based
// (go/internal/oidcbearer/resolver.go defaultTTL, 30s) with no invalidation
// hook. The OIDC provider is created through the admin drawer earlier in shape
// B, so the first JWT tools/call can hit `unknown_issuer` and surface as an
// `unauthenticated` envelope error until core refreshes its snapshot. Discovery
// went static (eshu-hq/eshu#9731), which removed the old
// pollForDiscoveryEnabled wait that used to absorb this skew.
//
// Why it must fail closed: the previous check read `structuredContent.total`
// (the real field is `structuredContent.data.total`), so it printed `total=0`
// for a success AND for an `unauthenticated` error envelope. Every verdict here
// goes through repositoryListFromEnvelope, which throws on any envelope error or
// missing `data.repositories` list.
import { extractToolCallStructuredContent, type McpJsonRpcResult } from "./authMcpE2EJsonRpc.ts";
import { repositoryListFromEnvelope } from "./authMcpE2ERepositoryRows.ts";

// The envelope error code that is retried. Core answers every OIDC bearer
// denial with this one code (unknown_issuer, wrong_audience, expired,
// bad_signature, malformed, no_grants, ...; go/internal/query/auth.go and
// querycontract/unauthorized.go), so it is also the stale-snapshot signal but
// cannot be told apart from a permanent denial at this layer. It is retried
// only because the stale snapshot is the transient case; exhausting the budget
// points at a permanent denial, not at lag.
const retriedErrorCode = "unauthenticated";

// scriptedTokenPollTimeoutMs MUST stay above go/internal/oidcbearer's
// defaultTTL (30s): a budget shorter than the snapshot TTL can time out on a
// healthy stack that simply has not refreshed yet. Do not shorten it.
export const scriptedTokenPollTimeoutMs = 60_000;
export const scriptedTokenPollIntervalMs = 2_000;

export type ScriptedTokenVerdict =
  | { readonly kind: "ok"; readonly repositoryCount: number }
  | { readonly kind: "retry"; readonly detail: string };

// envelopeErrorCode returns the envelope's error code when structuredContent is
// an object carrying `error.code`, else undefined. It only classifies; the
// fail-closed reading stays in repositoryListFromEnvelope.
function envelopeErrorCode(structured: unknown): string | undefined {
  if (structured === null || typeof structured !== "object") {
    return undefined;
  }
  const error = (structured as { error?: unknown }).error;
  if (error === null || typeof error !== "object") {
    return undefined;
  }
  const code = (error as { code?: unknown }).code;
  return typeof code === "string" ? code : undefined;
}

// classifyScriptedTokenCall turns one tools/call result into a verdict. An
// `unauthenticated` envelope error is retried (it may be core's stale provider
// snapshot; see retriedErrorCode). Every
// other problem (HTTP not 200, another error code, a missing or malformed
// payload) throws so it fails the step immediately.
export function classifyScriptedTokenCall(call: McpJsonRpcResult): ScriptedTokenVerdict {
  if (call.httpStatus !== 200) {
    throw new Error(`tools/call with the scripted OAuth token expected 200, got ${call.httpStatus}: ${call.bodyText}`);
  }
  const structured = extractToolCallStructuredContent(call);
  if (envelopeErrorCode(structured) === retriedErrorCode) {
    return { kind: "retry", detail: JSON.stringify((structured as { error: unknown }).error) };
  }
  return { kind: "ok", repositoryCount: repositoryListFromEnvelope(structured).rows.length };
}

export interface ConvergencePollOptions {
  readonly timeoutMs?: number;
  readonly intervalMs?: number;
  // now and sleep are injectable so tests run on a fake clock.
  readonly now?: () => number;
  readonly sleep?: (ms: number) => Promise<void>;
}

const realSleep = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

// pollScriptedTokenWorks calls `call` about every intervalMs until it succeeds,
// fails permanently, or timeoutMs elapses. It returns the step detail with the
// observed convergence time and repository count; on timeout it throws with the
// last observed error.
export async function pollScriptedTokenWorks(
  call: () => Promise<McpJsonRpcResult>,
  options: ConvergencePollOptions = {},
): Promise<string> {
  const timeoutMs = options.timeoutMs ?? scriptedTokenPollTimeoutMs;
  const intervalMs = options.intervalMs ?? scriptedTokenPollIntervalMs;
  const now = options.now ?? Date.now;
  const sleep = options.sleep ?? realSleep;
  const startedAt = now();
  let attempts = 0;
  for (;;) {
    attempts += 1;
    const verdict = classifyScriptedTokenCall(await call());
    const elapsedMs = now() - startedAt;
    if (verdict.kind === "ok") {
      return `tools/call succeeded with the scripted-OAuth-minted bearer token (core converged after ${elapsedMs}ms, ${attempts} attempt(s); repositories=${verdict.repositoryCount})`;
    }
    if (elapsedMs >= timeoutMs) {
      throw new Error(
        `scripted OAuth token still unauthenticated after ${timeoutMs}ms (${attempts} attempt(s)); ` +
          `that outlasts core's 30s provider-snapshot TTL, so suspect a permanent denial ` +
          `(unknown issuer, audience, missing grant mapping), not convergence lag; last observed error: ${verdict.detail}`,
      );
    }
    await sleep(intervalMs);
  }
}
