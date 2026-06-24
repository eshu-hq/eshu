// pages/NodesPage.tsx
// Browsable graph-entity explorer (issue #3396). Surfaces "what entities exist"
// from the live graph via GET /api/v0/graph/entities: per-kind stat tiles and
// filter chips with live counts, a name/account search, and a clickable table
// of first-class entities. Clicking a node opens it in the Graph Explorer.
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import type { SectionProvenance } from "../api/eshuConsoleLive";
import { loadGraphEntities } from "../api/graphEntities";
import type { GraphEntityKindCount, GraphEntityRow } from "../api/graphEntities";
import { Panel, StatTile, Badge, TruthChip, FreshDot } from "../components/atoms";
import { uiTruth, uiFresh, fmt } from "../console/types";
import "./liveInventory.css";

const PAGE_SIZE = 50;

// KIND_LABELS maps each facet key to its human chip label. Keys must match the
// backend facet catalog (graph_entity_inventory.go); unknown keys fall back to
// the raw key so a backend-added facet still renders.
const KIND_LABELS: Readonly<Record<string, string>> = {
  services: "Services",
  repositories: "Repositories",
  libraries: "Libraries",
  container_images: "Container images",
  environments: "Environments",
  cloud_resources: "Cloud resources",
  identity_iam: "Identity & IAM",
  networking: "Networking"
};

function chipLabel(kind: string): string {
  return KIND_LABELS[kind] ?? kind;
}

export function NodesPage({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const navigate = useNavigate();
  const [kinds, setKinds] = useState<readonly GraphEntityKindCount[]>([]);
  const [total, setTotal] = useState(0);
  const [entities, setEntities] = useState<readonly GraphEntityRow[] | null>(null);
  const [offset, setOffset] = useState(0);
  const [nextOffset, setNextOffset] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [provenance, setProvenance] = useState<SectionProvenance>("live");
  const [truthLevel, setTruthLevel] = useState<string | undefined>(undefined);
  const [freshState, setFreshState] = useState<string | undefined>(undefined);
  const [selectedKind, setSelectedKind] = useState<string | null>(null);
  const [q, setQ] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setEntities([]);
      setProvenance("unavailable");
      return;
    }
    setBusy(true);
    void loadGraphEntities(client, {
      kind: selectedKind ?? undefined,
      q,
      limit: PAGE_SIZE,
      offset
    }).then((page) => {
      if (cancelled) return;
      setKinds(page.kinds);
      setTotal(page.total);
      setEntities(selectedKind ? page.entities : []);
      setNextOffset(page.nextOffset);
      setProvenance(page.provenance);
      setTruthLevel(page.truth?.level);
      setFreshState(page.truth?.freshness.state);
      setBusy(false);
    });
    return () => {
      cancelled = true;
    };
  }, [client, selectedKind, q, offset]);

  function selectKind(kind: string | null): void {
    setSelectedKind(kind);
    setOffset(0);
  }

  function openNode(row: GraphEntityRow): void {
    const target = row.name || row.id;
    if (target === "") return;
    navigate(`/explorer?q=${encodeURIComponent(target)}`);
  }

  const kindCount = useMemo(
    () => kinds.reduce((sum, kind) => sum + kind.count, 0),
    [kinds]
  );
  const cloudResourceCount = kinds.find((kind) => kind.kind === "cloud_resources")?.count ?? null;
  const browsableCount = kindCount;

  const rows = entities ?? [];
  const sub = entities === null
    ? "loading…"
    : provenance === "unavailable"
      ? "unavailable"
      : selectedKind === null
        ? `${sourceLabel} · pick a kind to browse`
        : `${sourceLabel} · ${rows.length} shown`;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Nodes</h2>
        <p>
          Every first-class entity Eshu has materialised in the graph — services,
          repositories, libraries, container images, environments, cloud
          resources, identity, and networking — from{" "}
          <span className="mono">GET /api/v0/graph/entities</span>. Pick a kind and
          click a node to open it.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile
          label="Browsable entities"
          value={provenance === "unavailable" ? "—" : fmt(browsableCount)}
          color="var(--teal)"
          sub={`${kinds.length} node kinds`}
        />
        <StatTile
          label="Node kinds"
          value={provenance === "unavailable" ? "—" : kinds.length}
          color="var(--blue)"
          sub="first-class facets"
        />
        <StatTile
          label="Cloud resources"
          value={provenance === "unavailable" || cloudResourceCount === null ? "—" : fmt(cloudResourceCount)}
          color="var(--violet)"
          sub="VPC · LB · SG · IAM · data"
        />
        <StatTile
          label="Total in facets"
          value={provenance === "unavailable" ? "—" : fmt(total)}
          color="var(--ember)"
          sub="across all kinds"
        />
      </div>

      <Panel
        className="flush mt"
        title="Graph entities"
        sub={sub}
        action={
          <div className="panel-action-stack">
            {truthLevel ? <TruthChip level={uiTruth(truthLevel)} /> : null}
            {freshState ? <FreshDot state={uiFresh(freshState)} /> : null}
            <div className="searchbox compact">
              <input
                aria-label="Find a node by name, type or account"
                placeholder="Find a node by name, type or account…"
                value={q}
                onChange={(e) => {
                  setQ(e.target.value);
                  setOffset(0);
                }}
              />
            </div>
          </div>
        }
      >
        <div className="chip-row" role="group" aria-label="Filter by kind">
          <button
            type="button"
            className={`chip${selectedKind === null ? " active" : ""}`}
            onClick={() => selectKind(null)}
          >
            All <span className="chip-count">{provenance === "unavailable" ? "—" : fmt(kindCount)}</span>
          </button>
          {kinds.map((kind) => (
            <button
              key={kind.kind}
              type="button"
              className={`chip${selectedKind === kind.kind ? " active" : ""}`}
              onClick={() => selectKind(kind.kind)}
            >
              {chipLabel(kind.kind)} <span className="chip-count">{fmt(kind.count)}</span>
            </button>
          ))}
        </div>

        {entities === null ? (
          <div className="conn-state compact">
            <div className="conn-spinner" aria-hidden />
            <p>Loading graph entities…</p>
          </div>
        ) : provenance === "unavailable" ? (
          <p className="empty">
            Graph entity inventory unavailable from this source. An authoritative
            platform profile with a materialised graph is required.
          </p>
        ) : selectedKind === null ? (
          <p className="empty">Select a kind above to browse its entities.</p>
        ) : (
          <>
            <div className="table-scroll">
              <table className="tbl wide">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Kind</th>
                    <th>Account / scope</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={row.id || row.name} onClick={() => openNode(row)} style={{ cursor: "pointer" }}>
                      <td className="t-name">{row.name || "—"}</td>
                      <td><Badge tone="teal">{chipLabel(row.kind)}</Badge></td>
                      <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{row.account || "—"}</td>
                    </tr>
                  ))}
                  {rows.length === 0 ? (
                    <tr>
                      <td colSpan={3} className="empty">
                        {q !== "" ? "No nodes match this search." : "No nodes of this kind from this source."}
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>

            <div className="pager-row">
              <span className="t-mut" style={{ fontSize: ".76rem" }}>
                rows {offset + 1}–{offset + rows.length}
              </span>
              <button
                className="btn-ghost"
                disabled={busy || offset === 0}
                onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
              >
                ← Prev
              </button>
              <button
                className="btn-ghost"
                disabled={busy || nextOffset === null}
                onClick={() => { if (nextOffset !== null) setOffset(nextOffset); }}
              >
                Next →
              </button>
            </div>
          </>
        )}
      </Panel>
    </div>
  );
}
