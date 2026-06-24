// pages/VulnerabilitiesCatalog.tsx
// Browsable known-intelligence catalog over GET /api/v0/supply-chain/advisories.
// Rows are source intelligence (not service reachability) and link to the
// existing CVE detail page. Seeds from the snapshot's first page, then paginates,
// filters, and refreshes live through the catalog client.
import { useState } from "react";
import { Link } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import { fetchAdvisoryCatalogPage } from "../api/eshuConsoleAdvisories";
import type { AdvisoryCatalogCursor } from "../api/eshuConsoleAdvisories";
import { Panel, TruthChip, FreshDot } from "../components/atoms";
import { SEVERITY_COLOR, uiTruth, uiFresh } from "../console/types";
import type { ConsoleModel, Severity, AdvisoryRow } from "../console/types";

const PAGE_SIZE = 50;

interface Filters {
  readonly severity: string;
  readonly ecosystem: string;
  readonly q: string;
  readonly kev: boolean;
}

const EMPTY_FILTERS: Filters = { severity: "", ecosystem: "", q: "", kev: false };

export function AdvisoryCatalog({ model, client }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  // Seed from the snapshot's first page so the catalog renders instantly; the
  // snapshot truncates at PAGE_SIZE, so assume more pages exist when it is full.
  const seeded = model.advisories.slice();
  const [rows, setRows] = useState<readonly AdvisoryRow[]>(seeded);
  const [cursor, setCursor] = useState<AdvisoryCatalogCursor | null>(null);
  const [hasMore, setHasMore] = useState<boolean>(seeded.length >= PAGE_SIZE);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS);
  const [applied, setApplied] = useState<Filters>(EMPTY_FILTERS);

  const truth = model.truth.advisories;
  const unavailable = model.provenance.advisories === "unavailable";
  const filtersActive = applied !== EMPTY_FILTERS;

  async function load(next: Filters, append: boolean, from: AdvisoryCatalogCursor | null): Promise<void> {
    if (!client) {
      setError("Live API connection required to browse the catalog.");
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const page = await fetchAdvisoryCatalogPage(client, {
        limit: PAGE_SIZE,
        severity: next.severity || undefined,
        ecosystem: next.ecosystem || undefined,
        q: next.q || undefined,
        kev: next.kev || undefined,
        cursor: from
      });
      setRows((prev) => (append ? [...prev, ...page.rows] : page.rows));
      setCursor(page.nextCursor);
      setHasMore(page.nextCursor !== null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Catalog request failed.");
    } finally {
      setLoading(false);
    }
  }

  function applyFilters(): void {
    setApplied(draft);
    void load(draft, false, null);
  }

  function resetFilters(): void {
    setDraft(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
    setRows(seeded);
    setCursor(null);
    setHasMore(seeded.length >= PAGE_SIZE);
    setError(null);
  }

  return (
    <div>
      <div className="row" style={{ justifyContent: "space-between", alignItems: "center", margin: "0 0 var(--gap)", flexWrap: "wrap", gap: 8 }}>
        <p className="t-mut" style={{ fontSize: ".82rem", margin: 0 }}>
          Known intelligence — <span className="mono">GET /api/v0/supply-chain/advisories</span>. These advisories are not
          a claim of service impact.
        </p>
        <div className="row" style={{ gap: 8, alignItems: "center" }}>
          {truth ? <TruthChip level={uiTruth(truth.level)} /> : null}
          {truth ? <FreshDot state={uiFresh(truth.freshness.state)} /> : null}
        </div>
      </div>

      <Panel className="flush" title="CVE intelligence catalog" sub="Sorted by CVSS" action={
        <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
          <input className="popover-input" placeholder="Search id / package" value={draft.q}
            onChange={(e) => setDraft({ ...draft, q: e.target.value })} style={{ width: 160 }} aria-label="Search advisories" />
          <select className="popover-input" value={draft.severity} onChange={(e) => setDraft({ ...draft, severity: e.target.value })} aria-label="Severity filter">
            <option value="">Any severity</option>
            <option value="critical">Critical</option>
            <option value="high">High</option>
            <option value="medium">Medium</option>
            <option value="low">Low</option>
          </select>
          <input className="popover-input" placeholder="Ecosystem" value={draft.ecosystem}
            onChange={(e) => setDraft({ ...draft, ecosystem: e.target.value })} style={{ width: 110 }} aria-label="Ecosystem filter" />
          <label className="row" style={{ gap: 4, fontSize: ".78rem", alignItems: "center" }}>
            <input type="checkbox" checked={draft.kev} onChange={(e) => setDraft({ ...draft, kev: e.target.checked })} /> KEV only
          </label>
          <button className="btn-ghost active" onClick={applyFilters} disabled={loading || !client}>Apply</button>
          {filtersActive ? <button className="link-btn" onClick={resetFilters}>Clear</button> : null}
        </div>
      }>
        <table className="tbl">
          <thead><tr><th>ID</th><th>Severity</th><th>CVSS</th><th>KEV</th><th>Ecosystem</th><th>Package</th></tr></thead>
          <tbody>
            {rows.map((a) => (
              <tr key={a.id}>
                <td className="row" style={{ gap: 7 }}>
                  <Link to={`/vulnerabilities/${encodeURIComponent(a.id)}`} className="t-name link-btn" style={{ fontSize: ".8rem" }}>{a.id}</Link>
                  {a.ghsaId && a.ghsaId !== a.id ? <span className="t-mut mono" style={{ fontSize: ".68rem" }}>{a.ghsaId}</span> : null}
                </td>
                <td><span className="sev-tag" style={{ color: SEVERITY_COLOR[(a.severity as Severity) in SEVERITY_COLOR ? (a.severity as Severity) : "medium"] }}><i style={{ background: "currentColor" }} />{a.severity}</span></td>
                <td className="mono" style={{ fontSize: ".82rem" }}>{a.cvss || "—"}</td>
                <td>{a.kev ? <span className="kev-flag">KEV</span> : <span className="t-mut">—</span>}</td>
                <td className="t-mut" style={{ fontSize: ".76rem" }}>{a.ecosystems.length > 0 ? a.ecosystems.slice(0, 2).join(", ") : "—"}</td>
                <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{a.packageIds.length > 0 ? a.packageIds[0] : "—"}{a.packageIds.length > 1 ? ` +${a.packageIds.length - 1}` : ""}</td>
              </tr>
            ))}
            {rows.length === 0 && !loading ? (
              <tr><td colSpan={6} className="empty">
                {error
                  ? error
                  : unavailable
                    ? "The vulnerability-intelligence catalog is unavailable — the source fact read model did not respond."
                    : filtersActive
                      ? "No catalog advisories match these filters."
                      : "No catalog advisories yet — requires the vulnerability-intelligence collector."}
              </td></tr>
            ) : null}
          </tbody>
        </table>
        <div className="row" style={{ justifyContent: "space-between", padding: "10px 12px", alignItems: "center" }}>
          <span className="t-mut" style={{ fontSize: ".74rem" }}>{rows.length} loaded{hasMore ? " · more available" : ""}</span>
          <div className="row" style={{ gap: 8, alignItems: "center" }}>
            {error && rows.length > 0 ? <span className="t-mut" style={{ fontSize: ".74rem", color: "var(--crit)" }}>{error}</span> : null}
            <button className="btn-ghost" onClick={() => void load(applied, true, cursor)} disabled={!hasMore || loading || !client}>
              {loading ? "Loading…" : hasMore ? "Load more" : "End of catalog"}
            </button>
          </div>
        </div>
      </Panel>
    </div>
  );
}
