// api/repositoryFreshnessSelection.ts
// Collector selection evidence carried in the freshness response's always-
// present `selection` block (#7625, console side #7773). It answers "does any
// live collector selector still select this repository, and if not, why",
// which is what backs the not_selected freshness verdict. Split from
// repositoryFreshness.ts so that adapter stays under the file-size cap; it
// owns only the wire -> UI mapping and the state/reason labels.
//
// Decoding fails closed to "unknown": a missing block (an older API), an
// unrecognized state, or a malformed field never invents selection evidence.
// A reason is kept only for the states the wire contract allows it on
// (never for selected or unknown).

import type { FreshnessTone } from "./repositoryFreshness";

export type SelectionState =
  | "selected"
  | "not_selected"
  | "pending_confirmation"
  | "excluded_still_ingested"
  | "unknown";

export type SelectionReason = "not_listed" | "archived_excluded" | "rule_excluded";

export interface RepositoryFreshnessSelection {
  readonly state: SelectionState;
  readonly reason: SelectionReason | null;
  readonly stateSince: string | null;
  readonly lastListedAt: string | null;
  readonly evaluatedAt: string | null;
  readonly liveSelectorCount: number;
}

export interface FreshnessSelectionWire {
  readonly state?: string;
  readonly reason?: string | null;
  readonly state_since?: string | null;
  readonly last_listed_at?: string | null;
  readonly evaluated_at?: string | null;
  readonly live_selector_count?: number;
}

export const unknownSelection: RepositoryFreshnessSelection = {
  state: "unknown",
  reason: null,
  stateSince: null,
  lastListedAt: null,
  evaluatedAt: null,
  liveSelectorCount: 0,
};

const SELECTION_STATES: readonly SelectionState[] = [
  "selected",
  "not_selected",
  "pending_confirmation",
  "excluded_still_ingested",
  "unknown",
];

const SELECTION_REASONS: readonly SelectionReason[] = [
  "not_listed",
  "archived_excluded",
  "rule_excluded",
];

// selectionFromWire maps the wire block onto the UI shape. An unrecognized
// state collapses the whole block to unknownSelection rather than pairing
// stale timestamps with a state the console cannot interpret.
export function selectionFromWire(
  wire: FreshnessSelectionWire | null | undefined,
): RepositoryFreshnessSelection {
  const state = SELECTION_STATES.find((candidate) => candidate === wire?.state);
  if (!wire || state === undefined || state === "unknown") return unknownSelection;
  const reason = SELECTION_REASONS.find((candidate) => candidate === wire.reason) ?? null;
  return {
    state,
    reason: state === "selected" ? null : reason,
    stateSince: timestamp(wire.state_since),
    lastListedAt: timestamp(wire.last_listed_at),
    evaluatedAt: timestamp(wire.evaluated_at),
    liveSelectorCount: count(wire.live_selector_count),
  };
}

const STATE_LABEL: Record<
  SelectionState,
  { readonly label: string; readonly tone: FreshnessTone }
> = {
  selected: { label: "Selected", tone: "teal" },
  not_selected: { label: "Not selected", tone: "neutral" },
  pending_confirmation: { label: "Exclusion pending confirmation", tone: "violet" },
  excluded_still_ingested: { label: "Excluded, still ingesting", tone: "warn" },
  unknown: { label: "No live selector evidence", tone: "neutral" },
};

// selectionStateLabel returns the end-user label and badge tone for a state.
export function selectionStateLabel(state: SelectionState): {
  readonly label: string;
  readonly tone: FreshnessTone;
} {
  return STATE_LABEL[state];
}

const REASON_LABEL: Record<SelectionReason, string> = {
  not_listed: "not in the collector's listing",
  archived_excluded: "archived",
  rule_excluded: "excluded by a selection rule",
};

// selectionReasonLabel renders a reason in end-user language, or "" when the
// state carries no reason.
export function selectionReasonLabel(reason: SelectionReason | null): string {
  return reason === null ? "" : REASON_LABEL[reason];
}

function timestamp(value: string | null | undefined): string | null {
  const trimmed = value?.trim() ?? "";
  return trimmed === "" ? null : trimmed;
}

function count(value: number | undefined): number {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : 0;
}
