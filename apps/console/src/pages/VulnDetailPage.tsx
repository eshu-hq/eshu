// pages/VulnDetailPage.tsx
// Full-page CVE detail wired to GET /api/v0/supply-chain/vulnerabilities/{id}
// (#1435) with a centered affected-services graph. Affected services come from
// the live impact-findings rows in the console model. No fabricated fields:
// anything the advisory omits renders as "—".
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { affectedFromModel, affectedServicesGraph, loadVulnerabilityDetail } from "../api/vulnerability";
import type { VulnDetail } from "../api/vulnerability";
import type { ConsoleModel, Severity } from "../console/types";
import { SEVERITY_COLOR } from "../console/types";
import { Panel, StatTile, Badge } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";

const sevColor = (s: string): string => SEVERITY_COLOR[(s as Severity) in SEVERITY_COLOR ? (s as Severity) : "medium"];

export function VulnDetailPage({ model, client }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const { id = "" } = useParams<{ id: string }>();
  const [detail, setDetail] = useState<VulnDetail | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setDetail(null);
    if (!client) { setLoading(false); return; }
    void loadVulnerabilityDetail(client, id).then((d) => {
      if (!cancelled) { setDetail(d); setLoading(false); }
    });
    return () => { cancelled = true; };
  }, [client, id]);

  const services = affectedFromModel(id, model.vulnerabilities);
  const graph = affectedServicesGraph(id, services);

  return (
    <div className="page">
      <div className="page-intro">
        <Link to="/vulnerabilities" className="link-btn">← Vulnerabilities</Link>
        <h2 style={{ marginTop: 8 }}>{id}</h2>
        <p>Advisory detail from <span className="mono">GET /api/v0/supply-chain/vulnerabilities/{"{id}"}</span>, with affected services joined from impact findings.</p>
      </div>

      {loading ? (
        <div className="conn-state"><div className="conn-spinner" aria-hidden /><p>Loading advisory…</p></div>
      ) : !detail || detail.provenance === "unavailable" ? (
        <div className="conn-state"><h2>Advisory unavailable</h2><p>The vulnerability detail API did not return this advisory. It requires the vulnerability-intelligence collector and a live connection.</p></div>
      ) : (
        <>
          <div className="grid g-4">
            <StatTile label="Severity" value={detail.severity} color={sevColor(detail.severity)} sub={detail.kev ? "KEV-listed" : "not on KEV"} />
            <StatTile label="CVSS" value={detail.cvss ?? "—"} color="var(--crit)" sub={detail.cvssVector ?? "no vector"} />
            <StatTile label="EPSS" value={detail.epss !== null ? `${(detail.epss * 100).toFixed(0)}%` : "—"} color="var(--ember)" sub="exploit probability" />
            <StatTile label="Fix" value={detail.fixedVersion ?? "—"} color={detail.fixedVersion ? "var(--teal)" : "var(--crit)"} sub={detail.fixedVersion ? "patch available" : "no fixed version"} />
          </div>

          <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2fr)", gap: "var(--gap)" }}>
            <div className="grid" style={{ gap: "var(--gap)", alignContent: "start" }}>
              <Panel title="Identity">
                <div className="kv">
                  <div><span className="t-mut">Canonical</span><span className="mono">{detail.canonicalId}</span></div>
                  <div><span className="t-mut">CVE</span><span className="mono">{detail.cveIds.join(", ") || "—"}</span></div>
                  <div><span className="t-mut">GHSA</span><span className="mono">{detail.ghsaIds.join(", ") || "—"}</span></div>
                  <div><span className="t-mut">CWE</span><span className="mono">{detail.cwes.join(", ") || "—"}</span></div>
                </div>
              </Panel>
              <Panel title={`Affected packages (${detail.packages.length})`}>
                {detail.packages.length === 0 ? <p className="empty">No affected packages reported.</p> : (
                  <ul className="plain-list">
                    {detail.packages.map((p, i) => (
                      <li key={`${p.name}-${i}`} className="row" style={{ justifyContent: "space-between" }}>
                        <span className="mono" style={{ fontSize: ".78rem" }}>{p.name}</span>
                        {p.fixedVersion ? <Badge tone="teal">{p.fixedVersion}</Badge> : <Badge tone="crit">none</Badge>}
                      </li>
                    ))}
                  </ul>
                )}
              </Panel>
              {detail.references.length > 0 ? (
                <Panel title="References">
                  <ul className="plain-list">
                    {detail.references.slice(0, 8).map((r) => (
                      <li key={r}><a className="link-btn mono" style={{ fontSize: ".74rem" }} href={r} target="_blank" rel="noreferrer">{r}</a></li>
                    ))}
                  </ul>
                </Panel>
              ) : null}
            </div>

            <Panel className="flush" title="Affected services" sub={`${services.length} reachable · AFFECTED_BY`}>
              {services.length === 0 ? (
                <p className="empty" style={{ padding: 20 }}>No affected services joined from impact findings for this advisory.</p>
              ) : (
                <GraphCanvas graph={graph} layout="radial" height={420} />
              )}
            </Panel>
          </div>
        </>
      )}
    </div>
  );
}
