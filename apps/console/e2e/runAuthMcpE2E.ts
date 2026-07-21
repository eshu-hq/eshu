// MCP-identity E2E runner (F-9, issue #5170, epic #5161's E2E proof gate).
//
// A SIBLING of runAuthE2E.ts (issue #4971), not an extension of it — see
// /private/tmp/.../f9-design.md §1 for the full rationale. This runner drives
// a real browser AND a scripted RFC 9728 OAuth client against a freshly
// booted docker-compose.e2e.yaml stack (with mcp-server + mock-github
// additionally started — scripts/run-auth-mcp-e2e.sh owns that lifecycle,
// mirroring run-auth-e2e.sh's "always fresh, always torn down" contract) to
// prove the identity story specifically on the MCP HTTP transport
// (GET /sse, POST /mcp/message) across three sequential org shapes:
//
//   Shape A (token-only)   -> apps/console/e2e/authMcpE2ETokenFlow.ts
//   Shape C (GitHub, next) -> apps/console/e2e/authMcpE2EGithubFlow.ts
//   Shape B (OIDC, last)   -> apps/console/e2e/authMcpE2EShapeB.ts +
//                             authMcpE2EOauthClient.ts (scripted RFC 9728/PKCE)
//   Negative-leakage       -> apps/console/e2e/authMcpE2ELeakage.ts (runs last)
//
// Phase order is load-bearing (design §1): A needs zero providers; C's
// GitHub-only provider must NOT enable discovery (asserted before B's OIDC
// provider does); B ends with the require_sso flip; the leakage module runs
// last (it needs the OIDC provider active and both credential types).
//
// ESHU_E2E_MCP_MODULE selects a single module to run — "shapeA", "shapeC",
// "shapeB", or "leakage" (a lone module still needs the earlier shapes' state,
// so this is mainly for iterative debugging), or the standalone
// "credentialless" fast path (no browser/wizard) that
// scripts/verify-auth-mcp-e2e-sensitivity.sh (step 8) drives against a mutated
// mcp-server. Empty/unset runs every module.
import { mkdir, rm, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, type Browser } from "playwright";

import {
  buildPostgresDSN,
  e2eDefaultAuthSecretEncKey,
  retrieveInitialCredential,
} from "./authE2ECredential.ts";
import { startAuthE2EDevServer, stopAuthE2EDevServer, type AuthE2EDevServer } from "./authE2EDevServer.ts";
import { assertFreshStackShowsSetupWizard, driveSetupWizard } from "./authE2ESetupWizard.ts";
import { recordAuthE2EStep, type StepResult } from "./authE2EStepRecorder.ts";
import { chromiumLaunchArgsWithGithub, runShapeC } from "./authMcpE2EGithubFlow.ts";
import { SEEDED_REPOSITORY_ID, seedGraphRepository } from "./authMcpE2EGraphSeed.ts";
import { assertCredentialLessProbesDoNotLeak, runLeakageSuite } from "./authMcpE2ELeakage.ts";
import type { HostRewriteTable } from "./authMcpE2EOauthClient.ts";
import { runShapeB, type ShapeBContext } from "./authMcpE2EShapeB.ts";
import { runShapeA, type ShapeAContext } from "./authMcpE2ETokenFlow.ts";

const here = dirname(fileURLToPath(import.meta.url));
const consoleDir = resolve(here, "..");
const repoRoot = resolve(consoleDir, "..", "..");
const repoGoDir = resolve(repoRoot, "go");
const artifactsDir = resolve(repoRoot, "e2e-artifacts");
const screenshotsDir = resolve(artifactsDir, "auth-mcp-e2e-screenshots");
const reportPath = resolve(artifactsDir, "auth-mcp-e2e-report.json");

const apiBase = (process.env.ESHU_E2E_API_BASE ?? "http://127.0.0.1:29080").trim();
const mcpBase = (process.env.ESHU_E2E_MCP_BASE ?? "http://127.0.0.1:29081").trim();
const composeProject = (process.env.ESHU_E2E_PROJECT_NAME ?? "eshu-e2e-auth-mcp").trim();
const postgresDSN =
  (process.env.ESHU_E2E_POSTGRES_DSN ?? "").trim() ||
  buildPostgresDSN({
    host: (process.env.ESHU_E2E_POSTGRES_HOST ?? "127.0.0.1").trim(),
    port: (process.env.ESHU_E2E_POSTGRES_PORT ?? "29432").trim(),
    password: (process.env.ESHU_E2E_POSTGRES_PASSWORD ?? "change-me").trim(),
    database: "eshu",
  });
