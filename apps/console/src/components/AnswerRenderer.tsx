import type { GraphModel } from "../console/types";
import { uiFresh, uiTruth } from "../console/types";
import type {
  AnswerCompanion,
  AnswerEvidenceHandle,
  EvidenceCitation,
  EvidenceCitationPacket
} from "../api/answerPacket";
import {
  buildSourceCitationHref,
  sourceCitationLabel
} from "../api/answerPacket";
import { emptyAnswerGraph, type VisualizationPacket } from "../api/answerVisualization";
import { Badge, FreshDot, TruthChip } from "./atoms";
import { EvidencePacketReader } from "./EvidencePacketReader";
import { GraphCanvas } from "./GraphCanvas";
import "./answerRenderer.css";

interface AnswerRendererProps {
  readonly answer: AnswerCompanion;
  readonly citationPacket?: EvidenceCitationPacket | null;
  readonly graph?: GraphModel;
  readonly title?: string;
  readonly visualizationPacket?: VisualizationPacket | null;
}

interface EvidenceItem {
  readonly href?: string;
  readonly key: string;
  readonly label: string;
  readonly meta: string;
}

export function AnswerRenderer({
  answer,
  citationPacket = null,
  graph,
  title = "Answer",
  visualizationPacket = null
}: AnswerRendererProps): React.JSX.Element {
  const displayGraph = graph ?? emptyAnswerGraph();
  const evidenceItems = answerEvidenceItems(answer, citationPacket);
  const statusLabel = answerStatusLabel(answer);
  const limitations = [
    ...answer.limitations,
    ...(citationPacket?.coverage.truncated ? ["citation packet truncated"] : []),
    ...(visualizationPacket?.limitations ?? [])
  ];
  const missingEvidence = [
    ...answer.missingEvidence,
    ...(citationPacket?.missingHandles.map(sourceCitationLabel) ?? [])
  ];

  return (
    <section className="answer-renderer" aria-label={title}>
      <div className="answer-renderer-header">
        <div>
          <span className="entity-kind">{statusLabel}</span>
          <h3>{title}</h3>
        </div>
        <div className="answer-renderer-truth">
          {answer.truth === null ? (
            <Badge tone="warn">truth unavailable</Badge>
          ) : (
            <>
              <span className="mono">{answer.truth.capability}</span>
              <TruthChip level={uiTruth(answer.truth.level)} />
              <FreshDot state={uiFresh(answer.truth.freshness.state)} />
            </>
          )}
        </div>
      </div>

      <div className="answer-renderer-main">
        <div className="answer-renderer-copy">
          {answer.supported ? (
            <p className="answer-renderer-summary">
              {answer.summary || "Supported answer returned without a summary."}
            </p>
          ) : (
            <div className="answer-renderer-state">
              <strong>{answer.status === "unsupported" ? "Insufficient evidence" : "Answer unavailable"}</strong>
              <p>{unsupportedSentence(answer)}</p>
            </div>
          )}
          <div className="answer-renderer-badges">
            <Badge tone={answer.supported ? "teal" : "warn"}>{answer.truthClass || "unsupported"} answer</Badge>
            {answer.partial ? <Badge tone="warn">partial</Badge> : null}
            {answer.truncated ? <Badge tone="warn">truncated</Badge> : null}
            {answer.primaryRoute.length > 0 ? <span className="mono">{answer.primaryRoute}</span> : null}
          </div>
        </div>

        <div className="answer-renderer-grid">
          <EvidenceList evidenceItems={evidenceItems} />
          <StateList title="Missing evidence" values={missingEvidence} />
          <StateList title="Limitations" values={limitations} />
          <StateList title="Unsupported reasons" values={answer.unsupportedReasons} />
        </div>
      </div>

      {visualizationPacket !== null && !visualizationPacket.supported ? (
        <div className="answer-renderer-state">
          <strong>No renderable subgraph for this answer.</strong>
          <p>{visualizationPacket.limitations.join("; ") || "Visualization packet was unsupported."}</p>
        </div>
      ) : null}
      <EvidencePacketReader
        answer={answer}
        citationPacket={citationPacket}
        visualizationPacket={visualizationPacket}
      />
      <GraphCanvas graph={displayGraph} height={260} />
    </section>
  );
}

