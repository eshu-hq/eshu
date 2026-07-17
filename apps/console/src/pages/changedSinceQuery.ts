import type { ChangedSinceMode } from "../api/changedSince";

export interface ChangedSinceFormState {
  readonly mode: ChangedSinceMode;
  readonly repository: string;
  readonly sampleLimit: string;
  readonly scopeId: string;
  readonly serviceId: string;
  readonly sinceGenerationId: string;
  readonly sinceObservedAt: string;
}

export const changedSinceDefaultLimit = "25";

const scopeParams = [
  "mode",
  "repository",
  "scope_id",
  "service_id",
  "since_generation_id",
  "since_observed_at",
] as const;

export function changedSinceFormFromSearch(params: URLSearchParams): ChangedSinceFormState {
  const mode = params.get("mode") === "service" ? "service" : "repository";
  return {
    mode,
    repository: params.get("repository") ?? "",
    sampleLimit: params.get("sample_limit") ?? changedSinceDefaultLimit,
    scopeId: params.get("scope_id") ?? "",
    serviceId: params.get("service_id") ?? "",
    sinceGenerationId: params.get("since_generation_id") ?? "",
    sinceObservedAt: params.get("since_observed_at") ?? "",
  };
}

export function hasChangedSinceUserScope(params: URLSearchParams): boolean {
  return scopeParams.some((key) => (params.get(key) ?? "").trim().length > 0);
}

export function isBoundedChangedSince(form: ChangedSinceFormState): boolean {
  if (form.mode === "service") {
    return form.serviceId.trim().length > 0 && form.sinceGenerationId.trim().length > 0;
  }
  return (
    hasChangedSinceRepositoryScope(form) &&
    (form.sinceGenerationId.trim().length > 0 || form.sinceObservedAt.trim().length > 0)
  );
}

export function hasChangedSinceRepositoryScope(form: ChangedSinceFormState): boolean {
  const hasRepository = form.repository.trim().length > 0;
  const hasScopeID = form.scopeId.trim().length > 0;
  return hasRepository !== hasScopeID;
}

export function hasChangedSincePriorReference(form: ChangedSinceFormState): boolean {
  return form.sinceGenerationId.trim().length > 0 || form.sinceObservedAt.trim().length > 0;
}

export function parseChangedSinceLimit(value: string): number | undefined {
  const parsed = Number.parseInt(value.trim(), 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

export function optionalChangedSinceValue(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

export function addChangedSinceParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed.length > 0) params.set(key, trimmed);
}
