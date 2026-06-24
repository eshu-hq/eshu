import { useEffect, useRef } from "react";
import { Link } from "react-router-dom";

import { Badge, FreshDot, TruthChip } from "./atoms";
import type { AnswerEvidenceHandle, AnswerNextCall } from "../api/answerPacket";
import { buildSourceCitationHref } from "../api/answerPacket";
import type { VisualizationEdge, VisualizationNode, VisualizationPacket } from "../api/answerVisualization";
import type { EshuTruth } from "../api/envelope";
import { uiFresh, uiTruth } from "../console/types";
import "./evidenceDrawer.css";

// EvidenceSelection identifies the visualization element the operator is
// inspecting. It is the shared selection contract between the graph page and
// the drawer.
export type EvidenceSelection =
  | { readonly kind: "node"; readonly id: string }
  | { readonly kind: "edge"; readonly id: string };

// Truth labels the console maps onto a colored chip. Any other label (e.g.
// "ambiguous" from an incident-context packet) is rendered literally so the
// drawer never silently normalizes away an uncertainty signal it does not model.
const KNOWN_TRUTH_LABELS = new Set(["exact", "derived", "fallback"]);

// EvidenceDrawer renders the truth basis, provenance, source handle, freshness,
// limitations, and recommended next calls for a selected node or edge. Every row
// is driven by the visualization packet; absent optional fields render an
// explicit "not provided" state rather than collapsing the drawer or hiding
// uncertainty. It returns null when the selected id is not in the packet.
export function EvidenceDrawer({
  onClose,
  packet,
  selection
}: {
  readonly onClose: () => void;
  readonly packet: VisualizationPacket;
  readonly selection: EvidenceSelection;
}): React.JSX.Element | null {
  const closeRef = useRef<HTMLButtonElement>(null);
  const drawerRef = useRef<HTMLElement>(null);

  // Focus the close control once on open. Kept separate from the key handler so
  // a parent re-render (new onClose identity, busy/model updates) cannot snatch
  // focus back from an operator who has tabbed into the drawer body.
  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  useEffect(() => {
    function onKey(event: globalThis.KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const node = selection.kind === "node"
    ? packet.nodes.find((row) => row.id === selection.id)
    : undefined;
  const edge = selection.kind === "edge"
    ? packet.edges.find((row) => row.id === selection.id)
    : undefined;
  if (node === undefined && edge === undefined) {
    return null;
  }

  // Trap Tab focus inside the modal drawer. Without this, tabbing past the last
  // control moves focus to the page behind the scrim while aria-modal tells
  // assistive tech the background is unavailable, so Enter could activate a
  // hidden control. Cycle focus across the drawer's own focusable elements.
  function trapFocus(event: React.KeyboardEvent): void {
    if (event.key !== "Tab") {
      return;
    }
    const root = drawerRef.current;
    if (root === null) {
      return;
    }
    const focusables = root.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])'
    );
    if (focusables.length === 0) {
      return;
    }
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = root.ownerDocument.activeElement;
    if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  const title = node !== undefined
    ? node.label || node.id
    : edge?.relationship || "relationship";
  const truthLabel = node !== undefined ? node.truthLabel : edge?.truthLabel ?? "";
  const handle = node !== undefined ? node.evidenceHandle : edge?.evidenceHandle ?? null;

  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside
        ref={drawerRef}
        className="drawer evidence-drawer"
        role="dialog"
        aria-modal="true"
        aria-label={`Evidence for ${title}`}
        onKeyDown={trapFocus}
      >
        <div className="drawer-head">
          <div>
            <span className="entity-kind">{node !== undefined ? "Node evidence" : "Edge evidence"}</span>
            <h3>{title}</h3>
          </div>
          <button ref={closeRef} className="drawer-close" onClick={onClose} aria-label="Close" type="button">✕</button>
        </div>
        <div className="drawer-body">
          <TruthSection label={truthLabel} truth={packet.truth} />
          {node !== undefined ? <NodeFacts node={node} /> : null}
          {edge !== undefined ? <EdgeFacts edge={edge} packet={packet} /> : null}
          <EvidenceHandleSection handle={handle} />
          <StringList title="Limitations" values={packet.limitations} />
          <NextCallList calls={packet.recommendedNextCalls} />
        </div>
      </aside>
    </>
  );
}

