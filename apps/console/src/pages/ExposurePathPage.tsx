// pages/ExposurePathPage.tsx
// Code-to-cloud exposure trace from POST /api/v0/impact/trace-exposure-path
// (epic #2704, backend #2726). An operator supplies a source handler (name +
// repo, or entity id) and an optional max depth; the view renders the bounded
// reachability path "internet -> endpoint -> handler -> ... -> cloud sink".
//
// This surface is deliberately conservative. Every finding is derived
// (symbol-level reachability, not value-flow). When the finding is unresolved —
// e.g. a code-to-cloud bridge edge is not materialized — the view shows the
// honest coverage reason and explicitly states no path is proven. It never
// renders an empty success state that implies a path exists.
import { useCallback, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import {
  loadExposureFinding,
  type ExposureFinding,
  type ExposurePath,
  type ExposureRank,
  type Severity,
  type TraversalState
} from "../api/exposurePath";
import { Badge, FreshDot, Panel, TruthChip } from "../components/atoms";
import { uiFresh, uiTruth } from "../console/types";
import "./exposurePathPage.css";

type BadgeTone = "neutral" | "teal" | "ember" | "crit" | "warn" | "violet";

const SEVERITY_TONE: Record<Severity, BadgeTone> = {
  critical: "crit",
  high: "ember",
  medium: "warn",
  low: "neutral"
};

const STATE_TONE: Record<TraversalState, BadgeTone> = {
  exact: "teal",
  partial: "warn",
  ambiguous: "violet",
  unresolved: "neutral"
};

const RANK_TONE: Record<ExposureRank, BadgeTone> = {
  internet_exposed: "crit",
  network_reachable: "ember",
  internal: "neutral"
};

const RANK_LABEL: Record<ExposureRank, string> = {
  internet_exposed: "internet exposed",
  network_reachable: "network reachable",
  internal: "internal"
};

interface ExposureFormState {
  readonly source: string;
  readonly sourceEntityId: string;
  readonly repoId: string;
  readonly maxDepth: string;
}

export function ExposurePathPage({
  client
}: {
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const [form, setForm] = useState<ExposureFormState>(() => formFromSearch(searchParams));
  const [finding, setFinding] = useState<ExposureFinding | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canLoad = client !== undefined;

  const runTrace = useCallback(
    async (next: ExposureFormState) => {
      const source = next.source.trim();
      const sourceEntityId = next.sourceEntityId.trim();
      if (!client || (source.length === 0 && sourceEntityId.length === 0)) {
        return;
      }
      setBusy(true);
      setError("");
      try {
        const loaded = await loadExposureFinding(client, {
          source,
          sourceEntityId,
          repoId: next.repoId.trim(),
          maxDepth: parseDepth(next.maxDepth)
        });
        setFinding(loaded);
        if (loaded.provenance === "unavailable") {
          setError(loaded.error ?? "failed to trace exposure path");
        }
      } catch (traceError) {
        setFinding(null);
        setError(traceError instanceof Error ? traceError.message : "failed to trace exposure path");
      } finally {
        setBusy(false);
      }
    },
    [client]
  );

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const source = form.source.trim();
    const sourceEntityId = form.sourceEntityId.trim();
    if (source.length === 0 && sourceEntityId.length === 0) {
      setError("A source handler name or entity id is required.");
      return;
    }
    const params = new URLSearchParams();
    if (source.length > 0) params.set("source", source);
    if (sourceEntityId.length > 0) params.set("sourceEntityId", sourceEntityId);
    if (form.repoId.trim().length > 0) params.set("repoId", form.repoId.trim());
    if (form.maxDepth.trim().length > 0) params.set("maxDepth", form.maxDepth.trim());
    setSearchParams(params);
    void runTrace(form);
  }

  return (
    <div className="page exposure-page">
      <div className="page-intro impact-intro">
        <div>
          <h2>Exposure Path</h2>
          <p>
            Bounded code-to-cloud reachability from a source handler to a recognized cloud sink,
            via <span className="mono">POST /api/v0/impact/trace-exposure-path</span>. Findings are
            derived (symbol-level reachability, not value-flow) and never claim a path that is not
            proven.
          </p>
        </div>
        <Badge tone={canLoad ? "teal" : "warn"}>{canLoad ? "live API" : "connect live API"}</Badge>
      </div>

      <form className="impact-query exposure-query" onSubmit={submit}>
        <label className="impact-query-target">
          <span>Source handler</span>
          <input
            aria-label="Source handler name"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, source: event.target.value }))}
            placeholder="createWidgetHandler"
            value={form.source}
          />
        </label>
        <label>
          <span>Repo scope</span>
          <input
            aria-label="Repository scope"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, repoId: event.target.value }))}
            placeholder="optional repo_id"
            value={form.repoId}
          />
        </label>
        <label>
          <span>Source entity id</span>
          <input
            aria-label="Source entity id"
            className="popover-input mono"
            onChange={(event) => setForm((current) => ({ ...current, sourceEntityId: event.target.value }))}
            placeholder="optional entity id"
            value={form.sourceEntityId}
          />
        </label>
        <label>
          <span>Max depth</span>
          <input
            aria-label="Max depth"
            className="popover-input mono"
            inputMode="numeric"
            onChange={(event) => setForm((current) => ({ ...current, maxDepth: event.target.value }))}
            placeholder="5"
            value={form.maxDepth}
          />
        </label>
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Tracing…" : "Trace exposure"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {error ? <p className="src-err">{error}</p> : null}

      {busy ? (
        <div className="conn-state compact mt">
          <div aria-hidden className="conn-spinner" />
          <p>Tracing exposure path…</p>
        </div>
      ) : finding !== null && finding.provenance === "live" ? (
        <ExposureFindingView finding={finding} />
      ) : finding === null && !error ? (
        <p className="empty mt">Enter a source handler to trace its code-to-cloud exposure.</p>
      ) : null}
    </div>
  );
}

