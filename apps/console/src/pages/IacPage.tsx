// pages/IacPage.tsx
// Browse the Terraform/IaC inventory the API exposes at GET /api/v0/iac/resources.
// The snapshot loads a bounded page (limit=200); this page filters and paginates
// that set client-side so the operator can scan by type, name, provider, and
// module without re-querying. Truth and freshness come from the section envelope.
import { useCallback, useEffect, useMemo, useState } from "react";
import type { EshuApiClient } from "../api/client";
import { loadIacResourcesPage } from "../api/iacResources";
import type { IacResourceCursor, IacResourceKind, IacResourcePage } from "../api/iacResources";
import type { ConsoleModel, IacResourceRow } from "../console/types";
import { uiTruth, uiFresh } from "../console/types";
import { Panel, StatTile, TruthChip, FreshDot, Badge } from "../components/atoms";
import "./liveInventory.css";

const PAGE_SIZE = 25;
const LIVE_PAGE_LIMIT = 50;

interface IacFilters {
  readonly kind: IacResourceKind;
  readonly type: string;
  readonly provider: string;
  readonly module: string;
}

const DEFAULT_FILTERS: IacFilters = { kind: "resource", type: "", provider: "", module: "" };

function distinct(values: readonly string[]): readonly string[] {
  return [...new Set(values.filter((v) => v !== ""))].sort();
}

function matches(row: IacResourceRow, q: string, filters: IacFilters, serverSideFilters: boolean): boolean {
  // With a live client (non-demo), typed/provider/module/kind filters are server-side. Keep
  // the browser filter limited to free-text within the current bounded page.
  if (!serverSideFilters) {
    if (row.kind !== filters.kind) return false;
    if (filters.type !== "" && row.type !== filters.type) return false;
    if (filters.provider !== "" && row.provider !== filters.provider) return false;
    if (filters.module !== "" && row.module !== filters.module) return false;
  }
  if (q !== "") {
    const needle = q.toLowerCase();
    const haystack = `${row.name} ${row.resourceName} ${row.type} ${row.provider} ${row.service} ${row.category} ${row.module} ${row.relativePath}`.toLowerCase();
    if (!haystack.includes(needle)) return false;
  }
  return true;
}

