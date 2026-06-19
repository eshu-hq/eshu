// api/exposurePath.ts
// Loader for the code-to-cloud exposure trace served at
// POST /api/v0/impact/trace-exposure-path (epic #2704, backend #2726). It traces
// bounded symbol-level reachability from an internet-exposed handler source to a
// recognized cloud sink and returns a conservative, derived finding.
//
// The loader keeps the TypeScript view-model in lockstep with the Go wire
// contract in go/internal/query/exposure_path_mapping.go. It never fabricates a
// path: when the backend reports an unresolved finding (e.g. an unmaterialized
// bridge edge) the loader preserves the empty path set and the honest coverage
// reason so the view can say "no proven path" instead of implying one exists.
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import type { EshuApiClient } from "./client";

// ExposureRank is the honest exposure ranking of a source handler. It mirrors the
// Go ExposureRank vocabulary; an unknown wire value falls back to "internal" (the
// least-alarming rank) rather than inventing exposure.
export type ExposureRank = "internet_exposed" | "network_reachable" | "internal";

// TraversalState is the conservative per-path / per-finding truth-state. It
// mirrors the Go TraversalState vocabulary.
export type TraversalState = "exact" | "partial" | "ambiguous" | "unresolved";

// Severity is the closed severity vocabulary shared with the Go sink catalog.
export type Severity = "critical" | "high" | "medium" | "low";

// ExposureNode is one node on a traced exposure path.
export interface ExposureNode {
  readonly entityId: string;
  readonly name: string;
  readonly labels: readonly string[];
}

// ExposureSink is the recognized cloud sink terminating a path.
export interface ExposureSink {
  readonly kind: string;
  readonly displayName: string;
  readonly node: ExposureNode;
}

// ExposurePath is one assembled finding path: the ordered nodes from source to
// sink terminal, the recognized sink, and the computed state/severity/reason.
export interface ExposurePath {
  readonly nodes: readonly ExposureNode[];
  readonly sink: ExposureSink;
  readonly depth: number;
  readonly state: TraversalState;
  readonly severity: Severity;
  readonly reason: string;
}

// ExposureCoverage records the honest bounds of a trace.
export interface ExposureCoverage {
  readonly maxDepth: number;
  readonly pathsFound: number;
  readonly truncated: boolean;
  readonly unresolvedReason: string;
}

// ExposureFinding is the normalized console view-model for one source handler.
// `provenance` is the loader's own honesty flag, distinct from the finding state:
// - "live"        the backend returned a finding (resolved or unresolved)
// - "unavailable" the request failed; no finding to render
export interface ExposureFinding {
  readonly source: ExposureNode;
  readonly sourceKind: string;
  readonly exposureRank: ExposureRank;
  readonly truthLabel: string;
  readonly state: TraversalState;
  readonly paths: readonly ExposurePath[];
  readonly coverage: ExposureCoverage;
  readonly truth: EshuTruth | null;
  readonly provenance: "live" | "unavailable";
  // error carries the failure message when provenance is "unavailable".
  readonly error?: string;
}

// ExposurePathInput is the console-facing request. A caller supplies either a
// source name (resolved within repoId) or a source entity id.
export interface ExposurePathInput {
  readonly source?: string;
  readonly sourceEntityId?: string;
  readonly repoId?: string;
  readonly maxDepth?: number;
}

interface ExposureNodeWire {
  readonly entity_id?: string;
  readonly name?: string;
  readonly labels?: readonly string[] | null;
}

interface ExposureSinkWire {
  readonly kind?: string;
  readonly display_name?: string;
  readonly node?: ExposureNodeWire;
}

interface ExposurePathWire {
  readonly nodes?: readonly ExposureNodeWire[] | null;
  readonly sink?: ExposureSinkWire;
  readonly depth?: number;
  readonly state?: string;
  readonly severity?: string;
  readonly reason?: string;
}

interface ExposureCoverageWire {
  readonly max_depth?: number;
  readonly paths_found?: number;
  readonly truncated?: boolean;
  readonly unresolved_reason?: string;
}

interface ExposureFindingWire {
  readonly source?: ExposureNodeWire;
  readonly source_kind?: string;
  readonly exposure_rank?: string;
  readonly truth_label?: string;
  readonly state?: string;
  readonly paths?: readonly ExposurePathWire[] | null;
  readonly coverage?: ExposureCoverageWire;
}

const exposureRanks: ReadonlySet<ExposureRank> = new Set([
  "internet_exposed",
  "network_reachable",
  "internal"
]);

const traversalStates: ReadonlySet<TraversalState> = new Set([
  "exact",
  "partial",
  "ambiguous",
  "unresolved"
]);