function ExposureFindingView({ finding }: { readonly finding: ExposureFinding }): React.JSX.Element {
  const resolved = finding.state !== "unresolved" && finding.paths.length > 0;
  return (
    <div className="exposure-result mt">
      <Panel
        className="exposure-summary-panel"
        sub={finding.source.name || finding.source.entityId || "source handler"}
        title="Finding"
      >
        <div className="exposure-badges">
          <span className="exposure-badge-group">
            <span className="exposure-badge-label">Exposure rank</span>
            <Badge tone={RANK_TONE[finding.exposureRank]}>{RANK_LABEL[finding.exposureRank]}</Badge>
          </span>
          <span className="exposure-badge-group">
            <span className="exposure-badge-label">Truth state</span>
            <Badge tone={STATE_TONE[finding.state]}>{finding.state}</Badge>
          </span>
          <span className="exposure-badge-group">
            <span className="exposure-badge-label">Label</span>
            <TruthChip level={uiTruth(finding.truthLabel)} />
          </span>
          {finding.sourceKind ? (
            <span className="exposure-badge-group">
              <span className="exposure-badge-label">Source kind</span>
              <Badge tone="neutral">{finding.sourceKind.replace(/_/g, " ")}</Badge>
            </span>
          ) : null}
          {finding.truth ? (
            <span className="exposure-badge-group">
              <span className="exposure-badge-label">Capability</span>
              <span className="exposure-truth">
                <span className="mono">{finding.truth.capability}</span>
                <FreshDot state={uiFresh(finding.truth.freshness.state)} />
              </span>
            </span>
          ) : null}
        </div>
      </Panel>

      {resolved ? (
        <div className="exposure-paths">
          {finding.paths.map((path, index) => (
            <ExposurePathCard
              exposureRank={finding.exposureRank}
              key={pathKey(path, index)}
              path={path}
            />
          ))}
        </div>
      ) : (
        <UnresolvedNotice finding={finding} />
      )}
    </div>
  );
}

