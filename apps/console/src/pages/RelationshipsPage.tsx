// pages/RelationshipsPage.tsx
// Verb-first browser over the live typed edges Eshu has observed across the
// code-to-cloud graph. Stat tiles summarize the catalog; the LAYER filter scopes
// the verb list; selecting a verb lists its concrete edges with endpoints and
// evidence. A source_tool filter narrows the edge slice to edges stamped by a
// specific ingestion tool. Complements the entity-first Graph Explorer.
import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import {
  loadRelationshipEdges,
  loadRelationshipsCatalog,
  type RelationshipEdges,
  type RelationshipVerbTile,
} from "../api/relationshipsCatalog";
import { Badge, Panel, StatTile } from "../components/atoms";
import type { ConsoleModel, GraphLayer } from "../console/types";
import { LAYER_COLOR, fmt } from "../console/types";
import "./relationshipsPage.css";

const LAYERS: readonly GraphLayer[] = ["code", "deploy", "infra", "runtime", "security", "ops"];

type CatalogState =
  | { readonly status: "idle" | "loading" }
  | { readonly status: "error"; readonly message: string }
  | {
      readonly status: "ready";
      readonly verbs: readonly RelationshipVerbTile[];
      readonly totalEdges: number;
      readonly layerCount: number;
    };

type EdgesState =
  | { readonly status: "idle" | "loading" }
  | { readonly status: "error"; readonly message: string }
  | { readonly status: "ready"; readonly data: RelationshipEdges };

