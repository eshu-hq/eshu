// components/ServiceDrawer.tsx
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { TruthChip, FreshDot, Badge } from "./atoms";
import { GraphCanvas } from "./GraphCanvas";
import type { EshuApiClient } from "../api/client";
import { vulnerabilityRowKey } from "../api/eshuConsoleVulnerabilities";
import { loadBlastGraph, loadEntityGraph } from "../api/eshuGraph";
import { loadServiceSpotlight, spotlightFromRow } from "../api/eshuService";
import type { ServiceSpotlight } from "../api/eshuService";
import type { ConsoleModel, GraphModel, Severity } from "../console/types";
import { SEVERITY_COLOR } from "../console/types";

type DrillKind = "blast" | "callers" | "findings";

export function ServiceDrawer({
  name,
  model,
  client,
  onClose,
}: {
  readonly name: string;
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly onClose: () => void;
}): React.JSX.Element {
  const [spot, setSpot] = useState<ServiceSpotlight | undefined>();
  const [loading, setLoading] = useState(true);
  const [drill, setDrill] = useState<DrillKind | null>(null);
  const [graph, setGraph] = useState<GraphModel | null>(null);
  const [graphBusy, setGraphBusy] = useState(false);
  const [graphErr, setGraphErr] = useState("");
  const selectedService = model.services.find(
    (service) => service.id === name || service.name === name,
  );
  const exposureServiceID = canonicalWorkloadID(selectedService?.id ?? name);

  // Findings for this service derive from the same impact-findings rows the
  // Vulnerabilities page uses, so the count always equals the listed rows.
  const findings = model.vulnerabilities.filter((v) => v.services.includes(name));
  const sevCount: Record<Severity, number> = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
  findings.forEach((v) => {
    const k = v.severity as Severity;
    if (k in sevCount) sevCount[k] += 1;
  });

  async function openGraph(kind: "blast" | "callers"): Promise<void> {
    if (drill === kind) {
      setDrill(null);
      return;
    }
    setDrill(kind);
    setGraph(null);
    setGraphErr("");
    if (!client) {
      setGraphErr("requires a live connection");
      return;
    }
    setGraphBusy(true);
    try {
      const g =
        kind === "blast" ? await loadBlastGraph(client, name) : await loadEntityGraph(client, name);
      // Callers = incoming edges into the center; keep the center node + its sources.
      setGraph(
        kind === "callers"
          ? {
              nodes: g.nodes,
              edges: g.edges.filter((e) => e.t === g.nodes.find((n) => n.hero)?.id),
            }
          : g,
      );
    } catch (e) {
      setGraphErr(e instanceof Error ? e.message : "failed");
    } finally {
      setGraphBusy(false);
    }
  }

  useEffect(() => {
    let active = true;
    setLoading(true);
    if (client && model.source === "live") {
      loadServiceSpotlight(client, selectedService?.name ?? name)
        .then((s) => {
          if (active) {
            setSpot(s);
            setLoading(false);
          }
        })
        .catch(() => {
          if (active) {
            setSpot(selectedService ? spotlightFromRow(selectedService) : undefined);
            setLoading(false);
          }
        });
    } else {
      setSpot(selectedService ? spotlightFromRow(selectedService) : undefined);
      setLoading(false);
    }
    return () => {
      active = false;
    };
  }, [name, client, model, selectedService]);

  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-label={`${name} spotlight`}>
        <div className="drawer-head">
          <div>
            <div className="insp-kind">Service spotlight</div>
            <strong style={{ fontFamily: "var(--mono)", fontSize: "1.02rem" }}>{name}</strong>
          </div>
          <button className="drawer-close" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        <div className="drawer-body">
          {loading ? (
            <p className="empty">Loading…</p>
          ) : !spot ? (
            <p className="empty">No spotlight available for {name}.</p>
          ) : (
            <>
              <div className="row wrap" style={{ gap: 10 }}>
                <TruthChip level={spot.truth} />
                <FreshDot state={spot.freshness} />
                {spot.repo ? <Badge tone="neutral">{spot.repo}</Badge> : null}
                <Badge tone={spot.source === "live" ? "teal" : "neutral"}>{spot.source}</Badge>
              </div>
              <p style={{ color: "var(--muted)", lineHeight: 1.6, margin: 0 }}>{spot.story}</p>

              <div
                className="meta-dl"
                style={{ gridTemplateColumns: `repeat(${Math.max(2, spot.stats.length)},1fr)` }}
              >
                {spot.stats.map((s) => (
                  <div key={s.label}>
                    <dt>{s.label}</dt>
                    <dd>{s.value}</dd>
                  </div>
                ))}
              </div>

              <div>
                <div className="section-label">Deployment path</div>
                <div className="laneflow">
                  {spot.deploymentPath.map((stage, i) => (
                    <span key={i} style={{ display: "contents" }}>
                      <div className="lane-stage">
                        <div className="lane-stage-head">
                          <span className="lane-idx">{String(i + 1).padStart(2, "0")}</span>
                          <h4>{stage}</h4>
                        </div>
                      </div>
                      {i < spot.deploymentPath.length - 1 ? (
                        <div className="lane-arrow">→</div>
                      ) : null}
                    </span>
                  ))}
                </div>
              </div>

              <div>
                <div className="section-label">Dependencies</div>
                <div className="row wrap" style={{ gap: 8 }}>
                  {spot.dependencies.length ? (
                    spot.dependencies.map((d) => (
                      <span className="dep-chip" key={d}>
                        {d}
                      </span>
                    ))
                  ) : (
                    <span className="empty" style={{ padding: 0 }}>
                      No dependencies indexed.
                    </span>
                  )}
                </div>
              </div>

              <div>
                <div className="section-label">Drill-downs</div>
                <div className="row wrap" style={{ gap: 8 }}>
                  <Link
                    className="btn-ghost"
                    onClick={onClose}
                    to={`/exposure?service=${encodeURIComponent(exposureServiceID)}`}
                  >
                    Trace exposure →
                  </Link>
                  <button
                    className={`btn-ghost${drill === "blast" ? " active" : ""}`}
                    onClick={() => openGraph("blast")}
                  >
                    Blast radius →
                  </button>
                  <button
                    className={`btn-ghost${drill === "callers" ? " active" : ""}`}
                    onClick={() => openGraph("callers")}
                  >
                    Callers / importers →
                  </button>
                  <button
                    className={`btn-ghost${drill === "findings" ? " active" : ""}`}
                    onClick={() => setDrill(drill === "findings" ? null : "findings")}
                  >
                    Findings ({findings.length}) →
                  </button>
                </div>

                {drill === "blast" || drill === "callers" ? (
                  <div style={{ marginTop: 10 }}>
                    {graphBusy ? (
                      <p className="empty">Loading {drill} graph…</p>
                    ) : graphErr ? (
                      <p className="src-err" style={{ marginTop: 0 }}>
                        ⚠ {graphErr}
                      </p>
                    ) : graph && graph.edges.length ? (
                      <GraphCanvas graph={graph} layout="radial" height={300} />
                    ) : (
                      <p className="empty">
                        No {drill === "blast" ? "transitive dependents" : "callers"} found for{" "}
                        {name}.
                      </p>
                    )}
                  </div>
                ) : null}

                {drill === "findings" ? (
                  <div style={{ marginTop: 10 }}>
                    <div className="row wrap" style={{ gap: 8, marginBottom: 8 }}>
                      {(["critical", "high", "medium", "low"] as const)
                        .filter((k) => sevCount[k] > 0)
                        .map((k) => (
                          <span key={k} className="sev-tag" style={{ color: SEVERITY_COLOR[k] }}>
                            <i style={{ background: "currentColor" }} />
                            {sevCount[k]} {k}
                          </span>
                        ))}
                    </div>
                    {findings.length ? (
                      <ul className="plain-list">
                        {findings.map((v) => (
                          <li
                            key={vulnerabilityRowKey(v)}
                            className="row"
                            style={{ justifyContent: "space-between", gap: 8 }}
                          >
                            <Link
                              to={`/vulnerabilities/${encodeURIComponent(v.id)}`}
                              className="link-btn mono"
                              style={{ fontSize: ".76rem" }}
                              onClick={onClose}
                            >
                              {v.id}
                            </Link>
                            <span className="t-mut" style={{ fontSize: ".72rem" }}>
                              {v.package} · CVSS {v.cvss || "—"}
                            </span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="empty" style={{ padding: 0 }}>
                        No findings reachable for {name}.
                      </p>
                    )}
                  </div>
                ) : null}
              </div>

              {spot.environments.length ? (
                <div>
                  <div className="section-label">Environments</div>
                  <div className="row wrap" style={{ gap: 8 }}>
                    {spot.environments.map((e) => (
                      <span className="dep-chip" key={e}>
                        {e}
                      </span>
                    ))}
                  </div>
                </div>
              ) : null}

              <p
                className="t-mut"
                style={{
                  fontSize: ".72rem",
                  borderTop: "1px solid var(--line)",
                  paddingTop: 14,
                  margin: 0,
                  lineHeight: 1.5,
                }}
              >
                {spot.source === "live" ? (
                  <>
                    Live from <span className="mono">/api/v0/services/{name}/story</span> +{" "}
                    <span className="mono">/context</span>.
                  </>
                ) : (
                  <>Demo spotlight — connect to a live API for story + context evidence.</>
                )}
              </p>
            </>
          )}
        </div>
      </aside>
    </>
  );
}

function canonicalWorkloadID(value: string): string {
  return value.startsWith("workload:") ? value : `workload:${value}`;
}
