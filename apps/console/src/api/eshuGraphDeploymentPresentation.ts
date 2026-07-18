import type { EshuTruth } from "./envelope";
import type {
  DeploymentArtifactRecord,
  DeploymentInstanceRecord,
  NamedDeploymentRecord,
} from "./eshuGraphDeploymentWire";
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
  return compact([
    cleanText(artifact.evidence_kind) ? `evidence kind: ${cleanText(artifact.evidence_kind)}` : "",
    cleanText(artifact.path) ? `path: ${cleanText(artifact.path)}` : "",
    cleanText(artifact.environment) ? `environment: ${cleanText(artifact.environment)}` : "",
    cleanText(artifact.confidence_basis)
      ? `confidence basis: ${cleanText(artifact.confidence_basis)}`
      : "",
    cleanText(artifact.resolved_id) ? `resolved id: ${cleanText(artifact.resolved_id)}` : "",
  ]);
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
): GraphNode {
  return {
    col: 1,
    id: repo.id,
    kind: "repo",
    label: repo.name,
    source: filePath ? { filePath, repoId: repo.id, repoName: repo.name } : undefined,
    sub,
    truth: "derived",
  };
}

export function addIsolatedRecords(
  nodes: Map<string, GraphNode>,
  rows: readonly NamedDeploymentRecord[],
  limit: number,
  prefix: string,
  col: number,
  maxNodes: number,
  truth: UiTruth,
): void {
  rows.slice(0, limit).forEach((row, index) => {
    if (nodes.size >= maxNodes) return;
    const label =
      cleanText(row.name) || cleanText(row.target) || cleanText(row.id) || `${prefix} ${index + 1}`;
    const id = cleanText(row.id) || `${prefix}:${index}:${encodeKey(label)}`;
    if (!nodes.has(id))
      nodes.set(id, {
        col,
        id,
        kind: cleanText(row.kind) || cleanText(row.type) || prefix,
        label,
        sub:
          compact([
            cleanText(row.environment),
            cleanText(row.visibility),
            cleanText(row.reason),
          ]).join(" · ") || undefined,
        truth,
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
