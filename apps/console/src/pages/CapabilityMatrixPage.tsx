// pages/CapabilityMatrixPage.tsx
// Capability maturity matrix. Lists the reconciled capability catalog from
// GET /api/v0/capabilities — maturity, public surfaces, proof signals, owner
// package, known gaps, and linked issues. The same embedded artifact backs the
// HTTP API and the MCP get_capability_catalog tool, so this surface is in parity
// with both. It never fabricates state: an unavailable source renders a truthful
// empty state, not invented capabilities.
import { useEffect, useMemo, useState } from "react";

import { loadCapabilityCatalog } from "../api/capabilityCatalog";
import type { CapabilityRow } from "../api/capabilityCatalog";
import type { EshuApiClient } from "../api/client";
import { Panel, StatTile, Badge, TruthChip, FreshDot } from "../components/atoms";
import { uiTruth, uiFresh } from "../console/types";
import "./liveInventory.css";

const MATURITY_TONE: Record<string, "neutral" | "teal" | "ember" | "crit" | "warn" | "violet"> = {
  general_availability: "teal",
  experimental: "violet",
  preview: "warn",
  gated: "ember",
  degraded: "crit",
  not_implemented: "neutral"
};

function maturityLabel(maturity: string): string {
  return maturity.replace(/_/g, " ");
}

export function CapabilityMatrixPage({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [rows, setRows] = useState<readonly CapabilityRow[] | null>(null);
  const [provenance, setProvenance] = useState<"live" | "empty" | "unavailable">("live");
  const [truthLevel, setTruthLevel] = useState<string | undefined>(undefined);
  const [freshState, setFreshState] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) { setRows([]); return; }
    void loadCapabilityCatalog(client, { limit: 500 }).then((page) => {
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
        const hay = `${r.capability} ${r.displayName} ${r.maturity} ${r.ownerPackage}`.toLowerCase();
        return hay.includes(q.toLowerCase());
      }),
    [all, q]
  );

  const gaCount = all.filter((r) => r.maturity === "general_availability").length;
  const gatedCount = all.filter((r) => r.maturity === "gated" || r.maturity === "degraded").length;

  const sub =
    rows === null
      ? "loading…"
      : provenance === "unavailable"
        ? "unavailable"
        : `${sourceLabel} · ${filtered.length} shown`;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Capability Matrix</h2>
        <p>
          Reconciled capability catalog from <span className="mono">GET /api/v0/capabilities</span>:
          maturity, public surfaces, proof signals, owner, known gaps, and linked issues.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile label="Capabilities" value={rows === null || provenance === "unavailable" ? "—" : all.length} color="var(--teal)" sub="reconciled catalog entries" />
        <StatTile label="Generally available" value={rows === null || provenance === "unavailable" ? "—" : gaCount} color="var(--blue)" sub="production-supported" />
        <StatTile label="Gated / degraded" value={rows === null || provenance === "unavailable" ? "—" : gatedCount} color="var(--ember)" sub="operational caveats" />
        <StatTile label="Shown" value={rows === null || provenance === "unavailable" ? "—" : filtered.length} color="var(--violet)" sub="after filter" />
      </div>

      <Panel
        className="flush mt"
        title="Capability catalog"
        sub={sub}
        action={
          <div className="panel-action-stack">
            {truthLevel ? <TruthChip level={uiTruth(truthLevel)} /> : null}
            {freshState ? <FreshDot state={uiFresh(freshState)} /> : null}
            <div className="searchbox compact">
              <input placeholder="Filter capabilities…" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
          </div>
        }
      >
        {rows === null ? (
          <div className="conn-state compact">
            <div className="conn-spinner" aria-hidden />
            <p>Loading capability catalog…</p>
          </div>
        ) : provenance === "unavailable" ? (
          <p className="empty">Capability catalog unavailable from this source.</p>
        ) : (
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Capability</th>
                  <th>Maturity</th>
                  <th>Surfaces</th>
                  <th>Proof</th>
                  <th>Owner</th>
                  <th>Issues</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((r) => (
                  <tr key={r.capability}>
                    <td className="t-name">
                      {r.displayName}
                      <div className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.capability}</div>
                    </td>
                    <td>
                      <Badge tone={MATURITY_TONE[r.maturity] ?? "neutral"}>{maturityLabel(r.maturity)}</Badge>
                      {r.maturityReason ? <div className="t-mut" style={{ fontSize: ".72rem" }}>{r.maturityReason}</div> : null}
                    </td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>
                      {r.surfaces.length === 0 ? "—" : r.surfaces.map((s) => `${s.tool} (${s.kind})`).join(", ")}
                    </td>
                    <td className="t-mut" style={{ fontSize: ".74rem" }}>{r.proofSignals.length}</td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.ownerPackage || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".72rem" }}>
                      {r.linkedIssues.length === 0 ? "—" : r.linkedIssues.map((n) => `#${n}`).join(", ")}
                    </td>
                  </tr>
                ))}
                {filtered.length === 0 ? (
                  <tr>
                    <td colSpan={6} className="empty">
                      {q !== "" ? "No capabilities match this filter." : "No capabilities from this source."}
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