export function RelationshipsPage({
  model,
  client,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const live = model.source === "live" && !!client;
  const [searchParams, setSearchParams] = useSearchParams();
  const [catalog, setCatalog] = useState<CatalogState>({ status: "idle" });
  const [layer, setLayer] = useState<GraphLayer | "all">("all");
  const [selectedVerb, setSelectedVerb] = useState<string | null>(null);
  const [edges, setEdges] = useState<EdgesState>({ status: "idle" });

  // Initialize sourceTool from the URL query param on mount.
  const [sourceTool, setSourceTool] = useState<string>(() => searchParams.get("source_tool") ?? "");

  // Load the verb catalog once a live client is available. Demo mode renders the
  // snapshot relationships instead so prospects still see the surface populated.
  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setCatalog({ status: "idle" });
      return () => {
        cancelled = true;
      };
    }
    setCatalog({ status: "loading" });
    void loadRelationshipsCatalog(client)
      .then((data) => {
        if (cancelled) return;
        setCatalog({
          status: "ready",
          verbs: data.verbs,
          totalEdges: data.totalEdges,
          layerCount: data.layerCount,
        });
      })
      .catch((error) => {
        if (!cancelled)
          setCatalog({
            status: "error",
            message: error instanceof Error ? error.message : "failed to load relationships",
          });
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  // Demo mode: surface the snapshot relationship rows as verb tiles so the page
  // is not blank without a live API.
  const demoVerbs = useMemo<readonly RelationshipVerbTile[]>(
    () =>
      model.relationships.map((row) => ({
        verb: row.verb,
        layer: row.layer,
        count: row.count,
        evidence: row.detail,
        detail: row.detail,
      })),
    [model.relationships],
  );

  const verbs = catalog.status === "ready" ? catalog.verbs : live ? [] : demoVerbs;
  const totalEdges =
    catalog.status === "ready"
      ? catalog.totalEdges
      : verbs.reduce((sum, verb) => sum + verb.count, 0);
  const layerCount =
    catalog.status === "ready" ? catalog.layerCount : new Set(verbs.map((verb) => verb.layer)).size;

  const layerCounts = useMemo(() => {
    const counts = new Map<GraphLayer, number>();
    for (const verb of verbs) counts.set(verb.layer, (counts.get(verb.layer) ?? 0) + 1);
    return counts;
  }, [verbs]);

  const visibleVerbs = useMemo(
    () => (layer === "all" ? verbs : verbs.filter((verb) => verb.layer === layer)),
    [verbs, layer],
  );

  // Collect the tool set from all verb tiles that carry a breakdown.
  // This gives us a stable, server-authoritative list to offer as filter chips.
  const availableTools = useMemo<readonly string[]>(() => {
    const seen = new Set<string>();
    for (const verb of verbs) {
      if (verb.sourceTools) {
        for (const tool of Object.keys(verb.sourceTools)) seen.add(tool);
      }
    }
    return Array.from(seen).sort();
  }, [verbs]);

  function fetchEdges(verb: string, tool: string): void {
    if (!client) {
      setEdges({ status: "idle" });
      return;
    }
    setEdges({ status: "loading" });
    void loadRelationshipEdges(client, verb, 50, tool || undefined)
      .then((data) => setEdges({ status: "ready", data }))
      .catch((error) =>
        setEdges({
          status: "error",
          message: error instanceof Error ? error.message : "failed to load edges",
        }),
      );
  }

  function selectVerb(verb: string): void {
    setSelectedVerb(verb);
    fetchEdges(verb, sourceTool);
  }

  // Reflect source_tool changes in the URL and re-fetch the active verb slice.
  function applyToolFilter(tool: string): void {
    setSourceTool(tool);
    const next = new URLSearchParams(searchParams);
    if (tool) {
      next.set("source_tool", tool);
    } else {
      next.delete("source_tool");
    }
    setSearchParams(next, { replace: true });
    if (selectedVerb) fetchEdges(selectedVerb, tool);
  }

  return (
    <div className="page rel-page">
      <div className="page-intro">
        <h2>Relationships</h2>
        <p>
          Every typed edge Eshu has observed across the code-to-cloud graph — {fmt(totalEdges)}{" "}
          edges spanning {verbs.length} verbs. Each verb is a query: select one to list its concrete
          relationships with endpoints and the evidence behind them. This is how you answer who
          imports this, what deploys here, which workloads assume this role, what is affected by
          this change.
        </p>
      </div>

      <div className="rel-stats">
        <StatTile
          label="Relationships"
          value={fmt(totalEdges)}
          sub="typed edges observed"
          color={LAYER_COLOR.code}
        />
        <StatTile
          label="Typed verbs"
          value={verbs.length}
          sub="relationship types"
          color={LAYER_COLOR.deploy}
        />
        <StatTile
          label="Layers"
          value={layerCount}
          sub={LAYERS.join(" · ")}
          color={LAYER_COLOR.infra}
        />
        <div className="rel-nodes-tile">
          <StatTile
            label="Nodes"
            value={fmt(model.runtime.repositories)}
            sub="repositories"
            color={LAYER_COLOR.runtime}
          />
          <Link className="rel-nodes-browse" to="/explorer">
            Browse graph →
          </Link>
        </div>
      </div>

      {catalog.status === "error" ? (
        <div className="prov-banner warn">Relationships unavailable: {catalog.message}</div>
      ) : null}

      <div className="explorer-filters rel-layer-filter">
        <button
          className={`layer-toggle ${layer === "all" ? "on" : "off"}`}
          onClick={() => setLayer("all")}
        >
          <span>All</span>
          <strong className="rel-layer-count">{verbs.length}</strong>
        </button>
        {LAYERS.map((key) => (
          <button
            key={key}
            className={`layer-toggle ${layer === key ? "on" : "off"}`}
            style={{ "--lc": LAYER_COLOR[key] } as React.CSSProperties}
            onClick={() => setLayer(key)}
          >
            <i style={{ background: LAYER_COLOR[key] }} />
            <span style={{ textTransform: "capitalize" }}>{key}</span>
            <strong className="rel-layer-count">{layerCounts.get(key) ?? 0}</strong>
          </button>
        ))}
      </div>

      {availableTools.length > 0 || !!sourceTool ? (
        <ToolFilter available={availableTools} selected={sourceTool} onChange={applyToolFilter} />
      ) : null}

      <div className="rel-layout">
        <Panel
          title="Verb catalog"
          sub={
            catalog.status === "loading" ? "Loading…" : `${visibleVerbs.length} relationship types`
          }
        >
          {visibleVerbs.length === 0 ? (
            <p className="empty">
              {catalog.status === "loading"
                ? "Loading verb catalog…"
                : "No relationship verbs for this layer."}
            </p>
          ) : (
            <ul className="rel-verb-list">
              {visibleVerbs.map((verb) => (
                <li key={verb.verb}>
                  <button
                    className={`rel-verb-row${selectedVerb === verb.verb ? " active" : ""}`}
                    onClick={() => selectVerb(verb.verb)}
                  >
                    <span className="rel-verb-name">
                      <i style={{ background: LAYER_COLOR[verb.layer] }} />
                      <span className="mono">{verb.verb}</span>
                    </span>
                    <span className="rel-verb-meta">
                      <span className="rel-verb-layer" style={{ color: LAYER_COLOR[verb.layer] }}>
                        {verb.layer}
                      </span>
                      <strong>{fmt(verb.count)} edges</strong>
                      <span className="t-mut">{verb.evidence}</span>
                    </span>
                  </button>
                  {verb.sourceTools ? <SourceToolBreakdown tools={verb.sourceTools} /> : null}
                </li>
              ))}
            </ul>
          )}
        </Panel>

        <Panel
          title={selectedVerb ? `${selectedVerb} edges` : "Concrete edges"}
          sub={selectedVerb ? "endpoints + evidence" : "Select a verb to list its edges"}
        >
          {selectedVerb === null ? (
            <p className="empty">
              Select a verb to list its concrete relationships with endpoints and evidence.
            </p>
          ) : edges.status === "loading" ? (
            <p className="empty">Loading edges…</p>
          ) : edges.status === "error" ? (
            <p className="src-err">⚠ {edges.message}</p>
          ) : edges.status === "ready" ? (
            edges.data.edges.length === 0 ? (
              <p className="empty">
                {sourceTool
                  ? `No edges stamped by "${sourceTool}" for this verb.`
                  : "No concrete edges recorded for this verb yet."}
              </p>
            ) : (
              <>
                {edges.data.sourceTool ? (
                  <p className="rel-tool-active t-mut">
                    Filtered to <Badge tone="teal">{edges.data.sourceTool}</Badge> edges
                  </p>
                ) : null}
                <ul className="rel-edge-list">
                  {edges.data.edges.map((edge, index) => (
                    <li className="rel-edge-row" key={`${edge.sourceId}-${edge.targetId}-${index}`}>
                      <span className="rel-edge-endpoints">
                        <span className="rel-edge-node mono" title={edge.sourceId}>
                          {edge.sourceName}
                        </span>
                        <span className="rel-edge-arrow">→</span>
                        <span className="rel-edge-node mono" title={edge.targetId}>
                          {edge.targetName}
                        </span>
                        {edge.sourceTool ? <Badge tone="teal">{edge.sourceTool}</Badge> : null}
                      </span>
                      {edge.evidence ? (
                        <span className="rel-edge-evidence t-mut">{edge.evidence}</span>
                      ) : null}
                    </li>
                  ))}
                </ul>
                {edges.data.truncated ? (
                  <p className="t-mut rel-edge-trunc">
                    Showing the first {edges.data.limit} edges.
                  </p>
                ) : null}
                <ToolLegend />
              </>
            )
          ) : (
            <p className="empty">Select a verb to begin.</p>
          )}
        </Panel>
      </div>
    </div>
  );
}

// ToolFilter renders a row of chips letting the user filter the edge slice by
// source_tool. Selecting the active tool again clears the filter.
//
// When the active tool is not in the available list (e.g. the user landed via a
// stale shared link and the current graph has no tool-stamped edges), the chip
// is still rendered so the user can see and clear the active filter. The chip is
// user-supplied state, not an invented catalog entry.
function ToolFilter({
  available,
  selected,
  onChange,
}: {
  readonly available: readonly string[];
  readonly selected: string;
  readonly onChange: (tool: string) => void;
}): React.JSX.Element {
  // Render the active tool chip even when the catalog doesn't list it, so the
  // user can always see and clear an active URL filter.
  const extraChips = selected && !available.includes(selected) ? [selected] : [];

  return (
    <div className="rel-tool-filter" aria-label="Filter edges by source tool">
      <span className="rel-tool-filter-label t-mut">Tool:</span>
      <button
        className={`rel-tool-chip${!selected ? " active" : ""}`}
        onClick={() => onChange("")}
        aria-pressed={!selected}
      >
        All
      </button>
      {available.map((tool) => (
        <button
          key={tool}
          className={`rel-tool-chip${selected === tool ? " active" : ""}`}
          onClick={() => onChange(selected === tool ? "" : tool)}
          aria-pressed={selected === tool}
        >
          {tool}
        </button>
      ))}
      {extraChips.map((tool) => (
        <button
          key={tool}
          className="rel-tool-chip active"
          onClick={() => onChange("")}
          aria-pressed={true}
        >
          {tool}
        </button>
      ))}
    </div>
  );
}

// SourceToolBreakdown renders the per-tool edge counts inside a verb tile row.
// Only shown for Tier-2 verbs that carry source_tool.
function SourceToolBreakdown({
  tools,
}: {
  readonly tools: Readonly<Record<string, number>>;
}): React.JSX.Element {
  const entries = Object.entries(tools).sort((a, b) => b[1] - a[1]);
  return (
    <p className="rel-verb-tools t-mut">
      {entries.map(([tool, count], i) => (
        <span key={tool}>
          {i > 0 ? " · " : null}
          <span className="mono">{tool}</span> {fmt(count)}
        </span>
      ))}
    </p>
  );
}

// ToolLegend explains the source_tool / Tier-3 provenance model to the user
// so they understand why some edges have no tool badge.
function ToolLegend(): React.JSX.Element {
  return (
    <p className="rel-tool-legend t-mut">
      <Badge tone="teal">tool</Badge> badges indicate Tier-2 edges resolved from infrastructure
      tooling. Edges with no badge are Tier-1 (self-labeling) or Tier-3 code/structural edges that
      are not scoped to a specific tool.
    </p>
  );
}
