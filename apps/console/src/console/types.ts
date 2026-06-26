// console/types.ts
// View-model types + palettes shared across the redesigned console.
// Builds on the live snapshot shape from ../api/eshuConsoleLive.

import type { CollectorReadinessRow } from "../api/collectorReadiness";
import type {
  ConsoleSnapshot, RuntimeSummary, ServiceRow, LanguageRow,
  IngesterRow, FindingRow, VulnRow, SbomEvidenceRow, DependencyRow, ImageRow, IacResourceRow,
  AdvisoryRow, CloudResourceRow, SectionProvenance, SeriesBundle, ArgoCDAppRow
} from "../api/eshuConsoleLive";

export type {
  ConsoleSnapshot, RuntimeSummary, ServiceRow, LanguageRow,
  IngesterRow, FindingRow, VulnRow, SbomEvidenceRow, DependencyRow, ImageRow, IacResourceRow,
  AdvisoryRow, CloudResourceRow, SectionProvenance, SeriesBundle, ArgoCDAppRow
};
export type { CollectorReadinessRow };

export type Severity = "critical" | "high" | "medium" | "low" | "info";
export type UiTruth = "exact" | "derived" | "inferred";
export type UiFresh = "fresh" | "lagging" | "stale";
export type RelationshipConfidenceTier = "high" | "medium" | "low" | "unsupported";
export type RelationshipTruthState = "derived" | "heuristic" | "unsupported";

export type GraphLayer = "code" | "deploy" | "infra" | "runtime" | "security" | "ops";

/**
 * Indexed source location for a graph node when a query response can tie the
 * entity back to a repository file without another source lookup.
 */
export interface GraphSourceLocation {
  readonly repoId: string;
  readonly repoName?: string;
  readonly filePath: string;
  readonly startLine?: number;
  readonly endLine?: number;
}

export interface GraphNode {
  readonly id: string;
  readonly kind: string;
  readonly label: string;
  readonly sub?: string;
  readonly col: number;
  readonly hero?: boolean;
  readonly truth?: UiTruth;
  readonly source?: GraphSourceLocation;
}

export interface GraphEdge {
  readonly s: string;
  readonly t: string;
  readonly verb: string;
  readonly layer: GraphLayer;
  readonly evidence?: readonly string[];
  readonly confidenceTier?: RelationshipConfidenceTier;
  readonly truthState?: RelationshipTruthState;
  readonly sourceFamily?: string;
  readonly method?: string;
}

export interface GraphModel {
  readonly nodes: readonly GraphNode[];
  readonly edges: readonly GraphEdge[];
}

export interface RelationshipRow {
  readonly verb: string;
  readonly layer: GraphLayer;
  readonly count: number;
  readonly detail: string;
}

// The full model the UI renders: live snapshot + UI-only extras (graph, series).
export interface ConsoleModel extends ConsoleSnapshot {
  readonly graph: GraphModel;
  readonly relationships: readonly RelationshipRow[];
  readonly source: "demo" | "live";
}

export const SEVERITY_COLOR: Record<Severity, string> = {
  critical: "#f0506e", high: "#ff8a00", medium: "#f5b73d", low: "#14b8a6", info: "#6b7280"
};

export const LAYER_COLOR: Record<GraphLayer, string> = {
  code: "#14b8a6", deploy: "#ff8a00", infra: "#a17ef7",
  runtime: "#4f8cff", security: "#f0506e", ops: "#22c55e"
};

export const KIND_COLOR: Record<string, string> = {
  service: "#14b8a6", repo: "#f3ebdd", client: "#2dd4bf", library: "#c4b59a",
  image: "#22d3ee", workload: "#4f8cff", env: "#9ca3af", tf: "#8b5cf6",
  aws: "#ff9d2e", datastore: "#f59e0b", incident: "#22c55e", vuln: "#f0506e", workitem: "#60a5fa"
};

// API truth.level (exact|derived|fallback) -> UI chip; freshness state -> UI dot.
export function uiTruth(level: string | undefined): UiTruth {
  if (level === "fallback") return "inferred";
  if (level === "derived") return "derived";
  return "exact";
}
export function uiFresh(state: string | undefined): UiFresh {
  if (state === "building") return "lagging";
  if (state === "stale" || state === "unavailable") return "stale";
  return "fresh";
}
export const TRUTH_COLOR: Record<UiTruth, string> = { exact: "#14b8a6", derived: "#f5b73d", inferred: "#ff8a00" };
export const FRESH_COLOR: Record<UiFresh, string> = { fresh: "#14b8a6", lagging: "#f5b73d", stale: "#f0506e" };

export function fmt(n: number | undefined): string {
  if (n === undefined || n === null) return "—";
  if (Math.abs(n) >= 1e9) return `${(n / 1e9).toFixed(2)}B`;
  if (Math.abs(n) >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (Math.abs(n) >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(n);
}