function TruthSection({ label, truth }: { readonly label: string; readonly truth: EshuTruth | null }): React.JSX.Element {
  return (
    <section className="evd-section">
      <h4>Truth</h4>
      <div className="evd-truth-row">
        {label.length === 0 ? (
          <span className="evd-muted">truth label not provided</span>
        ) : KNOWN_TRUTH_LABELS.has(label) ? (
          <TruthChip level={uiTruth(label)} />
        ) : (
          <Badge tone="warn">{label}</Badge>
        )}
      </div>
      {truth === null ? (
        <p className="evd-muted">packet truth unavailable</p>
      ) : (
        <dl className="evd-meta">
          {truth.basis !== undefined && truth.basis.length > 0 ? <Row label="Basis" value={truth.basis} /> : null}
          <div className="evd-row"><dt>Level</dt><dd><TruthChip level={uiTruth(truth.level)} /></dd></div>
          <div className="evd-row"><dt>Freshness</dt><dd><FreshDot state={uiFresh(truth.freshness.state)} /></dd></div>
          {truth.reason !== undefined && truth.reason.length > 0 ? <Row label="Reason" value={truth.reason} /> : null}
        </dl>
      )}
    </section>
  );
}

function NodeFacts({ node }: { readonly node: VisualizationNode }): React.JSX.Element {
  return (
    <section className="evd-section">
      <h4>Node</h4>
      <dl className="evd-meta">
        <Row label="Type" value={node.type} />
        <Row label="Category" value={node.category} />
      </dl>
    </section>
  );
}

function EdgeFacts({ edge, packet }: { readonly edge: VisualizationEdge; readonly packet: VisualizationPacket }): React.JSX.Element {
  return (
    <section className="evd-section">
      <h4>Endpoints</h4>
      <dl className="evd-meta">
        <Row label="From" value={nodeLabel(packet, edge.source)} />
        <Row label="To" value={nodeLabel(packet, edge.target)} />
      </dl>
    </section>
  );
}

function EvidenceHandleSection({ handle }: { readonly handle: AnswerEvidenceHandle | null }): React.JSX.Element {
  if (handle === null) {
    return (
      <section className="evd-section">
        <h4>Evidence handle</h4>
        <p className="evd-muted">No evidence handle returned for this selection.</p>
      </section>
    );
  }
  const href = handle.repoId !== undefined && handle.relativePath !== undefined
    ? buildSourceCitationHref(handle)
    : undefined;
  const notProvided: string[] = [];
  if (handle.relativePath === undefined) notProvided.push("source path");
  if (handle.startLine === undefined) notProvided.push("line range");
  if (handle.evidenceFamily.length === 0) notProvided.push("evidence family");
  if (handle.entityId === undefined && handle.relativePath === undefined) notProvided.push("entity id");
  return (
    <section className="evd-section">
      <h4>Evidence handle</h4>
      <dl className="evd-meta">
        <Row label="Kind" value={handle.kind} />
        <Row label="Repository" value={handle.repoId ?? ""} />
        <Row label="Path" value={handle.relativePath ?? ""} />
        <Row label="Entity" value={handle.entityId ?? ""} />
        <Row label="Family" value={handle.evidenceFamily} />
        <Row label="Reason" value={handle.reason} />
        <Row label="Lines" value={lineRange(handle)} />
      </dl>
      {href !== undefined ? <Link className="link-btn mono" to={href}>Open source</Link> : null}
      {notProvided.length > 0 ? <p className="evd-muted">Not provided: {notProvided.join(", ")}</p> : null}
    </section>
  );
}

function StringList({ title, values }: { readonly title: string; readonly values: readonly string[] }): React.JSX.Element | null {
  const unique = [...new Set(values.map((value) => value.trim()).filter((value) => value.length > 0))];
  if (unique.length === 0) {
    return null;
  }
  return (
    <section className="evd-section">
      <h4>{title}</h4>
      <ul className="evd-list">{unique.map((value) => <li key={value}>{value}</li>)}</ul>
    </section>
  );
}

function NextCallList({ calls }: { readonly calls: readonly AnswerNextCall[] }): React.JSX.Element | null {
  if (calls.length === 0) {
    return null;
  }
  return (
    <section className="evd-section">
      <h4>Recommended next calls</h4>
      <ul className="evd-list">
        {calls.map((call, index) => (
          <li key={`${call.tool}-${call.route ?? ""}-${index}`}>
            <span className="mono">{call.tool || call.route}</span>
            {call.reason.length > 0 ? <span className="evd-reason">{call.reason}</span> : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function Row({ label, value }: { readonly label: string; readonly value: string }): React.JSX.Element | null {
  if (value.trim().length === 0) {
    return null;
  }
  return (
    <div className="evd-row">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function nodeLabel(packet: VisualizationPacket, id: string): string {
  return packet.nodes.find((node) => node.id === id)?.label || id;
}

function lineRange(handle: AnswerEvidenceHandle): string {
  if (handle.startLine === undefined) {
    return "";
  }
  return handle.endLine !== undefined && handle.endLine !== handle.startLine
    ? `${handle.startLine}-${handle.endLine}`
    : String(handle.startLine);
}
