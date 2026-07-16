// Single production route-path source of truth shared by the React router and
// the live console gate. A path added to the app cannot silently escape the
// catalog parity test.
export const APP_ROUTE_PATHS = Object.freeze({
  root: "/",
  status: "/status",
  dashboard: "/dashboard",
  ask: "/ask",
  semanticSearch: "/semantic-search",
  guidedQuestions: "/guided-questions",
  impact: "/impact",
  exposure: "/exposure",
  changedSince: "/changed-since",
  explorer: "/explorer",
  relationships: "/relationships",
  serviceStory: "/service-story",
  serviceStoryDetail: "/service-story/:serviceName",
  serviceReport: "/service-report",
  serviceReportDetail: "/service-report/:serviceName",
  nodes: "/nodes",
  codeGraph: "/code-graph",
  repositories: "/repositories",
  repositorySource: "/repositories/:id/source",
  cloud: "/cloud",
  ciCdRunCorrelations: "/ci-cd/run-correlations",
  cloudDrift: "/cloud-drift",
  secretsIam: "/secrets-iam",
  topology: "/topology",
  incidents: "/incidents",
  incidentContext: "/incidents/:incidentId/context",
  catalog: "/catalog",
  images: "/images",
  capabilities: "/capabilities",
  surfaceInventory: "/surface-inventory",
  iac: "/iac",
  replatforming: "/replatforming",
  findings: "/findings",
  deadCode: "/dead-code",
  vulnerabilities: "/vulnerabilities",
  vulnerabilityDetail: "/vulnerabilities/:id",
  sbom: "/sbom",
  dependencies: "/dependencies",
  observability: "/observability",
  collectorReadiness: "/collector-readiness",
  operations: "/operations",
  freshnessCausality: "/freshness-causality",
  profile: "/profile",
  admin: "/admin",
  workspace: "/workspace/:entityKind/:entityId",
} as const);

export type AppRoutePath = (typeof APP_ROUTE_PATHS)[keyof typeof APP_ROUTE_PATHS];

export function missingProductionRoutePaths(
  catalogPaths: readonly string[],
): readonly AppRoutePath[] {
  const covered = new Set(catalogPaths);
  return Object.values(APP_ROUTE_PATHS).filter((path) => !covered.has(path));
}
