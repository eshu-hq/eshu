// Declarative catalog for the private/live console gate. Keep browser execution
// and evaluation in their own modules so this inventory remains easy to audit
// against App.tsx and the sidebar navigation.

export interface ConsoleRoute {
  readonly path: string;
  readonly label: string;
  readonly area: RouteArea;
  readonly workflow?: RouteWorkflowSpec;
}

export interface WorkflowField {
  readonly selector: string;
  readonly value: string;
}

export type RouteWorkflowSpec =
  | {
      readonly id: string;
      readonly kind: "state";
      readonly anySelectors: readonly string[];
      readonly forbiddenText?: string;
    }
  | {
      readonly id: string;
      readonly kind: "fill";
      readonly selector: string;
      readonly value: string;
      readonly outcomeSelector?: string;
      readonly outcomeTextIncludes?: string;
      readonly requireOutcomeChange?: boolean;
      readonly expectedRequestPath?: string;
      readonly forbiddenText?: string;
    }
  | {
      readonly id: string;
      readonly kind: "click";
      readonly role: "button" | "tab";
      readonly name: string;
      readonly outcomeSelector: string;
      readonly forbiddenText?: string;
    }
  | {
      readonly id: string;
      readonly kind: "submit";
      readonly fields: readonly WorkflowField[];
      readonly role: "button";
      readonly name: string;
      readonly expectedRequestPath: string;
      readonly outcomeSelector: string;
      readonly forbiddenText?: string;
    };

export type RouteArea =
  | "dashboard"
  | "repositories"
  | "service"
  | "graph"
  | "cloud"
  | "observability"
  | "operations"
  | "security"
  | "ask"
  | "system";

export interface NetworkAllowRule {
  readonly method: string;
  readonly pathname: string;
  readonly status: number;
  readonly reason: string;
}

