export type DeploymentGraphDetail = "summary" | "expanded";

export interface DeploymentArtifactRecord {
  readonly artifact_id?: string;
  readonly id?: string;
  readonly artifact_family?: string;
  readonly commit_sha?: string;
  readonly confidence?: number;
  readonly confidence_basis?: string;
  readonly direction?: string;
  readonly end_line?: number;
  readonly environment?: string;
  readonly evidence_kind?: string;
  readonly evidence_source?: string;
  readonly extractor?: string;
  readonly generation_id?: string;
  readonly matched_alias?: string;
  readonly matched_value?: string;
  readonly outcome?: string;
  readonly path?: string;
  readonly provenance_only?: boolean;
  readonly rationale?: string;
  readonly relationship_type?: string;
  readonly resolution_source?: string;
  readonly resolved_id?: string;
  readonly runtime_platform_kind?: string;
  readonly source_freshness?: string;
  readonly source_repo_id?: string;
  readonly source_repo_name?: string;
  readonly state?: string;
  readonly start_line?: number;
  readonly target_repo_id?: string;
  readonly target_repo_name?: string;
}

export interface DeploymentPlatformRecord {
  readonly platform_confidence?: number;
  readonly platform_kind?: string;
  readonly platform_name?: string;
  readonly platform_reason?: string;
}

export interface DeploymentInstanceRecord {
  readonly environment?: string;
  readonly instance_id?: string;
  readonly materialization_confidence?: number;
  readonly materialization_provenance?: readonly string[];
  readonly platform_confidence?: number;
  readonly platform_kind?: string;
  readonly platform_name?: string;
  readonly platform_reason?: string;
  readonly platforms?: readonly DeploymentPlatformRecord[];
}

export interface DeploymentSourceRecord {
  readonly confidence?: number;
  readonly reason?: string;
  readonly repo_id?: string;
  readonly repo_name?: string;
}

export interface KubernetesResourceRecord {
  readonly entity_id?: string;
  readonly entity_name?: string;
  readonly kind?: string;
  readonly qualified_name?: string;
  readonly relative_path?: string;
}

export interface KubernetesRelationshipRecord {
  readonly reason?: string;
  readonly source_id?: string;
  readonly source_name?: string;
  readonly target_id?: string;
  readonly target_name?: string;
  readonly type?: string;
}

export interface NetworkPathRecord {
  readonly environment?: string;
  readonly from?: string;
  readonly from_type?: string;
  readonly path_type?: string;
  readonly reason?: string;
  readonly to?: string;
  readonly to_type?: string;
  readonly visibility?: string;
}

export interface NamedDeploymentRecord {
  readonly confidence?: number;
  readonly environment?: string;
  readonly id?: string;
  readonly kind?: string;
  readonly name?: string;
  readonly reason?: string;
  readonly target?: string;
  readonly type?: string;
  readonly visibility?: string;
}

export interface ServiceDeploymentContextResponse {
  readonly api_surface?: {
    readonly endpoint_count?: number;
    readonly endpoints?: readonly unknown[];
  };
  readonly deployment_evidence?: {
    readonly artifact_count?: number;
    readonly artifacts?: readonly DeploymentArtifactRecord[];
  };
  readonly entrypoints?: readonly NamedDeploymentRecord[];
  readonly id?: string;
  readonly instances?: readonly DeploymentInstanceRecord[];
  readonly name?: string;
  readonly network_paths?: readonly NetworkPathRecord[];
  readonly repo_id?: string;
  readonly repo_name?: string;
}

export interface DeploymentTraceResponse {
  readonly cloud_resources?: readonly NamedDeploymentRecord[];
  readonly deployment_evidence?: ServiceDeploymentContextResponse["deployment_evidence"];
  readonly deployment_sources?: readonly DeploymentSourceRecord[];
  readonly entrypoints?: readonly NamedDeploymentRecord[];
  readonly instances?: readonly DeploymentInstanceRecord[];
  readonly k8s_relationships?: readonly KubernetesRelationshipRecord[];
  readonly k8s_resources?: readonly KubernetesResourceRecord[];
  readonly network_paths?: readonly NetworkPathRecord[];
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly service_name?: string;
  readonly workload_id?: string;
}