function ExposurePathCard({
  exposureRank,
  path
}: {
  readonly exposureRank: ExposureRank;
  readonly path: ExposurePath;
}): React.JSX.Element {
  const origin = chainOrigin(exposureRank);
  return (
    <Panel
      className="exposure-path-panel"
      sub={`depth ${path.depth} · ${path.state}`}
      title={`Reaches ${path.sink.displayName}`}
    >
      <div className="exposure-path-meta">
        <span className="exposure-badge-group">
          <span className="exposure-badge-label">Severity</span>
          <Badge tone={SEVERITY_TONE[path.severity]}>{path.severity}</Badge>
        </span>
        <span className="exposure-badge-group">
          <span className="exposure-badge-label">Path state</span>
          <Badge tone={STATE_TONE[path.state]}>{path.state}</Badge>
        </span>
      </div>

      <ol className="exposure-chain" aria-label="Exposure path chain">
        {origin !== null ? (
          <li className={`exposure-chain-node ${origin.className}`}>
            <span className="exposure-node-kind">entry</span>
            <span className="exposure-node-name">{origin.label}</span>
          </li>
        ) : null}
        {path.nodes.map((node, index) => (
          <li className="exposure-chain-node" key={chainKey(node.entityId, node.name, index)}>
            <span className="exposure-node-kind">{nodeKindLabel(node.labels, index)}</span>
            <span className="exposure-node-name mono">{node.name || node.entityId || "node"}</span>
          </li>
        ))}
        <li className="exposure-chain-node exposure-chain-sink">
          <span className="exposure-node-kind">{path.sink.kind.replace(/_/g, " ") || "sink"}</span>
          <span className="exposure-node-name mono">
            {path.sink.node.name || path.sink.displayName}
          </span>
        </li>
      </ol>

      {path.reason ? <p className="exposure-reason">{path.reason}</p> : null}
    </Panel>
  );
}

function UnresolvedNotice({ finding }: { readonly finding: ExposureFinding }): React.JSX.Element {
  const reason =
    finding.coverage.unresolvedReason.trim().length > 0
      ? finding.coverage.unresolvedReason
      : "No reachable cloud sink was found within the traversal bound.";
  return (
    <Panel className="exposure-unresolved-panel" title="No proven exposure path">
      <p className="exposure-unresolved-lead">
        This source did not resolve to a recognized cloud sink. No path is implied.
      </p>
      <p className="exposure-reason">{reason}</p>
      <div className="exposure-coverage">
        <span>max depth {finding.coverage.maxDepth}</span>
        <span>{finding.coverage.pathsFound} paths found</span>
        {finding.coverage.truncated ? <span className="exposure-truncated">truncated</span> : null}
      </div>
    </Panel>
  );
}

// chainOrigin returns the synthetic leading chain node for a finding's exposure
// rank, or null when no origin should be drawn. It must never over-claim public
// reachability: only an internet_exposed source gets an "internet" entry. A
// network_reachable source proves only network reachability, so it gets a
// truthful "network boundary" entry; an internal source gets no synthetic
// origin at all (the path starts at the in-process handler).
function chainOrigin(
  rank: ExposureRank
): { readonly className: string; readonly label: string } | null {
  switch (rank) {
    case "internet_exposed":
      return { className: "exposure-chain-internet", label: "internet" };
    case "network_reachable":
      return { className: "exposure-chain-network", label: "network boundary" };
    default:
      return null;
  }
}

function formFromSearch(searchParams: URLSearchParams): ExposureFormState {
  return {
    source: searchParams.get("source") ?? "",
    sourceEntityId: searchParams.get("sourceEntityId") ?? "",
    repoId: searchParams.get("repoId") ?? "",
    maxDepth: searchParams.get("maxDepth") ?? ""
  };
}

function parseDepth(raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed.length === 0) {
    return undefined;
  }
  const value = Number.parseInt(trimmed, 10);
  return Number.isFinite(value) ? value : undefined;
}

// nodeKindLabel derives a short kind label from a node's graph labels. The first
// node is the source handler; subsequent nodes are intermediate calls.
function nodeKindLabel(labels: readonly string[], index: number): string {
  const handler = labels.find((label) => /handler|consumer|command/i.test(label));
  if (handler) {
    return "handler";
  }
  if (index === 0) {
    return "source";
  }
  return "call";
}

function pathKey(path: ExposurePath, index: number): string {
  const head = path.nodes[0]?.entityId ?? "";
  return `${head}:${path.sink.kind}:${path.depth}:${index}`;
}

function chainKey(entityId: string, name: string, index: number): string {
  return `${entityId || name || "node"}:${index}`;
}
