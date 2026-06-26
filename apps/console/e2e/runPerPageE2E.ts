// runPerPageE2E.ts — Runner for per-page Playwright e2e tests (84 pages).
//
// Starts the Vite dev server + Chromium once, seeds localStorage for private
// mode, installs mock API handlers, then iterates through all registered page
// tests. Each page test navigates to its route and runs assertions.
//
// Run via: npx tsx apps/console/e2e/runPerPageE2E.ts

import type { Page } from "playwright";
import { getPage, cleanup } from "./setup.js";
import type { PageTest, PageTestResult } from "./types.js";

import { pageTest as pt_admin } from "./pages/admin.e2e.ts";
import { pageTest as pt_adminAssignments } from "./pages/adminAssignments.e2e.ts";
import { pageTest as pt_adminAudit } from "./pages/adminAudit.e2e.ts";
import { pageTest as pt_adminGroupMappings } from "./pages/adminGroupMappings.e2e.ts";
import { pageTest as pt_adminInvitations } from "./pages/adminInvitations.e2e.ts";
import { pageTest as pt_adminProviders } from "./pages/adminProviders.e2e.ts";
import { pageTest as pt_adminRoles } from "./pages/adminRoles.e2e.ts";
import { pageTest as pt_adminTokens } from "./pages/adminTokens.e2e.ts";
import { pageTest as pt_ask } from "./pages/ask.e2e.ts";
import { pageTest as pt_capabilities } from "./pages/capabilities.e2e.ts";
import { pageTest as pt_catalog } from "./pages/catalog.e2e.ts";
import { pageTest as pt_changedSince } from "./pages/changedSince.e2e.ts";
import { pageTest as pt_changedSincePacket } from "./pages/changedSincePacket.e2e.ts";
import { pageTest as pt_cicdRunCorrelations } from "./pages/cicdRunCorrelations.e2e.ts";
import { pageTest as pt_cloud } from "./pages/cloud.e2e.ts";
import { pageTest as pt_cloudDrift } from "./pages/cloudDrift.e2e.ts";
import { pageTest as pt_cloudInventoryPanel } from "./pages/cloudInventoryPanel.e2e.ts";
import { pageTest as pt_codeGraph } from "./pages/codeGraph.e2e.ts";
import { pageTest as pt_collectorReadiness } from "./pages/collectorReadiness.e2e.ts";
import { pageTest as pt_dashboard } from "./pages/dashboard.e2e.ts";
import { pageTest as pt_dashboardRoot } from "./pages/dashboardRoot.e2e.ts";
import { pageTest as pt_deadCode } from "./pages/deadCode.e2e.ts";
import { pageTest as pt_dependencies } from "./pages/dependencies.e2e.ts";
import { pageTest as pt_deployableUnitPacket } from "./pages/deployableUnitPacket.e2e.ts";
import { pageTest as pt_explorer } from "./pages/explorer.e2e.ts";
import { pageTest as pt_exposure } from "./pages/exposure.e2e.ts";
import { pageTest as pt_exposurePathAdvanced } from "./pages/exposurePathAdvanced.e2e.ts";
import { pageTest as pt_findings } from "./pages/findings.e2e.ts";
import { pageTest as pt_freshnessCausality } from "./pages/freshnessCausality.e2e.ts";
import { pageTest as pt_iac } from "./pages/iac.e2e.ts";
import { pageTest as pt_images } from "./pages/images.e2e.ts";
import { pageTest as pt_impact } from "./pages/impact.e2e.ts";
import { pageTest as pt_incidentContext } from "./pages/incidentContext.e2e.ts";
import { pageTest as pt_incidentContextOther } from "./pages/incidentContextOther.e2e.ts";
import { pageTest as pt_incidents } from "./pages/incidents.e2e.ts";
import { pageTest as pt_incidentSections } from "./pages/incidentSections.e2e.ts";
import { pageTest as pt_login } from "./pages/login.e2e.ts";
import { pageTest as pt_nodes } from "./pages/nodes.e2e.ts";
import { pageTest as pt_observability } from "./pages/observability.e2e.ts";
import { pageTest as pt_operations } from "./pages/operations.e2e.ts";
import { pageTest as pt_profile } from "./pages/profile.e2e.ts";
import { pageTest as pt_relationships } from "./pages/relationships.e2e.ts";
import { pageTest as pt_relationshipTruthPanel } from "./pages/relationshipTruthPanel.e2e.ts";
import { pageTest as pt_replatforming } from "./pages/replatforming.e2e.ts";
import { pageTest as pt_repositories } from "./pages/repositories.e2e.ts";
import { pageTest as pt_repoSource } from "./pages/repoSource.e2e.ts";
import { pageTest as pt_repoSourceFrontend } from "./pages/repoSourceFrontend.e2e.ts";
import { pageTest as pt_repoSourceInfra } from "./pages/repoSourceInfra.e2e.ts";
import { pageTest as pt_repoSourceLedger } from "./pages/repoSourceLedger.e2e.ts";
import { pageTest as pt_repoSourcePayments } from "./pages/repoSourcePayments.e2e.ts";
import { pageTest as pt_sbom } from "./pages/sbom.e2e.ts";
import { pageTest as pt_secretsIam } from "./pages/secretsIam.e2e.ts";
import { pageTest as pt_serviceAtlasEvidence } from "./pages/serviceAtlasEvidence.e2e.ts";
import { pageTest as pt_serviceChangeSurface } from "./pages/serviceChangeSurface.e2e.ts";
import { pageTest as pt_serviceCodeInvestigation } from "./pages/serviceCodeInvestigation.e2e.ts";
import { pageTest as pt_serviceConfigInfluence } from "./pages/serviceConfigInfluence.e2e.ts";
import { pageTest as pt_serviceInvestigation } from "./pages/serviceInvestigation.e2e.ts";
import { pageTest as pt_serviceRelationshipExplorer } from "./pages/serviceRelationshipExplorer.e2e.ts";
import { pageTest as pt_serviceRelationshipInspector } from "./pages/serviceRelationshipInspector.e2e.ts";
import { pageTest as pt_serviceRelationshipWorkbench } from "./pages/serviceRelationshipWorkbench.e2e.ts";
import { pageTest as pt_serviceReport } from "./pages/serviceReport.e2e.ts";
import { pageTest as pt_serviceReportLedger } from "./pages/serviceReportLedger.e2e.ts";
import { pageTest as pt_serviceReportPayments } from "./pages/serviceReportPayments.e2e.ts";
import { pageTest as pt_serviceReportService } from "./pages/serviceReportService.e2e.ts";
import { pageTest as pt_serviceSpotlight } from "./pages/serviceSpotlight.e2e.ts";
import { pageTest as pt_serviceStory } from "./pages/serviceStory.e2e.ts";
import { pageTest as pt_serviceStoryLedger } from "./pages/serviceStoryLedger.e2e.ts";
import { pageTest as pt_serviceStoryPayments } from "./pages/serviceStoryPayments.e2e.ts";
import { pageTest as pt_serviceStoryService } from "./pages/serviceStoryService.e2e.ts";
import { pageTest as pt_serviceSupportEvidence } from "./pages/serviceSupportEvidence.e2e.ts";
import { pageTest as pt_serviceTrafficPath } from "./pages/serviceTrafficPath.e2e.ts";
import { pageTest as pt_status } from "./pages/status.e2e.ts";
import { pageTest as pt_surfaceInventory } from "./pages/surfaceInventory.e2e.ts";
import { pageTest as pt_topology } from "./pages/topology.e2e.ts";
import { pageTest as pt_vulnCatalog } from "./pages/vulnCatalog.e2e.ts";
import { pageTest as pt_vulnDetail } from "./pages/vulnDetail.e2e.ts";
import { pageTest as pt_vulnDetailExact } from "./pages/vulnDetailExact.e2e.ts";
import { pageTest as pt_vulnDetailMedium } from "./pages/vulnDetailMedium.e2e.ts";
import { pageTest as pt_vulnDetailOther } from "./pages/vulnDetailOther.e2e.ts";
import { pageTest as pt_vulnerabilities } from "./pages/vulnerabilities.e2e.ts";
import { pageTest as pt_vulnReachable } from "./pages/vulnReachable.e2e.ts";
import { pageTest as pt_workspace } from "./pages/workspace.e2e.ts";
import { pageTest as pt_workspaceEnv } from "./pages/workspaceEnv.e2e.ts";
import { pageTest as pt_workspaceWorkload } from "./pages/workspaceWorkload.e2e.ts";

