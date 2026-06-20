// pages/SurfaceInventoryPage.tsx
// Surface inventory readiness catalog. Lists the classified surfaces from
// GET /api/v0/surface-inventory — category, readiness lane, owner, proof, docs,
// and notes. The same embedded artifact backs the HTTP API, so this surface is
// in parity with it. It never fabricates state: an unavailable source renders a
// truthful empty state, not invented surfaces. Each readiness lane is shown
// exactly as classified — only "implemented" reads as production-ready; gated,
// foundation_only, and the other lanes get distinct, non-ready styling so the
// page never implies readiness a surface does not have.
import { useEffect, useMemo, useState } from "react";
import type { EshuApiClient } from "../api/client";
import { loadSurfaceInventory } from "../api/surfaceInventory";
import type { SurfaceRow } from "../api/surfaceInventory";
import { Panel, StatTile, Badge, TruthChip, FreshDot } from "../components/atoms";
import { uiTruth, uiFresh } from "../console/types";
import "./liveInventory.css";

// READINESS_TONE maps each readiness lane to a Badge tone. Only "implemented"
// uses the production-ready (teal) tone. Every other lane gets a distinct,
// caveat-bearing tone so the page never implies readiness a surface lacks.
const READINESS_TONE: Record<string, "neutral" | "teal" | "ember" | "crit" | "warn" | "violet"> = {
  implemented: "teal",
  partial: "warn",
  gated: "ember",
  foundation_only: "violet",
  fixture_only: "violet",
  research_only: "neutral",
  not_implemented: "neutral",
  unsupported: "crit"
};

function readinessLabel(readiness: string): string {
  return readiness.replace(/_/g, " ");
}

function categoryLabel(category: string): string {
  return category.replace(/_/g, " ");
}

export function SurfaceInventoryPage({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [rows, setRows] = useState<readonly SurfaceRow[] | null>(null);
  const [provenance, setProvenance] = useState<"live" | "empty" | "unavailable">("live");
  const [truthLevel, setTruthLevel] = useState<string | undefined>(undefined);
  const [freshState, setFreshState] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) { setRows([]); return; }
    void loadSurfaceInventory(client, { limit: 1000 }).then((page) => {
      if (cancelled) return;
      setRows(page.rows);
      setProvenance(page.provenance);
      setTruthLevel(page.truth?.level);
      setFreshState(page.truth?.freshness.state);
    });
    return () => { cancelled = true; };
  }, [client]);

  const all = rows ?? [];
  const filtered = useMemo(
    () =>
      all.filter((r) => {
        if (q === "") return true;
        const hay = `${r.category} ${r.name} ${r.readiness} ${r.owner}`.toLowerCase();
        return hay.includes(q.toLowerCase());
      }),
    [all, q]
  );

  const implementedCount = all.filter((r) => r.readiness === "implemented").length;
  const gatedCount = all.filter((r) => r.readiness === "gated" || r.readiness === "foundation_only").length;

  const sub =
    rows === null
      ? "loading…"
      : provenance === "unavailable"
        ? "unavailable"
        : `${sourceLabel} · ${filtered.length} shown`;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Surface Inventory</h2>
        <p>
          Readiness catalog from <span className="mono">GET /api/v0/surface-inventory</span>:
          category, readiness lane, owner, proof, docs, and notes. Only the
          <span className="mono"> implemented</span> lane is production-ready.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile label="Surfaces" value={rows === null || provenance === "unavailable" ? "—" : all.length} color="var(--teal)" sub="classified surfaces" />
        <StatTile label="Implemented" value={rows === null || provenance === "unavailable" ? "—" : implementedCount} color="var(--blue)" sub="production-ready" />
        <StatTile label="Gated / foundation" value={rows === null || provenance === "unavailable" ? "—" : gatedCount} color="var(--ember)" sub="not production-ready" />
        <StatTile label="Shown" value={rows === null || provenance === "unavailable" ? "—" : filtered.length} color="var(--violet)" sub="after filter" />
      </div>

      <Panel
        className="flush mt"
        title="Surface inventory"
        sub={sub}
        action={
          <div className="panel-action-stack">
            {truthLevel ? <TruthChip level={uiTruth(truthLevel)} /> : null}
            {freshState ? <FreshDot state={uiFresh(freshState)} /> : null}
            <div className="searchbox compact">
              <input placeholder="Filter surfaces…" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
          </div>
        }
      >
        {rows === null ? (
          <div className="conn-state compact">
            <div className="conn-spinner" aria-hidden />
            <p>Loading surface inventory…</p>
          </div>
        ) : provenance === "unavailable" ? (
          <p className="empty">Surface inventory unavailable from this source.</p>
        ) : (
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Surface</th>
                  <th>Category</th>
                  <th>Readiness</th>
                  <th>Owner</th>
                  <th>Proof</th>
                  <th>Docs</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((r) => (
                  <tr key={`${r.category}:${r.name}`}>
                    <td className="t-name">
                      {r.name}
                      {r.notes ? <div className="t-mut" style={{ fontSize: ".72rem" }}>{r.notes}</div> : null}
                    </td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>{categoryLabel(r.category)}</td>
                    <td>
                      <Badge tone={READINESS_TONE[r.readiness] ?? "neutral"}>{readinessLabel(r.readiness)}</Badge>
                    </td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.owner || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.proof || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>
                      {r.docs.length === 0 ? "—" : r.docs.join(", ")}
                    </td>
                  </tr>
                ))}
                {filtered.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="empty">
                      {q !== "" ? "No surfaces match this filter." : "No surfaces from this source."}
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </div>
  );
}
