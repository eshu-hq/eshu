import type { EshuTruth } from "./envelope";

export type EntityKind = "repositories" | "workloads" | "services";

export interface SearchCandidate {
  readonly description: string;
  readonly id: string;
  readonly kind: EntityKind;
  readonly label: string;
}

export interface EvidenceRow {
  readonly basis: string;
  readonly category?: string;
  readonly detailPath?: string;
  readonly source: string;
  readonly summary: string;
  readonly title?: string;
}

export interface DeploymentGraphNode {
  readonly column?: number;
  readonly detail?: string;
  readonly id: string;
  readonly kind:
    | "repository"
    | "service"
    | "workflow"
    | "trigger"
    | "artifact"
    | "environment"
    | "evidence";
  readonly label: string;
  readonly lane?: string;
}

export interface DeploymentGraphLink {
  readonly detail?: string;
  readonly label: string;
  readonly source: string;
  readonly target: string;
}

export interface DeploymentGraph {
  readonly links: readonly DeploymentGraphLink[];
  readonly nodes: readonly DeploymentGraphNode[];
}

export interface OverviewStat {
  readonly detail?: string;
  readonly label: string;
  readonly value: string;
}

export interface WorkspaceStory {
  readonly deploymentGraph: DeploymentGraph;
  readonly deploymentPath: readonly string[];
  readonly evidence: readonly EvidenceRow[];
  readonly findings: readonly string[];
  readonly id: string;
  readonly kind: EntityKind;
  readonly limitations: readonly string[];
  readonly overviewStats: readonly OverviewStat[];
  readonly story: string;
  readonly title: string;
  readonly truth: EshuTruth;
}

export interface DashboardMetric {
  readonly label: string;
  readonly value: string;
}

export interface CatalogRow {
  readonly coverage: string;
  readonly freshness: string;
  readonly id: string;
  readonly kind: EntityKind;
  readonly name: string;
}

export interface FindingRow {
  readonly entity: string;
  readonly findingType: string;
  readonly location: string;
  readonly name: string;
  readonly truthLevel: string;
}

const exactFreshTruth: EshuTruth = {
  basis: "canonical_graph",
  capability: "platform_impact.context_overview",
  freshness: { state: "fresh" },
  level: "exact",
  profile: "local_full_stack",
  reason: "resolved from graph and content evidence"
};

export const demoSearchCandidates: readonly SearchCandidate[] = [
  {
    description: "Service workload with deployment and support evidence",
    id: "workload:checkout-service",
    kind: "workloads",
    label: "checkout-service"
  },
  {
    description: "Repository containing checkout API code",
    id: "repository:checkout-api",
    kind: "repositories",
    label: "checkout-api"
  }
];

export const demoWorkspaceStories: readonly WorkspaceStory[] = [
  {
    deploymentGraph: {
      links: [
        {
          label: "builds",
          source: "services/checkout",
          target: "github-actions"
        },
        {
          label: "syncs",
          source: "github-actions",
          target: "argocd"
        },
        {
          label: "rolls out",
          source: "argocd",
          target: "kubernetes"
        }
      ],
      nodes: [
        { id: "services/checkout", kind: "repository", label: "services/checkout" },
        { id: "github-actions", kind: "workflow", label: "GitHub Actions" },
        { id: "argocd", kind: "evidence", label: "ArgoCD" },
        { id: "kubernetes", kind: "environment", label: "Kubernetes" }
      ]
    },
    deploymentPath: [
      "services/checkout",
      "GitHub Actions",
      "ArgoCD Application",
      "Kubernetes Deployment",
      "prod-us-east-1"
    ],
    evidence: [
      {
        basis: "relationship_evidence",
        source: "deploy/argocd/checkout.yaml",
        summary: "ArgoCD application points at the checkout service overlay."
      },
      {
        basis: "content_store",
        source: "services/checkout/README.md",
        summary: "Repository docs identify checkout as the public checkout API."
      }
    ],
    findings: ["1 derived dead-code candidate needs review"],
    id: "workload:checkout-service",
    kind: "workloads",
    limitations: ["Runtime evidence is fixture-backed in demo mode."],
    overviewStats: [
      { label: "Files", value: "12" },
      { label: "Workloads", value: "1" },
      { label: "Deployment evidence", value: "3" }
    ],
    story:
      "checkout-service deploys through ArgoCD into Kubernetes and is backed by indexed repository, deployment, and content evidence.",
    title: "checkout-service",
    truth: exactFreshTruth
  },
  {
    deploymentGraph: {
      links: [
        { label: "emits", source: "checkout-api", target: "parser-facts" },
        { label: "projects", source: "parser-facts", target: "graph" }
      ],
      nodes: [
        { id: "checkout-api", kind: "repository", label: "checkout-api" },
        { id: "parser-facts", kind: "evidence", label: "Parser facts" },
        { id: "graph", kind: "artifact", label: "Graph" }
      ]
    },
    deploymentPath: ["checkout-api repo", "parser facts", "content store", "graph"],
    evidence: [
      {
        basis: "repository_story",
        source: "repository:checkout-api",
        summary: "Repository story fixture links code and deployment metadata."
      }
    ],
    findings: ["2 dead-code candidates are available for cleanup triage"],
    id: "repository:checkout-api",
    kind: "repositories",
    limitations: ["Repository fixture is not connected to a live API."],
    overviewStats: [
      { label: "Files", value: "41" },
      { label: "Workloads", value: "1" },
      { label: "Deployment evidence", value: "2" }
    ],
    story:
      "checkout-api contains the service implementation and deployment references for checkout-service.",
    title: "checkout-api",
    truth: exactFreshTruth
  }
];

export const demoDashboardMetrics: readonly DashboardMetric[] = [
  { label: "Index status", value: "complete" },
  { label: "Graph readiness", value: "ready" },
  { label: "Configured collectors", value: "git, docs, registry preview" }
];

export const demoCatalogRows: readonly CatalogRow[] = [
  {
    coverage: "story, deployment, content",
    freshness: "fresh",
    id: "repository:checkout-api",
    kind: "repositories",
    name: "checkout-api"
  },
  {
    coverage: "story, deployment, evidence",
    freshness: "fresh",
    id: "workload:checkout-service",
    kind: "workloads",
    name: "checkout-service"
  }
];

export const demoFindingRows: readonly FindingRow[] = [
  {
    entity: "checkout-service",
    findingType: "Dead code",
    location: "services/checkout/src/discounts.ts",
    name: "legacyCheckoutDiscount",
    truthLevel: "derived"
  }
];

export function getDemoWorkspaceStory(
  kind: string | undefined,
  id: string | undefined
): WorkspaceStory | null {
  if (kind === undefined || id === undefined) {
    return null;
  }
  return (
    demoWorkspaceStories.find(
      (story) => story.kind === kind && story.id === decodeURIComponent(id)
    ) ?? null
  );
}
