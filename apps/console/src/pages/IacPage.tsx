// pages/IacPage.tsx
// Browse the Terraform/IaC inventory the API exposes at GET /api/v0/iac/resources.
// Live filters are URL-owned and sent to bounded server reads. Demo fixtures
// stay local, while truth and freshness come from the section envelope.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  distinctIacValues,
  iacSearchFromView,
  iacViewFromSearch,
  IacSourceLocation,
  matchesIacRow,
  type IacFilters,
} from "./iacPageSupport";
import type { EshuApiClient } from "../api/client";
import { loadIacResourcesPage } from "../api/iacResources";
import type { IacResourceCursor, IacResourceKind, IacResourcePage } from "../api/iacResources";
import { Panel, StatTile, TruthChip, FreshDot, Badge } from "../components/atoms";
import type { ConsoleModel } from "../console/types";
import { uiTruth, uiFresh } from "../console/types";
import "./liveInventory.css";

const PAGE_SIZE = 25;
const LIVE_PAGE_LIMIT = 50;

export function IacPage({
  model,
  client,
  sourceLabel = "live",
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const appliedView = useMemo(() => iacViewFromSearch(searchParams), [searchParams]);
  const applied = appliedView.filters;
  const appliedQuery = appliedView.query;
  const [livePage, setLivePage] = useState<IacResourcePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<IacFilters>(() => applied);
  const [stack, setStack] = useState<readonly (IacResourceCursor | null)[]>([null]);
  const requestSequence = useRef(0);
  const requestController = useRef<AbortController | null>(null);
  const all = livePage?.rows ?? model.iacResources;
  const provenance = livePage ? (all.length > 0 ? "live" : "empty") : model.provenance.iacResources;
  const truthLevel = livePage?.truth.level ?? model.truth.iacResources?.level;
  const freshnessState = livePage?.truth.freshness ?? model.truth.iacResources?.freshness.state;

  const [draftQuery, setDraftQuery] = useState(() => appliedQuery);
  const [page, setPage] = useState(0);
  const isDemo = sourceLabel === "demo fixtures";
  const localRows = !client || isDemo;

  // The applied view belongs to the URL. This restores explicit filter state
  // after browser back/forward without retaining rows from the newer request.
  useEffect(() => {
    setDraft(applied);
    setDraftQuery(appliedQuery);
    setStack([null]);
    setPage(0);
  }, [applied, appliedQuery]);

  // Clear stale live state when entering demo mode so private workspace rows
  // never render under the demo banner (privacy guarantee).
  useEffect(() => {
    if (isDemo) {
      requestController.current?.abort();
      requestController.current = null;
      setLivePage(null);
      setBusy(false);
      setErr("");
      setStack([null]);
    }
  }, [isDemo]);

  // Pagination requests are launched from button handlers, so the URL-owned
  // request effect does not own their cleanup. Abort whichever request is
  // current when the page itself unmounts.
  useEffect(
    () => () => {
      requestController.current?.abort();
      requestController.current = null;
    },
    [],
  );

  const fetchPage = useCallback(
    (filters: IacFilters, query: string, cursor: IacResourceCursor | null) => {
      if (!client || isDemo) return () => undefined;
      const request = ++requestSequence.current;
      requestController.current?.abort();
      const controller = new AbortController();
      requestController.current = controller;
      let cancelled = false;
      setBusy(true);
      setErr("");
      void loadIacResourcesPage(
        client,
        {
          cursor,
          includeFacets: true,
          kind: filters.kind,
          limit: LIVE_PAGE_LIMIT,
          module: filters.module.trim() || undefined,
          provider: filters.provider.trim() || undefined,
          query: query.trim() || undefined,
          repository: filters.repository.trim() || undefined,
          type: filters.type.trim() || undefined,
        },
        { signal: controller.signal },
      )
        .then((result) => {
          if (!cancelled && request === requestSequence.current) {
            setLivePage(result);
            setBusy(false);
            setPage(0);
          }
        })
        .catch((error) => {
          if (!cancelled && request === requestSequence.current) {
            setLivePage(null);
            setBusy(false);
            setErr(error instanceof Error ? error.message : "failed to load IaC resources");
          }
        });
      return () => {
        cancelled = true;
        controller.abort();
        if (requestController.current === controller) requestController.current = null;
      };
    },
    [client, isDemo],
  );

  useEffect(() => fetchPage(applied, appliedQuery, null), [fetchPage, applied, appliedQuery]);

  const summary = livePage?.summary;
  const types = useMemo(
    () =>
      summary
        ? summary.types.filter((facet) => facet.kind === draft.kind).map((facet) => facet.value)
        : distinctIacValues(all.map((row) => row.type)),
    [all, draft.kind, summary],
  );
  const providers = useMemo(
    () =>
      summary
        ? summary.providers.filter((facet) => facet.kind === draft.kind).map((facet) => facet.value)
        : distinctIacValues(all.map((row) => row.provider)),
    [all, draft.kind, summary],
  );
  const modules = useMemo(
    () =>
      summary?.modules.map((facet) => facet.value) ??
      distinctIacValues(all.map((row) => row.module)),
    [all, summary],
  );
  const repositories = useMemo(
    () =>
      summary?.repositories.map((facet) => facet.value) ??
      distinctIacValues(all.map((row) => row.repoId)),
    [all, summary],
  );
  const typeCount = summary?.truncated.types ? `${types.length}+` : types.length;
  const moduleCount = summary?.truncated.modules ? `${modules.length}+` : modules.length;

  const filtered = useMemo(
    () =>
      all.filter((row) =>
        matchesIacRow(row, localRows ? draftQuery : appliedQuery, applied, !localRows),
      ),
    [all, draftQuery, appliedQuery, applied, localRows],
  );

  // Clamp the page when filters shrink the result below the current offset.
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount - 1);
  const start = safePage * PAGE_SIZE;
  // The server cursor already bounds and advances live rows. Re-slicing that
  // page locally would skip rows between this page's display limit and the
  // server cursor. Only demo/model rows use the local pager.
  const visible = localRows ? filtered.slice(start, start + PAGE_SIZE) : filtered;

  function applyFilters(): void {
    setSearchParams(iacSearchFromView(draft, draftQuery));
  }

  function resetFilters(): void {
    setSearchParams(new URLSearchParams());
  }

  function onNext(): void {
    if (!livePage?.nextCursor) return;
    const next = livePage.nextCursor;
    setStack((current) => [...current, next]);
    fetchPage(applied, appliedQuery, next);
  }

  function onPrev(): void {
    if (stack.length <= 1) return;
    const nextStack = stack.slice(0, -1);
    setStack(nextStack);
    fetchPage(applied, appliedQuery, nextStack[nextStack.length - 1]);
  }

  const unavailable = provenance === "unavailable";
  const empty = !unavailable && (summary ? summary.total === 0 : all.length === 0);
  const noMatches = !unavailable && !empty && all.length === 0;
  const currentKindTotal = summary?.byKind[applied.kind] ?? livePage?.count ?? all.length;
  const kindLabel =
    applied.kind === "resource"
      ? "Resources"
      : applied.kind === "module"
        ? "Modules"
        : "Data sources";
  const sourceSub =
    client && !isDemo
      ? summary
        ? `${summary.total.toLocaleString("en-US")} current IaC objects`
        : `live page ${stack.length} · page-only count${livePage?.truncated ? " · more available" : ""}`
      : "bounded page from the graph";

  return (
    <div className="page">
      <div className="page-intro">
        <h2>IaC Inventory</h2>
        <p>
          Terraform resources, modules, data sources, and deployment evidence from{" "}
          <span className="mono">GET /api/v0/iac/resources</span>. Filter by type, name, provider,
          or module.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile
          label={`${kindLabel} (current)`}
          value={currentKindTotal.toLocaleString("en-US")}
          color="var(--violet)"
          sub={sourceSub}
        />
        <StatTile
          label="Rows on this page"
          value={filtered.length}
          color="var(--teal)"
          sub="bounded server result"
        />
        <StatTile
          label="Resource types"
          value={typeCount}
          color="var(--blue)"
          sub={summary ? "bounded authoritative selectors" : "distinct in this page"}
        />
        <StatTile
          label="Modules"
          value={moduleCount}
          color="var(--ember)"
          sub={summary ? "bounded authoritative selectors" : "distinct in this page"}
        />
      </div>

      <div className="evidence-workbench mt" aria-label="IaC evidence workbench">
        <Panel
          className="flush"
          title="Terraform / IaC resources"
          action={
            <span className="panel-action-stack">
              {truthLevel ? <TruthChip level={uiTruth(truthLevel)} /> : null}
              {freshnessState ? <FreshDot state={uiFresh(freshnessState)} /> : null}
            </span>
          }
        >
          <form
            className="evidence-toolbar"
            onSubmit={(event) => {
              event.preventDefault();
              applyFilters();
            }}
          >
            <input
              className="popover-input mono"
              placeholder="Search name, type, module, path…"
              value={draftQuery}
              onChange={(event) => {
                setDraftQuery(event.target.value);
                setPage(0);
              }}
              aria-label="Search IaC resources"
            />
            <select
              className="popover-input"
              value={draft.kind}
              onChange={(event) =>
                setDraft((current) => ({ ...current, kind: event.target.value as IacResourceKind }))
              }
              aria-label="Filter by kind"
            >
              <option value="resource">Resources</option>
              <option value="module">Modules</option>
              <option value="data-source">Data sources</option>
            </select>
            <input
              className="popover-input mono"
              list="iac-types"
              placeholder="type"
              value={draft.type}
              onChange={(e) => setDraft((current) => ({ ...current, type: e.target.value }))}
              aria-label="Filter by type"
            />
            <datalist id="iac-types">
              {types.map((t) => (
                <option key={t} value={t} />
              ))}
            </datalist>
            <input
              className="popover-input mono"
              list="iac-providers"
              placeholder="provider"
              value={draft.provider}
              onChange={(e) => setDraft((current) => ({ ...current, provider: e.target.value }))}
              aria-label="Filter by provider"
            />
            <datalist id="iac-providers">
              {providers.map((p) => (
                <option key={p} value={p} />
              ))}
            </datalist>
            <input
              className="popover-input mono"
              list="iac-modules"
              placeholder="module"
              value={draft.module}
              onChange={(e) => setDraft((current) => ({ ...current, module: e.target.value }))}
              aria-label="Filter by module"
            />
            <datalist id="iac-modules">
              {modules.map((m) => (
                <option key={m} value={m} />
              ))}
            </datalist>
            <input
              className="popover-input mono"
              list="iac-repositories"
              placeholder="repository"
              value={draft.repository}
              onChange={(event) =>
                setDraft((current) => ({ ...current, repository: event.target.value }))
              }
              aria-label="Filter by repository"
            />
            <datalist id="iac-repositories">
              {repositories.map((repository) => (
                <option key={repository} value={repository} />
              ))}
            </datalist>
            <button className="btn-ghost active" type="submit" disabled={busy}>
              {busy ? "Loading…" : "Apply"}
            </button>
            <button className="btn-ghost" type="button" onClick={resetFilters} disabled={busy}>
              Reset
            </button>
          </form>

          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Provider</th>
                  <th>Module</th>
                  <th>Path</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((r) => (
                  <tr key={r.id}>
                    <td className="cell-stack" style={{ maxWidth: 460 }}>
                      <span style={{ color: "var(--bone)", fontWeight: 600 }}>{r.name || "—"}</span>
                      <small>{r.resourceName || r.kind}</small>
                    </td>
                    <td className="t-name" style={{ fontSize: ".8rem" }}>
                      {r.type || "—"}
                    </td>
                    <td>
                      {r.provider ? (
                        <Badge tone="violet">{r.provider}</Badge>
                      ) : (
                        <span className="t-mut">—</span>
                      )}
                    </td>
                    <td className="t-name" style={{ fontSize: ".8rem" }}>
                      {r.module || "—"}
                    </td>
                    <td className="t-name" style={{ fontSize: ".78rem" }}>
                      <IacSourceLocation row={r} />
                    </td>
                  </tr>
                ))}
                {unavailable ? (
                  <tr>
                    <td colSpan={5} className="empty">
                      IaC inventory is not available from this API (it requires the authoritative
                      graph profile).
                    </td>
                  </tr>
                ) : err ? (
                  <tr>
                    <td colSpan={5} className="empty">
                      Failed to load IaC resources: {err}
                    </td>
                  </tr>
                ) : empty ? (
                  <tr>
                    <td colSpan={5} className="empty">
                      No Terraform/IaC resources have been indexed yet.
                    </td>
                  </tr>
                ) : noMatches || filtered.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="empty">
                      No resources match the current filter.
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>

          {client && !isDemo ? (
            <div className="pager-row">
              <button className="btn-ghost" disabled={busy || stack.length <= 1} onClick={onPrev}>
                Previous
              </button>
              <span className="t-mut" style={{ fontSize: ".78rem" }}>
                Page {stack.length} · {livePage?.limit ?? LIVE_PAGE_LIMIT} max rows
              </span>
              <button
                className="btn-ghost"
                disabled={busy || !livePage?.nextCursor}
                onClick={onNext}
              >
                Next
              </button>
            </div>
          ) : filtered.length > PAGE_SIZE ? (
            <div className="pager-row">
              <button
                className="btn-ghost"
                disabled={safePage <= 0}
                onClick={() => setPage(safePage - 1)}
              >
                Previous
              </button>
              <span className="t-mut" style={{ fontSize: ".78rem" }}>
                Page {safePage + 1} of {pageCount} · {filtered.length} resources
              </span>
              <button
                className="btn-ghost"
                disabled={safePage >= pageCount - 1}
                onClick={() => setPage(safePage + 1)}
              >
                Next
              </button>
            </div>
          ) : null}
        </Panel>
      </div>
    </div>
  );
}
