// console/defaultEntity.ts
// Shared defaults for the Overview investigation pages so they render real
// evidence on open instead of an empty form. The default entity is derived from
// the already-loaded live catalog (model.services), so there is no extra API
// round trip and the default is always a real entity in the current estate —
// never a hard-coded placeholder. Pages keep their picker/form as an override.

import type { ConsoleModel } from "./types";

// CHANGED_SINCE_DEFAULT_WINDOW_MS is the default "changed since" lookback used
// when a page opens without an explicit baseline: seven days, expressed as the
// since_observed_at instant the repository changed-since endpoint accepts.
const CHANGED_SINCE_DEFAULT_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

// defaultServiceName picks a sensible default service to auto-load from the live
// catalog. A service-kind row is preferred over a workload-kind row (services
// are the more complete investigation target); otherwise the first named row is
// used. Returns "" when the catalog carries no usable service so callers can
// keep the empty/needs-connection state instead of loading a fabricated entity.
export function defaultServiceName(model: ConsoleModel): string {
  const named = model.services.filter((service) => service.name.trim().length > 0);
  const preferred = named.find((service) => service.kind === "service") ?? named[0];
  return preferred?.name.trim() ?? "";
}

// DefaultChangedSinceParams is the auto-load baseline for the Changed Since page:
// a real repository scope plus a default observed-at lookback window. The page
// uses it only when the user opens without an explicit scope/baseline.
export interface DefaultChangedSinceParams {
  readonly repository: string;
  readonly sinceObservedAt: string;
}

// defaultChangedSinceParams derives a repository-mode baseline from the catalog:
// the first service that carries a repository, paired with a seven-day
// observed-at window. Returns null when no catalog row carries a repository, so
// the page keeps its "choose a scope" state rather than loading an unbounded or
// fabricated default. `now` is injected for deterministic testing.
export function defaultChangedSinceParams(
  model: ConsoleModel,
  now: Date = new Date()
): DefaultChangedSinceParams | null {
  const withRepo = model.services.find((service) => service.repo.trim().length > 0);
  if (withRepo === undefined) {
    return null;
  }
  const sinceObservedAt = new Date(now.getTime() - CHANGED_SINCE_DEFAULT_WINDOW_MS).toISOString();
  return { repository: withRepo.repo.trim(), sinceObservedAt };
}
