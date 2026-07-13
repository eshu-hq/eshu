// pages/operations/OperationsLiveBoard.tsx
// Live sections of the Operations page (issue #5137): health banner, stage
// tiles, collector heartbeat table, and the "now processing" live_activity
// table. Owns its own live-poll (useEffect + setInterval, mirroring
// StatusPage.tsx) against the bounded GET /api/v0/status/operations read.
//
// Extracted into its own module — and lazy-loaded from OperationsPage.tsx —
// so this section's code (plus its two CSS files) ships in a separate chunk
// instead of growing the console's eagerly-loaded main bundle past its
// budget (scripts/console-bundle-budget.mjs).

import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

import type { EshuApiClient } from "../../api/client";
import {
  humanizeAge,
  loadOperationsBoard,
  repoLabel,
  repositorySourceHref,
  type OperationsBoard,
  type OperationsCollectorRow,
  type OperationsActivityRow,
  type OperationsDomainBacklog,
  type OperationsHealthState,
  type OperationsStageSummary,
} from "../../api/operationsBoard";
import { Badge, CollectorGlyph, FreshDot, Panel, StatTile } from "../../components/atoms";
import { fmt } from "../../console/types";
import "../liveInventory.css";
import "./operationsLiveBoard.css";

// defaultPollMs matches StatusPage.tsx's live-refresh cadence (issue #3441).
const defaultPollMs = 12000;

const HEALTH_TONE: Record<OperationsHealthState, "crit" | "warn" | "teal" | "neutral"> = {
  healthy: "teal",
  progressing: "neutral",
  degraded: "warn",
  stalled: "crit",
  unknown: "neutral",
};

const HEALTH_LABEL: Record<OperationsHealthState, string> = {
  healthy: "Healthy",
  progressing: "Progressing",
  degraded: "Degraded",
  stalled: "Stalled",
  unknown: "Unknown",
};

const STATUS_TONE: Record<string, "teal" | "neutral" | "warn" | "crit"> = {
  running: "teal",
  claimed: "neutral",
  retrying: "warn",
  failed: "crit",
  dead_letter: "crit",
};

