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
//   Shape B (OIDC, last)   -> apps/console/e2e/authMcpE2EOauthClient.ts /
//                             authMcpE2EOidcFlow reuse
//
// Phase order is load-bearing (design §1): A needs zero providers; C's
// GitHub-only provider must NOT enable discovery (asserted before B's OIDC
// provider does); B ends with the require_sso flip. Modules land in that
// order across steps 4-6 of the work breakdown; this file only wires Shape A
// for step 4 and gains the later calls (each its own commit) as they land.
//
// ESHU_E2E_MCP_MODULE selects a single module to run (currently only "shapeA"
// is defined) — used by scripts/verify-auth-mcp-e2e-sensitivity.sh (step 8) to
// re-run just the negative/challenge module against a mutated service without
// paying for the full suite's wall time. Empty/unset runs every module.
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
import { chromiumLaunchArgsWithGithub, runShapeC, type ShapeCContext } from "./authMcpE2EGithubFlow.ts";
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

    await step("bootstrap_setup_wizard_completes", async () => {
      const retrieval = await retrieveInitialCredential(repoGoDir, postgresDSN, authSecretEncKey);
      if (retrieval.status !== "available" || !retrieval.credential) {
        throw new Error(
          `eshu admin initial-credential unavailable on a fresh stack (${retrieval.failureReason ?? "credential_command_failed"})`,
        );
      }
      await driveSetupWizard(
        adminPage,
        retrieval.credential.username,
        retrieval.credential.password,
        wizardNewPassword,
        navTimeoutMs,
      );
      await adminPage.screenshot({ path: resolve(screenshotsDir, "0-bootstrap-dashboard.png"), fullPage: true });
      return `wizard completed as ${retrieval.credential.username}; dashboard reached`;
    });

    const shapeCtx: ShapeCContext = {
      browser,
      baseUrl: devServer.baseUrl,
      mcpBase,
      apiBase,
      repoRoot,
      repoGoDir,
      project: composeProject,
      navTimeoutMs,
    };

    let personalToken = "";
    if (moduleSelected("shapeA")) {
      personalToken = await runShapeA(step, adminPage, shapeCtx as ShapeAContext);
    }
    if (moduleSelected("shapeC")) {
      await runShapeC(step, adminPage, shapeCtx, personalToken);
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