const allTests: readonly PageTest[] = [
  pt_admin, pt_adminAssignments, pt_adminAudit, pt_adminGroupMappings,
  pt_adminInvitations, pt_adminProviders, pt_adminRoles, pt_adminTokens,
  pt_ask, pt_capabilities, pt_catalog, pt_changedSince, pt_changedSincePacket,
  pt_cicdRunCorrelations, pt_cloud, pt_cloudDrift, pt_cloudInventoryPanel,
  pt_codeGraph, pt_collectorReadiness, pt_dashboard, pt_dashboardRoot,
  pt_deadCode, pt_dependencies, pt_deployableUnitPacket, pt_explorer,
  pt_exposure, pt_exposurePathAdvanced, pt_findings, pt_freshnessCausality,
  pt_iac, pt_images, pt_impact, pt_incidentContext, pt_incidentContextOther,
  pt_incidents, pt_incidentSections, pt_login, pt_nodes, pt_observability,
  pt_operations, pt_profile, pt_relationships, pt_relationshipTruthPanel,
  pt_replatforming, pt_repositories, pt_repoSource, pt_repoSourceFrontend,
  pt_repoSourceInfra, pt_repoSourceLedger, pt_repoSourcePayments, pt_sbom,
  pt_secretsIam, pt_serviceAtlasEvidence, pt_serviceChangeSurface,
  pt_serviceCodeInvestigation, pt_serviceConfigInfluence, pt_serviceInvestigation,
  pt_serviceRelationshipExplorer, pt_serviceRelationshipInspector,
  pt_serviceRelationshipWorkbench, pt_serviceReport, pt_serviceReportLedger,
  pt_serviceReportPayments, pt_serviceReportService, pt_serviceSpotlight,
  pt_serviceStory, pt_serviceStoryLedger, pt_serviceStoryPayments,
  pt_serviceStoryService, pt_serviceSupportEvidence, pt_serviceTrafficPath,
  pt_status, pt_surfaceInventory, pt_topology, pt_vulnCatalog, pt_vulnDetail,
  pt_vulnDetailExact, pt_vulnDetailMedium, pt_vulnDetailOther, pt_vulnerabilities,
  pt_vulnReachable, pt_workspace, pt_workspaceEnv, pt_workspaceWorkload
];

