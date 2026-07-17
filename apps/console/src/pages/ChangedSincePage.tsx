import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { discoverDefaultChangedSinceParams } from "./changedSinceDefault";
import { ChangedSincePacketComparison } from "./ChangedSincePacketComparison";
import {
  ChangedSinceRepositorySelector,
  changedSinceRepositoryLabel,
} from "./ChangedSinceRepositorySelector";
import {
  ChangedSinceCategoryRows,
  FilterInput,
  GenerationLifecycleRows,
  generationPair,
  impactLink,
  sampleTotal,
} from "./ChangedSincePresentation";
import {
  addChangedSinceParam,
  changedSinceDefaultLimit,
  changedSinceFormFromSearch,
  hasChangedSincePriorReference,
  hasChangedSinceRepositoryScope,
  hasChangedSinceUserScope,
  isBoundedChangedSince,
  optionalChangedSinceValue,
  parseChangedSinceLimit,
  type ChangedSinceFormState,
} from "./changedSinceQuery";
import {
  type ChangedSinceMode,
  type ChangedSincePageData,
  type GenerationLifecyclePage,
  loadGenerationLifecycle,
  loadRepositoryChangedSince,
  loadServiceChangedSince,
} from "../api/changedSince";
import type { EshuApiClient } from "../api/client";
import { buildEvidencePacketComparison } from "../api/evidencePacketDelta";
import type { RepoListItem } from "../api/repoCatalog";
import { Badge, FreshDot, Panel, StatTile, TruthChip } from "../components/atoms";
import { defaultChangedSinceParamsFromGenerations } from "../console/defaultEntity";
import { fmt, uiFresh, uiTruth } from "../console/types";
import "./changedSincePage.css";

interface DefaultDiscoveryOwner {
  readonly client: EshuApiClient;
  readonly controller: AbortController;
  readonly promise: Promise<Awaited<ReturnType<typeof discoverDefaultChangedSinceParams>>>;
  readonly repositoryKey: string;
}

