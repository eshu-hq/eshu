import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { EshuTruth } from "./envelope";
import { buildDeploymentStoryGraph } from "./eshuGraphDeploymentModel";
import type {
  DeploymentGraphDetail,
  DeploymentTraceResponse,
  ServiceDeploymentContextResponse,
} from "./eshuGraphDeploymentWire";
import { loadEntityMapGraph } from "./eshuGraphNeighborhood";
import { cleanText } from "./eshuGraphShared";
import type { GraphModel } from "../console/types";

export type {
  DeploymentGraphDetail,
  DeploymentTraceResponse,
  ServiceDeploymentContextResponse,
} from "./eshuGraphDeploymentWire";

interface RepositoryDeploymentContextResponse {
  readonly repository?: { readonly id?: string; readonly name?: string };
  readonly deployment_evidence?: ServiceDeploymentContextResponse["deployment_evidence"];
}

export interface DeploymentGraphOptions {
  readonly contextTruth?: EshuTruth | null;
  readonly detail?: DeploymentGraphDetail;
  readonly traceTruth?: EshuTruth | null;
}

export function deploymentStoryToGraph(
  data: ServiceDeploymentContextResponse,
  fallbackName: string,
  trace: DeploymentTraceResponse = {},
  options: DeploymentGraphOptions = {},
): GraphModel {
  return buildDeploymentStoryGraph(data, fallbackName, trace, options);
}

// Composes the service-context and deployment-trace contracts. The trace call
// is intentionally not treated as optional: silently falling back after an
// authorization or runtime failure would make a partial graph look complete.
export async function loadEntityStoryGraph(
  client: EshuApiClient,
  name: string,
  repoID?: string,
  detail: DeploymentGraphDetail = "summary",
): Promise<GraphModel> {
  const deploymentClient = client as EshuApiClient & {
    readonly get?: EshuApiClient["get"];
  };
  if (typeof deploymentClient.get !== "function") return loadEntityMapGraph(client, name);
  try {
    const contextEnvelope = await deploymentClient.get<ServiceDeploymentContextResponse>(
      `/api/v0/services/${encodeURIComponent(name)}/context`,
    );
    if (contextEnvelope.error) throw new EshuEnvelopeError(contextEnvelope.error);

    const context = contextEnvelope.data ?? {};
    const serviceName = cleanText(context.name) || name;
    const traceEnvelope = await client.post<DeploymentTraceResponse>(
      "/api/v0/impact/trace-deployment-chain",
      {
        direct_only: true,
        include_related_module_usage: false,
        max_depth: 2,
        service_name: serviceName,
      },
    );
    if (traceEnvelope.error) throw new EshuEnvelopeError(traceEnvelope.error);

    const graph = deploymentStoryToGraph(context, name, traceEnvelope.data ?? {}, {
      contextTruth: contextEnvelope.truth,
      detail,
      traceTruth: traceEnvelope.truth,
    });
    if (graph.nodes.length > 1 || graph.edges.length > 0) return graph;
  } catch (error) {
    if (!shouldFallbackFromServiceContext(error)) throw error;
  }

  const repositoryGraph = await loadRepositoryDeploymentStoryGraph(
    deploymentClient,
    name,
    repoID,
    detail,
  );
  if (repositoryGraph !== null) return repositoryGraph;
  return loadEntityMapGraph(client, name);
}

async function loadRepositoryDeploymentStoryGraph(
  client: EshuApiClient,
  name: string,
  repoID: string | undefined,
  detail: DeploymentGraphDetail,
): Promise<GraphModel | null> {
  const id = cleanText(repoID);
  if (id === "") return null;
  try {
    const envelope = await client.get<RepositoryDeploymentContextResponse>(
      `/api/v0/repositories/${encodeURIComponent(id)}/context`,
    );
    if (envelope.error) throw new EshuEnvelopeError(envelope.error);
    const data = envelope.data ?? {};
    const repoName = cleanText(data.repository?.name) || name;
    const graph = deploymentStoryToGraph(
      {
        name,
        repo_id: id,
        repo_name: repoName,
        deployment_evidence: data.deployment_evidence,
      },
      name,
      {},
      { contextTruth: envelope.truth, detail },
    );
    return graph.nodes.length > 1 || graph.edges.length > 0 ? graph : null;
  } catch (error) {
    if (shouldFallbackFromServiceContext(error)) return null;
    throw error;
  }
}

function shouldFallbackFromServiceContext(error: unknown): boolean {
  if (error instanceof EshuApiHttpError) return error.status === 404;
  if (!(error instanceof EshuEnvelopeError)) return false;
  const code = error.error.code.toLowerCase();
  return code === "not_found" || code === "service_not_found" || code === "unknown_service";
}
