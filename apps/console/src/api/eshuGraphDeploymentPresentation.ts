import type { EshuTruth } from "./envelope";
import type {
  DeploymentArtifactRecord,
  DeploymentInstanceRecord,
  NamedDeploymentRecord,
} from "./eshuGraphDeploymentWire";
import { deploymentArtifactID } from "./eshuGraphDeploymentWire";
import { cleanText } from "./eshuGraphShared";
import type { GraphNode, UiTruth } from "../console/types";

export function artifactAdmissionStatus(artifact: DeploymentArtifactRecord): string {
  const freshness = cleanText(artifact.source_freshness).toLowerCase();
  const outcome = `${cleanText(artifact.outcome)} ${cleanText(artifact.state)}`.toLowerCase();
  if (freshness === "stale") return "Deployment evidence · stale · relationship not admitted";
  if (artifact.provenance_only || outcome.includes("provenance"))
    return "Deployment evidence · provenance only · relationship not admitted";
  if (["ambiguous", "rejected", "unresolved"].some((state) => outcome.includes(state))) {
    return `Deployment evidence · ${cleanText(artifact.outcome) || cleanText(artifact.state)} · relationship not admitted`;
  }
  return "admitted";
}

export function artifactEvidence(artifact: DeploymentArtifactRecord): readonly string[] {
  const artifactID = deploymentArtifactID(artifact);
  return compact([
    artifactID ? `artifact id: ${artifactID}` : "",
    cleanText(artifact.generation_id) ? `generation id: ${cleanText(artifact.generation_id)}` : "",
    cleanText(artifact.evidence_kind) ? `evidence kind: ${cleanText(artifact.evidence_kind)}` : "",
    cleanText(artifact.path) ? `path: ${cleanText(artifact.path)}` : "",
    cleanText(artifact.environment) ? `environment: ${cleanText(artifact.environment)}` : "",
    cleanText(artifact.confidence_basis)
      ? `confidence basis: ${cleanText(artifact.confidence_basis)}`
      : "",
    cleanText(artifact.resolved_id) ? `resolved id: ${cleanText(artifact.resolved_id)}` : "",
    cleanText(artifact.extractor) ? `extractor: ${cleanText(artifact.extractor)}` : "",
    cleanText(artifact.evidence_source)
      ? `evidence source: ${cleanText(artifact.evidence_source)}`
      : "",
    sourceLineEvidence(artifact.start_line, artifact.end_line),
    cleanText(artifact.commit_sha) ? `commit: ${cleanText(artifact.commit_sha)}` : "",
    cleanText(artifact.runtime_platform_kind)
      ? `runtime platform kind: ${cleanText(artifact.runtime_platform_kind)}`
      : "",
    cleanText(artifact.matched_alias) ? `matched alias: ${cleanText(artifact.matched_alias)}` : "",
    cleanText(artifact.matched_value) ? `matched value: ${cleanText(artifact.matched_value)}` : "",
    cleanText(artifact.direction) ? `direction: ${cleanText(artifact.direction)}` : "",
    artifact.confidence !== undefined ? `confidence: ${artifact.confidence}` : "",
    cleanText(artifact.source_freshness)
      ? `source freshness: ${cleanText(artifact.source_freshness)}`
      : "",
    cleanText(artifact.outcome) ? `outcome: ${cleanText(artifact.outcome)}` : "",
    cleanText(artifact.rationale) ? `rationale: ${cleanText(artifact.rationale)}` : "",
  ]);
}

export function artifactMethod(artifact: DeploymentArtifactRecord): string | undefined {
  return (
    cleanText(artifact.resolution_source) ||
    cleanText(artifact.extractor) ||
    cleanText(artifact.evidence_source) ||
    undefined
  );
}

export function materializationEvidence(instance: DeploymentInstanceRecord): readonly string[] {
  return compact([
    ...(instance.materialization_provenance ?? []).map((value) => `provenance: ${value}`),
    instance.materialization_confidence !== undefined
      ? `confidence: ${instance.materialization_confidence}`
      : "",
  ]);
}

