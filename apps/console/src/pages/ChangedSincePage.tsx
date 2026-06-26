import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { ChangedSincePacketComparison } from "./ChangedSincePacketComparison";
import {
  type ChangedSinceMode,
  type ChangedSincePageData,
  type ChangeClassification,
  type ChangedSinceCategory,
  type GenerationLifecyclePage,
  loadGenerationLifecycle,
  loadRepositoryChangedSince,
  loadServiceChangedSince
} from "../api/changedSince";
import type { EshuApiClient } from "../api/client";
import { buildEvidencePacketComparison } from "../api/evidencePacketDelta";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { defaultChangedSinceParams, type DefaultChangedSinceParams } from "../console/defaultEntity";
import type { ConsoleModel } from "../console/types";
import { fmt, uiFresh, uiTruth } from "../console/types";
import "./changedSincePage.css";

interface FormState {
  readonly mode: ChangedSinceMode;
  readonly repository: string;
  readonly sampleLimit: string;
  readonly scopeId: string;
  readonly serviceId: string;
  readonly sinceGenerationId: string;
  readonly sinceObservedAt: string;
}

const defaultLimit = "25";
const classifications: readonly ChangeClassification[] = [
  "added",
  "updated",
  "retired",
  "superseded",
  "unchanged"
];

