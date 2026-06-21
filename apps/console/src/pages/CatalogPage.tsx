// pages/CatalogPage.tsx
import { useState } from "react";
import type { ConsoleModel } from "../console/types";
import { Panel, TruthChip, FreshDot } from "../components/atoms";
import { AsyncStateGuard } from "../components/AsyncStateGuard";

export function CatalogPage({ model, onOpenService }: { readonly model: ConsoleModel; readonly onOpenService?: (name: string) => void }): React.JSX.Element {
  const [q, setQ] = useState("");
  const rows = model.services.filter((s) => q === "" || `${s.name} ${s.repo} ${s.kind}`.toLowerCase().includes(q.toLowerCase()));
  const provenance = model.provenance.services ?? (model.source === "demo" ? "demo" : "loading");
  return (
    <div className="page">
      <div className="page-intro">
        <h2>Catalog</h2>
        <p>
          Every indexed service, repository and workload from{" "}
          <span className="mono">GET /api/v0/catalog?limit=2000</span>, with
          coverage, freshness and truth level.
        </p>
      </div>
      <Panel className="flush" title={`${rows.length} entries`} sub={model.source === "live" ? "live catalog rows" : "demo fixtures"}
        action={<div className="searchbox" style={{ minWidth: 220, height: 34 }}><input placeholder="Filter catalog…" value={q} onChange={(e) => setQ(e.target.value)} /></div>}>
        <AsyncStateGuard provenance={provenance} label="catalog">
          <table className="tbl">
            <thead><tr><th>Name</th><th>Kind</th><th>Repository</th><th>Environments</th><th>Truth</th><th>Freshness</th></tr></thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id} onClick={() => onOpenService?.(s.name)} style={{ cursor: "pointer" }}>
                  <td className="t-name">{s.name}</td>
                  <td className="t-mut">{s.kind}</td>
                  <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{s.repo || "—"}</td>
                  <td className="t-mut">{s.environments.length > 0 ? s.environments.join(", ") : "—"}</td>
                  <td><TruthChip level={s.truth === "fallback" ? "inferred" : s.truth === "derived" ? "derived" : "exact"} /></td>
                  <td><FreshDot state={s.freshness === "building" ? "lagging" : s.freshness === "stale" || s.freshness === "unavailable" ? "stale" : "fresh"} /></td>
                </tr>
              ))}
              {rows.length === 0 ? <tr><td colSpan={6} className="empty">No catalog entries from this source.</td></tr> : null}
            </tbody>
          </table>
        </AsyncStateGuard>
      </Panel>
    </div>
  );
}
