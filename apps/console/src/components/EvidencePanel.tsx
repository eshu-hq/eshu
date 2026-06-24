import { useEffect, useRef } from "react";
import { Link } from "react-router-dom";

import { Badge, FreshDot, TruthChip } from "./atoms";
import type { EshuTruth } from "../api/envelope";
import { uiFresh, uiTruth } from "../console/types";
import "./evidencePanel.css";

// EvidencePanelFact is one label/value row of joined evidence about the selected
// element. Empty values are dropped so the panel never renders a blank row.
export interface EvidencePanelFact {
  readonly label: string;
  readonly value: string;
}

// EvidencePanelSection is a titled group of fact rows. It lets a caller attach
// element-specific facts (e.g. provenance, endpoints, posture) without the panel
// needing to know any page's data shape.
export interface EvidencePanelSection {
  readonly title: string;
  readonly rows: readonly EvidencePanelFact[];
}

// EvidencePanelData is the packet-agnostic contract every clickable element maps
// its evidence into. It is intentionally decoupled from any one API shape (graph
// packet, service story, posture read model) so the same inline primitive backs
// the console-wide "everything clickable -> evidence" pattern, including future
// adopters. Optional fields render an explicit "not provided" state rather than
// collapsing the panel, so the UI never hides missing or uncertain evidence.
export interface EvidencePanelData {
  // kindLabel names what was selected, e.g. "Node evidence" or "Edge evidence".
  readonly kindLabel: string;
  // title is the primary identity of the element (entity label or relationship).
  readonly title: string;
  // truthLabel is the raw per-element truth signal. Known labels render as a
  // colored chip; unknown labels render literally to preserve uncertainty.
  readonly truthLabel: string;
  // truth is the envelope-level truth basis/level/freshness for the evidence
  // fetch, or null when the source did not return one.
  readonly truth: EshuTruth | null;
  // facts are the joined label/value rows shown in the default Facts section.
  readonly facts: readonly EvidencePanelFact[];
  // sections are additional titled fact groups for element-specific context.
  readonly sections?: readonly EvidencePanelSection[];
  // evidence is a free-form list of supporting evidence strings.
  readonly evidence?: readonly string[];
  // limitations are bounded-subset / coverage caveats to keep visible.
  readonly limitations?: readonly string[];
  // sourceHref deep-links into indexed source when the element ties to a file.
  readonly sourceHref?: string;
  // sourceLabel is the human-readable path/line shown next to the source link.
  readonly sourceLabel?: string;
}

// Truth labels the console maps onto a colored chip. Any other label is rendered
// literally so the panel never silently normalizes away an uncertainty signal it
// does not model. Kept in sync with EvidenceDrawer's known-label set.
const KNOWN_TRUTH_LABELS = new Set(["exact", "derived", "fallback"]);

// EvidencePanel is the reusable, inline evidence-panel primitive for the console.
// Any clickable element (graph node/edge, evidence-lane pill, stat tile, table
// row) maps its facts into EvidencePanelData and renders this panel to reveal the
// joined facts plus Truth (Exact/Derived/Inferred) and Freshness inline, without
// a modal scrim. It is keyboard-closable (Escape) and labels itself for assistive
// tech via the selected element's title.
export function EvidencePanel({
  data,
  onClose
}: {
  readonly data: EvidencePanelData;
  readonly onClose: () => void;
}): React.JSX.Element {
  const closeRef = useRef<HTMLButtonElement>(null);

  // Focus the close control once on open so keyboard users land inside the panel
  // when it appears. Kept separate from the key handler so a parent re-render
  // cannot snatch focus back from an operator reading the panel body.
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

  return (
    <aside className="evidence-panel" role="region" aria-label={`Evidence for ${data.title}`}>
      <div className="evp-head">
        <div>
          <span className="evp-kind">{data.kindLabel}</span>
          <h4 className="evp-title">{data.title}</h4>
        </div>
        <button ref={closeRef} className="evp-close" onClick={onClose} aria-label="Close" type="button">✕</button>
      </div>
      <div className="evp-body">
        <TruthSection label={data.truthLabel} truth={data.truth} />
        <FactSection title="Facts" rows={data.facts} />
        {(data.sections ?? []).map((section) => (
          <FactSection key={section.title} title={section.title} rows={section.rows} />
        ))}
        <StringList title="Evidence" values={data.evidence ?? []} />
        <SourceSection href={data.sourceHref} label={data.sourceLabel} />
        <StringList title="Limitations" values={data.limitations ?? []} />
      </div>
    </aside>
  );
}

function TruthSection({ label, truth }: { readonly label: string; readonly truth: EshuTruth | null }): React.JSX.Element {
  return (
    <section className="evp-section">
      <h5>Truth</h5>
      <div className="evp-truth-row">
        {label.length === 0 ? (
          <span className="evp-muted">truth label not provided</span>
        ) : KNOWN_TRUTH_LABELS.has(label) ? (
          <TruthChip level={uiTruth(label)} />
        ) : (
          <Badge tone="warn">{label}</Badge>
        )}
      </div>
      {truth === null ? (
        <p className="evp-muted">packet truth unavailable</p>
      ) : (
        <dl className="evp-meta">
          {truth.basis !== undefined && truth.basis.length > 0 ? <Row label="Basis" value={truth.basis} /> : null}
          <div className="evp-row"><dt>Level</dt><dd><TruthChip level={uiTruth(truth.level)} /></dd></div>
          <div className="evp-row"><dt>Freshness</dt><dd><FreshDot state={uiFresh(truth.freshness.state)} /></dd></div>
          {truth.reason !== undefined && truth.reason.length > 0 ? <Row label="Reason" value={truth.reason} /> : null}
        </dl>
      )}
    </section>
  );
}

function FactSection({ title, rows }: { readonly title: string; readonly rows: readonly EvidencePanelFact[] }): React.JSX.Element | null {
  const populated = rows.filter((row) => row.value.trim().length > 0);
  if (populated.length === 0) {
    return null;
  }
  return (
    <section className="evp-section">
      <h5>{title}</h5>
      <dl className="evp-meta">
        {populated.map((row) => <Row key={`${row.label}:${row.value}`} label={row.label} value={row.value} />)}
      </dl>
    </section>
  );
}

function SourceSection({ href, label }: { readonly href?: string; readonly label?: string }): React.JSX.Element | null {
  if (href === undefined || href.length === 0) {
    return null;
  }
  return (
    <section className="evp-section">
      <h5>Source</h5>
      {label !== undefined && label.length > 0 ? <p className="evp-source-path mono">{label}</p> : null}
      <Link className="evp-source-link mono" to={href}>Open source</Link>
    </section>
  );
}

function StringList({ title, values }: { readonly title: string; readonly values: readonly string[] }): React.JSX.Element | null {
  const unique = [...new Set(values.map((value) => value.trim()).filter((value) => value.length > 0))];
  if (unique.length === 0) {
    return null;
  }
  return (
    <section className="evp-section">
      <h5>{title}</h5>
      <ul className="evp-list">{unique.map((value) => <li key={value}>{value}</li>)}</ul>
    </section>
  );
}

function Row({ label, value }: { readonly label: string; readonly value: string }): React.JSX.Element | null {
  if (value.trim().length === 0) {
    return null;
  }
  return (
    <div className="evp-row">
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
