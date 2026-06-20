import type { CollectorReadinessRow, CollectorReadinessState } from "../api/collectorReadiness";
import type { SectionProvenance } from "../api/eshuConsoleLive";
import { Badge, CollectorGlyph, Panel, StatTile } from "../components/atoms";
import "./collectorReadinessPage.css";

interface CollectorReadinessPageProps {
  readonly provenance: SectionProvenance;
  readonly rows: readonly CollectorReadinessRow[];
}

const STATE_TONE: Record<CollectorReadinessState, "crit" | "neutral" | "teal" | "warn"> = {
  disabled: "neutral",
  failed: "crit",
  gated: "warn",
  implemented: "teal",
  partial: "warn",
  permission_hidden: "neutral",
  stale: "warn",
  unsupported: "neutral"
};

export function CollectorReadinessPage({ provenance, rows }: CollectorReadinessPageProps): React.JSX.Element {
  const groups = groupedRows(rows);
  const implemented = rows.filter((row) => row.state === "implemented").length;
  const blocked = rows.filter((row) => ["failed", "gated", "partial", "stale"].includes(row.state)).length;
  const hidden = rows.filter((row) => row.state === "permission_hidden").length;
  const unsupported = rows.filter((row) => row.state === "unsupported").length;
  return (
    <div className="page collector-readiness-page">
      <div className="page-intro">
        <h2>Collector Readiness</h2>
        <p>
          Promotion proof from <span className="mono">GET /api/v0/status/collector-readiness</span>.
          The API enumerates the generated readiness catalog, so missing families stay visible.
        </p>
      </div>
      <div className="grid g-4">
        <StatTile label="Implemented" value={implemented} color="var(--teal)" sub="promotion contract met" />
        <StatTile label="Blocked" value={blocked} color="var(--ember)" sub="partial, gated, stale, failed" />
        <StatTile label="Permission hidden" value={hidden} color="var(--muted)" sub="redacted by scope" />
        <StatTile label="Unsupported" value={unsupported} color="var(--subtle)" sub="no configured instance" />
      </div>
      <Panel className="mt" title="Readiness source" sub={`snapshot ${provenance}`}>
        <p className="empty" style={{ padding: "4px 0", textAlign: "left" }}>
          Each row carries last proof, queue/fact/reducer/API/MCP evidence, and the current blocking gate. Permission-hidden and unsupported states are separate so an absent collector is not confused with redacted evidence.
        </p>
      </Panel>
      {groups.map(([family, familyRows]) => (
        <section aria-label={family} className="collector-readiness-family mt" key={family}>
          <Panel className="flush" title={family} sub={`${familyRows.length} collectors`}>
            <div className="collector-readiness-scroll">
              <table className="tbl collector-readiness-table">
                <thead>
                  <tr><th>Collector</th><th>State</th><th>Last proof</th><th>Evidence</th><th>Blocking gate</th></tr>
                </thead>
                <tbody>
                  {familyRows.map((row) => (
                    <tr key={`${row.kind}:${row.instanceId}`}>
                      <td>
                        <span className="collector-readiness-collector">
                          <CollectorGlyph kind={row.kind} />
                          <span><strong>{row.displayName}</strong><small className="mono">{row.instanceId || row.kind}</small></span>
                        </span>
                      </td>
                      <td><Badge tone={STATE_TONE[row.state]}>{row.stateLabel}</Badge></td>
                      <td className="mono">{row.lastProof}</td>
                      <td><EvidenceList evidence={row.evidence} readback={row.reducerReadback} /></td>
                      <td><BlockingGate row={row} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Panel>
        </section>
      ))}
      {rows.length === 0 ? <Panel className="mt"><p className="empty">No collector readiness rows from this source.</p></Panel> : null}
    </div>
  );
}

function EvidenceList({
  evidence,
  readback
}: {
  readonly evidence: readonly string[];
  readonly readback: string;
}): React.JSX.Element {
  const labels = evidence.length > 0 ? evidence : ["no evidence"];
  return (
    <span className="collector-readiness-evidence">
      {labels.map((label) => <Badge key={label} tone={label === "no evidence" ? "neutral" : "teal"}>{label}</Badge>)}
      <small className="mono">{readback}</small>
    </span>
  );
}

function BlockingGate({ row }: { readonly row: CollectorReadinessRow }): React.JSX.Element {
  return (
    <span className="collector-readiness-blocker">
      <strong>{row.blockingGate}</strong>
      <small>{row.claimDriven ? `${row.claimState} / ${row.sourceScope}` : `direct / ${row.sourceScope}`}</small>
    </span>
  );
}

function groupedRows(rows: readonly CollectorReadinessRow[]): readonly (readonly [string, readonly CollectorReadinessRow[]])[] {
  const groups = new Map<string, CollectorReadinessRow[]>();
  for (const row of rows) {
    const group = groups.get(row.family) ?? [];
    group.push(row);
    groups.set(row.family, group);
  }
  return [...groups.entries()].map(([family, familyRows]) => [
    family,
    familyRows.slice().sort((a, b) => stateRank(a.state) - stateRank(b.state) || a.displayName.localeCompare(b.displayName))
  ] as const);
}

function stateRank(state: CollectorReadinessState): number {
  if (state === "failed") return 0;
  if (state === "partial" || state === "stale") return 1;
  if (state === "gated") return 2;
  if (state === "implemented") return 3;
  if (state === "permission_hidden") return 4;
  if (state === "unsupported") return 5;
  return 6;
}
