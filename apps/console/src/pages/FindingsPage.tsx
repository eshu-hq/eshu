// pages/FindingsPage.tsx
import { Link } from "react-router-dom";

import type { SectionProvenance } from "../api/eshuConsoleLive";
import { AsyncStateGuard } from "../components/AsyncStateGuard";
import { Panel, StatTile, TruthChip, Badge } from "../components/atoms";
import { uiTruth, type ConsoleModel, type FindingRow, type VulnRow } from "../console/types";

export function FindingsPage({ model }: { readonly model: ConsoleModel }): React.JSX.Element {
  const rows = worklistRows(model);
  const byType = new Map<string, number>();
  rows.forEach((row) => byType.set(row.type, (byType.get(row.type) ?? 0) + 1));
  // Combine the two worklist sources: only block the table when ALL sources are
  // non-ready. If either source responded (even partially), render available rows.
  // Partial failure (e.g. dead-code "unavailable" but supply-chain "live") must
  // not suppress vulnerability rows that are already in the model.
  const findingsProv = model.provenance.findings ?? (model.source === "demo" ? "demo" : "loading");
  const vulnProv =
    model.provenance.vulnerabilities ?? (model.source === "demo" ? "demo" : "loading");
  const provenance = combinedWorklist(findingsProv, vulnProv);
  const partialFailure = partialFailureMessage(findingsProv, vulnProv);
  const loadingNotice = partialLoadingMessage(findingsProv, vulnProv);
  return (
    <div className="page">
      <div className="page-intro">
        <h2>Findings</h2>
        <p>
          Unified worklist for dead code and reachable vulnerabilities from{" "}
          <span className="mono">POST /api/v0/code/dead-code</span> and{" "}
          <span className="mono">GET /api/v0/supply-chain/impact/findings</span>. Each row links to
          the live drilldown surface that owns the evidence.
        </p>
      </div>
      <div className="grid g-4">
        <StatTile
          label="Open findings"
          value={rows.length}
          color="var(--ember)"
          sub={model.source === "live" ? "live worklist rows" : "demo"}
        />
        <StatTile
          label="Dead code"
          value={byType.get("Dead code") ?? 0}
          color="var(--violet)"
          sub="graph-backed candidates"
        />
        <StatTile
          label="Vulnerability"
          value={byType.get("Vulnerability") ?? 0}
          color="var(--crit)"
          sub="reachable advisories"
        />
        <StatTile label="Types" value={byType.size} color="var(--blue)" sub="distinct categories" />
      </div>
      <Panel className="flush mt" title="Unified worklist">
        {partialFailure ? (
          <p className="src-err" role="alert">
            {partialFailure}
          </p>
        ) : null}
        {loadingNotice ? (
          <p className="empty-note" role="status">
            {loadingNotice}
          </p>
        ) : null}
        <AsyncStateGuard provenance={provenance} label="findings">
          <table className="tbl">
            <thead>
              <tr>
                <th>Finding</th>
                <th>Type</th>
                <th>Entity</th>
                <th>Source</th>
                <th>Truth</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <td className="cell-stack" style={{ maxWidth: 460 }}>
                    <span style={{ color: "var(--bone)", fontWeight: 600 }}>{row.title}</span>
                    <small>{row.detail}</small>
                  </td>
                  <td>
                    <Badge tone={row.type === "Vulnerability" ? "crit" : "neutral"}>
                      {row.type}
                    </Badge>
                  </td>
                  <td className="t-name" style={{ fontSize: ".8rem" }}>
                    {row.entity}
                  </td>
                  <td className="t-mut mono" style={{ fontSize: ".76rem" }}>
                    {row.source}
                  </td>
                  <td>
                    <TruthChip level={row.truth} />
                  </td>
                  <td>
                    <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                      {row.actions.map((action) => (
                        <Link key={action.label} className="btn-ghost" to={action.to}>
                          {action.label}
                        </Link>
                      ))}
                    </div>
                  </td>
                </tr>
              ))}
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={6} className="empty">
                    {partialFailure
                      ? "No findings from the available source."
                      : "No findings from this source."}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </AsyncStateGuard>
      </Panel>
    </div>
  );
}