const authSecretEncKey = (process.env.ESHU_AUTH_SECRET_ENC_KEY ?? "").trim() || e2eDefaultAuthSecretEncKey;
// Distinct from #4971's 5185 and the mock-mode harness's 5190, so this suite
// can run concurrently with both (design §1's "Both-locally-and-CI" note).
const devServerPort = 5195;
const navTimeoutMs = 30000;
const mockOidcPort = (process.env.ESHU_E2E_MOCK_OIDC_PORT ?? "29090").trim();
const mockOidcAdminPort = (process.env.ESHU_E2E_MOCK_OIDC_ADMIN_PORT ?? "29091").trim();
const mockGithubPort = (process.env.ESHU_E2E_MOCK_GITHUB_PORT ?? "29092").trim();
const nornicHttpPort = (process.env.ESHU_E2E_NORNICDB_HTTP_PORT ?? "29474").trim();
const nornicHttpBase = `http://127.0.0.1:${nornicHttpPort}`;
const wizardNewPassword = "E2E-auth-mcp-runner-P@ssw0rd-1";
const selectedModule = (process.env.ESHU_E2E_MCP_MODULE ?? "").trim();

const results: StepResult[] = [];
const step = (id: string, fn: () => Promise<string>): Promise<void> => recordAuthE2EStep(results, id, fn);

function moduleSelected(name: string): boolean {
  return selectedModule === "" || selectedModule === name;
}

