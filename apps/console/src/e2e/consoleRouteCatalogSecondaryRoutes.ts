import {
  bootstrapOwnership,
  getResponse,
  postResponse,
  routeOwnership,
} from "./consoleRouteResponseOwnership";
import { type ConsoleRoute, liveState } from "./consoleRouteCatalogTypes";

export const secondaryConsoleRoutes: readonly ConsoleRoute[] = [
  {
    path: "/operations",
    label: "Operations",
    area: "operations",
    workflow: {
      id: "operations-live-stages",
      kind: "state",
      anySelectors: [".ops-stage-tiles"],
      requiredResponses: [getResponse("/api/v0/status/operations")],
      forbiddenText: "Live operations board is unavailable from this source.",
    },
  },
  {
    path: "/collector-readiness",
    label: "Collector Readiness",
    area: "operations",
    workflow: liveState(
      "collector-readiness-live-table",
      [".collector-readiness-table tbody tr"],
      [],
      [
        {
          selector: ".collector-readiness-page .empty",
          exactText: "No collector readiness rows from this source.",
        },
      ],
      bootstrapOwnership(getResponse("/api/v0/status/collector-readiness")),
    ),
  },
  {
    path: "/capabilities",
    label: "Capabilities",
    area: "operations",
    workflow: liveState(
      "capabilities-live-matrix",
      [".table-scroll .tbl.wide tbody .t-name"],
      ["Capability matrix unavailable from this source."],
      [
        {
          selector: ".table-scroll .tbl.wide tbody td.empty",
          exactText: "No capabilities from this source.",
        },
      ],
      routeOwnership(getResponse("/api/v0/capabilities")),
    ),
  },
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
  {
    path: "/findings",
    label: "Findings",
    area: "security",
    workflow: {
      id: "findings-live-worklist",
      kind: "state",
      anySelectors: ['.findings-summary[data-source-ready="true"] ~ .panel [data-finding-row]'],
      emptyStates: [
        {
          selector:
            '.findings-summary[data-source-ready="true"] ~ .panel [data-authoritative-empty="true"]',
          exactText: "No findings from this source.",
        },
      ],
      requiredBootstrapResponses: [
        { path: "/api/v0/code/dead-code", method: "POST", acceptedStatuses: [200] },
        {
          path: "/api/v0/supply-chain/impact/findings",
          method: "GET",
          acceptedStatuses: [200],
          query: { impact_status: "affected_exact" },
        },
        {
          path: "/api/v0/supply-chain/impact/findings",
          method: "GET",
          acceptedStatuses: [200],
          query: { impact_status: "affected_derived" },
        },
      ],
      forbiddenSelectors: [".src-err", ".async-guard-error"],
    },
  },
  {
    path: "/vulnerabilities",
    label: "Vulnerabilities",
    area: "security",
    workflow: {
      id: "vulnerabilities-live-tabs",
      kind: "tabs",
      proveVulnerabilityServiceTruth: true,
      tabs: [
        {
          name: "Reachable in services",
          outcomeSelector: ".supply-chain-register-grid",
          forbiddenSelectors: [".async-guard-error"],
        },
        {
          name: "Known intelligence (catalog)",
          outcomeSelector: 'input[aria-label="Search advisories"]',
          forbiddenTexts: ["The vulnerability-intelligence catalog is unavailable"],
        },
      ],
      followLink: {
        selector: 'a[href^="/vulnerabilities/"]',
        expectedPathPrefix: "/vulnerabilities/",
        expectedRequestPathPrefix: "/api/v0/supply-chain/vulnerabilities/",
        expectedRequestMethod: "GET",
        acceptedResponseStatuses: [200],
        outcomeSelector: ".page .kv",
        forbiddenText: "Advisory unavailable",
        forbiddenSelectors: [".src-err"],
      },
    },
  },
  {
    path: "/secrets-iam",
    label: "Secrets & IAM",
    area: "security",
    workflow: {
      id: "secrets-iam-retained-scope",
      kind: "submit",
      fields: [
        {
          requestKey: "scope_id",
          selector: 'input[aria-label="Scope id"]',
          valueEnv: "ESHU_E2E_SECRETS_SCOPE_ID",
        },
      ],
      role: "button",
      name: "Load posture",
      scopeSelector: ".secrets-iam-query",
      expectedRequestPath: "/api/v0/secrets-iam/posture-summary",
      expectedRequestMethod: "GET",
      acceptedResponseStatuses: [200],
      outcomeSelector: ".secrets-iam-truth",
      forbiddenSelectors: [".secrets-iam-page .src-err"],
      forbiddenText: "No secrets/IAM posture data loaded.",
    },
  },
  {
    path: "/sbom",
    label: "SBOM",
    area: "security",
    workflow: liveState(
      "sbom-live-workbench",
      ['[aria-label="SBOM evidence workbench"] tbody .t-name'],
      ["Attachment detail unavailable from this source."],
      [
        {
          selector: '[aria-label="SBOM evidence workbench"] tbody td.empty',
          exactText: "No SBOM/attestation subjects from this source.",
        },
      ],
      routeOwnership(getResponse("/api/v0/supply-chain/sbom-attestations/attachments/inventory")),
    ),
  },
  {
    path: "/exposure",
    label: "Exposure Path",
    area: "security",
    workflow: {
      id: "exposure-path-retained-service",
      kind: "submit",
      fields: [
        {
          selector: 'input[aria-label="Service name"]',
          valueEnv: "ESHU_E2E_SERVICE_NAME",
        },
      ],
      role: "button",
      name: "Trace ingress",
      scopeSelector: ".exposure-entry-form",
      expectedRequestPath: "/api/v0/services/${ESHU_E2E_SERVICE_NAME}/context",
      expectedRequestMethod: "GET",
      acceptedResponseStatuses: [200],
      outcomeSelector: ".exposure-result, .exposure-unresolved-panel",
      forbiddenSelectors: [".exposure-page .src-err"],
      forbiddenText: "Enter an internet-facing service to trace its ingress chain.",
    },
  },
  {
    path: "/dependencies",
    label: "Dependencies",
    area: "security",
    workflow: liveState(
      "dependencies-live-workbench",
      ['[aria-label="Package graph workbench"] tbody .t-name'],
      ["Package graph unavailable from this source."],
      [
        {
          selector: '[aria-label="Package graph workbench"] tbody td.empty',
          exactText:
            "No package dependencies in the indexed package graph yet - requires the package registry collector.",
        },
      ],
      routeOwnership(getResponse("/api/v0/dependencies")),
    ),
  },
  {
    path: "/dead-code",
    label: "Dead Code",
    area: "graph",
    workflow: {
      id: "dead-code-exact-kind-filter",
      kind: "exactKind",
      groupSelector: '[aria-label="Dead-code kind filter"]',
      preferredName: "Trait",
      outcomeCellSelector: ".evidence-workbench tbody tr.cloud-row td:nth-child(2)",
      expectedRequestPath: "/api/v0/code/dead-code",
      expectedRequestMethod: "POST",
      acceptedResponseStatuses: [200],
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
      anySelectors: [".rel-verb-row"],
      requiredResponses: [
        {
          path: "/api/v0/relationships/catalog",
          method: "POST",
          acceptedStatuses: [200],
        },
      ],
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
      requestKey: "q",
      outcomeSelector: ".panel-body",
      expectedRequestPath: "/api/v0/graph/entities",
      expectedRequestMethod: "GET",
      acceptedResponseStatuses: [200],
      forbiddenText: "Graph entity inventory unavailable from this source.",
    },
  },
  {
    path: "/impact",
    label: "Impact",
    area: "graph",
    workflow: liveState(
      "impact-live-evidence",
      [".impact-truth"],
      [],
      [],
      routeOwnership(
        postResponse("/api/v0/impact/change-surface/investigate"),
        postResponse("/api/v0/impact/trace-deployment-chain"),
      ),
    ),
  },
  {
    path: "/changed-since",
    label: "Changed Since",
    area: "graph",
    workflow: liveState(
      "changed-since-live-evidence",
      [".changed-since-summary"],
      ["Changed-since data unavailable", "No changed-since data loaded."],
      [],
      routeOwnership(
        getResponse("/api/v0/freshness/generations"),
        getResponse("/api/v0/freshness/changed-since"),
      ),
    ),
  },
  {
    path: "/replatforming",
    label: "Replatforming",
    area: "service",
    workflow: {
      id: "replatforming-retained-scope",
      kind: "submit",
      fields: [
        {
          requestKey: "scope_id",
          selector: 'input[aria-label="Scope id"]',
          valueEnv: "ESHU_E2E_CLOUD_SCOPE_ID",
        },
      ],
      role: "button",
      name: "Review plan",
      scopeSelector: ".replatforming-query",
      expectedRequestPath: "/api/v0/replatforming/rollups",
      expectedRequestMethod: "POST",
      acceptedResponseStatuses: [200],
      outcomeSelector: ".replatforming-truth",
      forbiddenSelectors: [".replatforming-page .src-err"],
    },
  },
  {
    path: "/ci-cd/run-correlations",
    label: "CI/CD Run Correlations",
    area: "operations",
    workflow: liveState(
      "ci-cd-live-correlations",
      [".cicd-truth"],
      [],
      [],
      routeOwnership(
        getResponse("/api/v0/ci-cd/run-correlations/count"),
        getResponse("/api/v0/ci-cd/run-correlations/inventory"),
      ),
    ),
  },
  {
    path: "/ask",
    label: "Ask Eshu",
    area: "ask",
    workflow: {
      id: "ask-live-exact-indexed-repository-count",
      kind: "askExactCount",
      prompt:
        "How many repositories are currently indexed? Return the count and cite the evidence used.",
      fieldSelector: 'textarea[aria-label="Ask Eshu a question"]',
      role: "button",
      name: "Ask Eshu",
      expectedRequestPath: "/api/v0/ask",
      acceptedResponseStatuses: [200],
      outcomeSelector: ".answer-panel",
      resultRef: "eshu://api-result/repositories",
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
        {
          requestKey: "repo_id",
          selector: 'input[aria-label="Repository"]',
          valueEnv: "ESHU_E2E_SEMANTIC_REPOSITORY_ID",
        },
        {
          requestKey: "query",
          selector: 'input[aria-label="Search query"]',
          valueEnv: "ESHU_E2E_SEMANTIC_QUERY",
        },
      ],
      role: "button",
      name: "Search",
      scopeSelector: ".semantic-search-form",
      expectedRequestPath: "/api/v0/search/semantic",
      expectedRequestMethod: "POST",
      acceptedResponseStatuses: [200],
      outcomeSelector: ".sem-result-row",
      forbiddenSelectors: [".src-err"],
    },
  },
  {
    path: "/guided-questions",
    label: "Guided Questions",
    area: "ask",
    workflow: liveState(
      "guided-questions-live-catalog",
      ['ul[aria-label="Guided questions"] .evidence-card'],
      ["Guided questions catalog unavailable from this source."],
      [
        {
          selector: ".page > .empty.mt",
          exactText: "No guided questions are available from this source yet.",
        },
      ],
      routeOwnership(getResponse("/api/v0/query-playbooks")),
    ),
  },
  {
    path: "/profile",
    label: "Profile",
    area: "system",
    authMode: "browser_session",
    workflow: {
      id: "profile-current-session",
      kind: "state",
      anySelectors: ['tr[aria-current="true"]', ".empty-note"],
      requiredResponses: [
        getResponse("/api/v0/auth/profile"),
        getResponse("/api/v0/auth/sessions"),
        getResponse("/api/v0/auth/local/api-tokens"),
      ],
      forbiddenText: "Profile unavailable from this source.",
    },
  },
  {
    path: "/admin",
    label: "Admin",
    area: "system",
    authMode: "browser_session",
    workflow: {
      id: "admin-sign-in-policy-tab",
      kind: "click",
      role: "tab",
      name: "Sign-in policy",
      outcomeSelector: "#identity-access-panel-sign-in-policy",
      loadedStateSelector: "#policy-require-sso",
      expectedRequestPath: "/api/v0/auth/admin/sign-in-policy",
      expectedRequestMethod: "GET",
      acceptedResponseStatuses: [200],
      forbiddenText: "Sign-in policy unavailable from this source.",
    },
  },
];
