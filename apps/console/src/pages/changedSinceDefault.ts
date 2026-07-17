import {
  loadGenerationLifecycle,
  type GenerationLifecycleLoadOptions,
  type GenerationLifecyclePage,
  type GenerationLifecycleQuery,
  type GenerationLifecycleRow,
} from "../api/changedSince";
import type { EshuApiClient } from "../api/client";
import { EshuEnvelopeError } from "../api/envelope";
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

export interface ChangedSinceDefaultSelection {
  readonly repository: string;
  readonly scopeId: string;
  readonly sinceGenerationId: string;
}

// discoverDefaultChangedSinceParams finds a real, exact repository generation
// pair without relying on the globally ordered lifecycle page. Requests run in
// catalog-order batches so latency is bounded without changing which exact pair
// wins. One total deadline and the caller's signal cancel in-flight HTTP work.
export async function discoverDefaultChangedSinceParams(
  client: EshuApiClient,
  repositories: readonly RepoListItem[],
  options: ChangedSinceDiscoveryOptions = {},
): Promise<ChangedSinceDefaultSelection | null> {
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
            const baseline = await resolveChangedSinceBaseline(client, { repository }, lifecycle, {
              signal: controller.signal,
            });
            return baseline ? { ...baseline, repository } : null;
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

// resolveChangedSinceBaseline keeps the visible lifecycle page small while
// preserving baseline correctness when newer pending or failed rows hide the
// active/prior pair. Status probes are sequential so default discovery still
// honors its repository-level concurrency ceiling, and stop as soon as the
// preferred superseded/completed/failed predecessor is proven.
export async function resolveChangedSinceBaseline(
  client: EshuApiClient,
  selector: Pick<GenerationLifecycleQuery, "repository" | "scopeId">,
  lifecycle: GenerationLifecyclePage,
  options: GenerationLifecycleLoadOptions = {},
): Promise<DefaultChangedSinceParams | null> {
  const generations = [...lifecycle.generations];
  if (!lifecycle.truncated) {
    return defaultChangedSinceParamsFromGenerations(generations);
  }

  if (!hasActiveGeneration(generations)) {
    mergeGenerations(generations, await loadGenerationStatus(client, selector, "active", options));
  }
  if (!hasActiveGeneration(generations)) return null;

  for (const status of ["superseded", "completed", "failed"] as const) {
    if (!hasPriorStatus(generations, status)) {
      mergeGenerations(generations, await loadGenerationStatus(client, selector, status, options));
    }
    if (hasPriorStatus(generations, status)) {
      return defaultChangedSinceParamsFromGenerations(generations);
    }
  }
  return null;
}

async function loadGenerationStatus(
  client: EshuApiClient,
  selector: Pick<GenerationLifecycleQuery, "repository" | "scopeId">,
  status: "active" | "completed" | "failed" | "superseded",
  options: GenerationLifecycleLoadOptions,
): Promise<readonly GenerationLifecycleRow[]> {
  try {
    const page = await loadGenerationLifecycle(client, { ...selector, limit: 1, status }, options);
    return page.generations;
  } catch (error) {
    if (error instanceof EshuEnvelopeError && error.error.code === "scope_not_found") return [];
    throw error;
  }
}

function hasActiveGeneration(generations: readonly GenerationLifecycleRow[]): boolean {
  return generations.some(
    (generation) => generation.isActive && generation.generationId.trim() !== "",
  );
}

function hasPriorStatus(
  generations: readonly GenerationLifecycleRow[],
  status: "completed" | "failed" | "superseded",
): boolean {
  const activeGenerationIds = new Set(
    generations
      .filter((generation) => generation.isActive)
      .map((generation) => generation.generationId),
  );
  return generations.some(
    (generation) =>
      !generation.isActive &&
      generation.status === status &&
      generation.generationId.trim() !== "" &&
      !activeGenerationIds.has(generation.generationId),
  );
}

function mergeGenerations(
  target: GenerationLifecycleRow[],
  additions: readonly GenerationLifecycleRow[],
): void {
  for (const generation of additions) {
    if (
      target.some(
        (existing) =>
          existing.scopeId === generation.scopeId &&
          existing.generationId === generation.generationId,
      )
    ) {
      continue;
    }
    target.push(generation);
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