export function ChangedSincePage({
  client,
  repositories = [],
}: {
  readonly client?: EshuApiClient;
  readonly repositories?: readonly RepoListItem[];
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const defaultRequest = useRef(0);
  const defaultOwner = useRef<DefaultDiscoveryOwner | null>(null);
  const defaultAbortTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const defaultRepositoryKey = useMemo(
    () => JSON.stringify(repositories.map((repository) => repository.id.trim())),
    [repositories],
  );
  const userScoped = useMemo(() => hasChangedSinceUserScope(searchParams), [searchParams]);
  const [form, setForm] = useState<ChangedSinceFormState>(() =>
    changedSinceFormFromSearch(searchParams),
  );
  const [page, setPage] = useState<ChangedSincePageData | null>(null);
  const [generations, setGenerations] = useState<GenerationLifecyclePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [baselineState, setBaselineState] = useState("");
  const changedRequest = useRef(0);
  const generationsRequest = useRef(0);
  const generationsOwnerKey = useRef("");
  const request = useMemo(() => changedSinceFormFromSearch(searchParams), [searchParams]);
  const hasLiveClient = client !== undefined;
  const canLoadChanges = hasLiveClient && isBoundedChangedSince(request);
  const canLoadGenerations =
    hasLiveClient && request.mode === "repository" && hasChangedSinceRepositoryScope(request);

  useEffect(() => {
    if (defaultAbortTimer.current !== null) {
      clearTimeout(defaultAbortTimer.current);
      defaultAbortTimer.current = null;
    }
    const abortOwner = (owner: DefaultDiscoveryOwner): void => {
      owner.controller.abort(new DOMException("changed-since page changed", "AbortError"));
      if (defaultOwner.current === owner) defaultOwner.current = null;
    };
    const scheduleDiscoveryAbort = (owner: DefaultDiscoveryOwner | null): void => {
      if (!owner) return;
      defaultAbortTimer.current = setTimeout(() => {
        if (defaultOwner.current === owner) abortOwner(owner);
        defaultAbortTimer.current = null;
      }, 0);
    };
    const requestID = ++defaultRequest.current;
    let active = true;
    const existingOwner = defaultOwner.current;
    if (!client || userScoped) {
      if (existingOwner) abortOwner(existingOwner);
      return;
    }
    if (
      existingOwner &&
      (existingOwner.client !== client || existingOwner.repositoryKey !== defaultRepositoryKey)
    ) {
      abortOwner(existingOwner);
    }
    let owner = defaultOwner.current;
    if (!owner) {
      const controller = new AbortController();
      owner = {
        client,
        controller,
        promise: discoverDefaultChangedSinceParams(client, repositories, {
          signal: controller.signal,
        }).catch(() => null),
        repositoryKey: defaultRepositoryKey,
      };
      defaultOwner.current = owner;
    }
    void owner.promise.then((selected) => {
      if (!active || defaultRequest.current !== requestID || !selected) return;
      const params = new URLSearchParams();
      params.set("mode", "repository");
      params.set("repository", selected.repository);
      params.set("since_generation_id", selected.sinceGenerationId);
      setSearchParams(params, { replace: true });
    });
    return () => {
      active = false;
      scheduleDiscoveryAbort(owner);
    };
  }, [client, defaultRepositoryKey, repositories, setSearchParams, userScoped]);

  const load = useCallback(
    async (next: ChangedSinceFormState) => {
      if (!client || !isBoundedChangedSince(next)) return;
      const requestID = ++changedRequest.current;
      setBusy(true);
      setError("");
      try {
        const loaded =
          next.mode === "service"
            ? await loadServiceChangedSince(client, {
                sampleLimit: parseChangedSinceLimit(next.sampleLimit),
                serviceId: next.serviceId,
                sinceGenerationId: next.sinceGenerationId,
              })
            : await loadRepositoryChangedSince(client, {
                repository: optionalChangedSinceValue(next.repository),
                sampleLimit: parseChangedSinceLimit(next.sampleLimit),
                scopeId: optionalChangedSinceValue(next.scopeId),
                sinceGenerationId: optionalChangedSinceValue(next.sinceGenerationId),
                sinceObservedAt: optionalChangedSinceValue(next.sinceObservedAt),
              });
        if (changedRequest.current === requestID) setPage(loaded);
      } catch (loadError) {
        if (changedRequest.current !== requestID) return;
        setPage(null);
        setError(
          loadError instanceof Error ? loadError.message : "failed to load changed-since data",
        );
      } finally {
        if (changedRequest.current === requestID) setBusy(false);
      }
    },
    [client],
  );

  const loadGenerations = useCallback(
    async (next: ChangedSinceFormState) => {
      const requestID = ++generationsRequest.current;
      if (!client || next.mode !== "repository" || !hasChangedSinceRepositoryScope(next)) {
        setGenerations(null);
        return;
      }
      const ownerKey = `${next.repository.trim()}|${next.scopeId.trim()}`;
      try {
        const loaded = await loadGenerationLifecycle(client, {
          limit: 3,
          repository: optionalChangedSinceValue(next.repository),
          scopeId: optionalChangedSinceValue(next.scopeId),
        });
        if (generationsRequest.current !== requestID) return;
        generationsOwnerKey.current = ownerKey;
        setGenerations(loaded);
        if (hasChangedSincePriorReference(next)) {
          setBaselineState("");
          return;
        }
        const baseline = defaultChangedSinceParamsFromGenerations(loaded.generations);
        if (!baseline) {
          setBaselineState("No retained prior generation is available for this repository.");
          return;
        }
        const params = new URLSearchParams();
        params.set("mode", "repository");
        if (next.repository.trim() !== "") {
          params.set("repository", next.repository.trim());
        } else {
          params.set("scope_id", baseline.scopeId);
        }
        params.set("since_generation_id", baseline.sinceGenerationId);
        if (
          next.sampleLimit.trim() !== "" &&
          next.sampleLimit.trim() !== changedSinceDefaultLimit
        ) {
          params.set("sample_limit", next.sampleLimit.trim());
        }
        setBaselineState("");
        setSearchParams(params, { replace: true });
      } catch {
        if (generationsRequest.current === requestID) {
          generationsOwnerKey.current = "";
          setGenerations(null);
          setBaselineState("Repository generation history could not be loaded.");
        }
      }
    },
    [client, setSearchParams],
  );

  useEffect(() => {
    setForm(request);
    if (canLoadChanges) {
      void load(request);
    } else {
      changedRequest.current += 1;
      setPage(null);
      setError("");
      setBusy(false);
    }
    if (canLoadGenerations) {
      const ownerKey = `${request.repository.trim()}|${request.scopeId.trim()}`;
      if (generationsOwnerKey.current !== ownerKey) void loadGenerations(request);
    } else {
      generationsRequest.current += 1;
      generationsOwnerKey.current = "";
      setGenerations(null);
    }
  }, [canLoadChanges, canLoadGenerations, load, loadGenerations, request]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const params = new URLSearchParams();
    params.set("mode", form.mode);
    if (form.mode === "service") {
      addChangedSinceParam(params, "service_id", form.serviceId);
      addChangedSinceParam(params, "since_generation_id", form.sinceGenerationId);
    } else {
      addChangedSinceParam(params, "repository", form.repository);
      addChangedSinceParam(params, "scope_id", form.scopeId);
      addChangedSinceParam(params, "since_generation_id", form.sinceGenerationId);
      addChangedSinceParam(params, "since_observed_at", form.sinceObservedAt);
    }
    if (form.sampleLimit.trim() !== "" && form.sampleLimit.trim() !== changedSinceDefaultLimit) {
      params.set("sample_limit", form.sampleLimit.trim());
    }
    setSearchParams(params);
  }

  function selectRepository(repository: string): void {
    changedRequest.current += 1;
    generationsRequest.current += 1;
    generationsOwnerKey.current = "";
    setPage(null);
    setGenerations(null);
    setError("");
    setBaselineState("");
    setBusy(false);
    const params = new URLSearchParams();
    params.set("mode", "repository");
    addChangedSinceParam(params, "repository", repository);
    if (form.sampleLimit.trim() !== "" && form.sampleLimit.trim() !== changedSinceDefaultLimit) {
      params.set("sample_limit", form.sampleLimit.trim());
    }
    setSearchParams(params);
  }

  const categoryCount = page?.categories.length ?? 0;
  const sampleCount =
    page?.categories.reduce((sum, category) => sum + sampleTotal(category), 0) ?? 0;
  const impactHref = page ? impactLink(page) : "";
  const comparison = page && !page.unavailable ? buildEvidencePacketComparison(page) : null;
  const panelScopeLabel =
    page?.mode === "repository"
      ? changedSinceRepositoryLabel(repositories, page.scopeLabel || request.repository)
      : page?.scopeLabel || page?.scopeId || "selected scope";
  const selectedRepositoryId =
    form.repository.trim() !== ""
      ? form.repository
      : request.scopeId.trim() !== "" && page?.mode === "repository"
        ? page.scopeLabel
        : "";

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
            onChange={(event) =>
              setForm((current) => ({ ...current, mode: event.target.value as ChangedSinceMode }))
            }
          >
            <option value="repository">Repository</option>
            <option value="service">Service</option>
          </select>
        </label>
        {form.mode === "service" ? (
          <FilterInput
            label="Service ID"
            value={form.serviceId}
            onChange={(value) => setForm((current) => ({ ...current, serviceId: value }))}
          />
        ) : (
          <ChangedSinceRepositorySelector
            onChange={selectRepository}
            repositories={repositories}
            selectedRepositoryId={selectedRepositoryId}
          />
        )}
        <FilterInput
          label="Since generation"
          value={form.sinceGenerationId}
          onChange={(value) => setForm((current) => ({ ...current, sinceGenerationId: value }))}
        />
        {form.mode === "repository" ? (
          <FilterInput
            label="Since observed at"
            value={form.sinceObservedAt}
            onChange={(value) => setForm((current) => ({ ...current, sinceObservedAt: value }))}
          />
        ) : null}
        <FilterInput
          label="Sample limit"
          value={form.sampleLimit}
          onChange={(value) => setForm((current) => ({ ...current, sampleLimit: value }))}
        />
        <button
          className="btn-ghost active"
          disabled={!hasLiveClient || busy || !isBoundedChangedSince(form)}
          type="submit"
        >
          {busy ? "Loading..." : "Load changes"}
        </button>
      </form>

      {!hasLiveClient ? (
        <p className="inline-state">Live Eshu API connection unavailable.</p>
      ) : null}
      {!isBoundedChangedSince(request) ? (
        <p className="inline-state">
          Choose a repository/scope or service and a baseline to load changed-since evidence.
        </p>
      ) : null}
      {error ? <p className="src-err">{error}</p> : null}
      {baselineState ? <p className="inline-state">{baselineState}</p> : null}

      <div className="grid g-4 mt">
        <StatTile
          label="Changed"
          value={fmt(page?.changedCount ?? 0)}
          color="var(--teal)"
          sub={page ? "selected retained window" : "no baseline loaded"}
        />
        <StatTile
          label="Unchanged"
          value={fmt(page?.unchangedCount ?? 0)}
          color="var(--blue)"
          sub="stable across the selected window"
        />
        <StatTile
          label="Categories"
          value={fmt(categoryCount)}
          color="var(--violet)"
          sub="bounded endpoint categories"
        />
        <StatTile
          label="Samples"
          value={fmt(sampleCount)}
          color="var(--ember)"
          sub={page ? `${page.sampleLimit} max per verdict` : "not loaded"}
        />
      </div>

      <div className="changed-since-grid mt">
        <Panel
          title="Delta evidence"
          sub={page ? panelScopeLabel : "No changed-since data loaded"}
          action={
            page ? (
              <span className="panel-action-stack">
                <TruthChip level={uiTruth(page.truth.level)} />
                <FreshDot state={uiFresh(page.truth.freshness.state)} />
              </span>
            ) : null
          }
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
                <Link
                  className="btn-ghost"
                  to={impactHref}
                  aria-label={page.mode === "service" ? "Open service impact" : "Open blast radius"}
                >
                  {page.mode === "service" ? "Service impact" : "Blast radius"}
                </Link>
              </div>
              {comparison ? <ChangedSincePacketComparison comparison={comparison} /> : null}
              <div className="table-scroll" tabIndex={0}>
                <table className="tbl wide">
                  <thead>
                    <tr>
                      <th>Category</th>
                      <th>Counts</th>
                      <th>Evidence samples</th>
                    </tr>
                  </thead>
                  <tbody>
                    <ChangedSinceCategoryRows
                      categories={page.categories}
                      repositoryId={page.scopeLabel || request.repository}
                    />
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
                  <tr>
                    <th>Generation</th>
                    <th>Status</th>
                    <th>Observed</th>
                    <th>Queue</th>
                  </tr>
                </thead>
                <tbody>
                  <GenerationLifecycleRows generations={generations} />
                </tbody>
              </table>
            </div>
          ) : (
            <p className="empty">
              Repository lifecycle context loads after a repository or scope is selected.
            </p>
          )}
        </Panel>
      </div>
    </div>
  );
}