export function IacPage({ model, client, sourceLabel = "live" }: { readonly model: ConsoleModel; readonly client?: EshuApiClient; readonly sourceLabel?: string }): React.JSX.Element {
  const [livePage, setLivePage] = useState<IacResourcePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<IacFilters>(DEFAULT_FILTERS);
  const [applied, setApplied] = useState<IacFilters>(DEFAULT_FILTERS);
  const [stack, setStack] = useState<readonly (IacResourceCursor | null)[]>([null]);
  const all = livePage?.rows ?? model.iacResources;
  const provenance = livePage ? (all.length > 0 ? "live" : "empty") : model.provenance.iacResources;
  const truthLevel = livePage?.truth.level ?? model.truth.iacResources?.level;
  const freshnessState = livePage?.truth.freshness ?? model.truth.iacResources?.freshness.state;

  const [q, setQ] = useState("");
  const [page, setPage] = useState(0);
  const isDemo = sourceLabel === "demo fixtures";

  // Clear stale live state when entering demo mode so private workspace rows
  // never render under the demo banner (privacy guarantee).
  useEffect(() => {
    if (isDemo) {
      setLivePage(null);
      setErr("");
      setStack([null]);
    }
  }, [isDemo]);

  const fetchPage = useCallback((filters: IacFilters, cursor: IacResourceCursor | null) => {
    if (!client || isDemo) return () => undefined;
    let cancelled = false;
    setBusy(true); setErr("");
    void loadIacResourcesPage(client, {
      cursor,
      kind: filters.kind,
      limit: LIVE_PAGE_LIMIT,
      module: filters.module.trim() || undefined,
      provider: filters.provider.trim() || undefined,
      type: filters.type.trim() || undefined
    }).then((result) => {
      if (!cancelled) {
        setLivePage(result);
        setBusy(false);
        setPage(0);
      }
    }).catch((error) => {
      if (!cancelled) {
        setLivePage(null);
        setBusy(false);
        setErr(error instanceof Error ? error.message : "failed to load IaC resources");
      }
    });
    return () => { cancelled = true; };
  }, [client, isDemo]);

  useEffect(() => fetchPage(applied, null), [fetchPage, applied]);

  const types = useMemo(() => distinct(all.map((r) => r.type)), [all]);
  const providers = useMemo(() => distinct(all.map((r) => r.provider)), [all]);
  const modules = useMemo(() => distinct(all.map((r) => r.module)), [all]);

  const filtered = useMemo(
    () => all.filter((r) => matches(r, q, applied, !!client && !isDemo)),
    [all, q, applied, client, isDemo]
  );

  // Clamp the page when filters shrink the result below the current offset.
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount - 1);
  const start = safePage * PAGE_SIZE;
  const visible = filtered.slice(start, start + PAGE_SIZE);

  function applyFilters(): void {
    setStack([null]);
    setApplied(draft);
    setPage(0);
  }

  function resetFilters(): void {
    setDraft(DEFAULT_FILTERS);
    setStack([null]);
    setApplied(DEFAULT_FILTERS);
    setQ("");
    setPage(0);
  }

  function onNext(): void {
    if (!livePage?.nextCursor) return;
    const next = livePage.nextCursor;
    setStack((current) => [...current, next]);
    fetchPage(applied, next);
  }

  function onPrev(): void {
    if (stack.length <= 1) return;
    const nextStack = stack.slice(0, -1);
    setStack(nextStack);
    fetchPage(applied, nextStack[nextStack.length - 1]);
  }

  const unavailable = provenance === "unavailable";
  const empty = !unavailable && all.length === 0;
  const sourceSub = client && !isDemo
    ? `live page ${stack.length}${livePage?.truncated ? " · more available" : ""}`
    : "bounded page from the graph";

  return (
    <div className="page">
      <div className="page-intro">
        <h2>IaC Inventory</h2>
        <p>
          Terraform resources, modules, data sources, and deployment evidence from{" "}
          <span className="mono">GET /api/v0/iac/resources</span>. Filter by
          type, name, provider, or module.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile label="Resources (loaded)" value={livePage?.count ?? all.length} color="var(--violet)" sub={sourceSub} />
        <StatTile label="Matching filter" value={filtered.length} color="var(--teal)" sub="after type / name / provider / module" />
        <StatTile label="Resource types" value={types.length} color="var(--blue)" sub="distinct in this page" />
        <StatTile label="Modules" value={modules.length} color="var(--ember)" sub="distinct in this page" />
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
          <form className="evidence-toolbar" onSubmit={(event) => { event.preventDefault(); applyFilters(); }}>
            <input
              className="popover-input mono"
              placeholder="Search name, type, module, path…"
              value={q}
              onChange={(e) => { setQ(e.target.value); setPage(0); }}
              aria-label="Search IaC resources"
            />
            <select
              className="popover-input"
              value={draft.kind}
              onChange={(event) => setDraft((current) => ({ ...current, kind: event.target.value as IacResourceKind }))}
              aria-label="Filter by kind"
            >
              <option value="resource">Resources</option>
              <option value="module">Modules</option>
              <option value="data-source">Data sources</option>
            </select>
            <input className="popover-input mono" list="iac-types" placeholder="type" value={draft.type} onChange={(e) => setDraft((current) => ({ ...current, type: e.target.value }))} aria-label="Filter by type" />
            <datalist id="iac-types">{types.map((t) => <option key={t} value={t} />)}</datalist>
            <input className="popover-input mono" list="iac-providers" placeholder="provider" value={draft.provider} onChange={(e) => setDraft((current) => ({ ...current, provider: e.target.value }))} aria-label="Filter by provider" />
            <datalist id="iac-providers">{providers.map((p) => <option key={p} value={p} />)}</datalist>
            <input className="popover-input mono" list="iac-modules" placeholder="module" value={draft.module} onChange={(e) => setDraft((current) => ({ ...current, module: e.target.value }))} aria-label="Filter by module" />
            <datalist id="iac-modules">{modules.map((m) => <option key={m} value={m} />)}</datalist>
            <button className="btn-ghost active" type="submit" disabled={busy}>{busy ? "Loading…" : "Apply"}</button>
            <button className="btn-ghost" type="button" onClick={resetFilters} disabled={busy}>Reset</button>
          </form>

          <div className="table-scroll">
            <table className="tbl wide">
              <thead><tr><th>Name</th><th>Type</th><th>Provider</th><th>Module</th><th>Path</th></tr></thead>
              <tbody>
                {visible.map((r) => (
                  <tr key={r.id}>
                    <td className="cell-stack" style={{ maxWidth: 460 }}>
                      <span style={{ color: "var(--bone)", fontWeight: 600 }}>{r.name || "—"}</span>
                      <small>{r.resourceName || r.kind}</small>
                    </td>
                    <td className="t-name" style={{ fontSize: ".8rem" }}>{r.type || "—"}</td>
                    <td>{r.provider ? <Badge tone="violet">{r.provider}</Badge> : <span className="t-mut">—</span>}</td>
                    <td className="t-name" style={{ fontSize: ".8rem" }}>{r.module || "—"}</td>
                    <td className="t-name" style={{ fontSize: ".78rem" }}>{sourceLocation(r)}</td>
                  </tr>
                ))}
                {unavailable ? (
                  <tr><td colSpan={5} className="empty">IaC inventory is not available from this API (it requires the authoritative graph profile).</td></tr>
                ) : err ? (
                  <tr><td colSpan={5} className="empty">Failed to load IaC resources: {err}</td></tr>
                ) : empty ? (
                  <tr><td colSpan={5} className="empty">No Terraform/IaC resources have been indexed yet.</td></tr>
                ) : filtered.length === 0 ? (
                  <tr><td colSpan={5} className="empty">No resources match the current filter.</td></tr>
                ) : null}
              </tbody>
            </table>
          </div>

          {client && !isDemo ? (
            <div className="pager-row">
              <button className="btn-ghost" disabled={busy || stack.length <= 1} onClick={onPrev}>Previous</button>
              <span className="t-mut" style={{ fontSize: ".78rem" }}>
                Page {stack.length} · {livePage?.limit ?? LIVE_PAGE_LIMIT} max rows
              </span>
              <button className="btn-ghost" disabled={busy || !livePage?.nextCursor} onClick={onNext}>Next</button>
            </div>
          ) : filtered.length > PAGE_SIZE ? (
            <div className="pager-row">
              <button className="btn-ghost" disabled={safePage <= 0} onClick={() => setPage(safePage - 1)}>Previous</button>
              <span className="t-mut" style={{ fontSize: ".78rem" }}>
                Page {safePage + 1} of {pageCount} · {filtered.length} resources
              </span>
              <button className="btn-ghost" disabled={safePage >= pageCount - 1} onClick={() => setPage(safePage + 1)}>Next</button>
            </div>
          ) : null}
        </Panel>
      </div>
    </div>
  );
}

function sourceLocation(row: IacResourceRow): string {
  if (row.relativePath && row.lineNumber !== null) return `${row.relativePath}:${row.lineNumber}`;
  return row.relativePath || "—";
}