const severities: ReadonlySet<Severity> = new Set([
  "critical",
  "high",
  "medium",
  "low"
]);

function parseExposureRank(raw: string | undefined): ExposureRank {
  return raw !== undefined && exposureRanks.has(raw as ExposureRank)
    ? (raw as ExposureRank)
    : "internal";
}

// parseTraversalState defaults an unknown wire value to "unresolved" so the view
// never optimistically treats an unrecognized state as a proven path.
function parseTraversalState(raw: string | undefined): TraversalState {
  return raw !== undefined && traversalStates.has(raw as TraversalState)
    ? (raw as TraversalState)
    : "unresolved";
}

function parseSeverity(raw: string | undefined): Severity {
  return raw !== undefined && severities.has(raw as Severity)
    ? (raw as Severity)
    : "low";
}

function nodeFromWire(wire: ExposureNodeWire | undefined): ExposureNode {
  return {
    entityId: wire?.entity_id ?? "",
    name: wire?.name ?? "",
    labels: wire?.labels ?? []
  };
}

function sinkFromWire(wire: ExposureSinkWire | undefined): ExposureSink {
  return {
    kind: wire?.kind ?? "",
    displayName: wire?.display_name ?? wire?.kind ?? "",
    node: nodeFromWire(wire?.node)
  };
}

function pathFromWire(wire: ExposurePathWire): ExposurePath {
  return {
    nodes: (wire.nodes ?? []).map(nodeFromWire),
    sink: sinkFromWire(wire.sink),
    depth: wire.depth ?? 0,
    state: parseTraversalState(wire.state),
    severity: parseSeverity(wire.severity),
    reason: wire.reason ?? ""
  };
}

function coverageFromWire(wire: ExposureCoverageWire | undefined): ExposureCoverage {
  return {
    maxDepth: wire?.max_depth ?? 0,
    pathsFound: wire?.paths_found ?? 0,
    truncated: wire?.truncated ?? false,
    unresolvedReason: wire?.unresolved_reason ?? ""
  };
}

function findingFromWire(wire: ExposureFindingWire, truth: EshuTruth | null): ExposureFinding {
  const paths = (wire.paths ?? []).map(pathFromWire);
  const state = parseTraversalState(wire.state);
  return {
    source: nodeFromWire(wire.source),
    sourceKind: wire.source_kind ?? "",
    exposureRank: parseExposureRank(wire.exposure_rank),
    truthLabel: wire.truth_label ?? "derived",
    // An unresolved or empty-path finding stays unresolved no matter what the
    // wire state said: a finding with zero paths can never be "exact".
    state: paths.length === 0 && state !== "ambiguous" ? "unresolved" : state,
    paths,
    coverage: coverageFromWire(wire.coverage),
    truth,
    provenance: "live"
  };
}

// loadExposureFinding traces the code-to-cloud exposure path for one source
// handler. On any failure it returns an "unavailable" finding carrying the error
// message, never a fabricated path.
export async function loadExposureFinding(
  client: EshuApiClient,
  input: ExposurePathInput
): Promise<ExposureFinding> {
  const body: Record<string, unknown> = {};
  const source = input.source?.trim() ?? "";
  const sourceEntityId = input.sourceEntityId?.trim() ?? "";
  const repoId = input.repoId?.trim() ?? "";
  if (sourceEntityId.length > 0) body.source_entity_id = sourceEntityId;
  if (source.length > 0) body.source = source;
  if (repoId.length > 0) body.repo_id = repoId;
  if (input.maxDepth !== undefined && Number.isFinite(input.maxDepth)) {
    body.max_depth = clampDepth(input.maxDepth);
  }

  try {
    const env = await client.post<ExposureFindingWire>(
      "/api/v0/impact/trace-exposure-path",
      body
    );
    if (env.error !== null) {
      throw new EshuEnvelopeError(env.error);
    }
    if (env.data === null) {
      throw new Error("Eshu envelope success response is missing data");
    }
    return findingFromWire(env.data, env.truth ?? null);
  } catch (error) {
    return {
      source: { entityId: "", name: source || sourceEntityId, labels: [] },
      sourceKind: "",
      exposureRank: "internal",
      truthLabel: "derived",
      state: "unresolved",
      paths: [],
      coverage: { maxDepth: 0, pathsFound: 0, truncated: false, unresolvedReason: "" },
      truth: null,
      provenance: "unavailable",
      error: error instanceof Error ? error.message : "request failed"
    };
  }
}

// clampDepth keeps max_depth within the backend's documented bound (1..10).
function clampDepth(value: number): number {
  return Math.max(1, Math.min(10, Math.trunc(value)));
}