function EvidenceList({
  evidenceItems
}: {
  readonly evidenceItems: readonly EvidenceItem[];
}): React.JSX.Element {
  if (evidenceItems.length === 0) {
    return (
      <section className="answer-renderer-list">
        <h4>Evidence citations</h4>
        <p className="empty">No evidence handles returned.</p>
      </section>
    );
  }
  return (
    <section className="answer-renderer-list">
      <h4>Evidence citations</h4>
      {evidenceItems.map((item) => (
        <article key={item.key}>
          {item.href === undefined ? (
            <strong>{item.label}</strong>
          ) : (
            <a className="mono" href={item.href}>{item.label}</a>
          )}
          <small>{item.meta}</small>
        </article>
      ))}
    </section>
  );
}

function StateList({
  title,
  values
}: {
  readonly title: string;
  readonly values: readonly string[];
}): React.JSX.Element | null {
  const uniqueValues = unique(values);
  if (uniqueValues.length === 0) {
    return null;
  }
  return (
    <section className="answer-renderer-list">
      <h4>{title}</h4>
      {uniqueValues.map((value) => (
        <article key={value}>
          <strong>{value}</strong>
        </article>
      ))}
    </section>
  );
}

function answerEvidenceItems(
  answer: AnswerCompanion,
  citationPacket: EvidenceCitationPacket | null
): readonly EvidenceItem[] {
  const items = [
    ...(citationPacket?.citations.map(citationItem) ?? []),
    ...answer.evidenceHandles.map(handleItem)
  ];
  const seen = new Set<string>();
  return items.filter((item) => {
    const key = item.href ?? item.key;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function citationItem(citation: EvidenceCitation): EvidenceItem {
  const label = sourceCitationLabel(citation);
  return {
    href: citation.relativePath !== undefined && citation.repoId !== undefined
      ? buildSourceCitationHref(citation)
      : undefined,
    key: `citation:${citation.citationId || label}`,
    label,
    meta: [citation.entityName, citation.evidenceFamily, citation.reason]
      .filter((value) => value.length > 0)
      .join(" | ") || "resolved citation"
  };
}

function handleItem(handle: AnswerEvidenceHandle): EvidenceItem {
  const label = sourceCitationLabel(handle);
  return {
    href: handle.relativePath !== undefined && handle.repoId !== undefined
      ? buildSourceCitationHref(handle)
      : undefined,
    key: `handle:${label}:${handle.entityId ?? ""}`,
    label,
    meta: [handle.kind, handle.evidenceFamily, handle.reason]
      .filter((value) => value.length > 0)
      .join(" | ") || "evidence handle"
  };
}

function answerStatusLabel(answer: AnswerCompanion): string {
  if (answer.status === "partial") return "Partial answer";
  if (answer.status === "unsupported") return "Unsupported answer";
  if (answer.status === "unavailable") return "Answer unavailable";
  return "Supported answer";
}

function unsupportedSentence(answer: AnswerCompanion): string {
  if (answer.unsupportedReasons.length > 0) {
    return "The source route returned explicit unsupported reasons.";
  }
  if (answer.missingEvidence.length > 0) {
    return "The source route did not return enough evidence to support a summary.";
  }
  return "The source route did not return an answer packet.";
}

function unique(values: readonly string[]): readonly string[] {
  const seen = new Set<string>();
  return values
    .map((value) => value.trim())
    .filter((value) => {
      if (value.length === 0 || seen.has(value)) {
        return false;
      }
      seen.add(value);
      return true;
    });
}