export async function runAuthMcpE2E(): Promise<number> {
  const runStart = Date.now();
  await rm(screenshotsDir, { recursive: true, force: true });
  await rm(reportPath, { force: true });
  await mkdir(screenshotsDir, { recursive: true });

  process.stdout.write(`auth-mcp-e2e: api base ${apiBase}; mcp base ${mcpBase}\n`);
  if (selectedModule !== "") {
    process.stdout.write(`auth-mcp-e2e: ESHU_E2E_MCP_MODULE=${selectedModule} — running only that module\n`);
  }

  // Fast standalone path for the mutation-sensitivity gate (step 8): the
  // credential-less negative probes need no browser, wizard, token, or shape
  // state — only a live mcp-server + API. Skipping the ~10s browser/wizard
  // bootstrap keeps the mutated re-run cheap (a few seconds), and against a
  // mutated (allow-unauthenticated) mcp-server this module FAILS with a
  // non-zero exit — the inverted-exit signal the sensitivity script asserts.
  if (selectedModule === "credentialless") {
    await step("leakage_credentialless_probes_do_not_leak", () =>
      assertCredentialLessProbesDoNotLeak({ mcpBase, apiBase, repoRoot, project: composeProject, hostRewrite: {} }),
    );
    const failedCl = results.filter((r) => r.status === "fail");
    const totalMsCl = Date.now() - runStart;
    await writeFile(
      reportPath,
      JSON.stringify({ apiBase, mcpBase, module: selectedModule, totalMs: totalMsCl, results }, null, 2),
      "utf8",
    );
    process.stdout.write(
      `auth-mcp-e2e: ${results.length - failedCl.length}/${results.length} steps passed in ${totalMsCl}ms; report ${reportPath}\n`,
    );
    return failedCl.length > 0 ? 1 : 0;
  }

  let devServer: AuthE2EDevServer | undefined;
  let browser: Browser | undefined;
  try {
    devServer = await startAuthE2EDevServer(repoRoot, apiBase, devServerPort);
    browser = await chromium.launch({
      args: chromiumLaunchArgsWithGithub(mockOidcPort, mockOidcAdminPort, mockGithubPort),
    });
    const context = await browser.newContext({ baseURL: devServer.baseUrl });
    const adminPage = await context.newPage();

    // Bootstrap: identical first-run wizard flow #4971 already proved, needed
    // here only to reach an authenticated admin session this suite's shape
    // modules can drive (this suite does not re-assert every #4971 acceptance
    // item — that gate already owns that coverage).
    await step("bootstrap_setup_wizard_renders", async () => {
      await assertFreshStackShowsSetupWizard(adminPage, devServer!.baseUrl, navTimeoutMs);
      return "SetupPage rendered on first navigation";
    });

    let credentialUsername = "";
    let breakglassRecoveryCode = "";
    await step("bootstrap_setup_wizard_completes", async () => {
      const retrieval = await retrieveInitialCredential(repoGoDir, postgresDSN, authSecretEncKey);
      if (retrieval.status !== "available" || !retrieval.credential) {
        throw new Error(
          `eshu admin initial-credential unavailable on a fresh stack (${retrieval.failureReason ?? "credential_command_failed"})`,
        );
      }
      credentialUsername = retrieval.credential.username;
      breakglassRecoveryCode = await driveSetupWizard(
        adminPage,
        retrieval.credential.username,
        retrieval.credential.password,
        wizardNewPassword,
        navTimeoutMs,
      );
      await adminPage.screenshot({ path: resolve(screenshotsDir, "0-bootstrap-dashboard.png"), fullPage: true });
      return `wizard completed as ${retrieval.credential.username}; dashboard reached`;
    });

    // Maps the in-network Compose hostnames (only reachable from inside the
    // stack's own network) to their host-published ports, for the scripted
    // OAuth client's plain fetch() calls (Node has no --host-resolver-rules
    // equivalent — see authMcpE2EOauthClient.ts's header comment). Mirrors
    // chromiumLaunchArgsWithGithub's browser-side mapping exactly.
    const hostRewrite: HostRewriteTable = {
      "mock-oidc-idp:8080": `127.0.0.1:${mockOidcPort}`,
      "mock-oidc-idp-admin:8080": `127.0.0.1:${mockOidcAdminPort}`,
      "mock-github:8080": `127.0.0.1:${mockGithubPort}`,
    };

    // Seed ONE Repository node into the graph (NornicDB) BEFORE shape A, so
    // the AllScopes row-filter reads are non-vacuous (a zero-corpus stack has
    // nothing to filter). See authMcpE2EGraphSeed.ts for why a graph seed —
    // list_indexed_repositories is graph-backed here, not psql-backed, so the
    // design's "psql cross-tenant seed" wording is adapted to a graph seed,
    // and the real isolation dimension is scope grant, not tenant.
    await step("seed_graph_repository", async () => {
      await seedGraphRepository(nornicHttpBase, SEEDED_REPOSITORY_ID);
      return `seeded Repository node ${SEEDED_REPOSITORY_ID} into NornicDB (${nornicHttpBase})`;
    });

    const shapeCtx: ShapeBContext = {
      browser,
      baseUrl: devServer.baseUrl,
      mcpBase,
      apiBase,
      repoRoot,
      repoGoDir,
      project: composeProject,
      navTimeoutMs,
      hostRewrite,
      screenshotsDir,
      credentialUsername,
      wizardNewPassword,
      breakglassRecoveryCode,
    };

    let personalToken = "";
    let scopedBearer = "";
    let ssoAdminContext: Awaited<ReturnType<Browser["newContext"]>> | undefined;
    if (moduleSelected("shapeA")) {
      personalToken = await runShapeA(step, adminPage, shapeCtx as ShapeAContext);
    }
    if (moduleSelected("shapeC")) {
      await runShapeC(step, adminPage, shapeCtx, personalToken);
    }
    if (moduleSelected("shapeB")) {
      const shapeBResult = await runShapeB(step, adminPage, shapeCtx, personalToken);
      scopedBearer = shapeBResult.scopedBearer;
      ssoAdminContext = shapeBResult.ssoAdminContext;
    }
    // The negative-leakage module runs LAST: it needs the OIDC provider active
    // (shape B) for the denial matrix, shape A's scoped personal token and
    // shape B's scoped OAuth bearer, and shape B's surviving AllScopes
    // SSO-admin session for the non-vacuous row-filter proof.
    if (moduleSelected("leakage") && ssoAdminContext) {
      await runLeakageSuite(
        step,
        { mcpBase, apiBase, repoRoot, project: composeProject, hostRewrite },
        { personalToken, scopedBearer, ssoAdminPage: ssoAdminContext.pages()[0]! },
      );
    }
    if (ssoAdminContext) {
      await ssoAdminContext.close();
    }

    const failed = results.filter((r) => r.status === "fail");
    const totalMs = Date.now() - runStart;
    await writeFile(
      reportPath,
      JSON.stringify({ apiBase, mcpBase, module: selectedModule || "all", totalMs, results }, null, 2),
      "utf8",
    );
    process.stdout.write(
      `auth-mcp-e2e: ${results.length - failed.length}/${results.length} steps passed in ${totalMs}ms; report ${reportPath}\n`,
    );
    return failed.length > 0 ? 1 : 0;
  } finally {
    if (browser) {
      await browser.close().catch(() => undefined);
    }
    if (devServer) {
      await stopAuthE2EDevServer(devServer);
    }
  }
}
