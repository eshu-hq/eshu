import React, { useEffect, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  loadFreshnessCausality,
  type FreshnessCausalityPage as FreshnessModel,
  type FreshnessOverallState,
} from "../api/freshnessCausality";
import { Panel, StatTile, Badge, FreshDot } from "../components/atoms";
import { uiFresh } from "../console/types";

// stateToFresh maps the overall causality state to the console freshness dot.
function stateToFresh(state: FreshnessOverallState | "unknown"): "fresh" | "lagging" | "stale" {
  if (state === "building") return uiFresh("building");
  if (state === "stale" || state === "unknown") return uiFresh("stale");
  return uiFresh("fresh");
}

const initialModel: FreshnessModel = {
  state: "unknown",
  scoped: false,
  causes: [],
  generations: { active: 0, pending: 0, completed: 0, superseded: 0, failed: 0 },
  pending: { outstanding: 0, dead_letter: 0, domains: 0 },
  transitions: [],
  truth: null,
  provenance: "unavailable",
};

export function FreshnessCausalityPage({
  client,
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [model, setModel] = useState<FreshnessModel | null>(null);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setModel(initialModel);
      return;
    }
    void loadFreshnessCausality(client).then((page) => {
      if (cancelled) return;
      setModel(page);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const unavailable = model !== null && model.provenance === "unavailable";
  const observedCauses = (model?.causes ?? []).filter((c) => c.observed);

  const sub =
    model === null
      ? "loading…"
      : unavailable
        ? "unavailable"
        : `state: ${model.state}${model.scoped ? " · tenant-scoped view" : ""}`;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Freshness Causality</h2>
        <p>
          Why answers are stale and what graph evidence is retracting, from{" "}
          <span className="mono">GET /api/v0/status/freshness-causality</span>: each closed
          freshness cause, the generation lifecycle including retired generations, and pending
          projection work.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile
          label="Active generations"
          value={model === null || unavailable ? "—" : model.generations.active}
          color="var(--teal)"
          sub="current truth"
        />
        <StatTile
          label="Pending generations"
          value={model === null || unavailable ? "—" : model.generations.pending}
          color="var(--blue)"
          sub="catch-up in flight"
        />
        <StatTile
          label="Retired (superseded)"
          value={model === null || unavailable ? "—" : model.generations.superseded}
          color="var(--violet)"
          sub="evidence retracting"
        />
        <StatTile
          label="Pending projection"
          value={model === null || unavailable ? "—" : model.pending.outstanding}
          color="var(--ember)"
          sub={`${model?.pending.dead_letter ?? 0} dead-lettered`}
        />
      </div>

      <Panel
        className="flush mt"
        title="Freshness causes"
        sub={sub}
        action={
          model && !unavailable ? (
            <div className="panel-action-stack">
              <FreshDot state={stateToFresh(model.state)} />
              {model.scoped ? <Badge tone="violet">scoped view</Badge> : null}
            </div>
          ) : null
        }
      >
        {model === null ? (
          <div className="conn-state compact">
            <div className="conn-spinner" aria-hidden />
            <p>Loading freshness causality…</p>
          </div>
        ) : unavailable ? (
          <p className="empty">Freshness causality unavailable from this source.</p>
        ) : (
          <>
            <p className="muted" style={{ margin: "0 0 0.75rem" }}>
              {observedCauses.length === 0
                ? "No freshness causes are currently observed in the runtime."
                : `${observedCauses.length} cause${observedCauses.length === 1 ? "" : "s"} currently observed.`}
            </p>
            <div className="table-scroll">
              <table className="tbl wide">
                <thead>
                  <tr>
                    <th>Cause</th>
                    <th>State</th>
                    <th>Observability</th>
                    <th>Next check</th>
                  </tr>
                </thead>
                <tbody>
                  {model.causes.map((c) => (
                    <tr key={c.cause}>
                      <td className="mono">{c.cause}</td>
                      <td>
                        {c.observed ? (
                          <Badge tone="ember" dot>
                            observed
                          </Badge>
                        ) : (
                          <Badge tone="neutral">clear</Badge>
                        )}
                      </td>
                      <td>
                        <Badge tone={c.observability === "runtime" ? "teal" : "neutral"}>
                          {c.observability}
                        </Badge>
                      </td>
                      <td>{c.nextCheckReason || c.detail}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </Panel>

      <Panel className="flush mt" title="Recent generation transitions" sub="retractions and activations">
        {model === null || unavailable ? (
          <p className="empty">No transition evidence available.</p>
        ) : model.transitions.length === 0 ? (
          <p className="empty">No recent generation transitions.</p>
        ) : (
          <div className="table-scroll">
            <table className="tbl wide">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Trigger</th>
                  <th>Freshness hint</th>
                  {model.scoped ? null : <th>Scope</th>}
                  {model.scoped ? null : <th>Generation</th>}
                </tr>
              </thead>
              <tbody>
                {model.transitions.map((t, idx) => (
                  <tr key={`${t.status}-${t.scopeId ?? ""}-${t.generationId ?? ""}-${idx}`}>
                    <td>
                      <Badge tone={t.status === "superseded" ? "violet" : "teal"}>{t.status}</Badge>
                    </td>
                    <td>{t.triggerKind || "—"}</td>
                    <td>{t.freshnessHint || "—"}</td>
                    {model.scoped ? null : <td className="mono">{t.scopeId ?? "—"}</td>}
                    {model.scoped ? null : <td className="mono">{t.generationId ?? "—"}</td>}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </div>
  );
}