// Every entry is a real route enumerated from apps/console/src/App.tsx.
// Parameterized routes are exercised through their base listing route.
export const consoleRoutes: readonly ConsoleRoute[] = [
  { path: "/", label: "Dashboard", area: "dashboard" },
  { path: "/dashboard", label: "Dashboard (alias)", area: "dashboard" },
  {
    path: "/status",
    label: "Status",
    area: "operations",
    workflow: {
      id: "status-live-overview",
      kind: "state",
      anySelectors: [".status-hero"],
      forbiddenText: "Status is unavailable from this source.",
    },
  },
  { path: "/repositories", label: "Repositories", area: "repositories" },
  { path: "/catalog", label: "Service Catalog", area: "service" },
  { path: "/service-story", label: "Service Story", area: "service" },
  { path: "/service-report", label: "Service Report", area: "service" },
  { path: "/explorer", label: "Explorer", area: "graph" },
  {
    path: "/code-graph",
    label: "Code Graph",
    area: "graph",
    workflow: {
      id: "code-graph-live-canvas",
      kind: "state",
      anySelectors: [".gcanvas-svg", ".gcanvas .empty"],
      forbiddenText: "Failed to load live dead-code candidates:",
    },
  },
  { path: "/topology", label: "Topology", area: "graph" },
  {
    path: "/cloud",
    label: "Cloud",
    area: "cloud",
    workflow: {
      id: "cloud-resource-inventory",
      kind: "state",
      anySelectors: [".gcanvas-svg", ".page .empty"],
      forbiddenText: "Failed to load:",
    },
  },
  { path: "/cloud-drift", label: "Cloud Drift", area: "cloud" },
  { path: "/iac", label: "IaC", area: "cloud" },
  { path: "/images", label: "Images", area: "cloud" },
  { path: "/observability", label: "Observability", area: "observability" },
  { path: "/incidents", label: "Incidents", area: "observability" },
  { path: "/freshness-causality", label: "Freshness", area: "observability" },
  {
    path: "/operations",
    label: "Operations",
    area: "operations",
    workflow: {
      id: "operations-live-stages",
      kind: "state",
      anySelectors: [".ops-stage-tiles"],
      forbiddenText: "Live operations board is unavailable from this source.",
    },
  },
  { path: "/collector-readiness", label: "Collector Readiness", area: "operations" },
  { path: "/capabilities", label: "Capabilities", area: "operations" },
  {
    path: "/surface-inventory",
    label: "Surface Inventory",
    area: "operations",
    workflow: {
      id: "surface-inventory-filter",
      kind: "fill",
      selector: 'input[placeholder="Filter surfaces…"]',
      value: "reducer",
      outcomeSelector: "table tbody",
      requireOutcomeChange: true,
      forbiddenText: "Surface inventory is unavailable from this source.",
    },
  },
  { path: "/findings", label: "Findings", area: "security" },
  {
    path: "/vulnerabilities",
    label: "Vulnerabilities",
    area: "security",
    workflow: {
      id: "vulnerabilities-catalog-tab",
      kind: "click",
      role: "tab",
      name: "Known intelligence (catalog)",
      outcomeSelector: 'input[aria-label="Search advisories"]',
      forbiddenText: "The vulnerability-intelligence catalog is unavailable",
    },
  },
  { path: "/secrets-iam", label: "Secrets & IAM", area: "security" },
  { path: "/sbom", label: "SBOM", area: "security" },
  { path: "/exposure", label: "Exposure Path", area: "security" },
  { path: "/dependencies", label: "Dependencies", area: "security" },
  {
    path: "/dead-code",
    label: "Dead Code",
    area: "graph",
    workflow: {
      id: "dead-code-trait-filter",
      kind: "fill",
      selector: 'input[aria-label="Find dead-code candidate"]',
      value: "Trait",
      outcomeSelector: ".evidence-workbench tbody",
      outcomeTextIncludes: "Trait",
      requireOutcomeChange: true,
      forbiddenText: "Failed to load:",
    },
  },
  {
    path: "/relationships",
    label: "Relationships",
    area: "graph",
    workflow: {
      id: "relationships-live-verbs",
      kind: "state",
      anySelectors: [".rel-verb-row", ".rel-layout .empty"],
      forbiddenText: "Relationships unavailable:",
    },
  },
  {
    path: "/nodes",
    label: "Nodes",
    area: "graph",
    workflow: {
      id: "nodes-filter",
      kind: "fill",
      selector: 'input[aria-label="Find a node by name, type or account"]',
      value: "repository",
      outcomeSelector: ".panel-body",
      expectedRequestPath: "/api/v0/graph/entities",
      forbiddenText: "Graph entity inventory unavailable from this source.",
    },
  },
  { path: "/impact", label: "Impact", area: "graph" },
  { path: "/changed-since", label: "Changed Since", area: "graph" },
  { path: "/replatforming", label: "Replatforming", area: "service" },
  { path: "/ci-cd/run-correlations", label: "CI/CD Run Correlations", area: "operations" },
  {
    path: "/ask",
    label: "Ask Eshu",
    area: "ask",
    workflow: {
      id: "ask-live-answer",
      kind: "submit",
      fields: [
        {
          selector: 'textarea[aria-label="Ask Eshu a question"]',
          value: "Which repositories are indexed?",
        },
      ],
      role: "button",
      name: "Ask Eshu",
      expectedRequestPath: "/api/v0/ask",
      outcomeSelector: ".answer-panel",
    },
  },
  {
    path: "/semantic-search",
    label: "Semantic Search",
    area: "ask",
    workflow: {
      id: "semantic-search-live-result",
      kind: "submit",
      fields: [
        { selector: 'input[aria-label="Repository"]', value: "local" },
        { selector: 'input[aria-label="Search query"]', value: "deployment entrypoints" },
      ],
      role: "button",
      name: "Search",
      expectedRequestPath: "/api/v0/search/semantic",
      outcomeSelector: ".sem-result-announce",
    },
  },
  { path: "/guided-questions", label: "Guided Questions", area: "ask" },
  {
    path: "/profile",
    label: "Profile",
    area: "system",
    workflow: {
      id: "profile-current-session",
      kind: "state",
      anySelectors: ['tr[aria-current="true"]', ".empty-note"],
      forbiddenText: "Profile unavailable from this source.",
    },
  },
  {
    path: "/admin",
    label: "Admin",
    area: "system",
    workflow: {
      id: "admin-sign-in-policy-tab",
      kind: "click",
      role: "tab",
      name: "Sign-in policy",
      outcomeSelector: "#identity-access-panel-sign-in-policy",
    },
  },
];

// Only the exact browser-session fallback handshake is justified. The route
// evaluator matches pathname, method, and status exactly.
export const defaultNetworkAllowList: readonly NetworkAllowRule[] = [
  {
    method: "GET",
    pathname: "/eshu-api/api/v0/auth/browser-session",
    status: 401,
    reason: "no browser-session cookie; configured shared-key fallback follows",
  },
  {
    method: "POST",
    pathname: "/eshu-api/api/v0/auth/browser-session",
    status: 400,
    reason: "all-scope shared key has no tenant context; bearer fallback follows",
  },
];
