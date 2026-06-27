import type {
  EvidencePacketComparison,
  EvidencePacketDeltaHop,
  EvidencePacketDeltaVerdict,
} from "../api/evidencePacketDelta";
import { Badge } from "../components/atoms";
import { fmt } from "../console/types";

/**
 * Renders current-vs-baseline evidence packet deltas from the changed-since
 * read model while keeping deleted, retracted, and stale hops visible.
 */
export function ChangedSincePacketComparison({
  comparison,
}: {
  readonly comparison: EvidencePacketComparison;
}): React.JSX.Element {
  return (
    <section className="changed-since-packet" aria-label="Evidence packet comparison">
      <div className="changed-since-packet-head">
        <div>
          <h3>Evidence packet comparison</h3>
          <p>{comparison.boundedSampleNote}</p>
        </div>
        <div className="changed-since-generation-meta">
          <span>
            <b>Baseline generation</b>
            <em className="mono">{comparison.baselineGenerationId}</em>
          </span>
          <span>
            <b>Current generation</b>
            <em className="mono">{comparison.currentGenerationId}</em>
          </span>
          <span>
            <b>Baseline observed</b>
            <em className="mono">{comparison.sinceObservedAt ?? "-"}</em>
          </span>
          <span>
            <b>Current observed</b>
            <em className="mono">{comparison.currentObservedAt ?? "-"}</em>
          </span>
        </div>
      </div>
      <div className="changed-since-packet-groups">
        {comparison.groups.map((group) => (
          <article className="changed-since-packet-group" key={group.category}>
            <header>
              <strong>{group.category}</strong>
              <small>{group.hops.length} bounded evidence hops</small>
            </header>
            <div className="changed-since-packet-counts">
              {packetVerdicts.map((verdict) => (
                <span key={verdict}>
                  {packetVerdictLabel(verdict)}
                  <b>{fmt(group.counts[verdict])}</b>
                </span>
              ))}
            </div>
            <div className="changed-since-packet-hops">
              {group.hops.map((hop) => (
                <PacketHop hop={hop} key={`${hop.verdict}:${hop.stableFactKey}:${hop.factKind}`} />
              ))}
              {group.hops.length === 0 ? (
                <span className="t-mut">no bounded sample hops</span>
              ) : null}
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function PacketHop({ hop }: { readonly hop: EvidencePacketDeltaHop }): React.JSX.Element {
  return (
    <span className="changed-since-packet-hop">
      <Badge tone={packetVerdictTone(hop.verdict)}>{hop.verdictLabel}</Badge>
      <span className="mono">{hop.stableFactKey}</span>
      <small>{hop.factKind}</small>
      {hop.truncated ? <em>truncated</em> : null}
    </span>
  );
}

const packetVerdicts: readonly EvidencePacketDeltaVerdict[] = [
  "added",
  "changed",
  "removed",
  "stale",
  "unchanged",
];

function packetVerdictLabel(verdict: EvidencePacketDeltaVerdict): string {
  if (verdict === "removed") return "Removed";
  if (verdict === "stale") return "Stale";
  return `${verdict.slice(0, 1).toUpperCase()}${verdict.slice(1)}`;
}

function packetVerdictTone(verdict: EvidencePacketDeltaVerdict): "neutral" | "teal" | "warn" {
  if (verdict === "added") return "teal";
  if (verdict === "removed" || verdict === "stale") return "warn";
  return "neutral";
}
