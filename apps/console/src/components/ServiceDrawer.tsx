// components/ServiceDrawer.tsx
import { useEffect, useState } from "react";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel } from "../console/types";
import { loadServiceSpotlight, spotlightFromRow } from "../api/eshuService";
import type { ServiceSpotlight } from "../api/eshuService";
import { TruthChip, FreshDot, Badge } from "./atoms";

export function ServiceDrawer({ name, model, client, onClose }: {
  readonly name: string; readonly model: ConsoleModel;
  readonly client?: EshuApiClient; readonly onClose: () => void;
}): React.JSX.Element {
  const [spot, setSpot] = useState<ServiceSpotlight | undefined>();
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    setLoading(true);
    const row = model.services.find((s) => s.id === name || s.name === name);
    if (client && model.source === "live") {
      loadServiceSpotlight(client, row?.name ?? name)
        .then((s) => { if (active) { setSpot(s); setLoading(false); } })
        .catch(() => { if (active) { setSpot(row ? spotlightFromRow(row) : undefined); setLoading(false); } });
    } else {
      setSpot(row ? spotlightFromRow(row) : undefined);
      setLoading(false);
    }
    return () => { active = false; };
  }, [name, client, model]);

  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-label={`${name} spotlight`}>
        <div className="drawer-head">
          <div><div className="insp-kind">Service spotlight</div><strong style={{ fontFamily: "var(--mono)", fontSize: "1.02rem" }}>{name}</strong></div>
          <button className="drawer-close" onClick={onClose} aria-label="Close">✕</button>
        </div>
        <div className="drawer-body">
          {loading ? <p className="empty">Loading…</p> : !spot ? <p className="empty">No spotlight available for {name}.</p> : (
            <>
              <div className="row wrap" style={{ gap: 10 }}>
                <TruthChip level={spot.truth} /><FreshDot state={spot.freshness} />
                {spot.repo ? <Badge tone="neutral">{spot.repo}</Badge> : null}
                <Badge tone={spot.source === "live" ? "teal" : "neutral"}>{spot.source}</Badge>
              </div>
              <p style={{ color: "var(--muted)", lineHeight: 1.6, margin: 0 }}>{spot.story}</p>

              <div className="meta-dl" style={{ gridTemplateColumns: `repeat(${Math.max(2, spot.stats.length)},1fr)` }}>
                {spot.stats.map((s) => <div key={s.label}><dt>{s.label}</dt><dd>{s.value}</dd></div>)}
              </div>

              <div>
                <div className="section-label">Deployment path</div>
                <div className="laneflow">
                  {spot.deploymentPath.map((stage, i) => (
                    <span key={i} style={{ display: "contents" }}>
                      <div className="lane-stage"><div className="lane-stage-head"><span className="lane-idx">{String(i + 1).padStart(2, "0")}</span><h4>{stage}</h4></div></div>
                      {i < spot.deploymentPath.length - 1 ? <div className="lane-arrow">→</div> : null}
                    </span>
                  ))}
                </div>
              </div>

              <div>
                <div className="section-label">Dependencies</div>
                <div className="row wrap" style={{ gap: 8 }}>
                  {spot.dependencies.length ? spot.dependencies.map((d) => <span className="dep-chip" key={d}>{d}</span>) : <span className="empty" style={{ padding: 0 }}>No dependencies indexed.</span>}
                </div>
              </div>

              {spot.environments.length ? (
                <div>
                  <div className="section-label">Environments</div>
                  <div className="row wrap" style={{ gap: 8 }}>{spot.environments.map((e) => <span className="dep-chip" key={e}>{e}</span>)}</div>
                </div>
              ) : null}

              <p className="t-mut" style={{ fontSize: ".72rem", borderTop: "1px solid var(--line)", paddingTop: 14, margin: 0, lineHeight: 1.5 }}>
                {spot.source === "live"
                  ? <>Live from <span className="mono">/api/v0/services/{name}/story</span> + <span className="mono">/context</span>.</>
                  : <>Demo spotlight — connect to a live API for story + context evidence.</>}
              </p>
            </>
          )}
        </div>
      </aside>
    </>
  );
}
