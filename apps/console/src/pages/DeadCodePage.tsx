import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import { loadDeadCodePage } from "../api/deadCode";
import type { DeadCodePage as LiveDeadCodePage } from "../api/deadCode";
import { loadRepositoryNameMap } from "../api/repoCatalog";
import { Panel, StatTile, Badge, TruthChip } from "../components/atoms";
import type { ConsoleModel, FindingRow } from "../console/types";
import { fmt, uiTruth } from "../console/types";
import "./liveInventory.css";

const ANY = "all";
const LIVE_LIMIT = 100;

interface DeadCodeFilters {
  readonly language: string;
  readonly repoId: string;
}

const EMPTY_FILTERS: DeadCodeFilters = { language: "", repoId: "" };

export function DeadCodePage({
  client,
  model
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
}): React.JSX.Element {
  const [livePage, setLivePage] = useState<LiveDeadCodePage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<DeadCodeFilters>(EMPTY_FILTERS);
  const [applied, setApplied] = useState<DeadCodeFilters>(EMPTY_FILTERS);
  const [classification, setClassification] = useState(ANY);
  const [kind, setKind] = useState(ANY);
  const [query, setQuery] = useState(() => new URLSearchParams(window.location.search).get("q") ?? "");

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setLivePage(null);
      setBusy(false);
      setErr("");
      return;
    }
    setBusy(true);
    setErr("");
    void loadRepositoryNames(client)
      .then((repoNames) => loadDeadCodePage(client, {
        language: applied.language || undefined,
        limit: LIVE_LIMIT,
        repoId: applied.repoId || undefined
      }, repoNames))
      .then((page) => {
        if (!cancelled) {
          setLivePage(page);
          setBusy(false);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setLivePage(null);
          setBusy(false);
          setErr(error instanceof Error ? error.message : "failed to load dead-code candidates");
        }
      });
    return () => { cancelled = true; };
  }, [applied, client]);

  const all = (livePage?.rows ?? model.findings).filter((finding) => finding.type === "Dead code");
  const classifications = unique(all.map(classificationFromFinding).filter(Boolean));
  const kinds = unique(all.map(kindFromFinding).filter(Boolean));
  const filtered = all.filter((finding) =>
    (classification === ANY || classificationFromFinding(finding) === classification) &&
    (kind === ANY || kindFromFinding(finding) === kind) &&
    matchesQuery(finding, query)
  );
  const grouped = groupByRepository(filtered);
  const repositories = unique(all.map(repositoryScopeKey).filter(Boolean));
  const totalLoc = all.reduce((sum, finding) => sum + locFromFinding(finding), 0);
  const highConfidence = all.filter((finding) => finding.classification === "unused" || finding.truth === "exact").length;
  const source = client ? (busy ? "loading" : err ? "unavailable" : "live") : model.source;
  const scanLabel = livePage
    ? `${livePage.limit} candidate scan${livePage.truncated ? " · truncated" : ""}`
    : client
      ? "direct live scan"
      : "snapshot";

  function applyFilters(): void {
    setApplied({ language: draft.language.trim(), repoId: draft.repoId.trim() });
  }

  function resetLiveFilters(): void {
    setDraft(EMPTY_FILTERS);
    setApplied(EMPTY_FILTERS);
  }

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Dead code</h2>
        <p>
          Graph-backed dead-code candidates from{" "}
          <span className="mono">POST /api/v0/code/dead-code</span>. Candidates
          are grouped by repository and stay empty when the API has no analyzer
          output. Select a location to open the source file.
        </p>
      </div>

      <div className="grid g-4">
        <StatTile label="Dead symbols" value={all.length} color="var(--ember)" sub="0 inbound references" />
        <StatTile label="Repos affected" value={repositories.length} color="var(--blue)" sub="with candidates" />
        <StatTile label="Est. dead LOC" value={fmt(totalLoc)} color="var(--violet)" sub="line span estimate" />
        <StatTile label="High confidence" value={highConfidence} color="var(--teal)" sub={scanLabel} />
      </div>

      <div className="repo-toolbar mt">
        <div className="searchbox repo-search">
          <input
            aria-label="Find dead-code candidate"
            placeholder="Find a symbol, file or repo..."
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        {client ? (
          <>
            <input
              aria-label="Repository selector"
              className="popover-input mono"
              placeholder="repo_id or name"
              value={draft.repoId}
              onChange={(event) => setDraft((current) => ({ ...current, repoId: event.target.value }))}
            />
            <input
              aria-label="Language selector"
              className="popover-input mono"
              placeholder="language"
              value={draft.language}
              onChange={(event) => setDraft((current) => ({ ...current, language: event.target.value }))}
            />
            <button className="btn-ghost active" disabled={busy} onClick={applyFilters}>Apply</button>
            <button className="btn-ghost" disabled={busy} onClick={resetLiveFilters}>Reset</button>
          </>
        ) : null}
        <div className="seg" aria-label="Dead-code kind filter">
          {[ANY, ...kinds].map((value) => (
            <button key={value} className={kind === value ? "active" : ""} onClick={() => setKind(value)}>
              {value === ANY ? "All kinds" : `${value} · ${all.filter((finding) => kindFromFinding(finding) === value).length}`}
            </button>
          ))}
        </div>
        <div className="seg" aria-label="Dead-code classification filter">
          {[ANY, ...classifications].map((value) => (
            <button key={value} className={classification === value ? "active" : ""} onClick={() => setClassification(value)}>
              {value === ANY ? "Any" : value}
            </button>
          ))}
        </div>
      </div>

      <div className="evidence-workbench mt" aria-label="Dead-code workbench">
        <Panel className="flush" title={`${filtered.length} candidates`} sub={`Grouped by repository · ${source}`}>
          <div className="table-scroll">
            <table className="tbl wide">
              <thead><tr><th>Symbol</th><th>Kind</th><th>Location</th><th>Refs</th><th>LOC</th><th>Confidence</th><th>Why dead</th><th>Actions</th></tr></thead>
              <tbody>
                {grouped.map((group) => (
                  <DeadCodeGroup key={group.key} group={group} />
                ))}
                {filtered.length === 0 ? (
                  <tr><td colSpan={8} className="empty">{err ? `Failed to load: ${err}` : busy ? "Loading dead-code candidates..." : "No dead-code candidates from this source."}</td></tr>
                ) : null}
              </tbody>
            </table>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function DeadCodeGroup({ group }: { readonly group: DeadCodeGroupModel }): React.JSX.Element {
  const loc = group.rows.reduce((sum, finding) => sum + locFromFinding(finding), 0);
  return (
    <>
      <tr className="group-row">
        <td colSpan={8}>
          <span className="group-label" style={{ color: "var(--ember)" }}>{group.repository}</span>
          <span className="group-meta">{group.rows.length} dead · {fmt(loc)} LOC</span>
        </td>
      </tr>
      {group.rows.map((finding) => {
        const href = sourceHref(finding);
        return (
          <tr key={finding.id} className="cloud-row">
            <td className="cell-stack">
              <span className="mono" style={{ color: "var(--bone)", fontWeight: 600 }}>{symbolFromFinding(finding)}</span>
              <small>{finding.title}</small>
            </td>
            <td><Badge tone="neutral">{kindFromFinding(finding)}</Badge></td>
            <td className="t-mut mono" style={{ fontSize: ".74rem" }}>
              {href ? <Link className="mono" to={href}>{locationFromFinding(finding)}</Link> : locationFromFinding(finding)}
            </td>
            <td><span className="mono" style={{ color: "var(--crit)", fontWeight: 700 }}>0</span></td>
            <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{locFromFinding(finding) || "—"}</td>
            <td><TruthChip level={uiTruth(finding.truth)} /></td>
            <td className="t-mut" style={{ fontSize: ".78rem", maxWidth: 360 }}>{classificationFromFinding(finding) || "candidate"}</td>
            <td><Link className="btn-ghost" to={codeGraphHref(finding)}>Open graph</Link></td>
          </tr>
        );
      })}
    </>
  );
}

