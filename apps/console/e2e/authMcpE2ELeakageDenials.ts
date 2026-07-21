// authMcpE2ELeakageDenials.ts — the bad-credential distinct-denial matrix for
// the MCP-identity E2E negative-leakage module (F-9, issue #5170, design §5).
//
// Adapted evidence source (coordinator decision, no product change): denial-
// side governance_audit reason_codes DO NOT EXIST — go/internal/query's audit
// only emits an ALLOWED read event (F-9 part 1) and a single static
// scoped_route_not_enabled denial reason, neither of which distinguishes the
// bearer-validation failure classes. So distinct denials are proven from the
// TWO observables the product already emits:
//
//   1. The oidcbearer resolver's structured log outcome
//      (go/internal/oidcbearer/resolver.go deny(): {"msg":"oidc bearer token
//      denied","iss":...,"outcome":...}) — one of unknown_issuer /
//      wrong_audience / expired / bad_signature / malformed / no_grants,
//      carrying only issuer + outcome, never the raw token. Asserted PRESENT
//      and pairwise-distinct across the matrix rows by scraping mcp-server
//      logs.
//   2. The F-2 WWW-Authenticate challenge shape — a PRE-match denial
//      (unknown issuer, unparseable JWT) carries resource_metadata (steer to
//      OAuth discovery); a POST-match denial (wrong audience, expired) stays
//      a bare "Bearer". Asserted per-request, deterministically.
//
// Challenge shape alone cannot separate malformed from unknown_issuer (both
// pre-match -> resource_metadata); the log outcome does. That is exactly why
// both observables are asserted. Denial-side governance audit is a tracked
// follow-up: eshu-hq/eshu#5567.
import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import { collectComposeServiceLogs, parseOidcBearerDenialOutcomes } from "./authMcpE2EGraphSeed.ts";
import { mcpInitialize } from "./authMcpE2EJsonRpc.ts";
import { mintJwtFromMockIdp, rewriteInNetworkHost, type HostRewriteTable } from "./authMcpE2EOauthClient.ts";

export interface DenialMatrixContext {
  readonly mcpBase: string;
  readonly apiBase: string;
  readonly repoRoot: string;
  readonly project: string;
  readonly hostRewrite: HostRewriteTable;
}

const memberOidcIssuer = "http://mock-oidc-idp:8080";
const adminOidcIssuer = "http://mock-oidc-idp-admin:8080";

// runDistinctDenialMatrix sends the bad-credential rows, asserts each 401 with
// the expected challenge shape, then scrapes mcp-server logs to assert the
// resolver emitted the three distinct outcomes. Must run in shape-B state
// (OIDC provider active) — the oidcbearer resolver only engages, logs, and
// steers to discovery once at least one bearer issuer is active; before that
// every credential falls through to a bare 401 (verified live).
export async function runDistinctDenialMatrix(step: AuthE2EStep, ctx: DenialMatrixContext): Promise<void> {
  await step("leakage_denial_wrong_audience_bare_challenge", async () => {
    const token = await mintJwtFromMockIdp(rewriteInNetworkHost(memberOidcIssuer, ctx.hostRewrite), "https://wrong.example");
    const res = await mcpInitialize(ctx.mcpBase, token);
    assert401(res.httpStatus, "wrong-audience JWT");
    assertBareChallenge(res.wwwAuthenticate, "wrong-audience (post-match) denial");
    return "wrong-audience JWT -> 401 bare Bearer (post-match denial)";
  });

  await step("leakage_denial_unknown_issuer_resource_metadata_challenge", async () => {
    const token = await mintJwtFromMockIdp(rewriteInNetworkHost(adminOidcIssuer, ctx.hostRewrite), ctx.mcpBase);
    const res = await mcpInitialize(ctx.mcpBase, token);
    assert401(res.httpStatus, "unknown-issuer JWT");
    assertResourceMetadataChallenge(res.wwwAuthenticate, "unknown-issuer (pre-match) denial");
    return "unknown-issuer JWT -> 401 resource_metadata (pre-match denial)";
  });

  await step("leakage_denial_malformed_resource_metadata_challenge", async () => {
    // A JWT-SHAPED (X.Y.Z, per oidcbearer.isJWTShaped) but unparseable
    // credential: the resolver engages, fails to read an issuer, and denies
    // with outcome=malformed AND unrecognized=true -> resource_metadata.
    const res = await mcpInitialize(ctx.mcpBase, "aaaa.bbbb.cccc");
    assert401(res.httpStatus, "malformed JWT");
    assertResourceMetadataChallenge(res.wwwAuthenticate, "malformed (pre-parse) denial");
    return "malformed JWT-shaped credential -> 401 resource_metadata (pre-parse denial)";
  });

  await step("leakage_denial_outcomes_are_distinct", async () => {
    const logs = await collectComposeServiceLogs(ctx.repoRoot, ctx.project, "mcp-server");
    if (logs.trim().length === 0) {
      throw new Error("mcp-server logs were empty — cannot verify denial outcomes");
    }
    const outcomes = parseOidcBearerDenialOutcomes(logs);
    const expected = ["unknown_issuer", "wrong_audience", "malformed"];
    const missing = expected.filter((o) => !outcomes.has(o));
    if (missing.length > 0) {
      throw new Error(
        `oidcbearer denial outcomes missing from mcp-server logs: ${missing.join(", ")}; observed: [${[...outcomes].join(", ")}]`,
      );
    }
    // Distinctness is inherent (a Set), but assert the count so a future
    // regression that collapses two classes onto one outcome fails loudly.
    if (expected.filter((o) => outcomes.has(o)).length !== 3) {
      throw new Error(`expected 3 distinct denial outcomes, observed [${[...outcomes].join(", ")}]`);
    }
    return `3 distinct oidcbearer denial outcomes observed in mcp-server logs: [${expected.join(", ")}] (subset of ${outcomes.size} total)`;
  });
}

function assert401(status: number, label: string): void {
  if (status !== 401) {
    throw new Error(`${label} expected 401, got ${status}`);
  }
}

function assertBareChallenge(header: string | null, label: string): void {
  if (header !== "Bearer") {
    throw new Error(`${label}: expected bare "Bearer" challenge, got: ${header}`);
  }
}

function assertResourceMetadataChallenge(header: string | null, label: string): void {
  if (header === null || !header.includes("resource_metadata=")) {
    throw new Error(`${label}: expected a resource_metadata challenge, got: ${header}`);
  }
}