export function ChangedSincePage({
  client,
  model
}: {
  readonly client?: EshuApiClient;
  readonly model?: ConsoleModel;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  // Auto-load a sensible default on open: with no explicit scope/baseline in the
  // URL, seed a real repository scope plus a default observed-at window from the
  // live catalog so the page renders a delta immediately instead of an empty
  // form. Any explicit URL filter wins; the query form still overrides.
  const seedDefault = useMemo(
    () => (model && client ? defaultChangedSinceParams(model) : null),
    [client, model]
  );
  const [form, setForm] = useState<FormState>(() => formFromSearch(searchParams, seedDefault));
  const [page, setPage] = useState<ChangedSincePageData | null>(null);
  const [generations, setGenerations] = useState<GenerationLifecyclePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useMemo(() => formFromSearch(searchParams, seedDefault), [searchParams, seedDefault]);
  const hasLiveClient = client !== undefined;
  const canLoadChanges = hasLiveClient && isBounded(request);
  const canLoadGenerations = hasLiveClient && request.mode === "repository" && hasRepositoryScope(request);

  const load = useCallback(
    async (next: FormState) => {
      if (!client || !isBounded(next)) return;
      setBusy(true);
      setError("");
      try {
        const loaded = next.mode === "service"
          ? await loadServiceChangedSince(client, {
            sampleLimit: parsedLimit(next.sampleLimit),
            serviceId: next.serviceId,
            sinceGenerationId: next.sinceGenerationId
          })
          : await loadRepositoryChangedSince(client, {
            repository: optional(next.repository),
            sampleLimit: parsedLimit(next.sampleLimit),
            scopeId: optional(next.scopeId),
            sinceGenerationId: optional(next.sinceGenerationId),
            sinceObservedAt: optional(next.sinceObservedAt)
          });
        setPage(loaded);
      } catch (loadError) {
        setPage(null);
        setError(loadError instanceof Error ? loadError.message : "failed to load changed-since data");
      } finally {
        setBusy(false);
      }
    },
    [client]
  );

  const loadGenerations = useCallback(
    async (next: FormState) => {
      if (!client || next.mode !== "repository" || !hasRepositoryScope(next)) {
        setGenerations(null);
        return;
      }
      try {
        setGenerations(await loadGenerationLifecycle(client, {
          limit: 50,
          repository: optional(next.repository),
          scopeId: optional(next.scopeId)
        }));
      } catch {
        setGenerations(null);
      }
    },
    [client]
  );

  useEffect(() => {
    setForm(request);
    if (canLoadChanges) {
      void load(request);
    } else {
      setPage(null);
      setError("");
    }
    if (canLoadGenerations) {
      void loadGenerations(request);
    } else {
      setGenerations(null);
    }
  }, [canLoadChanges, canLoadGenerations, load, loadGenerations, request]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const params = new URLSearchParams();
    params.set("mode", form.mode);
    if (form.mode === "service") {
      addParam(params, "service_id", form.serviceId);
      addParam(params, "since_generation_id", form.sinceGenerationId);
    } else {
      addParam(params, "repository", form.repository);
      addParam(params, "scope_id", form.scopeId);
      addParam(params, "since_generation_id", form.sinceGenerationId);
      addParam(params, "since_observed_at", form.sinceObservedAt);
    }
    if (form.sampleLimit.trim() !== "" && form.sampleLimit.trim() !== defaultLimit) {
      params.set("sample_limit", form.sampleLimit.trim());
    }
    setSearchParams(params);
  }

  const categoryCount = page?.categories.length ?? 0;
  const sampleCount = page?.categories.reduce((sum, category) => sum + sampleTotal(category), 0) ?? 0;
  const impactHref = page ? impactLink(page) : "";
  const comparison = page && !page.unavailable ? buildEvidencePacketComparison(page) : null;

  return (
    <div className="page changed-since-page" style={{ maxWidth: "none" }}>
      <div className="page-intro changed-since-intro">
        <h2>Changed Since</h2>
        <Badge tone="teal">freshness</Badge>
      </div>

      <form className="changed-since-query" onSubmit={submit}>
        <label>
          <span>Mode</span>
          <select
            aria-label="Mode"
            className="popover-input"
            value={form.mode}
            onChange={(event) => setForm((current) => ({ ...current, mode: event.target.value as ChangedSinceMode }))}
          >
            <option value="repository">Repository</option>
            <option value="service">Service</option>
          </select>
        </label>
        {form.mode === "service" ? (
          <FilterInput label="Service ID" value={form.serviceId} onChange={(value) => setForm((current) => ({ ...current, serviceId: value }))} />
        ) : (
          <>
            <FilterInput label="Repository" value={form.repository} onChange={(value) => setForm((current) => ({ ...current, repository: value }))} />
            <FilterInput label="Scope ID" value={form.scopeId} onChange={(value) => setForm((current) => ({ ...current, scopeId: value }))} />
          </>
        )}
        <FilterInput label="Since generation" value={form.sinceGenerationId} onChange={(value) => setForm((current) => ({ ...current, sinceGenerationId: value }))} />
        {form.mode === "repository" ? (
          <FilterInput label="Since observed at" value={form.sinceObservedAt} onChange={(value) => setForm((current) => ({ ...current, sinceObservedAt: value }))} />
        ) : null}
        <FilterInput label="Sample limit" value={form.sampleLimit} onChange={(value) => setForm((current) => ({ ...current, sampleLimit: value }))} />
        <button className="btn-ghost active" disabled={!hasLiveClient || busy || !isBounded(form)} type="submit">
          {busy ? "Loading..." : "Load changes"}
        </button>
      </form>

      {!hasLiveClient ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {!isBounded(request) ? <p className="inline-state">Choose a repository/scope or service and a baseline to load changed-since evidence.</p> : null}
      {error ? <p className="src-err">{error}</p> : null}

      <div className="grid g-4 mt">
        <StatTile label="Changed" value={fmt(page?.changedCount ?? 0)} color="var(--teal)" sub={page ? "selected retained window" : "no baseline loaded"} />
        <StatTile label="Unchanged" value={fmt(page?.unchangedCount ?? 0)} color="var(--blue)" sub="stable across the selected window" />
        <StatTile label="Categories" value={fmt(categoryCount)} color="var(--violet)" sub="bounded endpoint categories" />
        <StatTile label="Samples" value={fmt(sampleCount)} color="var(--ember)" sub={page ? `${page.sampleLimit} max per verdict` : "not loaded"} />
      </div>

      <div className="changed-since-grid mt">
        <Panel
          title="Delta evidence"
          sub={page ? page.scopeLabel || page.scopeId || "selected scope" : "No changed-since data loaded"}
          action={page ? (
            <span className="panel-action-stack">
              <TruthChip level={uiTruth(page.truth.level)} />
              <FreshDot state={uiFresh(page.truth.freshness.state)} />
            </span>
          ) : null}
        >
          {page?.unavailable ? (
            <div className="changed-since-unavailable">
              <strong>Changed-since data unavailable</strong>
              <span className="mono">{page.unavailableReason || "unavailable"}</span>
            </div>
          ) : null}
          {page && !page.unavailable ? (
            <>
              <div className="changed-since-summary">
                <span className="mono">{generationPair(page)}</span>
                <Link className="btn-ghost" to={impactHref} aria-label={page.mode === "service" ? "Open service impact" : "Open blast radius"}>
                  {page.mode === "service" ? "Service impact" : "Blast radius"}
                </Link>
              </div>
              {comparison ? <ChangedSincePacketComparison comparison={comparison} /> : null}
              <div className="table-scroll" tabIndex={0}>
                <table className="tbl wide">
                  <thead>
                    <tr><th>Category</th><th>Counts</th><th>Evidence samples</th></tr>
                  </thead>
                  <tbody>
                    {page.categories.map((category) => <CategoryRow category={category} key={category.category} />)}
                    {page.categories.length === 0 ? <tr><td colSpan={3} className="empty">No delta categories returned for this retained window.</td></tr> : null}
                  </tbody>
                </table>
              </div>
            </>
          ) : null}
          {!page && !error ? <p className="empty">No changed-since data loaded.</p> : null}
        </Panel>

        <Panel title="Generation lifecycle" sub="Repository freshness baselines">
          {generations ? (
            <div className="table-scroll" tabIndex={0}>
              <table className="tbl wide">
                <thead>
                  <tr><th>Generation</th><th>Status</th><th>Observed</th><th>Queue</th></tr>
                </thead>
                <tbody>
                  {generations.generations.map((generation) => (
                    <tr key={generation.generationId}>
                      <td className="cell-stack">
                        <span className="mono">{generation.generationId}</span>
                        <small>{generation.sourceSystem || "source"} / {generation.collectorKind || "collector"}</small>
                      </td>
                      <td>{generation.isActive ? <Badge tone="teal">active</Badge> : <Badge>{generation.status || "unknown"}</Badge>}</td>
                      <td className="mono">{generation.observedAt ?? "-"}</td>
                      <td>{fmt(generation.queueOutstanding)} outstanding</td>
                    </tr>
                  ))}
                  {generations.generations.length === 0 ? <tr><td colSpan={4} className="empty">No generation lifecycle rows returned.</td></tr> : null}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="empty">Repository lifecycle context loads after a repository or scope is selected.</p>
          )}
        </Panel>
      </div>
    </div>
  );
}

function FilterInput({
  label,
  onChange,
  value
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}): React.JSX.Element {
  return (
    <label>
      <span>{label}</span>
      <input aria-label={label} className="popover-input mono" onChange={(event) => onChange(event.target.value)} value={value} />
    </label>
  );
}

function CategoryRow({ category }: { readonly category: ChangedSinceCategory }): React.JSX.Element {
  return (
    <tr>
      <td className="cell-stack">
        <strong>{category.category || "unknown"}</strong>
        <small>{fmt(category.changedCount)} changed</small>
      </td>
      <td className="changed-since-counts">
        {classifications.map((classification) => (
          <span key={classification}>
            {classification} <b>{fmt(category.counts[classification])}</b>
          </span>
        ))}
      </td>
      <td>
        <div className="changed-since-samples">
          {classifications.flatMap((classification) =>
            category.samples[classification].map((sample) => (
              <span key={`${classification}:${sample.stableFactKey}:${sample.factKind}`}>
                <Badge tone={classification === "retired" || classification === "superseded" ? "warn" : "neutral"}>{classification}</Badge>
                <span className="mono">{sample.stableFactKey || "-"}</span>
                <small>{sample.factKind || "fact"}</small>
                {category.truncated[classification] ? <em>truncated</em> : null}
              </span>
            ))
          )}
          {sampleTotal(category) === 0 ? <span className="t-mut">no samples</span> : null}
        </div>
      </td>
    </tr>
  );
}

// SCOPE_PARAMS are the URL keys that signal the user has already chosen a
// changed-since scope/baseline. When none are present (a fresh page open), the
// page falls back to the catalog-derived default so it loads a delta on open.
const SCOPE_PARAMS = [
  "mode",
  "repository",
  "scope_id",
  "service_id",
  "since_generation_id",
  "since_observed_at"
] as const;

function formFromSearch(params: URLSearchParams, seedDefault: DefaultChangedSinceParams | null = null): FormState {
  const userScoped = SCOPE_PARAMS.some((key) => (params.get(key) ?? "").trim().length > 0);
  const mode = params.get("mode") === "service" ? "service" : "repository";
  return {
    mode,
    repository: params.get("repository") ?? (userScoped ? "" : seedDefault?.repository ?? ""),
    sampleLimit: params.get("sample_limit") ?? defaultLimit,
    scopeId: params.get("scope_id") ?? "",
    serviceId: params.get("service_id") ?? "",
    sinceGenerationId: params.get("since_generation_id") ?? "",
    sinceObservedAt: params.get("since_observed_at") ?? (userScoped ? "" : seedDefault?.sinceObservedAt ?? "")
  };
}

function isBounded(form: FormState): boolean {
  if (form.mode === "service") {
    return form.serviceId.trim().length > 0 && form.sinceGenerationId.trim().length > 0;
  }
  return hasRepositoryScope(form) && (form.sinceGenerationId.trim().length > 0 || form.sinceObservedAt.trim().length > 0);
}

function hasRepositoryScope(form: FormState): boolean {
  return form.repository.trim().length > 0 || form.scopeId.trim().length > 0;
}

function parsedLimit(value: string): number | undefined {
  const parsed = Number.parseInt(value.trim(), 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function optional(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

function addParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed.length > 0) params.set(key, trimmed);
}

function generationPair(page: ChangedSincePageData): string {
  const since = page.sinceGenerationId || page.sinceObservedAt || "baseline";
  const current = page.currentActiveGenerationId || page.currentObservedAt || "current";
  return `${since} -> ${current}`;
}

function impactLink(page: ChangedSincePageData): string {
  const params = new URLSearchParams();
  params.set("kind", page.mode === "service" ? "service" : "repository");
  params.set("target", page.scopeLabel || page.scopeId);
  return `/impact?${params.toString()}`;
}

function sampleTotal(category: ChangedSinceCategory): number {
  return classifications.reduce((sum, classification) => sum + category.samples[classification].length, 0);
}
