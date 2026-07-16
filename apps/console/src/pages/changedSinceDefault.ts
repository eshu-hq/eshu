import { loadGenerationLifecycle } from "../api/changedSince";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import {
  defaultChangedSinceParamsFromGenerations,
  type DefaultChangedSinceParams,
} from "../console/defaultEntity";

const MAX_DEFAULT_REPOSITORY_PROBES = 25;
const DEFAULT_DISCOVERY_BUDGET_MS = 15_000;
const DEFAULT_DISCOVERY_CONCURRENCY = 5;

export interface ChangedSinceDiscoveryOptions {
  readonly budgetMs?: number;
  readonly maxConcurrency?: number;
  readonly signal?: AbortSignal;
}

// discoverDefaultChangedSinceParams finds a real, exact repository generation
// pair without relying on the globally ordered lifecycle page. Requests run in
// catalog-order batches so latency is bounded without changing which exact pair
// wins. One total deadline and the caller's signal cancel in-flight HTTP work.
export async function discoverDefaultChangedSinceParams(
  client: EshuApiClient,
  repositories: readonly RepoListItem[],
  options: ChangedSinceDiscoveryOptions = {},
): Promise<DefaultChangedSinceParams | null> {
  const repositoryIds = repositories
    .map((repository) => repository.id.trim())
    .filter((id, index, ids) => id !== "" && ids.indexOf(id) === index)
    .slice(0, MAX_DEFAULT_REPOSITORY_PROBES);

  const controller = new AbortController();
  const abortFromCaller = (): void => controller.abort(abortReason(options.signal));
  if (options.signal?.aborted) {
    abortFromCaller();
  } else {
    options.signal?.addEventListener("abort", abortFromCaller, { once: true });
  }
  const budgetMs = positiveInteger(options.budgetMs, DEFAULT_DISCOVERY_BUDGET_MS);
  const maxConcurrency = Math.min(
    MAX_DEFAULT_REPOSITORY_PROBES,
    positiveInteger(options.maxConcurrency, DEFAULT_DISCOVERY_CONCURRENCY),
  );
  const deadline = setTimeout(() => {
    controller.abort(
      new DOMException(`changed-since discovery exceeded ${budgetMs}ms`, "TimeoutError"),
    );
  }, budgetMs);

  try {
    for (let start = 0; start < repositoryIds.length; start += maxConcurrency) {
      if (controller.signal.aborted) return null;
      const batch = repositoryIds.slice(start, start + maxConcurrency);
      const candidates = await Promise.all(
        batch.map(async (repository) => {
          try {
            const lifecycle = await loadGenerationLifecycle(
              client,
              { limit: 3, repository },
              { signal: controller.signal },
            );
            return defaultChangedSinceParamsFromGenerations(lifecycle.generations);
          } catch {
            // Stale and slow catalog rows fail closed within the shared budget.
            return null;
          }
        }),
      );
      if (controller.signal.aborted) return null;
      const selected = candidates.find((candidate) => candidate !== null);
      if (selected) return selected;
    }
    return null;
  } finally {
    clearTimeout(deadline);
    options.signal?.removeEventListener("abort", abortFromCaller);
  }
}

function positiveInteger(value: number | undefined, fallback: number): number {
  return value !== undefined && Number.isFinite(value) && value > 0
    ? Math.max(1, Math.floor(value))
    : fallback;
}

function abortReason(signal: AbortSignal | undefined): Error {
  const reason: unknown = signal?.reason;
  return reason instanceof Error ? reason : new DOMException("discovery aborted", "AbortError");
}
