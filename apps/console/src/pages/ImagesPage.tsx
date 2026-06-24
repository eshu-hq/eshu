// pages/ImagesPage.tsx
// Container image (OCI) inventory browser. Lists images from
// GET /api/v0/images — digest, tags, registry/repository, media type, and size —
// with offset-based pagination via next_cursor and optional exact-match filters.
//
// Data note: ContainerImage nodes carry no workload edges in the graph
// (DEPLOYS_FROM is Repository->Repository), so this surface deliberately does NOT
// show a "deploying workloads" column. It surfaces image node properties only and
// never fabricates deployment links.
import { useEffect, useState } from "react";

import type { EshuApiClient } from "../api/client";
import type { SectionProvenance } from "../api/eshuConsoleLive";
import { loadImages } from "../api/imageInventory";
import type { ImageRow } from "../api/imageInventory";
import { Panel, StatTile, Badge, TruthChip, FreshDot } from "../components/atoms";
import { uiTruth, uiFresh } from "../console/types";
import "./liveInventory.css";

const PAGE_SIZE = 50;

function fmtBytes(n: number | null): string {
  if (n === null) return "—";
  if (n >= 1e9) return `${(n / 1e9).toFixed(2)} GB`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(2)} MB`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)} kB`;
  return `${n} B`;
}

function shortDigest(digest: string): string {
  if (digest === "") return "—";
  const body = digest.startsWith("sha256:") ? digest.slice("sha256:".length) : digest;
  return body.length > 12 ? `${digest.slice(0, digest.indexOf(":") + 1)}${body.slice(0, 12)}…` : digest;
}

export function ImagesPage({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [images, setImages] = useState<readonly ImageRow[] | null>(null);
  const [offset, setOffset] = useState(0);
  const [nextOffset, setNextOffset] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [provenance, setProvenance] = useState<SectionProvenance>("live");
  const [truthLevel, setTruthLevel] = useState<string | undefined>(undefined);
  const [freshState, setFreshState] = useState<string | undefined>(undefined);
  const [q, setQ] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) { setImages([]); return; }
    setBusy(true);
    void loadImages(client, { limit: PAGE_SIZE, offset })
      .then((page) => {
        if (cancelled) return;
        setImages(page.images);
        setNextOffset(page.nextOffset);
        setProvenance(page.provenance);
        setTruthLevel(page.truth?.level);
        setFreshState(page.truth?.freshness.state);
        setBusy(false);
      });
    return () => { cancelled = true; };
  }, [client, offset]);

  const rows = (images ?? []).filter((r) => {
    if (q === "") return true;
    const hay = `${r.name} ${r.repository} ${r.registry} ${r.tag} ${r.digest}`.toLowerCase();
    return hay.includes(q.toLowerCase());
  });
  const imageRows = images ?? [];
  const registryCount = distinctCount(imageRows.map((image) => image.registry));
  const repositoryCount = distinctCount(imageRows.map((image) => image.repository));
  const taggedCount = imageRows.filter((image) => image.tag !== "").length;

  const sub = images === null
    ? "loading…"
    : provenance === "unavailable"
      ? "unavailable"
      : `${sourceLabel} · ${rows.length} shown`;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Container Images</h2>
        <p>
          OCI registry inventory from <span className="mono">GET /api/v0/images</span>:
          digest, tags, registry/repository, media type, and size.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile label="Images loaded" value={images === null || provenance === "unavailable" ? "—" : imageRows.length} color="var(--teal)" sub="bounded page from OCI inventory" />
        <StatTile label="Registries" value={images === null || provenance === "unavailable" ? "—" : registryCount} color="var(--blue)" sub="distinct in this page" />
        <StatTile label="Repositories" value={images === null || provenance === "unavailable" ? "—" : repositoryCount} color="var(--violet)" sub="image repositories" />
        <StatTile label="Tagged" value={images === null || provenance === "unavailable" ? "—" : taggedCount} color="var(--ember)" sub="rows with tags" />
      </div>

      <Panel
        className="flush mt"
        title="Image inventory"
        sub={sub}
        action={
          <div className="panel-action-stack">
            {truthLevel ? <TruthChip level={uiTruth(truthLevel)} /> : null}
            {freshState ? <FreshDot state={uiFresh(freshState)} /> : null}
            <div className="searchbox compact">
              <input placeholder="Filter this page…" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
          </div>
        }
      >
        {images === null ? (
          <div className="conn-state compact">
            <div className="conn-spinner" aria-hidden />
            <p>Loading container images…</p>
          </div>
        ) : provenance === "unavailable" ? (
          <p className="empty">
            Container image inventory unavailable from this source. The OCI registry
            collector may not be enabled.
          </p>
        ) : (
          <>
            <div className="table-scroll">
              <table className="tbl wide">
                <thead>
                  <tr>
                    <th>Repository</th>
                    <th>Tag</th>
                    <th>Digest</th>
                    <th>Media type</th>
                    <th>Size</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r) => (
                    <tr key={r.id}>
                      <td className="t-name">
                        {r.repository || r.name || "—"}
                        {r.registry ? <div className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.registry}</div> : null}
                      </td>
                      <td>{r.tag ? <Badge tone="teal">{r.tag}</Badge> : <span className="t-mut">—</span>}</td>
                      <td className="t-mut mono" style={{ fontSize: ".74rem" }} title={r.digest || undefined}>{shortDigest(r.digest)}</td>
                      <td className="t-mut mono" style={{ fontSize: ".72rem" }}>{r.mediaType || r.artifactType || "—"}</td>
                      <td>{fmtBytes(r.sizeBytes)}</td>
                    </tr>
                  ))}
                  {rows.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="empty">
                        {q !== "" ? "No images match this filter." : "No container images from this source."}
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>

            <div className="pager-row">
              <span className="t-mut" style={{ fontSize: ".76rem" }}>
                rows {offset + 1}–{offset + (images?.length ?? 0)}
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

function distinctCount(values: readonly string[]): number {
  return new Set(values.filter((value) => value !== "")).size;
}
