// pages/VulnDetailPage.tsx
// Full-page CVE detail wired to GET /api/v0/supply-chain/vulnerabilities/{id}
// (#1435) with a centered affected-services graph. Affected services come from
// the live impact-findings rows in the console model. No fabricated fields:
// anything the advisory omits renders as "—".
import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import {
  loadSupplyChainImpactPacket,
  type InvestigationPacketResult,
  type SupplyChainImpactPacketQuery
} from "../api/investigationPacket";
import { affectedFromModel, affectedServicesGraph, detailFromModelRow, loadVulnerabilityDetail } from "../api/vulnerability";
import type { VulnDetail } from "../api/vulnerability";
import { Panel, StatTile, Badge } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";
import { InvestigationEvidencePacketReader } from "../components/InvestigationEvidencePacketReader";
import { SEVERITY_COLOR } from "../console/types";
import type { ConsoleModel, Severity } from "../console/types";

const sevColor = (s: string): string => SEVERITY_COLOR[(s as Severity) in SEVERITY_COLOR ? (s as Severity) : "medium"];

export function VulnDetailPage({ model, client }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const { id = "" } = useParams<{ id: string }>();
  const [detail, setDetail] = useState<VulnDetail | null>(null);
  const [packet, setPacket] = useState<InvestigationPacketResult | null>(null);
  const [packetError, setPacketError] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setDetail(null);
    setPacket(null);
    setPacketError("");
    if (!client) { setLoading(false); return; }
    void loadVulnerabilityDetail(client, id).then((d) => {
      if (!cancelled) { setDetail(d); setLoading(false); }
    });
    return () => { cancelled = true; };
  }, [client, id]);

  // Fall back to the list row when extended advisory evidence is unavailable.
  // The list row is source-backed by impact findings, not a fabricated advisory.
  const fallback = useMemo(
    () => detail?.provenance === "unavailable" || detail?.provenance === "empty"
      ? detailFromModelRow(id, model.vulnerabilities)
      : null,
    [detail, id, model.vulnerabilities]
  );
  const effective = useMemo(
    () => detail?.provenance === "live" ? detail : (fallback ?? detail),
    [detail, fallback]
  );
  const packetQuery = useMemo(() => supplyChainPacketQueryFromDetail(id, effective), [effective, id]);

  useEffect(() => {
    let cancelled = false;
    setPacket(null);
    setPacketError("");
    if (!client || !packetQuery) return () => { cancelled = true; };
    void loadSupplyChainImpactPacket(client, packetQuery).then((result) => {
      if (!cancelled) setPacket(result);
    }).catch((err: unknown) => {
      if (!cancelled) setPacketError(err instanceof Error ? err.message : "failed to load impact packet");
    });
    return () => { cancelled = true; };
  }, [client, packetQuery]);

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
      ) : !effective || effective.provenance === "unavailable" || effective.provenance === "empty" ? (
        <div className="conn-state"><h2>Advisory unavailable</h2><p>The vulnerability detail API did not return this advisory. It requires the vulnerability-intelligence collector and a live connection.</p></div>
      ) : (
        <>
          {effective.provenance === "derived" ? (
            <p className="t-mut" style={{ marginBottom: 12, fontSize: ".82rem" }}>
              Extended advisory evidence (EPSS, CVSS vector, CWEs, references) is unavailable from{" "}
              <span className="mono">GET /api/v0/supply-chain/vulnerabilities/{"{id}"}</span>. Showing
              reachable impact facts from <span className="mono">GET /api/v0/supply-chain/impact/findings</span>{" "}
              until issue #2217 proves the vulnerability-intelligence collector runtime path.
            </p>
          ) : null}
          <div className="grid g-4">
            <StatTile label="Severity" value={effective.severity} color={sevColor(effective.severity)} sub={effective.kev ? "KEV-listed" : "not on KEV"} />
            <StatTile label="CVSS" value={effective.cvss ?? "—"} color="var(--crit)" sub={effective.cvssVector ?? "no vector"} />
            <StatTile label="EPSS" value={effective.epss !== null ? `${(effective.epss * 100).toFixed(0)}%` : "—"} color="var(--ember)" sub="exploit probability" />
            <StatTile label="Fix" value={effective.fixedVersion ?? "—"} color={effective.fixedVersion ? "var(--teal)" : "var(--crit)"} sub={effective.fixedVersion ? "patch available" : "no fixed version"} />
          </div>

          <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2fr)", gap: "var(--gap)" }}>
            <div className="grid" style={{ gap: "var(--gap)", alignContent: "start" }}>
              <Panel title="Identity">
                <div className="kv">
                  <div><span className="t-mut">Canonical</span><span className="mono">{effective.canonicalId}</span></div>
                  <div><span className="t-mut">CVE</span><span className="mono">{effective.cveIds.join(", ") || "—"}</span></div>
                  <div><span className="t-mut">GHSA</span><span className="mono">{effective.ghsaIds.join(", ") || "—"}</span></div>
                  <div><span className="t-mut">CWE</span><span className="mono">{effective.cwes.join(", ") || "—"}</span></div>
                </div>
              </Panel>
              <Panel title={`Affected packages (${effective.packages.length})`}>
                {effective.packages.length === 0 ? <p className="empty">No affected packages reported.</p> : (
                  <ul className="plain-list">
                    {effective.packages.map((p, i) => (
                      <li key={`${p.name}-${i}`} className="row" style={{ justifyContent: "space-between" }}>
                        <span className="mono" style={{ fontSize: ".78rem" }}>{p.name}</span>
                        {p.fixedVersion ? <Badge tone="teal">{p.fixedVersion}</Badge> : <Badge tone="crit">none</Badge>}
                      </li>
                    ))}
                  </ul>
                )}
              </Panel>
              {effective.references.length > 0 ? (
                <Panel title="References">
                  <ul className="plain-list">
                    {effective.references.slice(0, 8).map((r) => (
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
          <div className="mt">
            {packet ? <InvestigationEvidencePacketReader packet={packet.packet} /> : null}
            {!packet && packetError ? <p className="src-err">{packetError}</p> : null}
          </div>
        </>
      )}
    </div>
  );
}

function supplyChainPacketQueryFromDetail(
  id: string,
  detail: VulnDetail | null,
): SupplyChainImpactPacketQuery | null {
  if (!detail || detail.provenance === "unavailable" || detail.provenance === "empty") return null;
  const packageId = detail.packages.find((pkg) => pkg.name.trim().length > 0)?.name.trim();
  if (!packageId) return null;
  const base = /^CVE-/i.test(id) ? { cveId: id } : { advisoryId: id };
  return { ...base, maxSourceFacts: 50, packageId };
}