const navTimeoutMs = 30000;
const settleMs = 1500;

async function runOneTest(page: Page, test: PageTest): Promise<PageTestResult> {
  const start = Date.now();
  const consoleErrors: string[] = [];

  const onConsole = (msg: { type: () => string; text: () => string }): void => {
    if (msg.type() === "error" && !msg.text().includes("Download the React DevTools")) {
      consoleErrors.push(msg.text());
    }
  };
  page.on("console", onConsole);

  try {
    await page.goto(test.path, { waitUntil: "domcontentloaded", timeout: navTimeoutMs });
    await page.waitForTimeout(settleMs);
    await test.assert(page);

    if (consoleErrors.length > 0) {
      return {
        path: test.path, label: test.label, passed: false,
        error: `console errors: ${consoleErrors.join("; ")}`,
        durationMs: Date.now() - start
      };
    }
    return { path: test.path, label: test.label, passed: true, durationMs: Date.now() - start };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    const errDetail = consoleErrors.length > 0
      ? `${msg} (+ console errors: ${consoleErrors.join("; ")})`
      : msg;
    return { path: test.path, label: test.label, passed: false, error: errDetail, durationMs: Date.now() - start };
  } finally {
    page.off("console", onConsole);
  }
}

async function main(): Promise<void> {
  process.stdout.write(`console-e2e-mock: starting (${allTests.length} pages)\n`);
  const { page } = await getPage();

  const results: PageTestResult[] = [];
  for (const test of allTests) {
    const result = await runOneTest(page, test);
    results.push(result);
    process.stdout.write(`  ${result.passed ? "PASS" : "FAIL"} ${test.path} (${test.label}) [${result.durationMs}ms]\n`);
    if (result.error) {
      process.stdout.write(`        ${result.error}\n`);
    }
  }

  const passed = results.filter((r) => r.passed).length;
  const failed = results.filter((r) => !r.passed).length;
  process.stdout.write(`console-e2e-mock: ${passed}/${results.length} passed, ${failed} failed\n`);

  await cleanup();
  if (failed > 0) process.exit(1);
}

void main();