interface DeadCodeGroupModel {
  readonly key: string;
  readonly repository: string;
  readonly rows: readonly FindingRow[];
}

function groupByRepository(rows: readonly FindingRow[]): readonly DeadCodeGroupModel[] {
  const groups = new Map<string, { readonly label: string; readonly rows: FindingRow[] }>();
  for (const row of rows) {
    const key = repositoryScopeKey(row);
    const group = groups.get(key);
    if (group) {
      groups.set(key, { label: group.label, rows: [...group.rows, row] });
    } else {
      groups.set(key, { label: row.entity || "repository", rows: [row] });
    }
  }
  return [...groups.entries()]
    .map(([key, group]) => ({ key, repository: group.label, rows: group.rows }))
    .sort((a, b) => b.rows.length - a.rows.length || a.repository.localeCompare(b.repository));
}

function repositoryScopeKey(finding: FindingRow): string {
  return finding.repoId?.trim() || finding.entity.trim() || finding.id;
}

function unique(values: readonly string[]): readonly string[] {
  return [...new Set(values)].sort((a, b) => a.localeCompare(b));
}

function matchesQuery(finding: FindingRow, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (q === "") return true;
  return [
    finding.title,
    finding.entity,
    finding.detail,
    finding.filePath ?? "",
    finding.language ?? ""
  ].join(" ").toLowerCase().includes(q);
}

function symbolFromFinding(finding: FindingRow): string {
  return finding.title.replace(/^Unreferenced symbol\s+/i, "").trim() || finding.title;
}

function classificationFromFinding(finding: FindingRow): string {
  return finding.classification ?? finding.detail.split(" · ")[1] ?? "";
}

function kindFromFinding(finding: FindingRow): string {
  return finding.labels?.[0]?.toLowerCase() ?? "symbol";
}

function locationFromFinding(finding: FindingRow): string {
  const path = finding.filePath ?? finding.detail.split(" · ")[0] ?? "source path unavailable";
  return finding.startLine ? `${path}:${finding.startLine}` : path;
}

function sourceHref(finding: FindingRow): string | null {
  const filePath = finding.filePath;
  const repository = finding.repoId ?? finding.entity;
  if (!filePath || !repository) return null;
  const params = new URLSearchParams({ path: filePath });
  if (finding.startLine !== undefined) params.set("lineStart", String(finding.startLine));
  if (finding.endLine !== undefined) params.set("lineEnd", String(finding.endLine));
  return `/repositories/${encodeURIComponent(repository)}/source?${params.toString()}`;
}

function codeGraphHref(finding: FindingRow): string {
  return `/code-graph?candidate=${encodeURIComponent(finding.id)}`;
}

function locFromFinding(finding: FindingRow): number {
  if (finding.startLine && finding.endLine && finding.endLine >= finding.startLine) {
    return finding.endLine - finding.startLine + 1;
  }
  return 0;
}

async function loadRepositoryNames(client: EshuApiClient): Promise<ReadonlyMap<string, string>> {
  try {
    return await loadRepositoryNameMap(client);
  } catch {
    return new Map();
  }
}
