import { Badge } from "./atoms";
import type { InvestigationEvidencePacket } from "../api/investigationPacket";
import "./answerRenderer.css";

interface InvestigationEvidencePacketReaderProps {
  readonly packet: InvestigationEvidencePacket;
}

interface PacketRow {
  readonly detail: string;
  readonly key: string;
  readonly label: string;
}

export function InvestigationEvidencePacketReader({
  packet
}: InvestigationEvidencePacketReaderProps): React.JSX.Element {
  const supported = packet.answer.supported === true;
  const truncated = packet.bounds.truncated === true;
  const redactionProfile = textField(packet.redaction, "profile") || textField(packet.redaction, "redaction_profile");

  return (
    <section className="evidence-packet-reader" aria-label="Investigation evidence packet">
      <div className="evidence-packet-reader-head">
        <div>
          <span className="entity-kind">{packet.schema || "investigation_evidence_packet.v2"}</span>
          <h3>Investigation packet</h3>
          <p className="mono t-mut">{packet.packetId}</p>
        </div>
        <div className="evidence-packet-reader-action">
          {supported ? <Badge tone="teal">supported</Badge> : <Badge tone="warn">partial</Badge>}
          {truncated ? <Badge tone="warn">truncated</Badge> : null}
        </div>
      </div>

      <div className="evidence-packet-reader-grid">
        <PacketSummary packet={packet} redactionProfile={redactionProfile} />
        <PacketObjectPanel title="Packet bounds" value={packet.bounds} />
        <PacketList title="Source facts" empty="No source facts returned." rows={packet.sourceFacts} />
        <PacketList title="Reducer decisions" empty="No reducer decisions returned." rows={packet.reducerDecisions} />
        <PacketList title="Graph answers" empty="No graph answers returned." rows={packet.graphAnswers} />
        <PacketList title="Missing evidence" empty="No missing evidence returned." rows={packet.missingEvidence} />
        <PacketList title="Semantic observations" empty="No semantic observations returned." rows={packet.semanticObservations} />
        <PacketList title="Reproduce handles" empty="No reproduce handles returned." rows={packet.reproduce} />
        <PacketObjectPanel title="Redaction and validation" value={{ ...packet.redaction, ...packet.validation }} />
      </div>
    </section>
  );
}

function PacketSummary({
  packet,
  redactionProfile
}: {
  readonly packet: InvestigationEvidencePacket;
  readonly redactionProfile: string;
}): React.JSX.Element {
  const family = textField(packet.identity, "family");
  const summary = textField(packet.answer, "summary");
  const truthClass = textField(packet.answer, "truth_class") || textField(packet.answer, "truthClass");
  return (
    <section className="evidence-packet-panel" aria-label="Packet answer">
      <h4>Packet answer</h4>
      <div className="evidence-packet-tags">
        {family.length > 0 ? <span>{family}</span> : null}
        {truthClass.length > 0 ? <span>{truthClass}</span> : null}
        {packet.refusal.length > 0 && packet.refusal !== "none" ? <Badge tone="warn">{packet.refusal}</Badge> : null}
        {redactionProfile.length > 0 ? <span>{redactionProfile}</span> : null}
      </div>
      {summary.length > 0 ? <p>{summary}</p> : <p className="empty">No packet summary returned.</p>}
    </section>
  );
}

function PacketObjectPanel({
  title,
  value
}: {
  readonly title: string;
  readonly value: Record<string, unknown>;
}): React.JSX.Element {
  const rows = Object.entries(value)
    .filter(([, raw]) => raw !== undefined && raw !== null && renderedValue(raw).length > 0)
    .slice(0, 8);
  return (
    <section className="evidence-packet-panel" aria-label={title}>
      <h4>{title}</h4>
      {rows.length === 0 ? (
        <p className="empty">No {title.toLowerCase()} returned.</p>
      ) : (
        <div className="evidence-packet-list">
          {rows.map(([key, raw]) => (
            <article key={key}>
              <strong>{key}</strong>
              <small>{renderedValue(raw)}</small>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function PacketList({
  empty,
  rows,
  title
}: {
  readonly empty: string;
  readonly rows: readonly Record<string, unknown>[];
  readonly title: string;
}): React.JSX.Element {
  const items = rows.map((row, index) => packetRow(row, index));
  return (
    <section className="evidence-packet-panel" aria-label={title}>
      <h4>{title}</h4>
      {items.length === 0 ? (
        <p className="empty">{empty}</p>
      ) : (
        <div className="evidence-packet-list">
          {items.map((item) => (
            <article key={item.key}>
              <strong>{item.label}</strong>
              {item.detail.length > 0 ? <small>{item.detail}</small> : null}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function packetRow(row: Record<string, unknown>, index: number): PacketRow {
  const label = firstTextField(row, [
    "fact_id", "stable_key", "id", "decision_id", "edge_id", "hop", "route", "tool", "command", "kind"
  ]) || `row ${index + 1}`;
  const detail = Object.entries(row)
    .filter(([key]) => key !== "payload" && key !== "raw_payload")
    .filter(([key, raw]) => key !== label && renderedValue(raw).length > 0)
    .slice(0, 5)
    .map(([key, raw]) => `${key}: ${renderedValue(raw)}`)
    .join(" · ");
  return {
    detail,
    key: `${label}:${index}`,
    label
  };
}

function firstTextField(row: Record<string, unknown>, keys: readonly string[]): string {
  for (const key of keys) {
    const value = textField(row, key);
    if (value.length > 0) return value;
  }
  return "";
}

function textField(row: Record<string, unknown>, key: string): string {
  const value = row[key];
  return typeof value === "string" ? value : "";
}

function renderedValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value)) {
    return value.map(renderedValue).filter((item) => item.length > 0).join(", ");
  }
  if (typeof value === "object" && value !== null) {
    return JSON.stringify(value);
  }
  return "";
}
