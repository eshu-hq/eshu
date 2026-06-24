import { Link } from "react-router-dom";

import type { EshuTruth } from "../api/envelope";
import {
  type IncidentContext,
  type IncidentEvidenceEdge,
  type IncidentMissingEvidence,
  type IncidentRecord,
  type IncidentRelatedChange,
  type IncidentTimelineEvent,
  type IncidentTruthLabel
} from "../api/incidentContext";
import { FreshDot, TruthChip } from "../components/atoms";
import { fmt, uiFresh, uiTruth } from "../console/types";

export function IncidentSummary({
  incident,
  onOpenService,
  truth
}: {
  readonly incident: IncidentRecord;
  readonly onOpenService?: (name: string) => void;
  readonly truth: EshuTruth | null;
}): React.JSX.Element {
  const serviceName = incident.service.summary || incident.service.id;
  return (
    <div className="incident-summary">
      <div>
        <div className="incident-title-row">
          <h3>{incident.title}</h3>
          <span className={`incident-truth-label incident-truth-${incident.status}`}>{incident.status}</span>
        </div>
        <div className="incident-meta">
          <span className="mono">{incident.providerIncidentId}</span>
          {incident.urgency ? <span>{incident.urgency}</span> : null}
          {incident.priority.summary ? <span>{incident.priority.summary}</span> : null}
          {incident.createdAt ? <span>{incident.createdAt}</span> : null}
        </div>
      </div>
      <TruthSummary truth={truth} />
      {serviceName ? (
        <div className="incident-actions">
          {onOpenService ? (
            <button className="btn-ghost active" type="button" onClick={() => onOpenService(serviceName)}>
              Open service
            </button>
          ) : null}
          {incident.service.id ? (
            <Link className="btn-ghost" to={`/workspace/services/${encodeURIComponent(incident.service.id)}`}>
              Workspace
            </Link>
          ) : null}
          <Link className="btn-ghost" to={`/impact?kind=service&target=${encodeURIComponent(serviceName)}`}>
            Impact
          </Link>
        </div>
      ) : null}
    </div>
  );
}

export function EvidencePath({ rows }: { readonly rows: readonly IncidentEvidenceEdge[] }): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">No evidence path returned.</p>;
  }
  return (
    <div className="incident-evidence-list">
      {rows.map((edge, index) => (
        <article key={`${edge.slot}:${index}`}>
          <div className="incident-edge-head">
            <strong>{formatSlot(edge.slot)}</strong>
            <TruthLabel label={edge.truthLabel} />
          </div>
          <p>{edge.explanation}</p>
          <KeyValueList values={edge.value} />
          {edge.evidence.length > 0 ? (
            <div className="incident-fact-row">
              {edge.evidence.slice(0, 3).map((evidence) => (
                <span key={`${evidence.factId}:${evidence.kind}:${evidence.source}`}>
                  {[evidence.source, evidence.kind, evidence.factId].filter(Boolean).join(" | ")}
                </span>
              ))}
            </div>
          ) : null}
        </article>
      ))}
    </div>
  );
}

export function MissingEvidence({ rows }: { readonly rows: readonly IncidentMissingEvidence[] }): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">No missing evidence reported.</p>;
  }
  return (
    <div className="incident-compact-list">
      {rows.map((row) => (
        <div key={`${row.slot}:${row.reason}`}>
          <strong>{formatSlot(row.slot)}</strong>
          <span>{row.reason}</span>
        </div>
      ))}
    </div>
  );
}

export function AmbiguousEvidence({ rows }: { readonly rows: readonly IncidentEvidenceEdge[] }): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">No ambiguity reported.</p>;
  }
  return (
    <div className="incident-evidence-list compact">
      {rows.map((edge, index) => (
        <article key={`${edge.slot}:${index}`}>
          <div className="incident-edge-head">
            <strong>{formatSlot(edge.slot)}</strong>
            <TruthLabel label={edge.truthLabel} />
          </div>
          <p>{edge.explanation}</p>
          <div className="incident-candidates">
            {edge.candidates.map((candidate) => (
              <span key={`${candidate.id}:${candidate.label}`}>{candidate.label}</span>
            ))}
          </div>
        </article>
      ))}
    </div>
  );
}

export function RelatedChanges({ rows }: { readonly rows: readonly IncidentRelatedChange[] }): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">No related changes returned.</p>;
  }
  return (
    <div className="incident-compact-list">
      {rows.map((row) => (
        <div key={row.changeId}>
          <strong>{row.summary}</strong>
          <span>{[row.source, row.timestamp, row.explanation].filter(Boolean).join(" | ")}</span>
        </div>
      ))}
    </div>
  );
}

export function Timeline({ rows }: { readonly rows: readonly IncidentTimelineEvent[] }): React.JSX.Element {
  if (rows.length === 0) {
    return <p className="empty">No timeline returned.</p>;
  }
  return (
    <div className="incident-compact-list">
      {rows.map((row) => (
        <div key={row.eventId}>
          <strong>{row.summary}</strong>
          <span>{[row.eventType, row.createdAt].filter(Boolean).join(" | ")}</span>
        </div>
      ))}
    </div>
  );
}

export function statRows(context: IncidentContext | null): readonly {
  readonly color: string;
  readonly label: string;
  readonly sub: string;
  readonly value: string | number;
}[] {
  if (context === null) {
    return [
      { color: "var(--teal)", label: "Evidence slots", sub: "not loaded", value: "-" },
      { color: "var(--ember)", label: "Missing", sub: "not loaded", value: "-" },
      { color: "var(--violet)", label: "Ambiguous", sub: "not loaded", value: "-" },
      { color: "var(--blue)", label: "Changes", sub: "not loaded", value: "-" }
    ];
  }
  return [
    { color: "var(--teal)", label: "Evidence slots", sub: context.truncated ? "truncated" : "bounded", value: fmt(context.evidencePath.length) },
    { color: "var(--ember)", label: "Missing", sub: "explicit gaps", value: fmt(context.missingEvidence.length) },
    { color: "var(--violet)", label: "Ambiguous", sub: "candidate slots", value: fmt(context.ambiguousEvidence.length) },
    { color: "var(--blue)", label: "Changes", sub: "fallback candidates", value: fmt(context.relatedChanges.length) }
  ];
}

function TruthSummary({ truth }: { readonly truth: EshuTruth | null }): React.JSX.Element {
  if (truth === null) {
    return <span className="t-mut">truth envelope unavailable</span>;
  }
  return (
    <span className="incident-truth-summary">
      <span className="mono">{truth.capability}</span>
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness.state)} />
    </span>
  );
}

function KeyValueList({ values }: { readonly values: Record<string, string> }): React.JSX.Element | null {
  const entries = Object.entries(values).filter(([, value]) => value.length > 0);
  if (entries.length === 0) {
    return null;
  }
  return (
    <dl className="incident-kv">
      {entries.slice(0, 4).map(([key, value]) => (
        <div key={key}>
          <dt>{formatSlot(key)}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function TruthLabel({ label }: { readonly label: IncidentTruthLabel }): React.JSX.Element {
  return (
    <span className={`incident-truth-label incident-truth-${label}`}>
      <i aria-hidden />
      <span>{label.replace(/_/g, " ")}</span>
    </span>
  );
}

function formatSlot(slot: string): string {
  return slot.replace(/_/g, " ");
}
