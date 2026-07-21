// authMcpE2EOauthClient.ts — a pure-fetch, scripted RFC 9728 / RFC 6749 OAuth
// client for the MCP-identity E2E suite (F-9, issue #5170, design §2 and
// §4 shape B item 4). MCP OAuth is a machine flow (no browser consent
// screen), so this drives the exact sequence a real MCP client performs:
//
//   1. POST {mcp}/mcp/message (initialize), no credential -> 401, parse the
//      WWW-Authenticate challenge's resource_metadata/scope directives.
//   2. GET the metadata URL -> RFC 9728 protected-resource document.
//   3. GET {issuer}/.well-known/openid-configuration -> authorize/token URLs.
//   4. GET {authorize}?...&code_challenge=<S256>&resource=... with
//      redirect:"manual" -> parse `code` from the Location header (the mock
//      IdP auto-approves with no login UI — see go/cmd/mock-oidc-idp).
//   5. POST {token} with the code + code_verifier (+ resource) -> JWT access
//      token (go/cmd/mock-oidc-idp's RFC 8707 resource-triggered mint, F-9
//      step 1; PKCE verified per RFC 7636, F-9 step 6).
//   6. POST {mcp}/mcp/message tools/call with the bearer token.
//
// PKCE (RFC 7636, S256) is exercised for real, not skipped: `eshu mcp
// setup`'s SSO snippets advertise Code+PKCE (design §9 decision 3), and the
// mock IdP now verifies it (go/cmd/mock-oidc-idp/server.go's verifyPKCE).
//
// Host-vs-network URL mapping: the RFC 9728 document's authorization_servers
// entry and the discovery document's authorize/token endpoints are all the
// in-network Compose hostname ("http://mock-oidc-idp:8080") — correct for
// the eshu/mcp-server containers, unreachable from this host-side Node
// process. rewriteInNetworkHost applies the same reachability mapping
// authE2EOidcFlow.ts's chromiumLaunchArgs gives the browser via
// --host-resolver-rules, just implemented as a URL string rewrite instead of
// a Chromium flag (a plain fetch() has no host-resolver-rules equivalent).
import { createHash, randomBytes } from "node:crypto";

import { callMcpMessage, extractToolCallStructuredContent, mcpInitialize, mcpToolsCall } from "./authMcpE2EJsonRpc.ts";

export interface HostRewriteTable {
  readonly [inNetworkHostPort: string]: string;
}

// rewriteInNetworkHost replaces url's host:port with its host-reachable
// mapping from table, when present; otherwise returns url unchanged (already
// host-reachable, e.g. the mcp base itself).
export function rewriteInNetworkHost(url: string, table: HostRewriteTable): string {
  const parsed = new URL(url);
  const hostPort = parsed.host;
  const mapped = table[hostPort];
  if (mapped === undefined) {
    return url;
  }
  parsed.host = mapped;
  return parsed.toString();
}

export interface ParsedChallenge {
  readonly resourceMetadata: string | null;
  readonly scope: string | null;
}

// parseWWWAuthenticateChallenge extracts the RFC 9728 resource_metadata and
// RFC 6750 scope directives from a WWW-Authenticate header value built by
// go/internal/query/auth_oauth_challenge_context.go's
// oauthWWWAuthenticateChallenge (`Bearer resource_metadata="...", scope="..."`
// or the bare `Bearer`). Returns nulls for a bare challenge.
export function parseWWWAuthenticateChallenge(header: string): ParsedChallenge {
  const resourceMetadataMatch = /resource_metadata="([^"]*)"/.exec(header);
  const scopeMatch = /scope="([^"]*)"/.exec(header);
  return {
    resourceMetadata: resourceMetadataMatch ? resourceMetadataMatch[1]! : null,
    scope: scopeMatch ? scopeMatch[1]! : null,
  };
}

// pkcePair generates a real RFC 7636 S256 code_verifier/code_challenge pair
// using Node's own crypto — the same shape a real MCP OAuth client produces,
// not a fixed test fixture.
export function pkcePair(): { readonly verifier: string; readonly challenge: string } {
  const verifier = randomBytes(32).toString("base64url");
  const challenge = createHash("sha256").update(verifier).digest("base64url");
  return { verifier, challenge };
}

export interface OAuthProtectedResourceMetadata {
  readonly resource: string;
  readonly authorization_servers?: readonly string[];
  readonly bearer_methods_supported?: readonly string[];
  readonly scopes_supported?: readonly string[];
}

export interface OidcDiscoveryDocument {
  readonly issuer: string;
  readonly authorization_endpoint: string;
  readonly token_endpoint: string;
}

export interface ScriptedOAuthResult {
  readonly accessToken: string;
  readonly metadata: OAuthProtectedResourceMetadata;
  readonly discovery: OidcDiscoveryDocument;
  readonly challengeScope: string;
}

