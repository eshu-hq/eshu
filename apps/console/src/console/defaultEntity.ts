// console/defaultEntity.ts
// Shared defaults for the Overview investigation pages so they render real
// evidence on open instead of an empty form. The default entity is derived from
// the already-loaded live service and repository catalogs, so there is no extra
// API round trip and the default is always a real entity in the current estate —
// never a hard-coded placeholder. Pages keep their picker/form as an override.

import type { ConsoleModel } from "./types";
import type { GenerationLifecycleRow } from "../api/changedSince";

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
  readonly scopeId: string;
  readonly sinceGenerationId: string;
}

// defaultChangedSinceParamsFromGenerations derives an exact comparison pair
// from the bounded lifecycle feed. It never fabricates a time window: a scope
// qualifies only when it has both the current active generation and a retained
// retained non-active generation that the changed-since endpoint can compare
// directly. Superseded/completed baselines are preferred, while failed
// generations remain valid exact fact-record baselines when that is the only
// retained predecessor for the scope.
export function defaultChangedSinceParamsFromGenerations(
  generations: readonly GenerationLifecycleRow[],
): DefaultChangedSinceParams | null {
  const byScope = new Map<string, GenerationLifecycleRow[]>();
  for (const generation of generations) {
    if (generation.scopeKind !== "repository" || generation.scopeId.trim() === "") continue;
    const rows = byScope.get(generation.scopeId) ?? [];
    rows.push(generation);
    byScope.set(generation.scopeId, rows);
  }

  for (const [scopeId, rows] of byScope) {
    const active = rows.find(
      (generation) => generation.isActive && generation.generationId.trim() !== "",
    );
    if (!active) continue;
    const prior = rows
      .filter(
        (generation) =>
          !generation.isActive &&
          ["superseded", "completed", "failed"].includes(generation.status) &&
          generation.generationId.trim() !== "" &&
          generation.generationId !== active.generationId,
      )
      .sort((left, right) => {
        const statusOrder = { superseded: 0, completed: 1, failed: 2 } as const;
        const statusDelta =
          statusOrder[left.status as keyof typeof statusOrder] -
          statusOrder[right.status as keyof typeof statusOrder];
        return statusDelta !== 0
          ? statusDelta
          : (right.observedAt ?? "").localeCompare(left.observedAt ?? "");
      })[0];
    if (prior) {
      return { scopeId, sinceGenerationId: prior.generationId };
    }
  }
  return null;
}