export function mergeDeploymentInstances(
  contextRows: readonly DeploymentInstanceRecord[],
  traceRows: readonly DeploymentInstanceRecord[],
): DeploymentInstanceRecord[] {
  const instances = new Map<string, DeploymentInstanceRecord>();
  [...contextRows, ...traceRows].map(normalizeDeploymentInstance).forEach((row) => {
    const id = row.instance_id?.trim() ?? "";
    if (id === "") return;
    const current = instances.get(id);
    if (!current) {
      instances.set(id, row);
      return;
    }
    instances.set(id, {
      environment: current.environment?.trim() ? current.environment : row.environment,
      instance_id: id,
      materialization_confidence:
        current.materialization_confidence ?? row.materialization_confidence,
      materialization_provenance: uniqueStrings([
        ...(current.materialization_provenance ?? []),
        ...(row.materialization_provenance ?? []),
      ]),
      platforms: uniquePlatforms([...(current.platforms ?? []), ...(row.platforms ?? [])]),
    });
  });
  return [...instances.values()];
}

function normalizeDeploymentInstance(row: DeploymentInstanceRecord): DeploymentInstanceRecord {
  const flatPlatform =
    row.platform_name?.trim() || row.platform_kind?.trim()
      ? [
          {
            platform_confidence: row.platform_confidence,
            platform_kind: row.platform_kind,
            platform_name: row.platform_name,
            platform_reason: row.platform_reason,
          },
        ]
      : [];
  return {
    ...row,
    platforms: uniquePlatforms([...flatPlatform, ...(row.platforms ?? [])]),
  };
}

export function uniqueDeploymentArtifacts(
  rows: readonly DeploymentArtifactRecord[],
): DeploymentArtifactRecord[] {
  return uniqueRecords(rows, deploymentArtifactKey);
}

export function uniqueNetworkPaths(rows: readonly NetworkPathRecord[]): NetworkPathRecord[] {
  return uniqueRecords(rows, networkPathKey);
}

export function uniqueNamedRecords(
  rows: readonly NamedDeploymentRecord[],
): NamedDeploymentRecord[] {
  return uniqueRecords(rows, namedDeploymentRecordKey);
}

export function deploymentArtifactKey(row: DeploymentArtifactRecord): string {
  const canonicalID = deploymentArtifactID(row);
  if (canonicalID !== "") return `canonical:${canonicalID}`;
  return [
    row.generation_id,
    row.source_repo_id,
    row.target_repo_id,
    row.relationship_type,
    row.artifact_family,
    row.evidence_kind,
    row.path,
    row.environment,
    row.outcome,
    String(row.provenance_only ?? false),
    row.resolution_source,
    row.resolved_id,
    row.source_freshness,
    row.commit_sha,
    row.start_line,
    row.end_line,
    row.extractor,
    row.evidence_source,
    row.matched_alias,
    row.matched_value,
    row.runtime_platform_kind,
    row.direction,
  ].join("\u0000");
}

export function deploymentArtifactID(row: DeploymentArtifactRecord): string {
  return row.artifact_id?.trim() || row.id?.trim() || "";
}

export function networkPathKey(row: NetworkPathRecord): string {
  return [
    row.from,
    row.from_type,
    row.to,
    row.to_type,
    row.path_type,
    row.environment,
    row.reason,
    row.visibility,
  ].join("\u0000");
}

export function namedDeploymentRecordKey(row: NamedDeploymentRecord): string {
  return [row.id, row.name, row.target, row.type, row.environment].join("\u0000");
}

export function deploymentPlatformKey(row: DeploymentPlatformRecord): string {
  return `${row.platform_kind ?? ""}\u0000${row.platform_name ?? ""}`;
}

function uniquePlatforms(rows: readonly DeploymentPlatformRecord[]): DeploymentPlatformRecord[] {
  const platforms = new Map<string, DeploymentPlatformRecord>();
  rows.forEach((row) => {
    const key = deploymentPlatformKey(row);
    const current = platforms.get(key);
    platforms.set(key, {
      platform_confidence: current?.platform_confidence ?? row.platform_confidence,
      platform_kind: current?.platform_kind ?? row.platform_kind,
      platform_name: current?.platform_name ?? row.platform_name,
      platform_reason: current?.platform_reason ?? row.platform_reason,
    });
  });
  return [...platforms.values()];
}

function uniqueStrings(values: readonly string[]): string[] {
  return [...new Set(values)];
}

function uniqueRecords<T>(rows: readonly T[], keyFor: (row: T) => string): T[] {
  const seen = new Set<string>();
  return rows.filter((row) => {
    const key = keyFor(row);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}
