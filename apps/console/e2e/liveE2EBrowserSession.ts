import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { Page } from "playwright";

import {
  assertFreshStackShowsSetupWizard,
  assertSetupRouteGone,
  driveSetupWizard,
} from "./authE2ESetupWizard.ts";
import { retrieveInitialCredential } from "./authE2ECredential.ts";
import type { ConsoleAuthMode } from "../src/e2e/consoleRouteCatalog.ts";
import { awaitApiQuiet, type ApiQuietResult, type ApiQuietTracker } from "./liveE2EPolicy.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoGoDir = resolve(here, "..", "..", "..", "go");

export interface RetainedWizardSessionDependencies {
  readonly retrieveCredential: typeof retrieveInitialCredential;
  readonly showSetupWizard: typeof assertFreshStackShowsSetupWizard;
  readonly driveWizard: typeof driveSetupWizard;
  readonly assertSetupGone: typeof assertSetupRouteGone;
}

const defaultDependencies: RetainedWizardSessionDependencies = {
  retrieveCredential: retrieveInitialCredential,
  showSetupWizard: assertFreshStackShowsSetupWizard,
  driveWizard: driveSetupWizard,
  assertSetupGone: assertSetupRouteGone,
};

export interface RetainedWizardSessionOptions {
  readonly consoleBaseUrl: string;
  readonly postgresDSN: string;
  readonly authSecretEncKey: string;
  readonly newPassword: string;
  readonly timeoutMs: number;
  readonly dependencies?: RetainedWizardSessionDependencies;
}

export function parseLiveE2EAuthMode(value: string | undefined): ConsoleAuthMode {
  const normalized = value?.trim() || "browser_session";
  if (normalized === "browser_session" || normalized === "bearer") return normalized;
  throw new Error(
    `ESHU_E2E_AUTH_MODE must be browser_session or bearer; received ${JSON.stringify(normalized)}`,
  );
}

// establishRetainedWizardSession claims the isolated identity surface attached
// to a retained corpus and leaves its real browser-session cookie on page.
// Credential values are consumed only by the setup form and never returned.
export async function establishRetainedWizardSession(
  page: Page,
  options: RetainedWizardSessionOptions,
): Promise<string> {
  const dependencies = options.dependencies ?? defaultDependencies;
  const retrieval = await dependencies.retrieveCredential(
    repoGoDir,
    options.postgresDSN,
    options.authSecretEncKey,
  );
  if (retrieval.status === "error") {
    throw new Error(
      `browser-session credential retrieval failed (${retrieval.failureReason ?? "credential_command_failed"})`,
    );
  }
  if (retrieval.status !== "available" || !retrieval.credential) {
    throw new Error(
      "browser-session live proof requires a fresh retained-proof identity with a retrievable initial credential",
    );
  }
  const credential = retrieval.credential;
  await dependencies.showSetupWizard(page, options.consoleBaseUrl, options.timeoutMs);
  await dependencies.driveWizard(
    page,
    credential.username,
    credential.password,
    options.newPassword,
    options.timeoutMs,
  );
  await dependencies.assertSetupGone(`${options.consoleBaseUrl}/eshu-api`);
  return `wizard owner session established for ${credential.username}`;
}

export type RetainedSessionQuietWait = (
  page: Page,
  tracker: ApiQuietTracker,
) => Promise<ApiQuietResult>;

const defaultSessionQuietWait: RetainedSessionQuietWait = (page, tracker) =>
  awaitApiQuiet(tracker, (duration) => page.waitForTimeout(duration));

// awaitRetainedWizardSessionQuiet keeps setup's post-login dashboard fan-out
// owned by the wizard phase. Route proof starts only after those requests have
// settled, so the reset navigation cannot manufacture aborted bootstrap calls.
export async function awaitRetainedWizardSessionQuiet(
  page: Page,
  tracker: ApiQuietTracker,
  waitForQuiet: RetainedSessionQuietWait = defaultSessionQuietWait,
): Promise<ApiQuietResult> {
  const result = await waitForQuiet(page, tracker);
  if (!result.settled) {
    throw new Error(
      `${result.inFlight} wizard-session API request(s) remained active after ${result.waitedMs}ms`,
    );
  }
  return result;
}
