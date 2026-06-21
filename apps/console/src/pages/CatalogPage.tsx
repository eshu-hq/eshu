// pages/CatalogPage.tsx
import { useState } from "react";
import type { ConsoleModel, FindingRow, Severity } from "../console/types";
import { SEVERITY_COLOR } from "../console/types";
import { Panel, TruthChip, FreshDot, Badge } from "../components/atoms";
import { AsyncStateGuard } from "../components/AsyncStateGuard";

// severityBarCounts aggregates finding rows by severity for one service name.
// Findings are matched by entity name so the bar only covers findings whose
// entity string matches the service name exactly (case-insensitive). The bar
// is a UI-only computation from data already in the snapshot; no extra API
// call is issued.
function severityBarCounts(
  name: string,
  findings: readonly FindingRow[]
): Partial<Record<Severity, number>> {
  const counts: Partial<Record<Severity, number>> = {};
  const lower = name.toLowerCase();
  for (const f of findings) {
    if (f.entity.toLowerCase() !== lower) continue;
    for (const label of f.labels ?? []) {
      const s = label.toLowerCase() as Severity;
      if (s in SEVERITY_COLOR) {
        counts[s] = (counts[s] ?? 0) + 1;
        break;
      }
    }
  }
  return counts;
}

// SeverityBar renders a compact stacked bar of finding counts per severity.
// Each segment is a coloured pill labelled with its count. Segments with zero
// count are omitted. Returns null when no counts exist so callers can render
// "—" instead.
function SeverityBar({ counts }: { readonly counts: Partial<Record<Severity, number>> }): React.JSX.Element | null {
  const ORDER: Severity[] = ["critical", "high", "medium", "low", "info"];
  const segments = ORDER.filter((s) => (counts[s] ?? 0) > 0);
  if (segments.length === 0) return null;
  return (
    <span style={{ display: "inline-flex", gap: 3, alignItems: "center" }}>
      {segments.map((s) => (
        <span
          key={s}
          title={`${counts[s]} ${s}`}
          style={{
            display: "inline-flex", alignItems: "center", justifyContent: "center",
            minWidth: 18, height: 16, borderRadius: 4, padding: "0 4px",
            background: SEVERITY_COLOR[s], color: "#fff",
            fontSize: ".68rem", fontWeight: 700, lineHeight: 1
          }}
        >
          {counts[s]}
        </span>
      ))}
    </span>
  );
}

// tierTone maps tier labels to Badge tone variants so tier-1 reads as critical
// (high-risk), tier-2 as ember (medium), tier-3 as neutral, and libraries as
// violet (supporting).
function tierTone(tier: string | undefined): "crit" | "ember" | "neutral" | "violet" {
  if (!tier) return "neutral";
  const t = tier.toLowerCase();
  if (t === "tier-1" || t === "1") return "crit";
  if (t === "tier-2" || t === "2") return "ember";
  if (t === "library" || t === "lib") return "violet";
  return "neutral";
}

export function CatalogPage({ model, onOpenService }: { readonly model: ConsoleModel; readonly onOpenService?: (name: string) => void }): React.JSX.Element {
  const [q, setQ] = useState("");
  const rows = model.services.filter(
    (s) =>
      q === "" ||
      `${s.name} ${s.repo} ${s.kind} ${s.tier ?? ""} ${s.category ?? ""} ${s.domain ?? ""}`.toLowerCase().includes(q.toLowerCase())
  );
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
      <Panel
        className="flush"
        title={`${rows.length} entries`}
        sub={model.source === "live" ? "live catalog rows" : "demo fixtures"}
        action={
          <div className="searchbox" style={{ minWidth: 220, height: 34 }}>
            <input placeholder="Filter catalog…" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
        }
      >
        <AsyncStateGuard provenance={provenance} label="catalog">
          <table className="tbl">
            <thead>
              <tr>
                <th>Name</th>
                <th>Kind</th>
                <th>Tier</th>
                <th>Category</th>
                <th>Domain</th>
                <th>Language</th>
                <th>Repository</th>
                <th>Environments</th>
                <th>Findings</th>
                <th>Truth</th>
                <th>Freshness</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => {
                const sevCounts = severityBarCounts(s.name, model.findings);
                return (
                  <tr key={s.id} onClick={() => onOpenService?.(s.name)} style={{ cursor: "pointer" }}>
                    <td className="t-name">{s.name}</td>
                    <td className="t-mut">{s.kind}</td>
                    <td>
                      {s.tier ? (
                        <Badge tone={tierTone(s.tier)}>{s.tier}</Badge>
                      ) : (
                        <span className="t-mut">—</span>
                      )}
                    </td>
                    <td className="t-mut">{s.category || "—"}</td>
                    <td className="t-mut">{s.domain || "—"}</td>
                    <td className="t-mut">{s.language || "—"}</td>
                    <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{s.repo || "—"}</td>
                    <td className="t-mut">
                      {s.environments.length > 0 ? `${s.environments.length} env` : "—"}
                    </td>
                    <td>
                      <SeverityBar counts={sevCounts} />
                      {Object.keys(sevCounts).length === 0 ? <span className="t-mut">—</span> : null}
                    </td>
                    <td><TruthChip level={s.truth === "fallback" ? "inferred" : s.truth === "derived" ? "derived" : "exact"} /></td>
                    <td><FreshDot state={s.freshness === "building" ? "lagging" : s.freshness === "stale" || s.freshness === "unavailable" ? "stale" : "fresh"} /></td>
                  </tr>
                );
              })}
              {rows.length === 0 ? <tr><td colSpan={11} className="empty">No catalog entries from this source.</td></tr> : null}
            </tbody>
          </table>
        </AsyncStateGuard>
      </Panel>
    </div>
  );
}