interface WorklistAction {
  readonly label: string;
  readonly to: string;
}

interface WorklistRow {
  readonly id: string;
  readonly type: string;
  readonly entity: string;
  readonly title: string;
  readonly detail: string;
  readonly source: string;
  readonly truth: "exact" | "derived" | "inferred";
  readonly actions: readonly WorklistAction[];
}

function worklistRows(model: ConsoleModel): readonly WorklistRow[] {
  return [...model.findings.map(findingRow), ...model.vulnerabilities.map(vulnerabilityRow)];
}

function findingRow(finding: FindingRow): WorklistRow {
  const actions: WorklistAction[] = [];
  if (finding.type === "Dead code") {
    actions.push({
      label: "Open graph",
      to: `/code-graph?candidate=${encodeURIComponent(finding.id)}`,
    });
    if (finding.filePath)
      actions.push({
        label: "Filter dead code",
        to: `/dead-code?q=${encodeURIComponent(finding.filePath)}`,
      });
  }
  if (finding.entity)
    actions.push({
      label: "Explore entity",
      to: `/explorer?q=${encodeURIComponent(finding.entity)}`,
    });
  return {
    actions,
    detail: finding.detail,
    entity: finding.entity,
    id: finding.id,
    source: findingSource(finding),
    title: finding.title,
    truth: uiTruth(finding.truth),
    type: finding.type,
  };
}

function vulnerabilityRow(vulnerability: VulnRow): WorklistRow {
  const primaryEntity = vulnerability.services[0] ?? "service not resolved";
  const detail = `${vulnerability.package} · CVSS ${vulnerability.cvss}${vulnerability.fixedVersion ? ` · fix ${vulnerability.fixedVersion}` : ""}`;
  const actions: WorklistAction[] = [
    { label: "Open CVE", to: `/vulnerabilities/${encodeURIComponent(vulnerability.id)}` },
  ];
  if (primaryEntity !== "service not resolved") {
    actions.push({
      label: "Explore entity",
      to: `/explorer?q=${encodeURIComponent(primaryEntity)}`,
    });
  }
  return {
    actions,
    detail,
    entity: primaryEntity,
    id: `vulnerability:${vulnerability.id}`,
    source: "GET /api/v0/supply-chain/impact/findings",
    title: `${vulnerability.id} · ${vulnerability.package}`,
    truth: "derived",
    type: "Vulnerability",
  };
}

function findingSource(finding: FindingRow): string {
  if (finding.type === "Dead code") return "POST /api/v0/code/dead-code";
  if (finding.type === "Vulnerability") return "GET /api/v0/supply-chain/impact/findings";
  return "live graph finding row";
}

// combinedWorklist derives a single AsyncStateGuard provenance for the unified
// worklist. The table must stay visible whenever at least one source has
// responded — partial failure (one source unavailable, the other live) must
// not suppress rows that are already present.
//
// Priority:
//   • Any source still loading  → "loading"  (both in flight; nothing to show)
//   • All sources unavailable   → "unavailable"
//   • At least one source ready → "live" (table renders; per-section empty
//     rows explain any absent section)
function combinedWorklist(a: SectionProvenance, b: SectionProvenance): SectionProvenance {
  if (a === "unavailable" && b === "unavailable") return "unavailable";
  if (isReady(a) || isReady(b)) return "live";
  if (a === "loading" || b === "loading") return "loading";
  return "live";
}

function partialFailureMessage(a: SectionProvenance, b: SectionProvenance): string {
  if (a === "unavailable" && isReady(b))
    return "Dead-code findings are unavailable; reachable vulnerability results remain visible.";
  if (b === "unavailable" && isReady(a))
    return "Reachable vulnerability findings are unavailable; dead-code results remain visible.";
  return "";
}

function partialLoadingMessage(a: SectionProvenance, b: SectionProvenance): string {
  if (a === "loading" && isReady(b))
    return "Dead-code findings are still loading; available vulnerability results remain visible.";
  if (b === "loading" && isReady(a))
    return "Reachable vulnerability findings are still loading; available dead-code results remain visible.";
  return "";
}

function isReady(provenance: SectionProvenance): boolean {
  return provenance === "live" || provenance === "empty" || provenance === "demo";
}