export function repoRef(
  idValue: string | undefined,
  nameValue: string | undefined,
): { readonly id: string; readonly name: string } | null {
  const id = cleanText(idValue);
  const name = cleanText(nameValue);
  if (id === "" && name === "") return null;
  return { id: id || `repository:${name}`, name: name || id };
}

export function repoNode(
  repo: { readonly id: string; readonly name: string },
  sub: string,
  filePath?: string,
  startLine?: number,
  endLine?: number,
): GraphNode {
  return {
    col: 1,
    id: repo.id,
    kind: "repo",
    label: repo.name,
    source: filePath
      ? { endLine, filePath, repoId: repo.id, repoName: repo.name, startLine }
      : undefined,
    sub,
    truth: "derived",
  };
}

export function addIsolatedRecords(
  rows: readonly NamedDeploymentRecord[],
  limit: number,
  prefix: string,
  col: number,
  addNode: (node: GraphNode) => boolean,
  truth: UiTruth | ((row: NamedDeploymentRecord) => UiTruth),
): void {
  rows.slice(0, limit).forEach((row, index) => {
    const label =
      cleanText(row.name) || cleanText(row.target) || cleanText(row.id) || `${prefix} ${index + 1}`;
    const id = cleanText(row.id) || `${prefix}:${index}:${encodeKey(label)}`;
    addNode({
      col,
      id,
      kind: cleanText(row.kind) || cleanText(row.type) || prefix,
      label,
      sub:
        compact([
          cleanText(row.environment),
          cleanText(row.visibility),
          cleanText(row.reason),
          row.confidence !== undefined ? `confidence: ${row.confidence}` : "",
        ]).join(" · ") || undefined,
      truth: typeof truth === "function" ? truth(row) : truth,
    });
  });
}

export function addOmissionSummary(
  summaries: GraphNode[],
  family: string,
  omitted: number,
  id: string,
  contract?: string,
): void {
  if (omitted > 0) summaries.push(summaryNode(id, `${omitted} ${family} not shown`, contract));
}

export function summaryNode(id: string, label: string, sub?: string): GraphNode {
  return {
    col: 5,
    id: `summary:${id}`,
    kind: "summary",
    label,
    sub: sub || "Bounded deployment evidence",
    truth: "derived",
  };
}

export function graphTruth(truth: EshuTruth | null | undefined): UiTruth {
  if (truth?.level === "exact") return "exact";
  if (truth?.level === "fallback") return "inferred";
  return "derived";
}

export function truthEvidence(truth: EshuTruth | null | undefined): readonly string[] {
  if (!truth) return [];
  return [
    `truth level: ${truth.level}`,
    `freshness: ${truth.freshness.state}`,
    `truth basis: ${truth.basis ?? "not provided"}`,
    `capability: ${truth.capability}`,
    `profile: ${truth.profile}`,
  ];
}

export function truthIsCurrent(truth: EshuTruth | null | undefined): boolean {
  return !truth || truth.freshness.state === "fresh";
}

export function omissionContract(limit: number, values: readonly string[]): string {
  const groups = [...new Set(values.filter((value) => value !== ""))];
  const visible = groups.slice(0, 8).join(", ") || "identity not provided";
  const remaining = groups.length > 8 ? `, and ${groups.length - 8} more groups` : "";
  return `Aggregation contract: first ${limit} rows retained in API order · omitted groups: ${visible}${remaining}`;
}

export function compact(values: readonly string[]): readonly string[] {
  return values.filter((value) => value !== "");
}

export function encodeKey(value: string): string {
  return encodeURIComponent(value.toLowerCase());
}

function sourceLineEvidence(startLine: number | undefined, endLine: number | undefined): string {
  if (startLine === undefined) return "";
  return `source lines: ${startLine}${endLine === undefined ? "" : `-${endLine}`}`;
}
