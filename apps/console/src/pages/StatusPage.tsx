import React, { useEffect, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  loadStatusOverview,
  type StatusCollectorRow,
  type StatusCollectorState,
  type StatusOverview,
  type StatusPipelineState
} from "../api/statusOverview";
import { Badge, CollectorGlyph, Panel } from "../components/atoms";
import { fmt } from "../console/types";
import "./statusPage.css";

// StatusPage is the operator overview answering "is Eshu busy / done / stuck,
// and where" (issue #3400). It is the first OVERVIEW nav item. The hero shows
// overall indexing %, the core table shows per-collector live state
// (Stalled / Catching Up / Up To Date) with a catch-up progress bar, work-item
// count, source volume, and last-run age, and the pipeline section shows the
// ingest -> reduce -> project -> query generation lifecycle. All data comes from
// bounded status reads and the view live-refreshes on a short poll so progress
// visibly moves during indexing.

const STATE_LABEL: Record<StatusCollectorState, string> = {
  stalled: "Stalled",
  catching_up: "Catching Up",
  up_to_date: "Up To Date"
};

const STATE_TONE: Record<StatusCollectorState, "crit" | "warn" | "teal"> = {
  stalled: "crit",
  catching_up: "warn",
  up_to_date: "teal"
};

const STATE_COLOR: Record<StatusCollectorState, string> = {
  stalled: "var(--crit)",
  catching_up: "var(--med)",
  up_to_date: "var(--teal)"
};

const PIPELINE_TONE: Record<StatusPipelineState, "crit" | "warn" | "teal" | "neutral"> = {
  fresh: "teal",
  building: "warn",
  stale: "crit",
  unknown: "neutral"
};

const PIPELINE_LABEL: Record<StatusPipelineState, string> = {
  fresh: "Up to date",
  building: "Catching up",
  stale: "Stalled",
  unknown: "Unknown"
};

// defaultPollMs keeps progress visibly moving during indexing. The four backing
// reads are now issued in parallel (fix for issue #3441), so each poll cycle
// takes ~2s at peak. 12s gives the cycle room to complete and the operator time
// to read the result before the next refresh fires.
const defaultPollMs = 12000;

