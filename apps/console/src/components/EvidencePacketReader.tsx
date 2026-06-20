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
import type { VisualizationPacket } from "../api/answerVisualization";
import type { EshuTruth } from "../api/envelope";
import { uiFresh, uiTruth } from "../console/types";
import { Badge, FreshDot, TruthChip } from "./atoms";

interface EvidencePacketReaderProps {
  readonly answer: AnswerCompanion;
  readonly citationPacket?: EvidenceCitationPacket | null;
  readonly visualizationPacket?: VisualizationPacket | null;
}

interface PacketItem {
  readonly detail: string;
  readonly href?: string;
  readonly key: string;
  readonly label: string;
}

type PacketItemKind = "reducer" | "semantic" | "source";

/** Renders answer, citation, and visualization packets as grouped proof rather than raw rows. */
export function EvidencePacketReader({
  answer,
  citationPacket = null,
  visualizationPacket = null
}: EvidencePacketReaderProps): React.JSX.Element {
  const citationItems = (citationPacket?.citations ?? []).map(citationPacketItem);
  const handleItems = answer.evidenceHandles.map(handlePacketItem);
  const sourceFacts = uniqueItems([
    ...citationItems.filter((item) => item.kind === "source").map((item) => item.item),
    ...handleItems.filter((item) => item.kind === "source").map((item) => item.item)
  ]);
  const reducerDecisions = uniqueItems(handleItems
    .filter((item) => item.kind === "reducer")
    .map((item) => item.item));
  const semanticLabels = uniqueItems(handleItems
    .filter((item) => item.kind === "semantic")
    .map((item) => item.item));
  const semanticNotes = answer.limitations.filter(isSemanticText);
  const missingEvidence = uniqueItems([
    ...answer.missingEvidence.map(stringPacketItem),
    ...(citationPacket?.missingHandles.map(handlePacketItem).map((item) => item.item) ?? [])
  ]);
  const hasBounds = citationPacket !== null || visualizationPacket !== null || answer.truncated || answer.partial;

  return (
    <section className="evidence-packet-reader" aria-label="Evidence packet reader">
      <div className="evidence-packet-reader-head">
        <div>
          <span className="entity-kind">Portable proof</span>
          <h3>Evidence packet reader</h3>
        </div>
        <PacketBounds
          citationPacket={citationPacket}
          partial={answer.partial}
          truncated={answer.truncated}
          visualizationPacket={visualizationPacket}
        />
      </div>

      <div className="evidence-packet-reader-grid">
        <TruthPanel truth={answer.truth} />
        {hasBounds ? (
          <BoundsPanel
            citationPacket={citationPacket}
            partial={answer.partial}
            truncated={answer.truncated}
            visualizationPacket={visualizationPacket}
          />
        ) : null}
        <PacketList title="Source facts" empty="No source facts returned." items={sourceFacts} />
        <PacketList title="Reducer decisions" empty="No reducer decisions returned." items={reducerDecisions} />
        <SemanticList items={semanticLabels} notes={semanticNotes} />
        <PacketList title="Missing evidence" empty="No missing evidence returned." items={missingEvidence} />
      </div>
    </section>
  );
}

function TruthPanel({ truth }: { readonly truth: EshuTruth | null }): React.JSX.Element {
  return (
    <section className="evidence-packet-panel" aria-label="Query truth and freshness">
      <h4>Query truth and freshness</h4>
      {truth === null ? (
        <p className="empty">Query truth unavailable.</p>
      ) : (
        <div className="evidence-packet-truth">
          <span className="mono">{truth.capability}</span>
          <TruthChip level={uiTruth(truth.level)} />
          <FreshDot state={uiFresh(truth.freshness.state)} />
          {truth.basis !== undefined && truth.basis.length > 0 ? <span>{truth.basis}</span> : null}
          {truth.reason !== undefined && truth.reason.length > 0 ? <small>{truth.reason}</small> : null}
        </div>
      )}
    </section>
  );
}

function BoundsPanel({
  citationPacket,
  partial,
  truncated,
  visualizationPacket
}: {
  readonly citationPacket: EvidenceCitationPacket | null;
  readonly partial: boolean;
  readonly truncated: boolean;
  readonly visualizationPacket: VisualizationPacket | null;
}): React.JSX.Element {
  const coverage = citationPacket?.coverage;
  const truncation = visualizationPacket?.truncation;
  const bounds = [
    coverage === undefined
      ? ""
      : `${coverage.resolvedCount} of ${coverage.inputHandleCount} citations resolved`,
    coverage === undefined || coverage.limit === 0 ? "" : `limit ${coverage.limit}`,
    coverage === undefined || coverage.missingCount === 0 ? "" : countLabel(coverage.missingCount, "missing handle"),
    truncation === undefined || truncation.droppedNodeCount === 0
      ? ""
      : `${countLabel(truncation.droppedNodeCount, "node")} dropped`,
    truncation === undefined || truncation.droppedEdgeCount === 0
      ? ""
      : `${countLabel(truncation.droppedEdgeCount, "edge")} dropped`
  ].filter((value) => value.length > 0);
  return (
    <section className="evidence-packet-panel" aria-label="Packet bounds">
      <h4>Packet bounds</h4>
      <div className="evidence-packet-tags">
        {partial ? <Badge tone="warn">partial packet</Badge> : null}
        {truncated || coverage?.truncated || truncation?.truncated ? <Badge tone="warn">truncated packet</Badge> : null}
        {bounds.map((value) => <span key={value}>{value}</span>)}
      </div>
    </section>
  );
}

