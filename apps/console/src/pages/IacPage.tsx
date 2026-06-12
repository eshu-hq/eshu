// pages/IacPage.tsx
// Browse the Terraform/IaC inventory the API exposes at GET /api/v0/iac/resources.
// The snapshot loads a bounded page (limit=200); this page filters and paginates
// that set client-side so the operator can scan by type, name, provider, and
// module without re-querying. Truth and freshness come from the section envelope.
import { useMemo, useState } from "react";
import type { ConsoleModel, IacResourceRow } from "../console/types";
import { uiTruth, uiFresh } from "../console/types";
import { Panel, StatTile, TruthChip, FreshDot, Badge } from "../components/atoms";
import "./liveInventory.css";

const PAGE_SIZE = 25;

function distinct(values: readonly string[]): readonly string[] {
  return [...new Set(values.filter((v) => v !== ""))].sort();
}

function matches(row: IacResourceRow, q: string, type: string, provider: string, module: string): boolean {
  if (type !== "" && row.type !== type) return false;
  if (provider !== "" && row.provider !== provider) return false;
  if (module !== "" && row.module !== module) return false;
  if (q !== "") {
    const needle = q.toLowerCase();
    const haystack = `${row.name} ${row.type} ${row.module} ${row.relativePath}`.toLowerCase();
    if (!haystack.includes(needle)) return false;
  }
  return true;
}

export function IacPage({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const all = model.iacResources;
  const provenance = model.provenance.iacResources;
  const sectionTruth = model.truth.iacResources;

  const [q, setQ] = useState("");
  const [type, setType] = useState("");
  const [provider, setProvider] = useState("");
  const [module, setModule] = useState("");
  const [page, setPage] = useState(0);

  const types = useMemo(() => distinct(all.map((r) => r.type)), [all]);
  const providers = useMemo(() => distinct(all.map((r) => r.provider)), [all]);
  const modules = useMemo(() => distinct(all.map((r) => r.module)), [all]);

  const filtered = useMemo(
    () => all.filter((r) => matches(r, q, type, provider, module)),
    [all, q, type, provider, module]
  );

  // Clamp the page when filters shrink the result below the current offset.
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount - 1);
  const start = safePage * PAGE_SIZE;
  const visible = filtered.slice(start, start + PAGE_SIZE);

  const reset = (set: (v: string) => void) => (v: string): void => { set(v); setPage(0); };

  const unavailable = provenance === "unavailable";
  const empty = !unavailable && all.length === 0;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>IaC Inventory</h2>
        <p>Terraform resources, modules, and data sources from the authoritative graph. Filter by type, name, provider, or module.</p>
      </div>

      <div className="grid g-4">
        <StatTile label="Resources (loaded)" value={all.length} color="var(--violet)" sub="bounded page from the graph" />
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
              {sectionTruth ? <TruthChip level={uiTruth(sectionTruth.level)} /> : null}
              {sectionTruth ? <FreshDot state={uiFresh(sectionTruth.freshness.state)} /> : null}
            </span>
          }
        >
          <div className="evidence-toolbar">
            <input
              className="popover-input mono"
              placeholder="Search name, type, module, path…"
              value={q}
              onChange={(e) => reset(setQ)(e.target.value)}
              aria-label="Search IaC resources"
            />
            <select className="popover-input" value={type} onChange={(e) => reset(setType)(e.target.value)} aria-label="Filter by type">
              <option value="">All types</option>
              {types.map((t) => <option key={t} value={t}>{t}</option>)}
            </select>
            <select className="popover-input" value={provider} onChange={(e) => reset(setProvider)(e.target.value)} aria-label="Filter by provider">
              <option value="">All providers</option>
              {providers.map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
            <select className="popover-input" value={module} onChange={(e) => reset(setModule)(e.target.value)} aria-label="Filter by module">
              <option value="">All modules</option>
              {modules.map((m) => <option key={m} value={m}>{m}</option>)}
            </select>
          </div>

          <div className="table-scroll">
            <table className="tbl wide">
              <thead><tr><th>Name</th><th>Type</th><th>Provider</th><th>Module</th><th>Path</th></tr></thead>
              <tbody>
                {visible.map((r) => (
                  <tr key={r.id}>
                    <td className="cell-stack" style={{ maxWidth: 460 }}>
                      <span style={{ color: "var(--bone)", fontWeight: 600 }}>{r.name || "—"}</span>
                      <small>{r.kind}</small>
                    </td>
                    <td className="t-name" style={{ fontSize: ".8rem" }}>{r.type || "—"}</td>
                    <td>{r.provider ? <Badge tone="violet">{r.provider}</Badge> : <span className="t-mut">—</span>}</td>
                    <td className="t-name" style={{ fontSize: ".8rem" }}>{r.module || "—"}</td>
                    <td className="t-name" style={{ fontSize: ".78rem" }}>{r.relativePath || "—"}</td>
                  </tr>
                ))}
                {unavailable ? (
                  <tr><td colSpan={5} className="empty">IaC inventory is not available from this API (it requires the authoritative graph profile).</td></tr>
                ) : empty ? (
                  <tr><td colSpan={5} className="empty">No Terraform/IaC resources have been indexed yet.</td></tr>
                ) : filtered.length === 0 ? (
                  <tr><td colSpan={5} className="empty">No resources match the current filter.</td></tr>
                ) : null}
              </tbody>
            </table>
          </div>

          {filtered.length > PAGE_SIZE ? (
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
