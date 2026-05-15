import type { EvidenceDrilldown, EvidenceMetric } from "./mockData";
import type {
  ContextConsumer,
  ContextResponse,
  DeploymentEvidenceArtifact,
  StorySection
} from "./repository";
import { isPresent, nonEmpty } from "./repositoryText";
import type { ServiceContextResponse } from "./serviceSpotlight";

export function drilldownForStorySection(
  section: StorySection,
  context: ContextResponse | undefined
): EvidenceDrilldown | undefined {
  switch (section.title) {
    case "api":
      return apiDrilldown(context);
    case "consumers":
      return consumersDrilldown(context);
    case "deployment":
      return deploymentShapeDrilldown(context);
    default:
      return undefined;
  }
}

export function deploymentArtifactDrilldown(
  family: string,
  artifacts: readonly DeploymentEvidenceArtifact[]
): EvidenceDrilldown {
  return {
    metrics: compactMetrics([
      { label: "Family", value: humanizeToken(family) },
      { label: "Evidence items", value: String(artifacts.length) },
      {
        label: "Environments",
        value: String(new Set(artifacts.map((artifact) => artifact.environment).filter(isPresent)).size)
      }
    ]),
    summary:
      "Artifact evidence is raw proof from deployment files. Use it to verify which repository and path produced the relationship.",
    table: {
      ariaLabel: "Deployment artifact evidence",
      columns: [
        { key: "repo", label: "Repository" },
        { key: "path", label: "Path" },
        { key: "verb", label: "Verb" },
        { key: "environment", label: "Environment" }
      ],
      rows: artifacts.slice(0, 12).map((artifact, index) => ({
        cells: {
          environment: artifact.environment ?? "not published",
          path: nonEmpty(artifact.source_location?.path, artifact.path, artifact.name, "not published"),
          repo: nonEmpty(artifact.source_repo_name, artifact.source_location?.repo_name, "not published"),
          verb: nonEmpty(artifact.relationship_type, artifact.evidence_kind, "deployment_evidence")
        },
        id: `artifact:${artifact.source_repo_name ?? family}:${artifact.path ?? index}:${index}`
      }))
    }
  };
}

function apiDrilldown(context: ContextResponse | undefined): EvidenceDrilldown | undefined {
  const apiSurface = context?.api_surface;
  if (apiSurface === undefined) {
    return undefined;
  }
  const endpoints = apiSurface.endpoints ?? [];
  const sourcePaths = apiSurface.source_paths ?? [];
  return {
    metrics: compactMetrics([
      { label: "Endpoints", value: String(apiSurface.endpoint_count ?? endpoints.length) },
      { label: "Methods", value: String(apiSurface.method_count ?? countEndpointMethods(endpoints)) },
      { label: "Spec files", value: String(sourcePaths.length) }
    ]),
    summary:
      "API evidence lists observed routes, HTTP methods, operation identifiers, and source files when Eshu has them.",
    table: {
      ariaLabel: "API endpoint evidence",
      columns: [
        { key: "path", label: "Endpoint" },
        { key: "methods", label: "Methods" },
        { key: "operation", label: "Operation" },
        { key: "source", label: "Source" }
      ],
      rows: endpoints.slice(0, 12).map((endpoint, index) => ({
        cells: {
          methods: normalizeList(endpoint.methods).map((method) => method.toUpperCase()).join(", "),
          operation: normalizeList(endpoint.operation_ids).join(", ") || "not published",
          path: nonEmpty(endpoint.path, "/"),
          source: normalizeList(endpoint.source_paths).join(", ") || sourcePaths.join(", ") || "not published"
        },
        id: `endpoint:${endpoint.path ?? index}:${index}`
      }))
    }
  };
}

function consumersDrilldown(context: ContextResponse | undefined): EvidenceDrilldown | undefined {
  const consumers = [
    ...(context?.consumer_repositories ?? []),
    ...(context?.consumers ?? []),
    ...(context?.graph_dependents ?? [])
  ];
  if (consumers.length === 0) {
    return undefined;
  }
  return {
    metrics: compactMetrics([
      { label: "Consumers", value: String(consumers.length) },
      { label: "Sample paths", value: String(consumers.flatMap((consumer) => consumer.sample_paths ?? []).length) }
    ]),
    summary:
      "Consumer evidence shows repositories that reference this service through graph relationships or content matches.",
    table: {
      ariaLabel: "Consumer evidence",
      columns: [
        { key: "repository", label: "Repository" },
        { key: "evidence", label: "Evidence" },
        { key: "path", label: "Sample path" }
      ],
      rows: consumers.slice(0, 12).map((consumer, index) => ({
        cells: {
          evidence: normalizeList([
            ...(consumer.evidence_kinds ?? []),
            ...(consumer.consumer_kinds ?? [])
          ]).join(", ") || "reference",
          path: consumer.sample_paths?.[0] ?? "not published",
          repository: consumerName(consumer)
        },
        id: `consumer:${consumerName(consumer)}:${index}`
      }))
    }
  };
}

function deploymentShapeDrilldown(
  context: ContextResponse | undefined
): EvidenceDrilldown | undefined {
  const lanes = context?.deployment_lanes ?? [];
  if (lanes.length === 0) {
    return undefined;
  }
  return {
    metrics: compactMetrics([
      { label: "Deployment lanes", value: String(lanes.length) },
      { label: "Source repos", value: String(new Set(lanes.flatMap((lane) => lane.source_repositories ?? [])).size) },
      { label: "Environments", value: String(new Set(lanes.flatMap((lane) => lane.environments ?? [])).size) }
    ]),
    summary:
      "Deployment shape separates runtime lanes from source repositories so Kubernetes, ArgoCD, Helm, Terraform, and ECS are not flattened together.",
    table: {
      ariaLabel: "Deployment lane evidence",
      columns: [
        { key: "lane", label: "Lane" },
        { key: "sources", label: "Sources" },
        { key: "environments", label: "Environments" },
        { key: "verbs", label: "Relationship verbs" }
      ],
      rows: lanes.slice(0, 12).map((lane, index) => ({
        cells: {
          environments: normalizeList(lane.environments).join(", ") || "not published",
          lane: humanizeToken(lane.lane_type ?? "deployment"),
          sources: normalizeList(lane.source_repositories).join(", ") || "not published",
          verbs: normalizeList(lane.relationship_types).join(", ") || "not published"
        },
        id: `lane:${lane.lane_type ?? index}:${index}`
      }))
    }
  };
}

function compactMetrics(metrics: readonly EvidenceMetric[]): readonly EvidenceMetric[] {
  return metrics.filter((metric) => metric.value.trim().length > 0);
}

function countEndpointMethods(
  endpoints: NonNullable<ServiceContextResponse["api_surface"]>["endpoints"]
): number {
  return (endpoints ?? []).reduce(
    (total, endpoint) => total + (endpoint.methods?.length ?? 0),
    0
  );
}

function consumerName(consumer: ContextConsumer): string {
  return nonEmpty(consumer.repo_name, consumer.repository, consumer.name, consumer.id);
}

function normalizeList(
  values: readonly (string | undefined)[] | readonly string[] | null | undefined
): readonly string[] {
  return (values ?? []).filter((value): value is string => isPresent(value));
}

function humanizeToken(value: string): string {
  return value
    .split(/[_-]+/)
    .filter((token) => token.length > 0)
    .map((token) => `${token.charAt(0).toUpperCase()}${token.slice(1).toLowerCase()}`)
    .join(" ");
}
