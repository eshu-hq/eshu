// appBoot.ts — extracted boot-phase helpers for App.tsx.
// Keeps App.tsx under 500 lines by isolating session-aware boot logic here.
import type { BrowserSessionResponse } from "./api/client";
import { EshuApiClient } from "./api/client";
import { loadCurrentSession } from "./api/authSession";
import { loadConsoleSnapshot } from "./api/eshuConsoleLive";
import { loadRepositories, type RepoListItem } from "./api/repoCatalog";
import { saveConsoleEnvironment } from "./config/environment";
import { modelFromSnapshot } from "./console/liveModel";
import type { ConsoleModel } from "./console/types";

export interface BootResult {
  readonly client: EshuApiClient;
  readonly model: ConsoleModel;
  readonly repositories: readonly RepoListItem[];
  readonly session: BrowserSessionResponse | null;
}

// bootFromSession attempts to load live data using the existing browser session
// cookie (no API key). Used on page load when a session cookie may already
// exist. Returns null if no session cookie is present.
export async function bootFromSession(baseUrl: string): Promise<BootResult | null> {
  const client = new EshuApiClient({ baseUrl });
  const session = await loadCurrentSession(client);
  if (session === null) {
    return null;
  }
  const [snap, repoRows] = await Promise.all([
    loadConsoleSnapshot(client),
    loadRepositories(client).catch((): readonly RepoListItem[] => [])
  ]);
  return {
    client,
    model: modelFromSnapshot(snap),
    repositories: repoRows,
    session
  };
}

// bootFromKey attempts to exchange an API key for a browser session, then load
// live data. Falls back to bearer reads when the key cannot create a session
// (status 400 — shared key without tenant context). Used from SourcePopover.
export async function bootFromKey(base: string, key: string): Promise<BootResult | null> {
  const { EshuApiHttpError } = await import("./api/client");
  let nextClient = new EshuApiClient({ baseUrl: base, apiKey: key });
  let session: BrowserSessionResponse | null = null;
  if (key.trim().length > 0) {
    try {
      const created = await nextClient.createBrowserSession();
      session = created;
      // Session established — switch to cookie-only client.
      nextClient = new EshuApiClient({ baseUrl: base });
    } catch (e) {
      if (!(e instanceof EshuApiHttpError && e.status === 400)) {
        throw e;
      }
      // 400: shared key cannot create a session; continue with bearer key.
    }
  } else {
    // No key — try to use an existing cookie session.
    session = await loadCurrentSession(nextClient);
    if (session === null) {
      // No key and no existing session for this base. Do not read data
      // unauthenticated (those reads 401 and would strand the user in an error
      // state). Persist the selected base and signal no-session so the caller
      // routes to local login for this deployment. (#3685 P2)
      saveConsoleEnvironment({ mode: "private", apiBaseUrl: base, apiKey: "", recentApiBaseUrls: [base] });
      return null;
    }
  }
  const [snap, repoRows] = await Promise.all([
    loadConsoleSnapshot(nextClient),
    loadRepositories(nextClient).catch((): readonly RepoListItem[] => [])
  ]);
  saveConsoleEnvironment({ mode: "private", apiBaseUrl: base, apiKey: "", recentApiBaseUrls: [base] });
  return {
    client: nextClient,
    model: modelFromSnapshot(snap),
    repositories: repoRows,
    session
  };
}
