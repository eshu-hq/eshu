import type { EshuApiClient } from "./client";

export interface DeploymentConfigInfluence {
  readonly coverage: DeploymentConfigCoverage;
  readonly repositories: readonly DeploymentConfigRepository[];
  readonly sections: readonly DeploymentConfigSection[];
  readonly serviceName: string;
  readonly summary: string;
}

export interface DeploymentConfigCoverage {
  readonly limit: number;
  readonly queryShape: string;
  readonly truncated: boolean;
}

export interface DeploymentConfigRepository {
  readonly name: string;
  readonly roles: readonly string[];
}

export interface DeploymentConfigSection {
  readonly count: number;
  readonly items: readonly DeploymentConfigItem[];
  readonly label: string;
}

export interface DeploymentConfigItem {
  readonly action?: string;
  readonly evidenceKind: string;
  readonly label: string;
  readonly line?: number;
  readonly path: string;
  readonly repoName: string;
  readonly value: string;
}

export interface LoadDeploymentConfigInfluenceOptions {
  readonly environment?: string;
  readonly serviceName: string;
}

export interface DeploymentConfigInfluenceResponse {
  readonly coverage?: {
    readonly limit?: number;
    readonly query_shape?: string;
    readonly truncated?: boolean;
  };
  readonly image_tag_sources?: readonly DeploymentConfigRow[];
  readonly influencing_repositories?: readonly DeploymentConfigRepoRow[];
  readonly read_first_files?: readonly DeploymentConfigRow[];
  readonly rendered_targets?: readonly DeploymentConfigRow[];
  readonly resource_limit_sources?: readonly DeploymentConfigRow[];
  readonly runtime_setting_sources?: readonly DeploymentConfigRow[];
  readonly service_name?: string;
  readonly story?: string;
  readonly values_layers?: readonly DeploymentConfigRow[];
}

interface DeploymentConfigRepoRow {
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly roles?: readonly string[];
}

interface DeploymentConfigRow {
  readonly end_line?: number;
  readonly entity_name?: string;
  readonly evidence_kind?: string;
  readonly image_ref?: string;
  readonly kind?: string;
  readonly matched_alias?: string;
  readonly matched_value?: string;
  readonly name?: string;
  readonly namespace?: string;
  readonly next_call?: string;
  readonly reason?: string;
  readonly relative_path?: string;
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly source?: string;
  readonly start_line?: number;
}

export async function loadDeploymentConfigInfluence(
  client: EshuApiClient,
  options: LoadDeploymentConfigInfluenceOptions
): Promise<DeploymentConfigInfluence> {
  const response = await client.postJson<DeploymentConfigInfluenceResponse>(
    "/api/v0/impact/deployment-config-influence",
    {
      environment: options.environment,
      limit: 25,
      service_name: options.serviceName
    }
  );
  return deploymentConfigInfluenceFromResponse(response);
}

export function deploymentConfigInfluenceFromResponse(
  response: DeploymentConfigInfluenceResponse
): DeploymentConfigInfluence {
  return {
    coverage: {
      limit: response.coverage?.limit ?? 25,
      queryShape: response.coverage?.query_shape ?? "deployment_config_influence_story",
      truncated: response.coverage?.truncated ?? false
    },
    repositories: repositoryRows(response.influencing_repositories ?? []),
    sections: [
      section("Values layers", response.values_layers ?? []),
      section("Image tags", response.image_tag_sources ?? []),
      section("Runtime settings", response.runtime_setting_sources ?? []),
      section("Resource limits", response.resource_limit_sources ?? []),
      section("Rendered targets", response.rendered_targets ?? []),
      section("Read first", response.read_first_files ?? [])
    ],
    serviceName: response.service_name ?? "",
    summary: response.story ?? ""
  };
}

function repositoryRows(
  rows: readonly DeploymentConfigRepoRow[]
): readonly DeploymentConfigRepository[] {
  return rows.map((row) => ({
    name: nonEmpty(row.repo_name, row.repo_id),
    roles: [...(row.roles ?? [])].sort()
  }));
}

function section(
  label: string,
  rows: readonly DeploymentConfigRow[]
): DeploymentConfigSection {
  const items = rows.map(itemFromRow);
  return {
    count: rows.length,
    items,
    label
  };
}

function itemFromRow(row: DeploymentConfigRow): DeploymentConfigItem {
  const target = nonEmpty(row.matched_alias, row.name, row.entity_name, row.kind, row.image_ref);
  const renderedTarget = [row.kind, row.namespace, row.name, row.entity_name]
    .filter((value): value is string => value !== undefined && value.trim().length > 0)
    .join(" / ");
  return {
    action: row.next_call,
    evidenceKind: nonEmpty(row.evidence_kind, row.source, row.reason),
    label: nonEmpty(target, renderedTarget, "evidence"),
    line: row.start_line,
    path: row.relative_path ?? "",
    repoName: nonEmpty(row.repo_name, row.repo_id),
    value: nonEmpty(row.matched_value, row.image_ref, renderedTarget)
  };
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
