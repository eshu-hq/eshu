import { useState } from "react";
import type { ConsoleModel, FindingRow } from "../console/types";
import { fmt, uiTruth } from "../console/types";
import { Panel, StatTile, Badge, TruthChip } from "../components/atoms";
import "./liveInventory.css";

const ANY = "all";

export function DeadCodePage({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const all = model.findings.filter((finding) => finding.type === "Dead code");
  const [classification, setClassification] = useState(ANY);
  const [kind, setKind] = useState(ANY);
  const [query, setQuery] = useState("");
  const classifications = unique(all.map(classificationFromFinding).filter(Boolean));
  const kinds = unique(all.map(kindFromFinding).filter(Boolean));
  const filtered = all.filter((finding) =>
    (classification === ANY || classificationFromFinding(finding) === classification) &&
    (kind === ANY || kindFromFinding(finding) === kind) &&
    matchesQuery(finding, query)
  );
  const grouped = groupByRepository(filtered);
  const repositories = unique(all.map((finding) => finding.entity).filter(Boolean));
  const totalLoc = all.reduce((sum, finding) => sum + locFromFinding(finding), 0);
  const highConfidence = all.filter((finding) => finding.classification === "unused" || finding.truth === "exact").length;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Dead code</h2>
        <p>Graph-backed dead-code candidates from <span className="mono">/api/v0/code/dead-code</span>. Candidates are grouped by repository and stay empty when the API has no analyzer output.</p>
      </div>

      <div className="grid g-4">
        <StatTile label="Dead symbols" value={all.length} color="var(--ember)" sub="0 inbound references" />
        <StatTile label="Repos affected" value={repositories.length} color="var(--blue)" sub="with candidates" />
        <StatTile label="Est. dead LOC" value={fmt(totalLoc)} color="var(--violet)" sub="line span estimate" />
        <StatTile label="High confidence" value={highConfidence} color="var(--teal)" sub="unused or exact" />
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
        <Panel className="flush" title={`${filtered.length} candidates`} sub="Grouped by repository">
          <div className="table-scroll">
            <table className="tbl wide">
              <thead><tr><th>Symbol</th><th>Kind</th><th>Location</th><th>Refs</th><th>LOC</th><th>Confidence</th><th>Why dead</th></tr></thead>
              <tbody>
                {grouped.map((group) => (
                  <DeadCodeGroup key={group.repository} group={group} />
                ))}
                {filtered.length === 0 ? (
                  <tr><td colSpan={7} className="empty">No dead-code candidates from this source.</td></tr>
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
        <td colSpan={7}>
          <span className="group-label" style={{ color: "var(--ember)" }}>{group.repository}</span>
          <span className="group-meta">{group.rows.length} dead · {fmt(loc)} LOC</span>
        </td>
      </tr>
      {group.rows.map((finding) => (
        <tr key={finding.id} className="cloud-row">
          <td className="cell-stack">
            <span className="mono" style={{ color: "var(--bone)", fontWeight: 600 }}>{symbolFromFinding(finding)}</span>
            <small>{finding.title}</small>
          </td>
          <td><Badge tone="neutral">{kindFromFinding(finding)}</Badge></td>
          <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{locationFromFinding(finding)}</td>
          <td><span className="mono" style={{ color: "var(--crit)", fontWeight: 700 }}>0</span></td>
          <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{locFromFinding(finding) || "—"}</td>
          <td><TruthChip level={uiTruth(finding.truth)} /></td>
          <td className="t-mut" style={{ fontSize: ".78rem", maxWidth: 360 }}>{classificationFromFinding(finding) || "candidate"}</td>
        </tr>
      ))}
    </>
  );
}

interface DeadCodeGroupModel {
  readonly repository: string;
  readonly rows: readonly FindingRow[];
}

function groupByRepository(rows: readonly FindingRow[]): readonly DeadCodeGroupModel[] {
  const groups = new Map<string, FindingRow[]>();
  for (const row of rows) {
    const key = row.entity || "repository";
    groups.set(key, [...(groups.get(key) ?? []), row]);
  }
  return [...groups.entries()]
    .map(([repository, groupRows]) => ({ repository, rows: groupRows }))
    .sort((a, b) => b.rows.length - a.rows.length || a.repository.localeCompare(b.repository));
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

function locFromFinding(finding: FindingRow): number {
  if (finding.startLine && finding.endLine && finding.endLine >= finding.startLine) {
    return finding.endLine - finding.startLine + 1;
  }
  return 0;
}