export function OperationsLiveBoard({
  client,
  pollMs = defaultPollMs,
}: {
  readonly client?: EshuApiClient;
  readonly pollMs?: number;
}): React.JSX.Element {
  const [board, setBoard] = useState<OperationsBoard | null>(null);

  useEffect(() => {
    if (!client) {
      setBoard(null);
      return;
    }
    let cancelled = false;
    const refresh = (): void => {
      void loadOperationsBoard(client).then((next) => {
        if (!cancelled) setBoard(next);
      });
    };
    refresh();
    const timer = setInterval(refresh, pollMs > 0 ? pollMs : defaultPollMs);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [client, pollMs]);

  if (board === null) {
    return (
      <Panel className="mt" title="Live operations board" sub="GET /api/v0/status/operations">
        <p className="empty">Loading live operations board…</p>
      </Panel>
    );
  }

  const showHealthBanner =
    board.provenance === "unavailable" ||
    board.health.state === "degraded" ||
    board.health.state === "stalled";

  return (
    <>
      {board.provenance === "unavailable" ? (
        <Panel className="mt" title="Live operations board" sub="GET /api/v0/status/operations">
          <p className="empty">Live operations board is unavailable from this source.</p>
        </Panel>
      ) : (
        <>
          {showHealthBanner ? (
            <HealthBanner state={board.health.state} reasons={board.health.reasons} />
          ) : null}
          <div className="grid ops-stage-tiles mt">
            <StatTile
              label="Actively collecting"
              value={collectingCount(board.collectors)}
              color="var(--teal)"
              sub={`${board.collectors.length} collectors`}
            />
            <StatTile
              label="Reducing"
              value={stageValue(board.stageSummaries, "reduc")}
              color="var(--violet)"
              sub="running + claimed"
            />
            <StatTile
              label="Projecting"
              value={stageValue(board.stageSummaries, "project")}
              color="var(--ember)"
              sub="running + claimed"
            />
            <StatTile
              label="Queue outstanding"
              value={fmt(board.queue.outstanding)}
              color="var(--blue)"
              sub={`${fmt(board.queue.inFlight)} in-flight`}
            />
            <StatTile
              label="Dead letters"
              value={fmt(board.queue.deadLetter)}
              color="var(--crit)"
              sub="needs replay"
            />
          </div>
          <CollectorHeartbeatTable rows={board.collectors} />
          <DomainBacklogTable rows={board.domainBacklogs} />
          <LiveActivityTable board={board} />
          <p className="ops-board-footer t-mut">
            Last updated <span className="mono">{board.asOf ?? "—"}</span>
            {board.scoped ? <> · scoped view (worker/repo identity redacted)</> : null}
          </p>
        </>
      )}
    </>
  );
}

function HealthBanner({
  state,
  reasons,
}: {
  readonly state: OperationsHealthState;
  readonly reasons: readonly string[];
}): React.JSX.Element {
  return (
    <Panel
      className="mt"
      title="Pipeline health"
      action={
        <Badge tone={HEALTH_TONE[state]} dot>
          {HEALTH_LABEL[state]}
        </Badge>
      }
    >
      {reasons.length > 0 ? (
        <ul className="ops-health-reasons">
          {reasons.map((reason) => (
            <li key={reason} className="mono">
              {reason}
            </li>
          ))}
        </ul>
      ) : (
        <p className="empty">No reasons reported.</p>
      )}
    </Panel>
  );
}

function CollectorHeartbeatTable({
  rows,
}: {
  readonly rows: readonly OperationsCollectorRow[];
}): React.JSX.Element {
  return (
    <Panel className="flush mt" title="Collector heartbeats" sub={`${rows.length} reporting`}>
      {rows.length === 0 ? (
        <p className="empty">No collector heartbeats from this source.</p>
      ) : (
        <div className="table-scroll">
          <table className="tbl wide ops-collector-table">
            <thead>
              <tr>
                <th>Instance</th>
                <th>Kind</th>
                <th>Mode</th>
                <th>Health</th>
                <th>Heartbeat</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.instanceId}>
                  <td>
                    <span className="row" style={{ gap: 10 }}>
                      <CollectorGlyph kind={row.kind} />
                      <span style={{ fontWeight: 600 }}>{row.displayName}</span>
                    </span>
                  </td>
                  <td className="mono">{row.kind}</td>
                  <td className="mono">{row.mode}</td>
                  <td>{row.health}</td>
                  <td>
                    <FreshDot state={row.freshness} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

// DomainBacklogTable (issue #5172) renders the server's already-sorted,
// already-bounded top-N reducer/projection domain backlog list. It is a
// compact aggregate view -- distinct from the health banner (which only
// narrates a single dominant domain, and only while a specific stalled/
// progressing condition holds) and from "Now processing" below (a per-item
// list of in-flight work only, with no per-domain pending/outstanding
// rollup).
//
// Includes Dead-letter and Failed columns (#5172 cold-review P2-3): the
// backend's domainBacklogQuery keeps a row whose only pressure is dead-letter
// or failed counts even when outstanding/pending/in_flight have all drained
// to zero, and the adapter already parses those fields. Dropping the columns
// would render such a row as all-zero and indistinguishable from "no
// pressure", hiding exactly the work that needs a replay. Surfacing them is
// the operator-honest choice over filtering the row out.
function DomainBacklogTable({
  rows,
}: {
  readonly rows: readonly OperationsDomainBacklog[];
}): React.JSX.Element {
  return (
    <Panel
      className="flush mt"
      title="Top domain backlogs"
      sub={`${rows.length} domain${rows.length === 1 ? "" : "s"}`}
    >
      {rows.length === 0 ? (
        <p className="empty">No outstanding domain backlog — pipeline idle</p>
      ) : (
        <div className="table-scroll">
          <table className="tbl wide ops-domain-backlog-table">
            <thead>
              <tr>
                <th>Domain</th>
                <th className="num">Outstanding</th>
                <th className="num">Pending</th>
                <th className="num">In flight</th>
                <th className="num">Dead-letter</th>
                <th className="num">Failed</th>
                <th>Oldest</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row, i) => (
                // Composite key (#5172 cold-review P2-1): `domain` alone can
                // collide -- an empty/unparseable wire domain cleans to the
                // same "—" fallback on more than one row -- so every row key
                // includes its position in the server's already-deduplicated
                // order, not only the fallback rows, for one consistent
                // keying rule instead of a conditional one.
                <tr key={`${row.domain}-${i}`}>
                  <td className="mono">{row.domain}</td>
                  <td className="num mono">{fmt(row.outstanding)}</td>
                  <td className="num mono">{fmt(row.pending)}</td>
                  <td className="num mono">{fmt(row.inFlight)}</td>
                  <td className="num mono">{fmt(row.deadLetter)}</td>
                  <td className="num mono">{fmt(row.failed)}</td>
                  <td className="mono">{humanizeAge(row.oldestAgeSeconds)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

function LiveActivityTable({ board }: { readonly board: OperationsBoard }): React.JSX.Element {
  const rows = board.liveActivity;
  return (
    <Panel
      className="flush mt"
      title="Now processing"
      sub={`${rows.length} in-flight work item${rows.length === 1 ? "" : "s"}${board.truncated ? " · truncated" : ""}`}
    >
      {rows.length === 0 ? (
        <p className="empty">No in-flight work — pipeline idle</p>
      ) : (
        <>
          <div className="table-scroll">
            <table className="tbl wide ops-activity-table">
              <thead>
                <tr>
                  <th>Repo</th>
                  <th>Stage</th>
                  <th>Status</th>
                  <th>Domain</th>
                  <th>Worker</th>
                  <th className="num">Attempts</th>
                  <th>Age</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <LiveActivityRowView key={row.workItemId} row={row} />
                ))}
              </tbody>
            </table>
          </div>
          {board.truncated ? (
            <p className="empty">
              Showing the first {board.limit} in-flight work items. More are outstanding.
            </p>
          ) : null}
        </>
      )}
    </Panel>
  );
}

function LiveActivityRowView({ row }: { readonly row: OperationsActivityRow }): React.JSX.Element {
  const repo = repoLabel(row);
  // Show the raw source_key as a tooltip when it differs from the rendered
  // label (i.e. source_display resolved to a human name) — cheap secondary
  // identity for an operator who wants the opaque key.
  const repoTitle = row.sourceKey && row.sourceKey !== repo ? row.sourceKey : undefined;
  // A "stale" row (#5138) belongs to a generation the scope has since
  // superseded -- still shown (hiding it would erase the evidence a dead
  // generation is still consuming retry budget) but dimmed and badged so an
  // operator does not mistake it for genuinely live work.
  const isStale = row.generationState === "stale";
  // #5171: link the repo label to the same repository freshness route
  // RepositoriesPage links to, for rows a repository catalog id can be
  // resolved for. Unresolvable rows (non-repository scopes, or a scoped
  // caller's redacted source_key) stay plain text -- no dead link. The link
  // itself carries no color/decoration styling of its own (matches
  // DeadCodePage/ExplorerPage's `Link` inside a `mono` cell): it inherits the
  // td's color via the global `a { color: inherit }` rule, so the stale-row
  // dimming in operationsLiveBoard.css (`.ops-activity-row-stale td.mono`)
  // keeps applying to it automatically -- the class stays on the `<td>`,
  // never moves to the `<Link>`.
  const repoHref = repositorySourceHref(row);
  return (
    <tr className={isStale ? "ops-activity-row-stale" : undefined}>
      <td className="mono" title={repoTitle}>
        {repoHref ? <Link to={repoHref}>{repo}</Link> : repo}
      </td>
      <td className="mono">{row.stage}</td>
      <td>
        <Badge tone={STATUS_TONE[row.status] ?? "neutral"} dot>
          {row.status}
        </Badge>
        {isStale ? (
          <Badge tone="warn" dot>
            stale
          </Badge>
        ) : null}
      </td>
      <td className="mono">{row.domain}</td>
      <td className="mono">{row.leaseOwner ?? "—"}</td>
      <td className="num mono">{fmt(row.attemptCount)}</td>
      <td className="mono">{humanizeAge(row.ageSeconds)} ago</td>
    </tr>
  );
}

function collectingCount(rows: readonly OperationsCollectorRow[]): string {
  const active = rows.filter((row) => row.freshness === "fresh").length;
  return `${active}/${rows.length}`;
}

function stageValue(stages: readonly OperationsStageSummary[], stageNameIncludes: string): string {
  const match = stages.find((s) => s.stage.toLowerCase().includes(stageNameIncludes));
  if (!match) return "—";
  return fmt(match.running + match.claimed);
}
