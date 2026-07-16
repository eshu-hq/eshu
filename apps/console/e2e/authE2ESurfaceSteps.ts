import type { Page } from "playwright";

import type { AuthE2EStep } from "./authE2EStepRecorder.ts";
import { consoleRoutes } from "../src/e2e/consoleRouteCatalog.ts";
import {
  assertAdminSessionSurface,
  assertProfileSessionSurface,
  assertWholeDashboardSessionAccess,
} from "./authE2ESurfaceProof.ts";

interface SessionSurfaceProofs {
  readonly profile: (page: Page, timeoutMs: number) => Promise<string>;
  readonly admin: (page: Page, timeoutMs: number) => Promise<string>;
  readonly wholeDashboard: (page: Page, timeoutMs: number) => Promise<string>;
}

const defaultProofs: SessionSurfaceProofs = {
  profile: assertProfileSessionSurface,
  admin: assertAdminSessionSurface,
  wholeDashboard: (page, timeoutMs) =>
    assertWholeDashboardSessionAccess(page, consoleRoutes, timeoutMs),
};

export async function runSessionSurfaceProofSteps(
  step: AuthE2EStep,
  page: Page,
  timeoutMs: number,
  proofs: SessionSurfaceProofs = defaultProofs,
): Promise<void> {
  await step("item2_profile_session_surface", () => proofs.profile(page, timeoutMs));
  await step("item2_admin_session_surface", () => proofs.admin(page, timeoutMs));
  await step("item2_whole_dashboard_session_access", () => proofs.wholeDashboard(page, timeoutMs));
}
