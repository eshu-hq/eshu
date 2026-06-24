// EvidenceList.tsx — the collapsible "Evidence" expander for an answer.
//
// Evidence handles are loosely typed on the wire, so each row renders only the
// bounded fields the console understands (kind + a human label derived from the
// handle). Handles are informational here; the console does not fabricate
// navigation targets from an unknown ref shape.
import { ChevronRight, ShieldCheck } from "lucide-react";
import { useState } from "react";

import { cx } from "./cx";
import type { AskEvidenceHandle } from "../../api/askEshu";

/** A collapsible list of evidence handles backing the answer. */
export function EvidenceList({ handles }: { readonly handles: readonly AskEvidenceHandle[] }): React.JSX.Element | null {
  const [open, setOpen] = useState(false);
  if (handles.length === 0) {
    return null;
  }
  return (
    <div className={cx("evidence-box", open && "is-open")}>
      <button aria-expanded={open} className="evidence-toggle" onClick={() => setOpen((value) => !value)} type="button">
        <ShieldCheck aria-hidden size={14} /> Evidence <span className="evidence-count">{handles.length}</span>
        <ChevronRight aria-hidden className="evidence-caret" size={14} />
      </button>
      {open ? (
        <ul className="evidence-list">
          {handles.map((handle, index) => (
            <li key={index}>
              <div className="evidence-item">
                <span className="evidence-kind">{handleKind(handle)}</span>
                <span className="evidence-label mono">{handleLabel(handle)}</span>
              </div>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function handleKind(handle: AskEvidenceHandle): string {
  return typeof handle.kind === "string" && handle.kind.length > 0 ? handle.kind : "evidence";
}

function handleLabel(handle: AskEvidenceHandle): string {
  if (typeof handle.label === "string" && handle.label.length > 0) {
    return handle.label;
  }
  if (typeof handle.relative_path === "string" && handle.relative_path.length > 0) {
    return typeof handle.start_line === "number"
      ? `${handle.relative_path}:${handle.start_line}`
      : handle.relative_path;
  }
  if (typeof handle.ref === "string" && handle.ref.length > 0) {
    return handle.ref;
  }
  return "Evidence handle";
}