export function StatusPage({
  client,
  pollMs = defaultPollMs
}: {
  readonly client?: EshuApiClient;
  readonly pollMs?: number;
}): React.JSX.Element {
  const [overview, setOverview] = useState<StatusOverview | null>(null);

  useEffect(() => {
    if (!client) {
      setOverview(null);
      return;
    }
    let cancelled = false;
    const refresh = (): void => {
      void loadStatusOverview(client).then((next) => {
        if (!cancelled) setOverview(next);
      });
    };
    refresh();
    const timer = setInterval(refresh, pollMs > 0 ? pollMs : defaultPollMs);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [client, pollMs]);

  if (overview === null) {
    return (
      <div className="page status-page">
        <div className="conn-state compact">
          <div className="conn-spinner" aria-hidden />
          <p>Loading status…</p>
        </div>
      </div>
    );
  }

  const unavailable = overview.provenance === "unavailable";
  return (
    <div className="page status-page">
      <div className="page-intro">
        <h2>Status</h2>
        <p>
          What every collector and service is doing right now, from the bounded status reads{" "}
          <span className="mono">collector-readiness</span>, <span className="mono">index-status</span>, and{" "}
          <span className="mono">freshness-causality</span>. Live-refreshing.
        </p>
      </div>

      {unavailable ? (
        <Panel className="mt">
          <p className="empty">Status is unavailable from this source.</p>
        </Panel>
      ) : (
        <>
          <StatusHero overview={overview} />
          <CollectorTable rows={overview.collectors} />
          <PipelineSection overview={overview} />
        </>
      )}
    </div>
  );
}

function StatusHero({ overview }: { readonly overview: StatusOverview }): React.JSX.Element {
  const pct = overview.indexingPercent;
  const tone: StatusCollectorState = pct >= 99 ? "up_to_date" : pct >= 60 ? "catching_up" : "stalled";
  return (
    <section className="status-hero" aria-label="Overall indexing">
      <div className="status-hero-main">
        <div
          className="status-hero-ring"
          role="progressbar"
          aria-valuenow={pct}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label="Overall indexing percent"
          style={{ "--pct": `${pct}`, "--ring": STATE_COLOR[tone] } as React.CSSProperties}
        >
          <strong>{pct}%</strong>
          <span>indexed</span>
        </div>
        <div className="status-hero-meta">
          <p className="status-hero-headline">{heroHeadline(pct)} · {pct}% indexed</p>
          <div className="status-hero-stats">
            <span><strong>{fmt(overview.repositories)}</strong> repositories</span>
            <span><strong>{fmt(overview.queue.outstanding + overview.queue.inFlight)}</strong> work items in flight</span>
            <span><strong>{fmt(overview.queue.deadLetter)}</strong> dead-lettered</span>
            <span>status <span className="mono">{overview.indexStatusLabel}</span></span>
          </div>
        </div>
      </div>
    </section>
  );
}

function heroHeadline(pct: number): string {
  if (pct >= 99) return "Up to date";
  if (pct >= 60) return "Catching up";
  return "Indexing";
}

function CollectorTable({ rows }: { readonly rows: readonly StatusCollectorRow[] }): React.JSX.Element {
  return (
    <Panel className="flush mt" title="Collectors" sub={`${rows.length} configured · live state`}>
      {rows.length === 0 ? (
        <p className="empty">No collectors reported by this source.</p>
      ) : (
        <div className="table-scroll">
          <table className="tbl wide status-collector-table">
            <thead>
              <tr>
                <th>Collector</th>
                <th>State</th>
                <th className="num">Work items</th>
                <th className="num">Volume</th>
                <th>Last run</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={`${row.kind}:${row.instanceId}`}>
                  <td>
                    <span className="status-collector">
                      <CollectorGlyph kind={row.kind} />
                      <span>
                        <strong>{row.displayName}</strong>
                        <small className="status-schedule">{row.schedule}</small>
                      </span>
                    </span>
                  </td>
                  <td>
                    <div className="status-state-cell">
                      <Badge tone={STATE_TONE[row.state]} dot>{STATE_LABEL[row.state]}</Badge>
                      <ProgressBar value={row.progress} color={STATE_COLOR[row.state]} label={`${row.displayName} catch-up`} />
                    </div>
                  </td>
                  <td className="num mono">{row.workItems > 0 ? fmt(row.workItems) : "—"}</td>
                  <td className="num mono">{row.volume === null ? "—" : fmt(row.volume)}</td>
                  <td className="mono">{row.lastRunLabel}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

function ProgressBar({
  value,
  color,
  label
}: {
  readonly value: number;
  readonly color: string;
  readonly label: string;
}): React.JSX.Element {
  const pct = Math.round(Math.max(0, Math.min(1, value)) * 100);
  return (
    <span
      className="status-progress"
      role="progressbar"
      aria-valuenow={pct}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={label}
    >
      <span className="status-progress-fill" style={{ width: `${pct}%`, background: color }} />
    </span>
  );
}

function PipelineSection({ overview }: { readonly overview: StatusOverview }): React.JSX.Element {
  const p = overview.pipeline;
  const stages: readonly { readonly label: string; readonly value: string }[] = [
    { label: "ingest", value: `${fmt(overview.queue.outstanding)} queued` },
    { label: "reduce", value: `${fmt(p.pendingGenerations)} pending gen` },
    { label: "project", value: `${fmt(p.pendingProjection)} pending` },
    { label: "query", value: `${fmt(p.activeGenerations)} active gen` }
  ];
  return (
    <Panel
      className="flush mt"
      title="Pipeline & services"
      sub="ingest → reduce → project → query"
      action={<Badge tone={PIPELINE_TONE[p.state]} dot>{PIPELINE_LABEL[p.state]}</Badge>}
    >
      <div className="status-pipeline">
        {stages.map((stage, idx) => (
          <React.Fragment key={stage.label}>
            <div className="status-stage">
              <span className="status-stage-label">{stage.label}</span>
              <span className="status-stage-value mono">{stage.value}</span>
            </div>
            {idx < stages.length - 1 ? <span className="status-stage-arrow" aria-hidden>→</span> : null}
          </React.Fragment>
        ))}
      </div>
      {p.deadLetters > 0 ? (
        <p className="status-pipeline-warn">
          <Badge tone="crit" dot>{fmt(p.deadLetters)} dead-lettered</Badge> projection work needs attention.
        </p>
      ) : null}
    </Panel>
  );
}