// runScriptedOAuthClient drives steps 1-5 above end to end and returns the
// minted JWT access token plus the intermediate documents (so callers can
// assert on them without re-fetching). Throws with the failing step named in
// the message on any unexpected status/shape.
export async function runScriptedOAuthClient(
  mcpBase: string,
  expectedResourceURI: string,
  hostRewrite: HostRewriteTable,
): Promise<ScriptedOAuthResult> {
  // Step 1: credential-less initialize -> 401 with the OAuth challenge.
  const unauthed = await mcpInitialize(mcpBase, undefined);
  if (unauthed.httpStatus !== 401) {
    throw new Error(`scripted-oauth step1: expected 401 from credential-less initialize, got ${unauthed.httpStatus}`);
  }
  if (unauthed.wwwAuthenticate === null) {
    throw new Error("scripted-oauth step1: 401 response carries no WWW-Authenticate header");
  }
  const challenge = parseWWWAuthenticateChallenge(unauthed.wwwAuthenticate);
  if (!challenge.resourceMetadata) {
    throw new Error(
      `scripted-oauth step1: WWW-Authenticate carried no resource_metadata directive: ${unauthed.wwwAuthenticate}`,
    );
  }

  // Step 2: fetch the RFC 9728 protected-resource metadata document.
  const metadataURL = rewriteInNetworkHost(challenge.resourceMetadata, hostRewrite);
  const metadataRes = await fetch(metadataURL);
  if (!metadataRes.ok) {
    throw new Error(`scripted-oauth step2: GET ${metadataURL} returned ${metadataRes.status}`);
  }
  const metadata = (await metadataRes.json()) as OAuthProtectedResourceMetadata;
  if (metadata.resource !== expectedResourceURI) {
    throw new Error(
      `scripted-oauth step2: metadata resource = ${metadata.resource}, want ${expectedResourceURI}`,
    );
  }
  const issuer = metadata.authorization_servers?.[0];
  if (!issuer) {
    throw new Error(`scripted-oauth step2: metadata carries no authorization_servers: ${JSON.stringify(metadata)}`);
  }

  // Step 3: fetch the issuer's OIDC discovery document.
  const discoveryURL = rewriteInNetworkHost(`${issuer}/.well-known/openid-configuration`, hostRewrite);
  const discoveryRes = await fetch(discoveryURL);
  if (!discoveryRes.ok) {
    throw new Error(`scripted-oauth step3: GET ${discoveryURL} returned ${discoveryRes.status}`);
  }
  const discovery = (await discoveryRes.json()) as OidcDiscoveryDocument;

  // Step 4: authorize with PKCE (S256) and the resource indicator, following
  // NO redirect (the mock IdP auto-approves with a 302 carrying `code`).
  const { verifier, challenge: pkceChallenge } = pkcePair();
  const redirectURI = "http://127.0.0.1:0/mcp-oauth-client-callback";
  const authorizeURL = new URL(rewriteInNetworkHost(discovery.authorization_endpoint, hostRewrite));
  authorizeURL.searchParams.set("response_type", "code");
  authorizeURL.searchParams.set("client_id", "eshu-mcp-e2e-scripted-client");
  authorizeURL.searchParams.set("redirect_uri", redirectURI);
  authorizeURL.searchParams.set("scope", challenge.scope ?? "");
  authorizeURL.searchParams.set("resource", metadata.resource);
  authorizeURL.searchParams.set("code_challenge", pkceChallenge);
  authorizeURL.searchParams.set("code_challenge_method", "S256");
  const authorizeRes = await fetch(authorizeURL, { redirect: "manual" });
  if (authorizeRes.status !== 302 && authorizeRes.status !== 303) {
    throw new Error(`scripted-oauth step4: GET ${authorizeURL} expected a redirect, got ${authorizeRes.status}`);
  }
  const location = authorizeRes.headers.get("Location");
  if (!location) {
    throw new Error("scripted-oauth step4: authorize redirect carries no Location header");
  }
  const code = new URL(location, redirectURI).searchParams.get("code");
  if (!code) {
    throw new Error(`scripted-oauth step4: redirect Location carries no code: ${location}`);
  }

  // Step 5: exchange the code (+ code_verifier, + resource) for a token.
  const tokenURL = rewriteInNetworkHost(discovery.token_endpoint, hostRewrite);
  const tokenRes = await fetch(tokenURL, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: redirectURI,
      code_verifier: verifier,
      resource: metadata.resource,
      client_id: "eshu-mcp-e2e-scripted-client",
    }),
  });
  if (!tokenRes.ok) {
    throw new Error(`scripted-oauth step5: POST ${tokenURL} returned ${tokenRes.status}: ${await tokenRes.text()}`);
  }
  const tokenBody = (await tokenRes.json()) as { access_token?: string; token_type?: string };
  if (!tokenBody.access_token) {
    throw new Error(`scripted-oauth step5: token response carries no access_token: ${JSON.stringify(tokenBody)}`);
  }

  return {
    accessToken: tokenBody.access_token,
    metadata,
    discovery,
    challengeScope: challenge.scope ?? "",
  };
}

// assertScriptedTokenWorksAgainstMcp drives step 6: a tools/call authenticated
// with the scripted client's minted bearer token must succeed (200) and
// return real structured content — proving the OAuth-minted token is a
// genuinely accepted MCP credential, not merely that the flow completed.
export async function assertScriptedTokenWorksAgainstMcp(mcpBase: string, accessToken: string): Promise<string> {
  const call = await mcpToolsCall(mcpBase, "list_indexed_repositories", { limit: 10, offset: 0 }, accessToken);
  if (call.httpStatus !== 200) {
    throw new Error(`tools/call with the scripted OAuth token expected 200, got ${call.httpStatus}: ${call.bodyText}`);
  }
  const parsed = extractToolCallStructuredContent(call) as { total?: number };
  return `tools/call succeeded with the scripted-OAuth-minted bearer token (total=${parsed.total ?? 0})`;
}

export { callMcpMessage };
