// askEshuNormalize.ts — defensive normalization for POST /api/v0/ask payloads.
//
// The Ask engine emits answers and trace steps whose optional fields may be
// absent. These helpers coerce the wire shape into the typed, fully-populated
// structures the UI renders, so a component never sees `undefined` where it
// expects an array and never trusts an unknown truth class. Kept separate from
// askEshu.ts to hold both files under the package size cap. Types are imported
// type-only, so there is no runtime import cycle.

import type {
  AskAnswer,
  AskArtifact,
  AskError,
  AskEvidenceHandle,
  AskNarrationState,
  AskTraceStep,
  AskTruthClass
} from "./askEshu";

const KNOWN_TRUTH_CLASSES: readonly AskTruthClass[] = [
  "deterministic",
  "derived",
  "fallback",
  "semantic_observation",
  "code_hint",
  "unsupported"
];

/** Coerce a raw ask response into a fully-populated AskAnswer. */
export function normalizeAnswer(raw: unknown): AskAnswer {
  const record = asRecord(raw);
  return {
    answer_prose: typeof record.answer_prose === "string" ? record.answer_prose : "",
    artifacts: asArray(record.artifacts).map(normalizeArtifact),
    truth_class: normalizeTruthClass(record.truth_class),
    query_trace: asArray(record.query_trace).map(normalizeTraceStep),
    partial: record.partial === true,
    limitations: asArray(record.limitations).filter(
      (value): value is string => typeof value === "string"
    ),
    evidence_handles: asArray(record.evidence_handles).map(normalizeEvidenceHandle)
  };
}

/** Coerce a raw artifact into an AskArtifact with a guaranteed issues array. */
export function normalizeArtifact(raw: unknown): AskArtifact {
  const record = asRecord(raw);
  return {
    format: typeof record.format === "string" ? record.format : "text",
    content: typeof record.content === "string" ? record.content : "",
    issues: asArray(record.issues).filter((value): value is string => typeof value === "string")
  };
}

/** Coerce a raw trace event into an AskTraceStep, dropping empty optionals. */
export function normalizeTraceStep(raw: unknown): AskTraceStep {
  const record = asRecord(raw);
  const step: {
    tool: string;
    args?: Record<string, unknown>;
    supported?: boolean;
    truth_class?: string;
    err?: string;
  } = {
    tool: typeof record.tool === "string" ? record.tool : "tool"
  };
  if (record.args && typeof record.args === "object") {
    step.args = record.args as Record<string, unknown>;
  }
  if (typeof record.supported === "boolean") {
    step.supported = record.supported;
  }
  if (typeof record.truth_class === "string") {
    step.truth_class = record.truth_class;
  }
  if (typeof record.err === "string" && record.err.length > 0) {
    step.err = record.err;
  }
  return step;
}

/** Preserve a raw evidence handle as a bounded, indexable record. */
export function normalizeEvidenceHandle(raw: unknown): AskEvidenceHandle {
  return asRecord(raw) as AskEvidenceHandle;
}

/** Coerce an SSE `error` event payload into a bounded AskError. */
export function normalizeStreamError(raw: unknown): AskError {
  const record = asRecord(raw);
  const reason = typeof record.reason === "string" ? record.reason : "Ask Eshu reported an error.";
  return { state: normalizeErrorState(record.state), reason };
}

function normalizeErrorState(value: unknown): AskError["state"] {
  if (value === "forbidden" || value === "bad_request" || value === "error") {
    return value;
  }
  return "unavailable";
}

/** Map an unknown truth class to a known value, defaulting to `fallback`. */
export function normalizeTruthClass(value: unknown): AskTruthClass {
  return KNOWN_TRUTH_CLASSES.find((entry) => entry === value) ?? "fallback";
}

/** Unwrap the status envelope `data` object (or the bare body) for the probe. */
export function narrationData(parsed: unknown): Record<string, unknown> {
  const record = asRecord(parsed);
  if (record.data && typeof record.data === "object") {
    return record.data as Record<string, unknown>;
  }
  return record;
}

/** Map an unknown narration state to a known value, defaulting to `unavailable`. */
export function narrationState(value: unknown): AskNarrationState {
  if (value === "available" || value === "disabled") {
    return value;
  }
  return "unavailable";
}

// narrationProviderConfigured reads the `provider_configured` gate, which the
// backend sets to `adapterReady` — whether the ask provider adapter was
// actually constructed. When it is false, POST /api/v0/ask has a nil Asker and
// every submit returns 503, so the page must show the disabled state rather
// than an askable evidence-only input. Defaults to true when the field is
// absent (older backends) so the submit-time 503 still handles it gracefully.
export function narrationProviderConfigured(value: unknown): boolean {
  return value === false ? false : true;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : {};
}

function asArray(value: unknown): readonly unknown[] {
  return Array.isArray(value) ? value : [];
}
