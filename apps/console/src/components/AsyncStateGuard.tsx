// components/AsyncStateGuard.tsx
// Reusable guard that distinguishes three async states for model-backed data tabs:
//   loading   – request in flight; show a spinner, never the empty state
//   error     – section unavailable; show an explicit error message
//   ready     – 200 response received (rows may still be empty)
//
// Usage:
//   <AsyncStateGuard provenance={model.provenance.services ?? "loading"} label="catalog">
//     {/* rendered only when provenance is "live" | "demo" | "empty" */}
//     {rows.length === 0 ? <EmptyRow /> : <DataRows />}
//   </AsyncStateGuard>
//
// This is the UX safety net for issue #3395: slow tabs previously rendered the
// empty state during the fetch, falsely signalling "no data". Now they show a
// spinner until the API responds.

import type { ReactNode } from "react";

import type { SectionProvenance } from "../api/eshuConsoleLive";

interface AsyncStateGuardProps {
  /** Provenance value for the section being guarded. */
  readonly provenance: SectionProvenance;
  /** Human-readable label used in the loading/error messages (e.g. "catalog"). */
  readonly label: string;
  /** Rendered only when provenance indicates a completed response. */
  readonly children: ReactNode;
}

/**
 * AsyncStateGuard renders a spinner while data is in flight, an explicit error
 * when the section is unavailable, and its children for every completed-response
 * state (including empty-200). Callers retain full control over the empty-row
 * presentation inside children.
 */
export function AsyncStateGuard({ provenance, label, children }: AsyncStateGuardProps): React.JSX.Element {
  if (provenance === "loading") {
    return (
      <div className="async-guard-loading" role="status" aria-label={`Loading ${label}`}>
        <div className="conn-spinner" aria-hidden />
        <span>Loading {label}…</span>
      </div>
    );
  }
  if (provenance === "unavailable") {
    return (
      <p className="empty async-guard-error">
        {label.charAt(0).toUpperCase() + label.slice(1)} data unavailable — the API did not respond for this section.
      </p>
    );
  }
  // provenance is "live" | "demo" | "empty": response received, children decide presentation.
  return <>{children}</>;
}