function PacketBounds({
  citationPacket,
  partial,
  truncated,
  visualizationPacket
}: {
  readonly citationPacket: EvidenceCitationPacket | null;
  readonly partial: boolean;
  readonly truncated: boolean;
  readonly visualizationPacket: VisualizationPacket | null;
}): React.JSX.Element {
  const bounded = partial || truncated || citationPacket?.coverage.truncated ||
    citationPacketHasMissingHandles(citationPacket) || visualizationPacket?.truncation.truncated;
  return (
    <div className="evidence-packet-reader-action">
      {bounded ? <Badge tone="warn">bounded</Badge> : <Badge tone="teal">complete</Badge>}
    </div>
  );
}

function citationPacketHasMissingHandles(citationPacket: EvidenceCitationPacket | null): boolean {
  return (citationPacket?.coverage.missingCount ?? 0) > 0 || (citationPacket?.missingHandles.length ?? 0) > 0;
}

function countLabel(count: number, singular: string): string {
  return `${count} ${singular}${count === 1 ? "" : "s"}`;
}

function PacketList({
  empty,
  items,
  title
}: {
  readonly empty: string;
  readonly items: readonly PacketItem[];
  readonly title: string;
}): React.JSX.Element {
  return (
    <section className="evidence-packet-panel" aria-label={title}>
      <h4>{title}</h4>
      {items.length === 0 ? (
        <p className="empty">{empty}</p>
      ) : (
        <div className="evidence-packet-list">
          {items.map((item) => <PacketItemRow item={item} key={item.key} />)}
        </div>
      )}
    </section>
  );
}

function SemanticList({
  items,
  notes
}: {
  readonly items: readonly PacketItem[];
  readonly notes: readonly string[];
}): React.JSX.Element {
  const noteItems = notes.map(stringPacketItem);
  return (
    <section className="evidence-packet-panel" aria-label="Semantic labels">
      <h4>Semantic labels</h4>
      {items.length === 0 && noteItems.length === 0 ? (
        <p className="empty">No semantic labels returned.</p>
      ) : (
        <div className="evidence-packet-list">
          {[...items, ...noteItems].map((item) => <PacketItemRow item={item} key={item.key} />)}
        </div>
      )}
    </section>
  );
}

function PacketItemRow({ item }: { readonly item: PacketItem }): React.JSX.Element {
  const detail = item.detail === item.label ? "" : item.detail;
  return (
    <article>
      {item.href === undefined ? (
        <strong>{item.label}</strong>
      ) : (
        <a className="mono" href={item.href}>{item.label}</a>
      )}
      {detail.length > 0 ? <small>{detail}</small> : null}
    </article>
  );
}

function citationPacketItem(citation: EvidenceCitation): { readonly item: PacketItem; readonly kind: PacketItemKind } {
  const item = {
    detail: citation.reason,
    href: citation.repoId !== undefined && citation.relativePath !== undefined
      ? buildSourceCitationHref(citation)
      : undefined,
    key: `citation:${citation.citationId || sourceCitationLabel(citation)}`,
    label: sourceCitationLabel(citation)
  };
  return { item, kind: itemKind(citation.kind, citation.evidenceFamily, citation.reason) };
}

function handlePacketItem(handle: AnswerEvidenceHandle): { readonly item: PacketItem; readonly kind: PacketItemKind } {
  const kind = itemKind(handle.kind, handle.evidenceFamily, handle.reason);
  const label = kind === "reducer" ? reducerLabel(handle) : sourceCitationLabel(handle);
  const item = {
    detail: handle.reason,
    href: handle.repoId !== undefined && handle.relativePath !== undefined
      ? buildSourceCitationHref(handle)
      : undefined,
    key: `handle:${kind}:${label}:${handle.entityId ?? ""}`,
    label
  };
  return { item, kind };
}

function reducerLabel(handle: AnswerEvidenceHandle): string {
  const entityId = handle.entityId ?? "";
  const separator = entityId.indexOf(":");
  return separator >= 0 ? entityId.slice(separator + 1) : entityId || sourceCitationLabel(handle);
}

function itemKind(kind: string, family: string, reason: string): PacketItemKind {
  const text = `${kind} ${family} ${reason}`.toLowerCase();
  if (text.includes("reducer") || text.includes("decision") || text.includes("admission")) {
    return "reducer";
  }
  if (isSemanticText(text)) {
    return "semantic";
  }
  return "source";
}

function isSemanticText(value: string): boolean {
  const text = value.toLowerCase();
  return text.includes("semantic") || text.includes("provider") || text.includes("policy-gated");
}

function stringPacketItem(value: string): PacketItem {
  return {
    detail: "",
    key: `string:${value}`,
    label: value
  };
}

function uniqueItems(items: readonly PacketItem[]): readonly PacketItem[] {
  const seen = new Set<string>();
  return items.filter((item) => {
    const key = item.href ?? `${item.label}:${item.detail}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}
