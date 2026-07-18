export type DeploymentGraphDetail = "summary" | "expanded";

export interface DeploymentArtifactRecord {
  readonly artifact_family?: string;
  readonly confidence?: number;
  readonly confidence_basis?: string;
  readonly environment?: string;
  readonly evidence_kind?: string;
  readonly outcome?: string;
  readonly path?: string;
  readonly provenance_only?: boolean;
  readonly rationale?: string;
  readonly relationship_type?: string;
  readonly resolution_source?: string;
  readonly resolved_id?: string;
  readonly source_freshness?: string;
  readonly source_repo_id?: string;
  readonly source_repo_name?: string;
  readonly state?: string;
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
  [...contextRows, ...traceRows].forEach((row) => {
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

export function uniqueDeploymentArtifacts(
  rows: readonly DeploymentArtifactRecord[],
): DeploymentArtifactRecord[] {
  return uniqueRecords(rows, (row) =>
    [
      row.source_repo_id,
      row.target_repo_id,
      row.relationship_type,
      row.artifact_family,
      row.evidence_kind,
      row.path,
      row.environment,
    ].join("\u0000"),
  );
}

export function uniqueNetworkPaths(rows: readonly NetworkPathRecord[]): NetworkPathRecord[] {
  return uniqueRecords(rows, (row) =>
    [row.from, row.to, row.path_type, row.environment, row.reason].join("\u0000"),
  );
}

export function uniqueNamedRecords(
  rows: readonly NamedDeploymentRecord[],
): NamedDeploymentRecord[] {
  return uniqueRecords(rows, (row) =>
    [row.id, row.name, row.target, row.type, row.environment].join("\u0000"),
  );
}

function uniquePlatforms(rows: readonly DeploymentPlatformRecord[]): DeploymentPlatformRecord[] {
  return uniqueRecords(rows, (row) => `${row.platform_kind ?? ""}\u0000${row.platform_name ?? ""}`);
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
